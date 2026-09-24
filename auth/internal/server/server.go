// Package server owns auth's HTTP router and handler lifecycle.
package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/server/assets"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

// Config carries every process dependency the handlers need. Google
// credentials stay inside Google; this struct does not repeat them.
type Config struct {
	Store           *store.Store
	Google          *google.Client
	Now             func() time.Time
	Rand            io.Reader
	Stderr          io.Writer
	WorkspaceDomain string
}

// Server is auth's HTTP service.
type Server struct {
	st     *store.Store
	gc     *google.Client
	now    func() time.Time
	rand   io.Reader
	stderr io.Writer
	cfg    Config

	httpServer *http.Server
}

// New constructs the router used by auth's HTTP service.
func New(cfg Config) *Server {
	s := &Server{
		st:     cfg.Store,
		gc:     cfg.Google,
		now:    cfg.Now,
		rand:   cfg.Rand,
		stderr: cfg.Stderr,
		cfg:    cfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /login/google", s.handleLoginGoogle)
	mux.HandleFunc("GET /login/google/callback", s.handleLoginGoogleCallback)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /check", s.handleCheck)
	mux.HandleFunc("GET /me", s.handleMe)
	mux.HandleFunc("POST /tokens", s.handleCreateToken)
	mux.HandleFunc("POST /tokens/{id}/{action}", s.handleTokenAction)
	mux.HandleFunc("GET /assets/{name}", s.handleAsset)

	s.httpServer = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return s
}

// ServeHTTP routes one request through the service.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.httpServer.Handler.ServeHTTP(w, r)
}

// writeServerError answers a failed operation without exposing identity or
// implementation details to the client.
func (s *Server) writeServerError(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Del(HeaderUserID)
	w.Header().Del(HeaderUserEmail)
	s.writeDiagnostic(r, err)
	writePlainError(w, http.StatusInternalServerError, "internal server error")
}

func (s *Server) writeDiagnostic(r *http.Request, err error) {
	if s.stderr == nil || err == nil {
		return
	}
	id := r.Header.Get("X-Request-Id")
	if id == "" {
		id = "-"
	}
	_, _ = s.stderr.Write([]byte("auth: request " + id + ": " + err.Error() + "\n"))
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var contentType string
	switch name {
	case "index.html":
		contentType = "text/html; charset=utf-8"
	case "app.js":
		contentType = "text/javascript; charset=utf-8"
	case "style.css":
		contentType = "text/css; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	body, err := assets.Files.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// DrainError reports requests still in progress after the drain deadline.
type DrainError struct{ Unfinished int }

func (e *DrainError) Error() string {
	if e.Unfinished == 1 {
		return "stopped with 1 request unfinished"
	}
	return "stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"
}

// Serve handles HTTP/1.1 on the supplied listener until cancellation or failure.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error {
	if ctx.Err() != nil {
		_ = ln.Close()
		return nil
	}
	var active atomic.Int64
	var connections sync.Mutex
	pending := make(map[net.Conn]struct{})
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			active.Add(1)
			defer active.Add(-1)
			h.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
		ConnState: func(conn net.Conn, state http.ConnState) {
			connections.Lock()
			if state == http.StateNew {
				pending[conn] = struct{}{}
			} else {
				delete(pending, conn)
			}
			connections.Unlock()
			if state == http.StateNew && ctx.Err() != nil {
				_ = conn.Close()
			}
		},
	}
	// Accept begins only after net/http has registered the listener for Shutdown.
	accepting := make(chan struct{})
	serveResult := make(chan error, 1)
	go func() { serveResult <- httpServer.Serve(&readyListener{Listener: ln, accepting: accepting}) }()
	select {
	case err := <-serveResult:
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return errors.New("http server stopped before cancellation")
		}
		return err
	case <-ctx.Done():
	}
	deadline, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	select {
	case <-accepting:
	case <-serveResult:
		_ = ln.Close()
		return nil
	}
	_ = ln.Close()
	connections.Lock()
	toClose := make([]net.Conn, 0, len(pending))
	for conn := range pending {
		toClose = append(toClose, conn)
	}
	connections.Unlock()
	for _, conn := range toClose {
		_ = conn.Close()
	}
	err := httpServer.Shutdown(deadline)
	if err == nil {
		return nil
	}
	unfinished := int(active.Load())
	_ = httpServer.Close()
	if unfinished > 0 {
		return &DrainError{Unfinished: unfinished}
	}
	return nil
}

type readyListener struct {
	net.Listener
	accepting chan struct{}
	started   atomic.Bool
}

func (l *readyListener) Accept() (net.Conn, error) {
	if l.started.CompareAndSwap(false, true) {
		close(l.accepting)
	}
	return l.Listener.Accept()
}
