# design.md

Architecture of groww-go. Signatures only, no bodies. Update this in the same
commit as any public API change.

> **Unverified.** Endpoint paths, JSON field names and enum values below are
> drafted from the Groww Python SDK and cURL docs and have **not** been checked
> against the live API. Everything marked ⚠ needs confirming before it is
> implemented. `types.Money` and the concurrency model are not affected.

## Status

Last reconciled against the code: 2026-08-21.

| File | Zone | State |
| --- | --- | --- |
| `types/money.go` | written | done, tested |
| `errors.go` | red | types and `classify` done; **no tests yet** |
| `types/enums.go` | plain declarations | not started |
| `types/*.go` (order, portfolio, marketdata, tick) | plain declarations | not started |
| `client.go`, `auth.go` | red | **next** — `classify` has no caller until these exist |
| `transport.go` | red | not started |
| `orders.go` | red | not started |
| `portfolio.go`, `marketdata.go` | red | not started |
| `feed.go`, `feed_buffer.go` | red | not started |

`go build`, `go vet`, `gofmt -l` and `go test -race ./...` are clean.
`golangci-lint` has not been run — it is not installed locally, and `classify`
and `errorEnvelope` currently have no callers, so `unused` (U1000) will fail
`make lint` until `transport.go` lands.

Authorship rule (from CLAUDE.md, tightened 2026-08-17): anything containing a
branch, a loop or arithmetic is typed by hand. Only config, plain declarations
and `.md` are written directly.

## Package layout

```
groww-go/
├── client.go          Client, Option, New, WithLiveTrading
├── auth.go            token acquisition, expiry, refresh
├── transport.go       request execution, retry, backoff, rate limit
├── errors.go          error hierarchy
├── orders.go          place, modify, cancel, status, list
├── portfolio.go       holdings, positions, margin
├── marketdata.go      ltp, quote, ohlc, historical candles
├── feed.go            WebSocket lifecycle: connect, reconnect, resubscribe
├── feed_buffer.go     bounded tick buffer, drop policy, counters
├── types/             data only — no I/O, standard library only
│   ├── money.go       exact rupee amounts
│   ├── enums.go       Exchange, Segment, OrderType, ProductType, …
│   ├── order.go       order requests and responses
│   ├── portfolio.go   holdings, positions, margin
│   ├── marketdata.go  quotes, OHLC, candles
│   └── tick.go        feed payloads
└── testdata/          recorded JSON fixtures; CI never touches the network
```

Rationale for the split: ADR 001. `types` imports nothing of ours, so it cannot
participate in an import cycle and its tests need no HTTP.

## Client

```go
type Client struct{ /* unexported */ }

type Option func(*Client)

func New(apiKey, apiSecret string, opts ...Option) (*Client, error)

func WithHTTPClient(h *http.Client) Option
func WithBaseURL(u string) Option
func WithTimeout(d time.Duration) Option        // default when ctx has no deadline
func WithRetryPolicy(p RetryPolicy) Option
func WithLiveTrading() Option                   // required for every mutating call
func WithLogger(l *slog.Logger) Option
```

`New` returns an error rather than panicking on a bad base URL or empty
credentials. Options are `func(*Client)` — the plain functional-options pattern;
no builder, no config struct, because the option set is small and additive.

**`WithLiveTrading` is a safety interlock.** Every mutating call (place, modify,
cancel) returns `ErrLiveTradeNotSupported` unless it was passed. The live API costs
₹499+tax/month and places real orders against a real account; the default must be
that a mistake is inert.

## Errors

Hierarchy supports `errors.Is` for kinds and `errors.As` for detail.

```go
// Sentinels — match with errors.Is.
var (
	ErrUnauthorized          error // 401, bad or expired credentials
	ErrForbidden             error // 403, subscription or permission
	ErrRateLimited           error // 429
	ErrNotFound              error // 404
	ErrInvalidRequest        error // other 4xx, caller's fault
	ErrLiveTradeNotSupported error // mutating call without WithLiveTrading
	ErrOrderStateUnknown     error // place/modify timed out; landed or not, we cannot say

	// 5xx is two levels: the leaves wrap the parent, so errors.Is matches
	// either. See ADR 006.
	ErrServer             error // any 5xx
	ErrInternal           error // 500, wraps ErrServer
	ErrServiceUnavailable error // 503, wraps ErrServer
)

// APIError — the server answered with an error response.
type APIError struct {
	StatusCode int
	Code       string // Groww's error code ⚠
	Message    string
	RequestID  string
	Retryable  bool   // set by classify, consumed by transport.go
}

func (e *APIError) Error() string
func (e *APIError) Is(target error) bool   // maps StatusCode → sentinel

// TransportError — the request never produced a usable response.
type TransportError struct {
	Op       string // "place_order"
	Attempts int
	Err      error  // net.Error, url.Error, context deadline
}

func (e *TransportError) Error() string
func (e *TransportError) Unwrap() error

// ValidationError — rejected locally, before any network call.
type ValidationError struct {
	Field  string
	Value  any    // what the caller actually passed
	Reason string
}

func (e *ValidationError) Error() string
func (e *ValidationError) Is(target error) bool  // → ErrInvalidRequest
```

