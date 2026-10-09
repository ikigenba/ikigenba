package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	prompts "github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/server"
	"github.com/ikigenba/ikigenba/prompts/internal/settings"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/web"
	"golang.org/x/sys/unix"
)

type guardedWriter struct {
	mu     sync.Mutex
	w      io.Writer
	sealed bool
}

func (w *guardedWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sealed || w.w == nil {
		return len(b), nil
	}
	return w.w.Write(b)
}
func (w *guardedWriter) seal() { w.mu.Lock(); w.sealed = true; w.mu.Unlock() }

func (w *guardedWriter) finish(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.sealed && w.w != nil {
		_, _ = w.w.Write([]byte("prompts: " + text + "\n"))
	}
	w.sealed = true
}

type guardedReader struct {
	mu sync.Mutex
	r  io.Reader
}

func (r *guardedReader) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Read(b)
}

type deadlineSink struct {
	mu       sync.Mutex
	sink     telemetry.Sink
	deadline time.Time
	sealed   bool
}

func (s *deadlineSink) Deliver(ctx context.Context, e telemetry.Event) error {
	s.mu.Lock()
	blocked := s.sealed || (!s.deadline.IsZero() && !time.Now().Before(s.deadline))
	s.mu.Unlock()
	if blocked {
		return telemetry.ErrRejected
	}
	return s.sink.Deliver(ctx, e)
}
func durationSeconds(n int64) time.Duration {
	if n > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}
