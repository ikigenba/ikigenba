package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/server"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/web"
)

type serialReader struct {
	sync.Mutex
	r io.Reader
}

func (r *serialReader) Read(b []byte) (int, error) { r.Lock(); defer r.Unlock(); return r.r.Read(b) }

type serialWriter struct {
	sync.Mutex
	w         io.Writer
	closed    bool
	quietStop bool
}

func (w *serialWriter) Write(b []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	if w.closed || w.quietStop && bytesStopping(b) {
		return len(b), nil
	}
	if w.w == nil {
		return len(b), nil
	}
	return w.w.Write(b)
}
func bytesStopping(b []byte) bool { return bytes.Contains(b, []byte(`"event":"service.stopping"`)) }

type serviceSink struct {
	mu       sync.Mutex
	sink     telemetry.Sink
	deadline context.Context
	ready    bool
}

func (s *serviceSink) Deliver(ctx context.Context, e telemetry.Event) error {
	s.mu.Lock()
	deadline, ready := s.deadline, s.ready
	s.mu.Unlock()
	if e.Name == "service.stopping" && !ready {
		return nil
	}
	if deadline != nil && deadline.Err() != nil {
		return telemetry.ErrRejected
	}
	return s.sink.Deliver(ctx, e)
}
func drainDeadline(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func duration(n int64) time.Duration {
	if n > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}
func inherited(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "systemd listener")
	if f == nil {
		return nil, fmt.Errorf("invalid socket descriptor")
	}
	defer func() { _ = f.Close() }()
	ln, e := net.FileListener(f)
	if u, ok := ln.(*net.UnixListener); ok {
		u.SetUnlinkOnClose(false)
	}
	return ln, e
}

type closeListener struct {
	net.Listener
	before func()
	once   sync.Once
}

func (l *closeListener) Close() error { l.once.Do(l.before); return l.Listener.Close() }

type drainContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *drainContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