Callers match kinds without type assertions:

```go
if errors.Is(err, groww.ErrRateLimited) { … }

var apiErr *groww.APIError
if errors.As(err, &apiErr) && apiErr.Code == "INSUFFICIENT_FUNDS" { … }
```

`Retryable` lives on the error, not in a status-code table in `transport.go`.
A 400 carrying a duplicate-order code must never be retried; a connection reset
before any byte was written can be. That distinction cannot be made from the
status code alone.

### Classification

One unexported funnel turns a response into a typed error. Every HTTP path in
the SDK goes through it, so there is exactly one place where status codes are
interpreted.

```go
// errorEnvelope is the error body shape. ⚠ Field names unverified; the API may
// use "message", "error", or neither, so every field is optional.
type errorEnvelope struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func classify(resp *http.Response, body []byte) error   // nil on 2xx
```

Behaviour:

- 2xx → `nil`. The success path returns an untyped nil, never a typed nil
  `*APIError`.
- Non-2xx → `*APIError` carrying status, `X-Request-ID` ⚠, and whatever the
  envelope yielded. A body that is not JSON at all — an HTML error page from an
  intermediate proxy — is tolerated: the unmarshal error is deliberately
  discarded and `Error()` falls back to `http.StatusText`.
- `Retryable` is set to true for 429, 500, 502, 503 and 504, and left false
  everywhere else.

**`Retryable` means "the server-side failure is transient", not "it is safe to
send this request again."** Those are different questions and only the caller
knows the second one. A 502 on `GET /holdings` can be replayed freely; a 502 on
`POST /order` may have been generated *after* the order was accepted, so
replaying it can place a second order. `transport.go` must gate on both
`Retryable` **and** whether the operation is mutating; `orders.go` reconciles
rather than retries. See ADR 007 and `flow.md` flow 2.

## Transport

```go
type RetryPolicy struct {
	MaxAttempts int           // total, including the first
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}
```

Backoff is exponential with **full jitter**: `sleep = rand(0, min(MaxDelay,
BaseDelay × 2^attempt))`. Full jitter rather than equal jitter or plain
exponential because every client retrying at the same offset after a shared
outage reproduces the thundering herd the backoff was meant to prevent.

Rate limiting is a token bucket per endpoint class, held on the `Client` and
shared by all goroutines using it. A `429` response overrides the local bucket
and, if `Retry-After` is present, that value wins over the computed backoff.

Retries are only ever issued for requests that are safe to repeat. For orders
that means the idempotency key (below), not the HTTP method.

## Orders

```go
func (c *Client) PlaceOrder(ctx context.Context, req types.PlaceOrderRequest) (*types.Order, error)
func (c *Client) ModifyOrder(ctx context.Context, req types.ModifyOrderRequest) (*types.Order, error)
func (c *Client) CancelOrder(ctx context.Context, ref types.OrderRef) (*types.Order, error)
func (c *Client) OrderStatus(ctx context.Context, ref types.OrderRef) (*types.Order, error)
func (c *Client) ListOrders(ctx context.Context, f types.OrderFilter) ([]types.Order, error)
```

**Idempotency.** `PlaceOrderRequest` carries a caller-supplied
`ClientOrderID`; if empty, the SDK generates one and returns it on the response
regardless of outcome. A timeout on place-order does not tell us whether the
order landed. The recovery path is: on `ErrOrderStateUnknown`, reconcile by
listing orders and matching `ClientOrderID` before deciding to retry. Retrying a
place-order without this is how you buy the same lot twice.

The reconciliation window is bounded — an order placed but not yet visible in the
list is indistinguishable from one never placed, so reconcile polls with backoff
up to a deadline, then surfaces `ErrOrderStateUnknown` to the caller. **The SDK
never silently re-places.**

