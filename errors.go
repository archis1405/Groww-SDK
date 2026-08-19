package groww

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrServer = errors.New("groww: server error")

	ErrInvalidRequest = errors.New("groww: invalid request")
	ErrUnauthorized   = errors.New("groww: unauthorized request")
	ErrForbidden      = errors.New("groww: forbidden request")
	ErrNotFound       = errors.New("groww: resource not found")

	ErrInternal           = fmt.Errorf("%w: internal", ErrServer)
	ErrServiceUnavailable = fmt.Errorf("%w: service unavailable", ErrServer)

	ErrRateLimited           = errors.New("groww: rate limited request")
	ErrLiveTradeNotSupported = errors.New("groww: live trade not supported")
	ErrOrderStateUnknown     = errors.New("groww: order state unknown")
)

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	Retryable  bool
}

func (e *APIError) Error() string {
	detail := e.Message

	if detail == "" {
		detail = http.StatusText(e.StatusCode)
	}
	if e.Code != "" {
		detail = e.Code + ": " + detail
	}
	if e.RequestID != "" {
		return fmt.Sprintf("groww: api error %d: %s (request %s)", e.StatusCode, detail, e.RequestID)
	}

	return fmt.Sprintf("groww: api error %d: %s", e.StatusCode, detail)
}

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrInvalidRequest:
		switch e.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusTooManyRequests:
			return false
		}
		return e.StatusCode >= 400 && e.StatusCode < 500
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrInternal:
		return e.StatusCode == http.StatusInternalServerError
	case ErrServiceUnavailable:
		return e.StatusCode == http.StatusServiceUnavailable
	case ErrServer:
		return e.StatusCode >= 500
	}
	return false
}

type TransportError struct {
	Op       string
	Attempts int
	Err      error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("groww: transport error %s after %d attempts: %v", e.Op, e.Attempts, e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

type ValidationError struct {
	Field  string
	Value  any
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("groww: validation error on field '%s' with value '%v': %s", e.Field, e.Value, e.Reason)
}

func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalidRequest
}

type errorEnvelope struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func classify(resp *http.Response, body []byte) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		RequestID:  resp.Header.Get("X-Request-ID"),
	}

	var env errorEnvelope

	if err := json.Unmarshal(body, &env); err == nil {
		apiErr.Code = env.Code

		apiErr.Message = env.Message

		if apiErr.Message == "" {
			apiErr.Message = env.Error
		}
	}

	switch resp.StatusCode {

	case http.StatusTooManyRequests:
		apiErr.Retryable = true

	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		apiErr.Retryable = true

	}

	return apiErr
}
