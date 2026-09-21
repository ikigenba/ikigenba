// Package server owns auth's HTTP router and handler lifecycle.
package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
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

	s.httpServer = &http.Server{Handler: mux}
	return s
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

// Serve listens on addr and runs the service until Shutdown closes it.
func (s *Server) Serve(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	err = s.httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully finishes active requests and closes the listener.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
