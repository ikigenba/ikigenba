// Package server builds and runs a suite app's loopback-only HTTP server:
// routing, the unauthenticated RFC 9728 protected-resource metadata document,
// the identity-header gate, the ungated health route, security headers, and graceful
// shutdown. It is the uniform HTTP layer lifted from every path-routed service's
// internal/server (appkit extraction, PLAN §B).
//
// A suite app is loopback-only. nginx (owned by the dashboard) terminates TLS,
// introspects every request via auth_request against the dashboard, strips the
// /srv/<app>/ prefix, and injects X-Owner-Email / X-Client-Id authoritatively.
// Services trust those headers and do NO token logic of their own
// (requireIdentityHeaders; PLAN §2.9).
//
// The server supports two shapes through one Router seam:
//   - path-routed services (crm/ledger/notify/dropbox/wiki/prompts) get the
//     standard route table (PRM + ungated /health + gated MCP + optional /feed)
//     plus any extra routes their Spec.Handlers register;
//   - the dashboard apex supplies its WHOLE route table via Spec.Handlers,
//     bypassing the PRM/identity routes (it issues identity, it does not consume
//     it). See PLAN §B1 map §3 risk 3.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"appkit/logging"
	"appkit/web"

	"eventplane/consumer"
	"eventplane/outbox"
)

// shutdownTimeout bounds how long Run waits for in-flight requests to finish
// before forcing the server down.
const shutdownTimeout = 10 * time.Second

// Options configures the HTTP server.
type Options struct {
	Addr       string       // listen address, e.g. "127.0.0.1:3101" (required)
	Logger     *slog.Logger // structured logger (required)
	ResourceID string       // this service's canonical resource id (required unless Apex)
	AuthServer string       // the dashboard authorization-server base URL (required unless Apex)

	// Apex bypasses the standard PRM/health routes and the identity gate: the
	// dashboard registers its own complete route table via Register and consumes
	// no injected identity. When Apex is true, ResourceID/AuthServer are optional
	// and no PRM route is mounted.
	Apex bool

	// MCP, when set, is mounted at POST /mcp behind requireIdentityHeaders by the
	// standard route table. Services that expose an MCP surface pass it here; the
	// dashboard leaves it nil and registers its own routes via Register.
	MCP http.Handler

	// Feed, when set, is mounted unauthenticated at GET /feed by the standard
	// route table (event-protocol.md §2 — loopback-only, off the public proxy).
	// appkit/feed supplies this for producers; nil for non-producers.
	Feed http.Handler

	// FeedPath is the path the Feed handler mounts at (default "/feed"). It
	// mirrors Spec.Feed so a producer that names a non-default feed path stays
	// consistent.
	FeedPath string

	// Register is the service's route-registration hook (Spec.Handlers). It runs
	// after the standard routes are installed (or instead of them, when Apex), so
	// a service mounts its own extra routes — gated or unauthenticated — using the
	// Router seam. May be nil.
	Register func(*Router) error

	// Version is the build-stamped version string for the health envelope
	// (required for the standard table).
	Version string
	// Service is the service name (spec.App) for the health envelope.
	Service string
	// Health is the optional per-service details reporter for the health envelope.
	Health func(ctx context.Context) (map[string]any, error)
	// Events is the published event-family registry, exposed to the Register hook
	// (rt.Events()) so a service wires its reflection tool from the same source the
	// producer outbox validates against. Empty for non-producers.
	Events outbox.Registry
	// Subscriptions is the live provider of what this service listens to, exposed
	// to the Register hook (rt.Subscriptions()) so the reflection tool reports the
	// live in-edges (mirrors Health). nil for non-consumers.
	Subscriptions func() []consumer.Subscription
	// Publishes is the live provider of what this service currently publishes,
	// exposed to the Register hook (rt.Publishes()) so the reflection tool's
	// `publishes` half reports the live out-edges of a dynamic producer (mirrors
	// Subscriptions). When set it is preferred over the static Events; nil for
	// static producers, which keep rendering reflection from Events.
	Publishes func() outbox.Registry
	// WWW is the loaded web site for services that opt into chassis web assets.
	// When set on a non-apex server, New mounts GET /static/ automatically and
	// exposes the Site through Router.WWW() for service-owned page routes.
	WWW *web.Site

	// DB is the shared single-writer SQLite handle appkit opened and migrated. It
	// is exposed to the Register hook (rt.DB()) so a service builds its domain over
	// the same connection appkit (and any producer outbox) uses. May be nil (apex
	// services that own their whole stack, or tests that register no DB-backed
	// routes). database/sql is stdlib, so this keeps appkit/server decoupled from
	// the event-plane library.
	DB *sql.DB
}

