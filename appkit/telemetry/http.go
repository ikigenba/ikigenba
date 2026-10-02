package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

// Middleware records every request and provides its caller context.
func Middleware(w *Writer, next http.Handler) http.Handler {
	if w == nil {
		panic("telemetry writer is nil")
	}
	return http.HandlerFunc(func(out http.ResponseWriter, incoming *http.Request) {
		r := incoming
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			var err error
			id, err = w.mintRequestID()
			if err != nil {
				b := make([]byte, 16)
				rand.Read(b)
				id = hex.EncodeToString(b)
			}
			r = r.Clone(r.Context())
			r.Header.Set("X-Request-Id", id)
		}
		caller := identity.Caller{UserID: r.Header.Get("X-User-Id"), Email: r.Header.Get("X-User-Email"), RequestID: id}
		r = r.WithContext(identity.NewContext(r.Context(), caller))
		w.Emit(r.Context(), "request.started", Attrs{"method": r.Method, "path": r.URL.Path})
		response := &statusWriter{ResponseWriter: out}
		start := w.Now()
		returned := false
		defer func() {
			end := w.Now()
			status := response.status
			if status == 0 {
				status = http.StatusOK
				if !returned {
					status = http.StatusInternalServerError
				}
			}
			w.Emit(r.Context(), "request.finished", Attrs{"status": int64(status), "duration_us": elapsedUS(start, end)})
		}()
		next.ServeHTTP(response, r)
		returned = true
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 && status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func elapsedUS(start, end time.Time) int64 {
	d := int64(end.Sub(start) / time.Microsecond)
	if d < 0 {
		return 0
	}
	return d
}

// SiblingClient forwards identity and records each transport exchange.
func SiblingClient(w *Writer, target string, base http.RoundTripper) *http.Client {
	if w == nil {
		panic("telemetry writer is nil")
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{Transport: siblingTransport{w, target, base}}
}

type siblingTransport struct {
	writer *Writer
	target string
	base   http.RoundTripper
}

func (s siblingTransport) RoundTrip(incoming *http.Request) (*http.Response, error) {
	r := incoming.Clone(incoming.Context())
	if caller, ok := identity.FromContext(r.Context()); ok {
		identity.Forward(caller, r)
	}
	start := s.writer.Now()
	response, err := s.base.RoundTrip(r)
	end := s.writer.Now()
	status := 0
	if err == nil {
		status = response.StatusCode
	}
	s.writer.Emit(r.Context(), "sibling.called", Attrs{"target": s.target, "method": r.Method, "path": r.URL.Path, "status": int64(status), "duration_us": elapsedUS(start, end)})
	return response, err
}

// SocketTransport connects HTTP requests to the given unix socket.
func SocketTransport(socket string) *http.Transport {
	return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
}
