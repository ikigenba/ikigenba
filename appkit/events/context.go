package events

import (
	"context"
	"net/http"
	"strconv"
)

// Cause identifies the event currently being handled and its depth.
type Cause struct {
	ID    string
	Depth int
}
type causeKey struct{}

// CauseHeader carries the cause's event id across service calls.
const CauseHeader = "X-Event-Cause"

// DepthHeader carries the cause's decimal depth across service calls.
const DepthHeader = "X-Event-Depth"

// NewContext derives a context carrying c.
func NewContext(ctx context.Context, c Cause) context.Context {
	return context.WithValue(ctx, causeKey{}, c)
}

// FromContext returns the innermost cause in ctx.
func FromContext(ctx context.Context) (Cause, bool) {
	c, ok := ctx.Value(causeKey{}).(Cause)
	return c, ok
}

// Forward replaces the outgoing cause headers when ctx carries a cause.
func Forward(ctx context.Context, r *http.Request) {
	c, ok := FromContext(ctx)
	if !ok {
		return
	}
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Set(CauseHeader, c.ID)
	r.Header.Set(DepthHeader, strconv.Itoa(c.Depth))
}
func decimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

// Middleware carries well-formed cause headers on the request context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, depth := r.Header.Get(CauseHeader), r.Header.Get(DepthHeader)
		if validEventID(id) && decimalDigits(depth) {
			if n, err := strconv.Atoi(depth); err == nil {
				r = r.WithContext(NewContext(r.Context(), Cause{id, n}))
			}
		}
		next.ServeHTTP(w, r)
	})
}