// Router is the seam a service's Spec.Handlers uses to register routes on the
// server appkit stands up. It exposes the underlying mux, the identity gate, and
// the resolved resource id, so a service can mount gated and unauthenticated
// routes without re-deriving the chassis.
type Router struct {
	mux        *http.ServeMux
	app        *appHandler
	logger     *slog.Logger
	resourceID string
	authServer string
	db         *sql.DB
}

// Handle registers h for pattern on the mux verbatim (no gating). Use for
// public routes or routes the caller has already wrapped with RequireIdentity.
func (rt *Router) Handle(pattern string, h http.Handler) {
	rt.mux.Handle(pattern, h)
}

// HandleFunc is the http.HandlerFunc convenience over Handle.
func (rt *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	rt.mux.Handle(pattern, h)
}

// HandleLoopback registers h as a loopback-only route. Requests that transited
// the front door receive a bare 404 without invoking h.
func (rt *Router) HandleLoopback(pattern string, h http.Handler) {
	rt.mux.Handle(pattern, LoopbackOnly(h))
}

// LoopbackOnly wraps a handler that must never be reachable through the front
// door. nginx stamps X-Forwarded-Proto on every proxied request, so any
// non-empty value is answered with a bare 404. Identity headers are deliberately
// ignored because legitimate loopback machine callers assert them themselves.
func LoopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-Proto") != "" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireIdentity wraps h with the identity-header gate: it rejects (401 +
// resource_metadata challenge) any request that did not arrive through nginx's
// authenticated front door, and otherwise stashes the caller identity on the
// context. Services wrap their gated routes with this.
func (rt *Router) RequireIdentity(h http.Handler) http.Handler {
	return rt.app.requireIdentityHeaders(h)
}

// Logger returns the server's structured logger.
func (rt *Router) Logger() *slog.Logger { return rt.logger }

// ResourceID returns this service's canonical resource id.
func (rt *Router) ResourceID() string { return rt.resourceID }

// AuthServer returns the dashboard authorization-server base URL.
func (rt *Router) AuthServer() string { return rt.authServer }

// DB returns the shared single-writer SQLite handle appkit opened and migrated,
// so the service builds its domain over the same connection (the producer outbox
// shares it too). It is nil only when New was given no DB (apex/test).
func (rt *Router) DB() *sql.DB { return rt.db }

// Version returns the build-stamped version string for the health envelope, so a
// service wires its MCP health tool from the same source as the HTTP /health route.
func (rt *Router) Version() string { return rt.app.version }

// Service returns the service name for the health envelope.
func (rt *Router) Service() string { return rt.app.service }

// Health returns the optional per-service details reporter (nil when unset), so
// the MCP health tool renders the same details as the HTTP /health route.
func (rt *Router) Health() func(context.Context) (map[string]any, error) { return rt.app.health }

// Events returns the published event-family registry, so the MCP reflection tool's
// `publishes` half renders from the same source the producer outbox validates
// against. Empty for non-producers.
func (rt *Router) Events() outbox.Registry { return rt.app.events }

// Subscriptions returns the live provider of what this service listens to (nil
// when unset), so the MCP reflection tool's `subscribes` half reports the live
// in-edges. Mirrors Health.
func (rt *Router) Subscriptions() func() []consumer.Subscription { return rt.app.subscriptions }

// Publishes returns the live provider of what this service currently publishes
// (nil when unset), so the MCP reflection tool's `publishes` half can PREFER it
// over the static Events for a dynamic producer (cron). The precedence is the
// service's to apply at its reflection seam: call Publishes() when non-nil and
// render its returned Registry; otherwise fall back to Events(). Mirrors
// Subscriptions.
func (rt *Router) Publishes() func() outbox.Registry { return rt.app.publishes }

