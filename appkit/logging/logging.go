// Package logging configures a suite app's structured logger: log/slog
// emitting JSON records to a writer at a configured level, plus correlation
// middleware that tags each request and emits begin/end debug records. It also
// folds in ULID id generation (formerly each service's internal/ids).
//
// This is the uniform half lifted verbatim from every service's
// internal/logging + internal/ids (appkit extraction, PLAN §B).
package logging

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"eventplane/correlation"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ParseLevel maps a level name (debug|info|warn|error) to a slog.Level,
// erroring on anything else. Surrounding whitespace and case are ignored.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (want debug|info|warn|error)", name)
	}
}

// New returns a slog.Logger that writes JSON records at or above level to w.
// It does not mutate the package default; callers inject the returned logger
// explicitly.
func New(level slog.Level, w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var enc = base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)

// NewULID returns a Crockford-base32 ULID (26-char): 48 bits of
// millisecond time followed by 80 bits of cryptographic randomness, time-ordered
// and unguessable. No external dependency is needed for that property. It is the
// fold-in of each service's internal/ids.NewULID.
func NewULID() string {
	now := uint64(time.Now().UnixMilli())
	var randomness [10]byte
	if _, err := rand.Read(randomness[:]); err != nil {
		// crypto/rand failure is non-recoverable.
		panic("crypto/rand failed: " + err.Error())
	}

	// A ULID is 128 bits represented by 26 base32 characters (130 bits), so
	// the two padding bits precede the timestamp. Encoding all 16 bytes with
	// encoding/base32 would instead append padding bits after the randomness,
	// shifting the timestamp representation away from the ULID wire format.
	var id [26]byte
	id[0] = crockfordAlphabet[(now>>45)&7]
	for i, shift := range []uint{40, 35, 30, 25, 20, 15, 10, 5, 0} {
		id[i+1] = crockfordAlphabet[(now>>shift)&31]
	}
	copy(id[10:], enc.EncodeToString(randomness[:]))
	return string(id[:])
}

// RequestID returns the correlation id on the context, or "" if none.
func RequestID(ctx context.Context) string {
	return correlation.FromContext(ctx)
}

// CorrelationMiddleware propagates a valid inbound correlation id or mints a
// fresh one, echoes it under both correlation and legacy request-id headers,
// and emits per-request begin/end debug records. It logs only the presence of
// identity headers, never their values.
func CorrelationMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.Header.Get(correlation.Header)
		if correlation.Valid(id) {
			ctx = correlation.WithContext(ctx, id)
		} else {
			ctx, id = correlation.Ensure(ctx)
		}
		w.Header().Set(correlation.Header, id)
		w.Header().Set("X-Request-ID", id)

		start := time.Now()
		logger.LogAttrs(ctx, slog.LevelDebug, "request.begin",
			slog.String("correlation_id", id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote", r.RemoteAddr),
			slog.Bool("has_owner_email", r.Header.Get("X-Owner-Email") != ""),
		)

		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		logger.LogAttrs(ctx, slog.LevelDebug, "request.end",
			slog.String("correlation_id", id),
			slog.Int("status", rw.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.NewResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