## Portfolio and market data

```go
func (c *Client) Holdings(ctx context.Context) ([]types.Holding, error)
func (c *Client) Positions(ctx context.Context, seg types.Segment) ([]types.Position, error)
func (c *Client) Margin(ctx context.Context) (*types.Margin, error)

func (c *Client) LTP(ctx context.Context, keys ...types.InstrumentKey) (map[types.InstrumentKey]types.Money, error)
func (c *Client) Quote(ctx context.Context, key types.InstrumentKey) (*types.Quote, error)
func (c *Client) OHLC(ctx context.Context, keys ...types.InstrumentKey) (map[types.InstrumentKey]types.OHLC, error)
func (c *Client) Candles(ctx context.Context, req types.CandleRequest) ([]types.Candle, error)
```

All read-only, all safe to retry, none gated behind `WithLiveTrading`.

## Feed — concurrency model

This is the part to be able to draw on a whiteboard.

```go
type Feed struct{ /* unexported */ }

func (c *Client) NewFeed(opts ...FeedOption) *Feed

func WithBufferSize(n int) FeedOption
func WithDropPolicy(p DropPolicy) FeedOption

func (f *Feed) Connect(ctx context.Context) error
func (f *Feed) Subscribe(ctx context.Context, keys ...types.InstrumentKey) error
func (f *Feed) Unsubscribe(ctx context.Context, keys ...types.InstrumentKey) error
func (f *Feed) Ticks() <-chan types.Tick
func (f *Feed) OrderUpdates() <-chan types.OrderUpdate
func (f *Feed) Errs() <-chan error
func (f *Feed) Stats() FeedStats     // Dropped, Reconnects, LastTick
func (f *Feed) Close() error
```

### Goroutines

| Goroutine | Count | Started by | Exits when |
| --- | --- | --- | --- |
| supervisor | 1 per Feed | `Connect` | lifetime ctx cancelled, or `Close` |
| reader | 1 per connection | supervisor | read error, or connection closed |
| heartbeat | 1 per connection | supervisor | its connection's context is cancelled |

The caller's own goroutine is the consumer, ranging over `Ticks()`.

### Ownership

- **Connection** — owned solely by the supervisor. Reader and heartbeat receive
  it as an argument and never replace it. Only the supervisor dials, and only
  the supervisor discards.
- **Subscription set** — owned by the `Feed`, guarded by a mutex, because
  `Subscribe` is called from caller goroutines while the supervisor reads it
  during resubscribe. The mutex protects the map only; it is never held across a
  network write.
- **`ticks` / `orderUpdates` / `errs` channels** — created in `NewFeed`, written
  by the reader through the buffer, **closed by the supervisor and only after
  every reader has exited** (tracked with a `sync.WaitGroup`). Closed exactly
  once, from the sending side. A receiver must never close a channel it reads,
  and a second close panics.
- **Per-connection context** — derived from the lifetime ctx with
  `context.WithCancel`. Cancelling it is what stops the reader and heartbeat for
  a dead connection without disturbing the supervisor.

### Reconnect and resubscribe

On read error the reader exits and signals the supervisor. The supervisor
cancels the per-connection context, waits for the heartbeat to exit, sleeps
backoff-with-jitter, redials, and **replays the entire subscription set** before
declaring the connection live. The tick channel is not closed across a
reconnect — from the consumer's point of view the stream pauses and resumes.

Failure to replay the subscription set is the defining bug of this class of
client: the socket is healthy, the consumer sees silence, and nothing errors.
`FeedStats.Reconnects` and a resubscribe count exist so this is observable.

### Backpressure

Ticks arrive faster than a slow consumer drains them. The bounded channel is
`WithBufferSize` (default 1024) and the policy is explicit:

- `DropOldest` (default) — evict the oldest tick, increment `FeedStats.Dropped`.
  Correct for market data, where the newest price is the only one that matters.
- `Block` — apply backpressure to the reader, which eventually stalls the socket
  and lets the server's own buffering take over.

Order updates use a **separate channel and never drop** — a missed fill is not
recoverable by waiting for the next one.

## Testing

Table-driven throughout. HTTP paths test against `httptest.Server` replaying
fixtures from `testdata/`; the feed tests run a real WebSocket server in-process
so reconnect and resubscribe are exercised for real. `go test ./...` in CI makes
no outbound connection. Live-API tests sit behind a `live` build tag and are
never in `make ci`.
