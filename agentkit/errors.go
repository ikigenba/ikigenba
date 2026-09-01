package agentkit

import (
	"errors"
	"fmt"
	"time"
)

// Category is a closed enumeration of provider failure kinds.
type Category int

const (
	// CategoryUnknown is an unclassifiable failure and is never retried.
	CategoryUnknown Category = iota
	// CategoryAuth is a rejected credential or permission failure.
	CategoryAuth
	// CategoryInvalidRequest is a malformed or unsupported request.
	CategoryInvalidRequest
	// CategoryRateLimit is provider throttling.
	CategoryRateLimit
	// CategoryOverloaded is a transient upstream or server failure.
	CategoryOverloaded
	// CategoryInsufficientQuota is an exhausted credit or balance failure.
	CategoryInsufficientQuota
	// CategoryTimeout is a deadline or connection timeout failure.
	CategoryTimeout
	// CategoryTransport is a network failure before a usable response.
	CategoryTransport
)

// Error is the single error type returned for a provider interaction.
type Error struct {
	Category   Category
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
	Endpoint   Identity
	err        error
}

// Error returns the category, provider message, and HTTP status for the failure.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	return fmt.Sprintf("%s: %s (status %d)", categoryName(e.Category), e.Message, e.Status)
}

// Unwrap exposes the underlying transport cause or sentinel.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.err
}

// Retryable reports whether err contains a transient agentkit provider error.
func Retryable(err error) bool {
	var providerError *Error
	if !errors.As(err, &providerError) {
		return false
	}

	switch providerError.Category {
	case CategoryRateLimit, CategoryOverloaded, CategoryTimeout, CategoryTransport:
		return true
	case CategoryUnknown, CategoryAuth, CategoryInvalidRequest, CategoryInsufficientQuota:
		return false
	default:
		return false
	}
}

// ErrInvalidConfig identifies configuration that cannot be honored.
var ErrInvalidConfig = errors.New("agentkit: invalid configuration")

// ErrClosed identifies a conversation that can no longer accept a Send.
var ErrClosed = errors.New("agentkit: conversation closed")

func categoryName(category Category) string {
	switch category {
	case CategoryAuth:
		return "auth"
	case CategoryInvalidRequest:
		return "invalid request"
	case CategoryRateLimit:
		return "rate limit"
	case CategoryOverloaded:
		return "overloaded"
	case CategoryInsufficientQuota:
		return "insufficient quota"
	case CategoryTimeout:
		return "timeout"
	case CategoryTransport:
		return "transport"
	case CategoryUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

func invalidConfigError(identity Identity, cause error) *Error {
	wrapped := ErrInvalidConfig
	message := ErrInvalidConfig.Error()
	if cause != nil {
		if errors.Is(cause, ErrInvalidConfig) {
			wrapped = cause
		} else {
			wrapped = fmt.Errorf("%w: %w", ErrInvalidConfig, cause)
		}
		message = cause.Error()
	}

	return &Error{
		Category: CategoryInvalidRequest,
		Message:  message,
		Endpoint: identity,
		err:      wrapped,
	}
}

func wrapProviderError(err error, category Category, status int, identity Identity) error {
	if err == nil {
		return nil
	}

	var providerError *Error
	if errors.As(err, &providerError) {
		return providerError
	}

	return &Error{
		Category: category,
		Status:   status,
		Message:  err.Error(),
		Endpoint: identity,
		err:      err,
	}
}
