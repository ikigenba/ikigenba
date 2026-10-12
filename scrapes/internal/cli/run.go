package cli

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/server"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
	"github.com/ikigenba/ikigenba/scrapes/internal/web"
)

// SweepInterval is the interval between retention sweeps.
const SweepInterval = time.Hour

func diagnostic(p Process, text string, code int) int {
	_, _ = p.Stderr.Write([]byte("scrapes: " + text + "\n"))
	return code
}

// Run handles a command or serves the inherited listener until cancellation.
func Run(ctx context.Context, p Process) int {
	if code, handled := command(ctx, p); handled {
		return code
	}
	cfg, err := settings.Read(p.LookupEnv)
	if err != nil {
		return diagnostic(p, err.Error(), ExitUsage)
	}
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	count, countErr := strconv.ParseUint(fds, 10, 64)
	if pid != strconv.Itoa(p.Pid) || countErr != nil || count == 0 {
		return diagnostic(p, "no socket was passed in\n\nrun it under systemd, with a listening socket passed in", ExitUsage)
	}
	if count != 1 {
		return diagnostic(p, fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in", ExitUsage)
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	ln, err := p.Inherit(3)
	if err != nil {
		return diagnostic(p, "cannot take listening socket: "+strings.ReplaceAll(err.Error(), "\n", " "), ExitServerFailed)
	}
	defer func() { _ = ln.Close() }()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "scrapes.db"), Migrations: app.Migrations(), Now: p.Now})
	if err != nil {
		return diagnostic(p, "cannot open database state/scrapes.db: "+strings.ReplaceAll(err.Error(), "\n", " "), ExitServerFailed)
	}
	defer func() { _ = d.Close() }()
	random := &lockedReader{reader: p.Rand}
	if p.Sink == nil {
		p.Sink = telemetry.NewSocketSink()
	}
	if p.EventSink == nil {
		p.EventSink = events.NewSocketSink()
	}
	gate := &deliveryGate{}
	writer := telemetry.New(telemetry.Config{Service: "scrapes", Version: p.Version, Sink: telemetryGate{gate, p.Sink}, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: random})
	writer.Ready()
	emitter := events.New(events.Config{Service: "scrapes", Sink: eventGate{gate, p.EventSink}, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: random, Telemetry: writer, Emits: trail.Emits()})
	s := store.New(d, store.Config{Now: p.Now, Rand: random})
	servicesPath, _ := p.LookupEnv(services.Variable)
	registry := scrapers.Default(scrapers.Config{HTTP: p.HTTP, Telemetry: writer, Now: p.Now, Sleep: p.Sleep, UserAgent: pages.ServiceName + " (" + pages.Description + ")", Settings: cfg})
	engine := runs.New(runs.Config{Store: s, Scrapers: registry, Telemetry: writer, Events: emitter})
	err = engine.Recover(ctx)
	if err != nil {
		emitter.Shutdown(context.Background())
		_ = writer.Flush(context.Background())
		return diagnostic(p, store.Unreachable, ExitServerFailed)
	}
	_ = prune(ctx, s, cfg)
	var ms *mcp.Server
	if p.MCP != nil {
		ms = p.MCP(writer)
	} else {
		ms = mcp.NewServer(mcp.ServerConfig{Name: "scrapes", Version: p.Version, Telemetry: writer})
	}
	h := web.Handler(web.Config{Banner: p.Banner, MCP: ms, ServicesPath: servicesPath, Store: s, Scrapers: registry, Runs: engine, Telemetry: writer, Events: emitter})
	notify, _ := p.LookupEnv("NOTIFY_SOCKET")
	if notify != "" {
		notifyReady(notify)
	}
	sweepCtx, stopSweep := context.WithCancel(ctx)
	swept := make(chan struct{})
	go func() { defer close(swept); sweep(sweepCtx, p, s, cfg) }()
	var drainCtx context.Context
	var drainCancel context.CancelFunc
	defer func() {
		if drainCancel != nil {
			drainCancel()
		}
	}()
	var stopOnce sync.Once
	stop := func(dc context.Context) {
		stopOnce.Do(func() {
			deadline, _ := dc.Deadline()
			drainCtx, drainCancel = newDrain(deadline)
			gate.set(drainCtx)
			stopSweep()
			<-swept
			engine.Drain(drainCtx)
		})
	}
	err = server.Serve(ctx, ln, h, cfg.Drain(), stop)
	stopOnce.Do(func() {
		drainCtx, drainCancel = newDrain(time.Now().Add(cfg.Drain()))
		if ctx.Err() != nil {
			drainCancel()
		}
		gate.set(drainCtx)
		stopSweep()
		<-swept
		engine.Drain(drainCtx)
	})

	emitter.Shutdown(drainCtx)
	reason := "serve failed"
	if cause := context.Cause(ctx); cause != nil {
		reason = cause.Error()
	}
	writer.Shutdown(drainCtx, reason)
	if err != nil {
		return diagnostic(p, err.Error(), ExitServerFailed)
	}
	return ExitSuccess
}

func prune(ctx context.Context, s *store.Store, c settings.Settings) error {
	_, err := s.PruneRuns(ctx, c.KeepRuns)
	return err
}
func sweep(ctx context.Context, p Process, s *store.Store, c settings.Settings) {
	after := p.After
	if after == nil {
		after = time.After
	}
	for {
		if ctx.Err() != nil {
			return
		}
		timer := after(SweepInterval)
		select {
		case <-ctx.Done():
			return
		case <-timer:
			if ctx.Err() != nil {
				return
			}
			_ = prune(ctx, s, c)
		}
	}
}
func notifyReady(path string) {
	if strings.HasPrefix(path, "@") {
		path = "\x00" + path[1:]
	}
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if err == nil {
		_, _ = c.Write([]byte("READY=1"))
		_ = c.Close()
	}
}

type deliveryGate struct {
	mu  sync.Mutex
	ctx context.Context
}

func (g *deliveryGate) set(ctx context.Context) { g.mu.Lock(); g.ctx = ctx; g.mu.Unlock() }
func (g *deliveryGate) allow() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ctx != nil {
		return g.ctx.Err()
	}
	return nil
}

type telemetryGate struct {
	gate *deliveryGate
	sink telemetry.Sink
}

func (g telemetryGate) Deliver(ctx context.Context, e telemetry.Event) error {
	if err := g.gate.allow(); err != nil {
		return err
	}
	return g.sink.Deliver(ctx, e)
}

type eventGate struct {
	gate *deliveryGate
	sink events.Sink
}

func (g eventGate) Deliver(ctx context.Context, e events.Event) error {
	if err := g.gate.allow(); err != nil {
		return err
	}
	return g.sink.Deliver(ctx, e)
}

func newDrain(deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(context.Background(), deadline)
}
