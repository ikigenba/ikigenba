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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

// Config carries every process dependency the handlers need. Google
// credentials stay inside Google; this struct does not repeat them.
type Config struct {
	Store           *store.Store
	Google          *google.Client
	Now             func() time.Time
	Rand            io.Reader
	Telemetry       *telemetry.Writer
	WorkspaceDomain string
	PublicURL       string
	CallbackURL     string
	Banner          func(u page.User) page.Banner
}

// Server is auth's HTTP service.
type Server struct {
	st   *store.Store
	gc   *google.Client
	now  func() time.Time
	rand io.Reader
	cfg  Config

	httpServer *http.Server
}

// New constructs the router used by auth's HTTP service.
func New(cfg Config) *Server {
	s := &Server{
		st:   cfg.Store,
		gc:   cfg.Google,
		now:  cfg.Now,
		rand: cfg.Rand,
		cfg:  cfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /login/google", s.handleLoginGoogle)
	mux.HandleFunc("GET /login/google/callback", s.handleLoginGoogleCallback)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /check", s.handleCheck)
	mux.HandleFunc("GET /check/open", s.handleCheck)
	mux.HandleFunc("GET /me", s.handleMe)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleOAuthMetadata)
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("GET /authorize", s.handleAuthorize)
	mux.HandleFunc("POST /authorize", s.handleAuthorize)
	mux.HandleFunc("POST /token", s.handleOAuthToken)
	mux.HandleFunc("POST /tokens", s.handleCreateToken)
	mux.HandleFunc("POST /tokens/{id}/{action}", s.handleTokenAction)

	static := page.Static()
	s.httpServer = &http.Server{Handler: telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, page.StaticPrefix) {
			static.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})), ReadHeaderTimeout: 10 * time.Second}
	return s
}

// ServeHTTP routes one request through the service.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.httpServer.Handler.ServeHTTP(w, r)
}

// writeServerError answers a failed operation without exposing identity or
// implementation details to the client.
func (s *Server) writeServerError(w http.ResponseWriter, _ *http.Request, _ error) {
	w.Header().Del(HeaderUserID)
	w.Header().Del(HeaderUserEmail)
	writePlainError(w, http.StatusInternalServerError, "internal server error")
}

// record attaches the identity auth resolved without altering middleware's envelope.
func (s *Server) record(r *http.Request, name, user string, attrs telemetry.Attrs) {
	caller, _ := identity.FromContext(r.Context())
	caller.UserID, caller.Email = user, ""
	s.cfg.Telemetry.Emit(identity.NewContext(r.Context(), caller), name, attrs)
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