func serve(ctx context.Context, p Process) int {
	stderr := &guardedWriter{w: p.Stderr}
	defer stderr.seal()
	diagnostic := func(s string) { write(stderr, "prompts: "+s+"\n") }
	lookup := p.LookupEnv
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}
	s, e := settings.Read(lookup)
	if e != nil {
		diagnostic(e.Error())
		return ExitUsage
	}
	pid, pidOK := lookup("LISTEN_PID")
	fds, fdsOK := lookup("LISTEN_FDS")
	digits := len(fds) > 0
	for _, ch := range fds {
		digits = digits && ch >= '0' && ch <= '9'
	}
	count, err := strconv.ParseUint(fds, 10, 64)
	if !pidOK || pid != strconv.Itoa(p.Pid) || !fdsOK || !digits || (err == nil && count < 1) {
		write(stderr, "prompts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n")
		return ExitUsage
	}
	if err != nil || count != 1 {
		write(stderr, "prompts: "+fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n")
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	inherit := p.Inherit
	if inherit == nil {
		inherit = inheritListener
	}
	ln, e := inherit(3)
	if e != nil {
		diagnostic(e.Error())
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	_ = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
	keys := map[agentkit.Host]string{}
	for _, host := range []agentkit.Host{agentkit.HostAnthropic, agentkit.HostOpenAI, agentkit.HostGemini, agentkit.HostXAI, agentkit.HostOpenRouter} {
		keys[host], _ = lookup(agent.KeyVariable(host))
	}
	d, e := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "prompts.db"), Migrations: prompts.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: stderr})
	if e != nil {
		if ctx.Err() != nil {
			return ExitSuccess
		}
		diagnostic("cannot open database state/prompts.db: " + strings.ReplaceAll(e.Error(), "\n", " "))
		return ExitServerFailed
	}
	defer func() { _ = d.Close() }()
	source := p.Rand
	if source == nil {
		source = rand.Reader
	}
	random := &guardedReader{r: source}
	st := store.New(d, store.Config{Now: p.Now, Rand: random})
	runsDir := filepath.Join(p.Dir, "state", "runs")
	if e = os.MkdirAll(runsDir, 0700); e != nil {
		diagnostic("cannot create directory state/runs: " + e.Error())
		return ExitServerFailed
	}
	cg, why := prepareCgroup(p.Cgroup, p.Pid, s)
	if why != "" {
		diagnostic("runs are unavailable: " + why)
	}
	path, _ := lookup("PATH")
	services, _ := lookup("IKIGENBA_SERVICES")
	sink := p.Sink
	if sink == nil {
		sink = telemetry.NewSocketSink()
	}
	gate := &deadlineSink{sink: sink}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: gate, Stderr: stderr, Now: p.Now, Sleep: p.Sleep, Rand: random})
	defer func() { gate.mu.Lock(); gate.sealed = true; gate.mu.Unlock() }()
	core := runs.New(runs.Config{Store: st, Writer: w, Runs: runsDir, Path: path, Services: services, Cgroup: cg, Unavailable: why, Keys: keys, BaseURL: p.BaseURL, PromptSeconds: s.PromptSeconds, OutputMaxBytes: s.OutputMaxBytes, MaxToolCalls: s.RunMaxToolCalls, KeepDays: s.RunKeepDays, KeepCount: s.RunKeepCount, RunMemoryMaxBytes: s.RunMemoryMaxBytes, RunPidsMax: s.RunPidsMax, MaxActive: s.RunMaxActive, MaxQueued: s.RunMaxQueued, Now: p.Now, ScriptAfter: p.ScriptAfter, Rand: random})
	drain := durationSeconds(s.DrainSeconds)
	flush := func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), drain)
		defer cancel()
		_ = w.Flush(flushCtx)
	}
	if e = core.Recover(context.Background()); e == nil {
		e = core.Prune(context.Background())
	}
	if e != nil {
		flush()
		diagnostic(store.Unreachable)
		return ExitServerFailed
	}
	if ctx.Err() != nil {
		flush()
		return ExitSuccess
	}
	h := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), ServicesPath: services, Store: st, Runs: core, KeepDays: s.RunKeepDays, KeepCount: s.RunKeepCount, Telemetry: w})
	address, _ := lookup("NOTIFY_SOCKET")
	if address != "" {
		if e = notify(address); e != nil {
			flush()
			diagnostic(e.Error())
			return ExitServerFailed
		}
	}
	w.Ready()
	drained := make(chan struct{})
	drainCtx, drainCancel := context.WithCancel(context.Background())
	defer drainCancel()
	var drainTimer *time.Timer
	begin := sync.OnceFunc(func() {
		gate.mu.Lock()
		gate.deadline = time.Now().Add(drain)
		gate.mu.Unlock()
		drainTimer = time.AfterFunc(drain, drainCancel)
		go func() { core.Drain(drainCtx); close(drained) }()
	})
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	callback := context.AfterFunc(ctx, begin)
	defer callback()
	stop := func(stopCtx context.Context) {
		begin()
		<-drained
		w.Shutdown(stopCtx, context.Cause(ctx).Error())
		drainCancel()
	}
	e = server.Serve(ctx, ln, h, drain, stop)
	if e != nil {
		if ctx.Err() == nil {
			begin()
			<-drained
			w.Shutdown(drainCtx, e.Error())
			drainCancel()
		}
		stderr.finish(e.Error())
		return ExitServerFailed
	}
	return ExitSuccess
}
func inheritListener(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "listener")
	if f == nil {
		return nil, errors.New("invalid listener descriptor")
	}
	defer func() { _ = f.Close() }()
	ln, e := net.FileListener(f)
	if u, ok := ln.(*net.UnixListener); ok {
		u.SetUnlinkOnClose(false)
	}
	return ln, e
}
func notify(address string) error {
	conn, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if e != nil {
		return e
	}
	defer func() { _ = conn.Close() }()
	_, e = conn.Write([]byte("READY=1"))
	return e
}
func prepareCgroup(path string, pid int, s settings.Settings) (string, string) {
	if path == "" {
		return "", "no control group was found"
	}
	fail := func(e error) (string, string) { return "", strings.ReplaceAll(e.Error(), "\n", " ") }
	root, e := os.OpenRoot(path)
	if e != nil {
		return fail(e)
	}
	defer func() { _ = root.Close() }()
	procs, e := root.ReadFile("cgroup.procs")
	if e != nil {
		return fail(e)
	}
	fields := strings.FieldsFunc(string(procs), func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' })
	if len(fields) != 1 || fields[0] != strconv.Itoa(pid) {
		return "", "control group does not contain only this process"
	}
	main := filepath.Join(path, "main")
	if e = os.MkdirAll(main, 0750); e != nil {
		return fail(e)
	}
	if e = os.WriteFile(filepath.Join(main, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0600); e != nil {
		return fail(e)
	}
	if e = os.WriteFile(filepath.Join(path, "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0600); e != nil {
		return fail(e)
	}
	group := filepath.Join(path, "runs")
	if e = os.MkdirAll(group, 0750); e != nil {
		return fail(e)
	}
	cpu := "max"
	if s.RunsCPUPercent <= math.MaxInt64/1000 {
		cpu = strconv.FormatInt(s.RunsCPUPercent*1000, 10)
	}
	for _, f := range []struct{ name, value string }{{"memory.max", strconv.FormatInt(s.RunsMemoryMaxBytes, 10)}, {"cpu.max", cpu + " 100000"}, {"cgroup.subtree_control", "+memory +pids"}} {
		if e = os.WriteFile(filepath.Join(group, f.name), []byte(f.value), 0600); e != nil {
			return fail(e)
		}
	}
	return group, ""
}
