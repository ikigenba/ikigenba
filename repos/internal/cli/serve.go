package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
	"github.com/ikigenba/ikigenba/repos/internal/server"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	smarthttp "github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

// output serializes diagnostics with asynchronous event fallback, and disables
// late event writes before control returns to the caller.
type output struct {
	mu     sync.Mutex
	writer io.Writer
	closed bool
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.writer == nil {
		return len(p), nil
	}
	return o.writer.Write(p)
}
func (o *output) close()                    { o.mu.Lock(); o.closed = true; o.mu.Unlock() }
func (o *output) diagnostic(message string) { _, _ = o.Write([]byte("repos: " + message + "\n")) }

type randomSource struct {
	mu     sync.Mutex
	source io.Reader
}

func (r *randomSource) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.source.Read(p)
}

func seconds(n int64) time.Duration {
	if n > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}

func socketCount(p Process) (string, bool, bool) {
	pid, ok := p.LookupEnv("LISTEN_PID")
	fds, found := p.LookupEnv("LISTEN_FDS")
	if !ok || !found || pid != strconv.Itoa(p.Pid) || fds == "" {
		return fds, false, false
	}
	for _, b := range []byte(fds) {
		if b < '0' || b > '9' {
			return fds, false, false
		}
	}
	digits := strings.TrimLeft(fds, "0")
	return fds, digits != "", digits == "1"
}

func serve(ctx context.Context, p Process) int {
	diagnostic := &output{writer: p.Stderr}
	defer diagnostic.close()
	cfg, err := settings.Read(p.LookupEnv)
	if err != nil {
		diagnostic.diagnostic(err.Error())
		return ExitUsage
	}
	fds, passed, one := socketCount(p)
	if !passed {
		_, _ = diagnostic.Write([]byte("repos: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if !one {
		_, _ = diagnostic.Write([]byte("repos: " + fds + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	inherit := p.Inherit
	if inherit == nil {
		inherit = inheritedListener
	}
	ln, err := inherit(3)
	if err != nil {
		diagnostic.diagnostic(err.Error())
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	path := environmentValue(p, "PATH")
	g, err := git.Find(path, p.Environ)
	if err != nil {
		diagnostic.diagnostic("git not found on PATH")
		return ExitServerFailed
	}
	if ctx.Err() != nil {
		return ExitSuccess
	}
	source := p.Rand
	if source == nil {
		source = rand.Reader
	}
	random := &randomSource{source: source}
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "repos.db"), Migrations: repos.Migrations(), Now: p.Now})
	if err != nil {
		if ctx.Err() != nil {
			return ExitSuccess
		}
		diagnostic.diagnostic("cannot open database state/repos.db: " + strings.ReplaceAll(err.Error(), "\n", " "))
		return ExitServerFailed
	}
	defer func() { _ = d.Close() }()
	s, err := store.Open(ctx, d, store.Config{Root: filepath.Join(p.Dir, "state", "repos"), Git: g, Now: p.Now, Rand: random})
	if err != nil {
		if ctx.Err() != nil {
			return ExitSuccess
		}
		if errors.Is(err, store.ErrRoot) {
			diagnostic.diagnostic("cannot create directory state/repos: " + strings.ReplaceAll(err.Error(), "\n", " "))
		} else {
			diagnostic.diagnostic("cannot open database state/repos.db: " + strings.ReplaceAll(err.Error(), "\n", " "))
		}
		return ExitServerFailed
	}
	w := telemetry.New(telemetry.Config{Service: web.ServiceName, Version: Version, Sink: p.Sink, Stderr: diagnostic, Now: p.Now, Sleep: p.Sleep, Rand: random})
	err = s.Verify(ctx, w)
	if ctx.Err() != nil {
		flushStart(w)
		return ExitSuccess
	}
	if err != nil {
		flushStart(w)
		diagnostic.diagnostic("cannot open database state/repos.db: " + strings.ReplaceAll(err.Error(), "\n", " "))
		return ExitServerFailed
	}
	emitter := events.New(events.Config{Service: web.ServiceName, Sink: p.EventSink, Stderr: diagnostic, Now: p.Now, Sleep: p.Sleep, Rand: random, Telemetry: w, Emits: smarthttp.Emits()})
	defer func() { done, cancel := context.WithCancel(context.Background()); cancel(); emitter.Shutdown(done) }()
	servicesPath := environmentValue(p, "IKIGENBA_SERVICES")
	mcpServer := p.MCP(w)
	l := limits.New(cfg, limits.Clock{Now: p.Now, After: p.After})
	h := web.Handler(web.Config{Banner: p.Banner, MCP: mcpServer, ServicesPath: servicesPath, Store: s, Git: g, Limits: l, Telemetry: w, Events: emitter})
	address := environmentValue(p, "NOTIFY_SOCKET")
	if ctx.Err() != nil {
		flushStart(w)
		return ExitSuccess
	}
	if err = notifyReady(address); err != nil {
		flushStart(w)
		diagnostic.diagnostic(err.Error())
		return ExitServerFailed
	}
	w.Ready()
	scheduler := maintenance.Start(maintenance.Config{Store: s, Git: g, Limits: l, Telemetry: w})
	// Drain queued operations immediately on cancellation, while running requests
	// retain their independent contexts until server.Serve's drain deadline.
	stopped := make(chan struct{})
	drainWatch := context.AfterFunc(ctx, func() {
		l.Drain()
		drainCtx, cancel := context.WithTimeout(context.Background(), seconds(cfg.DrainSeconds))
		defer cancel()
		scheduler.Stop(drainCtx)
		close(stopped)
	})
	var gitRequests sync.WaitGroup
	var requestsMu sync.Mutex
	requestsClosed := false
	tracked := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		if requestsClosed {
			requestsMu.Unlock()
			return
		}
		segment, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if r.URL.Path == "/mcp" || strings.HasSuffix(segment, ".git") {
			gitRequests.Add(1)
			defer gitRequests.Done()
		}
		requestsMu.Unlock()
		h.ServeHTTP(rw, r)
	})
	stop := func(drainCtx context.Context) {
		<-stopped
		emitter.Shutdown(drainCtx)
		w.Shutdown(drainCtx, context.Cause(ctx).Error())
	}
	err = server.Serve(ctx, ln, tracked, seconds(cfg.DrainSeconds), stop)
	if !drainWatch() {
		<-stopped
	}
	// Serve closes unfinished connections and cancels their contexts. Wait for
	// protocol/tool handlers to reap their git processes before returning.
	requestsMu.Lock()
	requestsClosed = true
	requestsMu.Unlock()
	gitRequests.Wait()
	if ctx.Err() == nil {
		done, cancel := context.WithCancel(context.Background())
		cancel()
		scheduler.Stop(done)
		emitter.Shutdown(done)
		w.Shutdown(done, "accept failed")
	}
	if err != nil {
		diagnostic.diagnostic(err.Error())
		return ExitServerFailed
	}
	return ExitSuccess
}

// A failed or cancelled start has no ready/stopping lifecycle transition.
// Finish any verification events while the diagnostic destination is active.
func flushStart(w *telemetry.Writer) {
	_ = w.Flush(context.Background())
}

func environmentValue(p Process, key string) string {
	value, ok := p.LookupEnv(key)
	if !ok {
		return ""
	}
	return value
}
