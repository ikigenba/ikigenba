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

// Handler returns the HTTP handler served by dummy.
func Handler() http.Handler {
	return http.NotFoundHandler()
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
