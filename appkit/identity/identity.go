// Package identity carries the caller authenticated by nginx.
package identity

import (
	"context"
	"io"
	"net/http"
	"sync"
)

// Caller identifies a user and the request made on their behalf.
type Caller struct {
	UserID, Email, RequestID string
}

// MissingBody is the response when nginx did not provide a user identity.
const MissingBody = "identity header missing\n"

type callerKey struct{}

// NewContext derives a context carrying c.
func NewContext(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// FromContext returns the innermost caller carried by ctx.
func FromContext(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// Require requires the caller headers nginx supplies before invoking next.
func Require(app string, stderr io.Writer, next http.Handler) http.Handler {
	var diagnostics sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := Caller{r.Header.Get("X-User-Id"), r.Header.Get("X-User-Email"), r.Header.Get("X-Request-Id")}
		if c.UserID == "" {
			if stderr != nil {
				requestID := c.RequestID
				if requestID == "" {
					requestID = "-"
				}
				line := []byte(app + ": request " + requestID + ": X-User-Id is missing\n")
				diagnostics.Lock()
				_, _ = stderr.Write(line)
				diagnostics.Unlock()
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, MissingBody)
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), c)))
	})
}

// Forward replaces the outgoing request's identity headers with c.
func Forward(c Caller, r *http.Request) {
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	for _, field := range []struct{ name, value string }{
		{"X-User-Id", c.UserID},
		{"X-User-Email", c.Email},
		{"X-Request-Id", c.RequestID},
	} {
		if field.value == "" {
			r.Header.Del(field.name)
		} else {
			r.Header.Set(field.name, field.value)
		}
	}
}
