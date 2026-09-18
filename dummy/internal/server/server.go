package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

// IndexText is the visible text of the index page.
const IndexText = "Hello from Dummy!"

// NotFoundBody and MethodNotAllowedBody are the handler's plain-text errors.
const (
	NotFoundBody         = "not found\n"
	MethodNotAllowedBody = "method not allowed\n"
)

// Handler returns the HTTP handler served by dummy.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, NotFoundBody)
			}
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = io.WriteString(w, MethodNotAllowedBody)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "<!doctype html>\n<html><body>"+IndexText+"</body></html>\n")
		}
	})
}

// Serve serves h on ln until ctx is cancelled or the server fails.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	if ctx.Err() != nil {
		_ = ln.Close()
		return nil
	}

	httpServer := &http.Server{
		Handler:           h,
		ErrorLog:          log.New(io.Discard, "", 0),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- httpServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		_ = httpServer.Shutdown(context.Background())
		<-serveResult
		return nil
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
			return nil
		}
		return err
	}
}
