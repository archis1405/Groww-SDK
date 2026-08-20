package groww

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.groww.in"

const defaultTimeout = 30 * time.Second

type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type Client struct {
	httpClient  *http.Client
	baseURL     string
	tokens      TokenSource
	userAgent   string
	liveTrading bool
}

type Option func(*Client) error

func New(tokens TokenSource, options ...Option) (*Client, error) {
	if tokens == nil {
		return nil, &ValidationError{
			Field:  "tokens",
			Value:  nil,
			Reason: "TokenSource is mandatory",
		}
	}

	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    DefaultBaseURL,
		tokens:     tokens,
		userAgent:  "groww-go/0.1.0",
	}

	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func WithHTTPClient(hc *http.Client) Option {

	return func(c *Client) error {
		if hc == nil {
			return &ValidationError{
				Field:  "httpClient",
				Value:  nil,
				Reason: "httpClient cannot be nil",
			}
		}

		c.httpClient = hc
		return nil
	}
}

func WithBaseURL(raw string) Option {

	return func(c *Client) error {

		u, err := url.Parse(raw)
		if err != nil {
			return &ValidationError{
				Field:  "baseURL",
				Value:  raw,
				Reason: "invalid URL",
			}
		}

		if u.Scheme != "http" && u.Scheme != "https" {
			return &ValidationError{
				Field:  "baseURL",
				Value:  raw,
				Reason: "URL must have http or https protocol",
			}
		}

		if u.Host == "" {
			return &ValidationError{
				Field:  "baseURL",
				Value:  raw,
				Reason: "URL must have a host",
			}
		}

		c.baseURL = strings.TrimRight(raw, "/")
		return nil
	}
}

func WithUserAgent(ua string) Option {
	return func(c *Client) error {

		if ua == "" {
			return &ValidationError{
				Field:  "userAgent",
				Value:  ua,
				Reason: "userAgent cannot be empty",
			}
		}

		c.userAgent = ua
		return nil
	}
}

func WithTimeout(d time.Duration) Option {

	return func(c *Client) error {

		if d <= 0 {
			return &ValidationError{
				Field:  "timeout",
				Value:  d,
				Reason: "timed out",
			}
		}

		c.httpClient.Timeout = d
		return nil
	}
}