// WWW returns the loaded web site for services that opt into chassis web assets.
// It is nil when Spec.WWW is unset, so unconverted services keep their old shape.
func (rt *Router) WWW() *web.Site { return rt.app.www }

// appHandler holds the HTTP layer's auth dependencies. Methods on it implement
// the PRM document, the identity gate, and health. Unexported: the package's
// public surface is New/Run/Router.
type appHandler struct {
	logger        *slog.Logger
	resourceID    string
	authServer    string
	version       string
	service       string
	health        func(ctx context.Context) (map[string]any, error)
	events        outbox.Registry
	subscriptions func() []consumer.Subscription
	publishes     func() outbox.Registry
	www           *web.Site
}

// New builds the HTTP server with its routes, security headers, and pinned
// timeouts. It validates required config at this wiring seam so a misconfigured
// boot fails loudly here rather than at first request. It does not start
// listening.
func New(opts Options) (*http.Server, error) {
	if opts.Logger == nil {
		return nil, errors.New("server: Logger is required")
	}
	if !opts.Apex {
		if opts.ResourceID == "" {
			return nil, errors.New("server: ResourceID is required")
		}
		if opts.AuthServer == "" {
			return nil, errors.New("server: AuthServer is required")
		}
	}

	a := &appHandler{
		logger:        opts.Logger,
		resourceID:    opts.ResourceID,
		authServer:    opts.AuthServer,
		version:       opts.Version,
		service:       opts.Service,
		health:        opts.Health,
		events:        opts.Events,
		subscriptions: opts.Subscriptions,
		publishes:     opts.Publishes,
		www:           opts.WWW,
	}
	mux := http.NewServeMux()
	rt := &Router{mux: mux, app: a, logger: opts.Logger, resourceID: opts.ResourceID, authServer: opts.AuthServer, db: opts.DB}

	if !opts.Apex {
		// Standard path-routed service route table.
		// Unauthenticated: RFC 9728 protected-resource metadata — the only route
		// NOT behind the identity gate, so a client can discover the AS.
		mux.Handle("GET /.well-known/oauth-protected-resource", a.handlePRMetadata())
		// Ungated: the liveness health route (DECISIONS §5) — joins PRM and /feed
		// as an unauthenticated route, so it survives an auth outage.
		mux.Handle("GET /health", a.handleHealth())
		// Authenticated: the JSON-RPC MCP endpoint (when the service exposes one).
		if opts.MCP != nil {
			mux.Handle("POST /mcp", a.requireIdentityHeaders(opts.MCP))
		}
		// Event plane: the SSE feed is unauthenticated and loopback-only,
		// deliberately not behind the identity gate or reachable via nginx.
		if opts.Feed != nil {
			fp := opts.FeedPath
			if fp == "" {
				fp = "/feed"
			}
			mux.Handle("GET "+fp, LoopbackOnly(opts.Feed))
		}
		if opts.WWW != nil {
			mux.Handle("GET /static/", opts.WWW.Static())
		}
	}

	if opts.Register != nil {
		if err := opts.Register(rt); err != nil {
			return nil, fmt.Errorf("server: register routes: %w", err)
		}
	}

	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           securityHeaders(logging.RequestIDMiddleware(opts.Logger, mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return srv, nil
}

// Run starts srv and blocks until ctx is cancelled, then shuts it down
// gracefully within shutdownTimeout. A clean shutdown returns nil; a listen
// failure returns that error.
func Run(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	// Long-lived handlers (the SSE /feed stream) park on their request context.
	// http.Server.Shutdown does NOT cancel in-flight request contexts, so without
	// this they block Shutdown until shutdownTimeout, exiting 1 on every swap.
	// Derive request contexts from a cancelable base and cancel it at shutdown so
	// parked handlers return promptly and Shutdown drains cleanly.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv.BaseContext = func(net.Listener) context.Context { return baseCtx }

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutdown initiated")
		cancelBase() // release parked long-lived (/feed) handlers so Shutdown drains promptly
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		logger.Info("shutdown complete")
		return nil
	}
}