// Run executes a command or serves and drains the inherited listener.
func Run(ctx context.Context, p Process) int {
	if handled, code := command(p.Args, p.Stdout, p.Stderr); handled {
		return code
	}
	stderr := &serialWriter{w: p.Stderr}
	defer func() { stderr.Lock(); stderr.closed = true; stderr.Unlock() }()
	fail := func(code int, text string) int {
		stderr.Lock()
		stderr.closed = true
		if stderr.w != nil {
			_, _ = stderr.w.Write([]byte("scripts: " + text + "\n"))
		}
		stderr.Unlock()
		return code
	}
	cfg, e := settings.Read(p.LookupEnv)
	if e != nil {
		return fail(ExitUsage, e.Error())
	}
	pid, pidOK := p.LookupEnv("LISTEN_PID")
	fds, fdsOK := p.LookupEnv("LISTEN_FDS")
	passed := pidOK && pid == strconv.Itoa(p.Pid) && fdsOK && len(fds) > 0
	for _, b := range []byte(fds) {
		passed = passed && b >= '0' && b <= '9'
	}
	n, parse := strconv.ParseUint(fds, 10, 64)
	passed = passed && (parse == nil && n >= 1 || parse != nil && n == math.MaxUint64)
	hint := "\n\nrun it under systemd, with a listening socket passed in"
	if !passed {
		return fail(ExitUsage, "no socket was passed in"+hint)
	}
	if n != 1 {
		return fail(ExitUsage, fds+" sockets were passed in, expected 1"+hint)
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	inherit := p.Inherit
	if inherit == nil {
		inherit = inherited
	}
	ln, e := inherit(3)
	if e != nil {
		return fail(ExitServerFailed, e.Error())
	}
	defer func() { _ = ln.Close() }()
	path, _ := p.LookupEnv("PATH")
	g, e := git.Find(path, p.Environ)
	if e != nil {
		return fail(ExitServerFailed, "git not found on PATH")
	}
	if _, e = runner.Find(path); e != nil {
		return fail(ExitServerFailed, runner.Interpreter+" not found on PATH")
	}
	dir := p.Dir
	if dir == "" {
		dir, e = os.Getwd()
		if e != nil {
			return fail(ExitServerFailed, e.Error())
		}
	}
	database := p.Database
	if database == "" {
		database = filepath.Join(dir, "state", "scripts.db")
	}
	random := p.Rand
	if random == nil {
		random = rand.Reader
	}
	random = &serialReader{r: random}
	catalog, e := store.Open(ctx, store.Config{Source: database, Now: p.Now, Rand: random})
	if e != nil {
		if ctx.Err() != nil {
			return ExitSuccess
		}
		return fail(ExitServerFailed, "cannot open database state/scripts.db: "+e.Error())
	}
	defer func() { _ = catalog.Close() }()
	runDir := filepath.Join(dir, "state", "runs")
	if e = os.MkdirAll(runDir, 0700); e != nil {
		return fail(ExitServerFailed, "cannot create directory state/runs: "+e.Error())
	}
	sink := p.Sink
	if sink == nil {
		sink = telemetry.NewSocketSink()
	}
	delivery := &serviceSink{sink: sink}
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: Version, Sink: delivery, Stderr: stderr, Now: p.Now, Sleep: p.Sleep, Rand: random})
	lim := limits.New(cfg, limits.Clock{After: p.After})
	repos := cfg.ReposDir
	if !filepath.IsAbs(repos) {
		repos = filepath.Join(dir, repos)
	}
	src := source.New(source.Config{Repos: repos, Git: g, Limits: lim})
	services, _ := p.LookupEnv("IKIGENBA_SERVICES")
	core := runs.New(runs.Config{Store: catalog, Source: src, Writer: writer, Runs: runDir, Path: path, Services: services, ScriptSeconds: cfg.ScriptSeconds, OutputMaxBytes: cfg.OutputMaxBytes, KeepDays: cfg.RunKeepDays, KeepCount: cfg.RunKeepCount, Now: p.Now, ScriptAfter: p.ScriptAfter, Rand: random})
	abort := func() {
		stderr.Lock()
		stderr.quietStop = true
		stderr.Unlock()
		deadline, cancel := context.WithTimeout(context.Background(), duration(cfg.DrainSeconds))
		defer cancel()
		writer.Shutdown(deadline, "")
	}
	if e = core.Recover(context.Background()); e == nil {
		e = core.Prune(context.Background())
	}
	if e != nil {
		abort()
		return fail(ExitServerFailed, store.Unreachable)
	}
	if ctx.Err() != nil {
		abort()
		return ExitSuccess
	}
	handler := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(writer), ServicesPath: services, Store: catalog, Limits: lim, Source: src, Runs: core, Telemetry: writer})
	notify, _ := p.LookupEnv("NOTIFY_SOCKET")
	if ctx.Err() != nil {
		abort()
		return ExitSuccess
	}
	if notify != "" {
		c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: notify, Net: "unixgram"})
		if err == nil {
			_, err = c.Write([]byte("READY=1"))
			_ = c.Close()
		}
		if err != nil {
			abort()
			return fail(ExitServerFailed, err.Error())
		}
	}
	delivery.mu.Lock()
	delivery.ready = true
	delivery.mu.Unlock()
	writer.Ready()
	var drainOnce sync.Once
	var drainCtx context.Context
	var drainCancel context.CancelFunc
	drained := make(chan struct{})
	begin := func() {
		drainOnce.Do(func() {
			drainCtx, drainCancel = drainDeadline(duration(cfg.DrainSeconds))
			delivery.mu.Lock()
			delivery.deadline = drainCtx
			delivery.mu.Unlock()
			context.AfterFunc(drainCtx, lim.Halt)
			entered := make(chan struct{})
			hook := &drainContext{Context: drainCtx, entered: entered}
			go func() { core.Drain(hook); close(drained) }()
			<-entered
		})
	}
	err := server.Serve(ctx, &closeListener{Listener: ln, before: begin}, handler, duration(cfg.DrainSeconds), func(_ context.Context) {
		begin()
		if drainCtx.Err() != nil {
			lim.Halt()
		}
		<-drained
		writer.Shutdown(drainCtx, context.Cause(ctx).Error())
	})
	begin()
	if ctx.Err() == nil {
		lim.Halt()
		drainCancel()
		<-drained
		writer.Shutdown(drainCtx, "accept failed")
	}
	drainCancel()
	if err != nil {
		return fail(ExitServerFailed, err.Error())
	}
	return ExitSuccess
}
