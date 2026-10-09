// Package cli runs webhooks with injected process resources.
package cli

import (
	"context"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/pages"
	"github.com/ikigenba/ikigenba/webhooks/internal/server"
	"github.com/ikigenba/ikigenba/webhooks/internal/settings"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/sweeper"
	"github.com/ikigenba/ikigenba/webhooks/internal/trail"
	"github.com/ikigenba/ikigenba/webhooks/internal/web"
)

// Process supplies the resources of one program invocation.
type Process struct {
	Args           []string
	LookupEnv      func(string) (string, bool)
	Unsetenv       func(string) error
	Pid            int
	Stdout, Stderr io.Writer
	Version        string
	Inherit        func(uintptr) (net.Listener, error)
	Now            func() time.Time
	Sleep          func(context.Context, time.Duration)
	After          func(time.Duration) <-chan time.Time
	Rand           io.Reader
	Dir            string
	Sink           telemetry.Sink
	EventSink      events.Sink
	Banner         func(page.User) page.Banner
	MCP            func(*telemetry.Writer) *mcp.Server
}

type serialWriter struct {
	mu     sync.Mutex
	out    io.Writer
	closed bool
}

func (s *serialWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.out == nil {
		return len(p), nil
	}
	return s.out.Write(p)
}
func (s *serialWriter) close() { s.mu.Lock(); s.closed = true; s.mu.Unlock() }

type serialRandom struct {
	mu     sync.Mutex
	source io.Reader
}

func (s *serialRandom) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source.Read(p)
}

// Run dispatches the command or serves the inherited socket.
func Run(ctx context.Context, p Process) int {
	if p.Stdout == nil {
		p.Stdout = io.Discard
	}
	if p.Stderr == nil {
		p.Stderr = io.Discard
	}
	if code, handled := dispatchCommand(ctx, p); handled {
		return code
	}
	out := &serialWriter{out: p.Stderr}
	p.Stderr = out
	defer out.close()
	if p.LookupEnv == nil {
		p.LookupEnv = os.LookupEnv
	}
	cfg, err := settings.Read(p.LookupEnv)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitUsage
	}
	pid, pidOK := p.LookupEnv("LISTEN_PID")
	fds, fdsOK := p.LookupEnv("LISTEN_FDS")
	digits := fds != ""
	nonzero := false
	for _, b := range []byte(fds) {
		digits = digits && b >= '0' && b <= '9'
		nonzero = nonzero || b != '0'
	}
	if !pidOK || pid != strconv.Itoa(p.Pid) || !fdsOK || !digits || !nonzero {
		diagnostic(p, "no socket was passed in\n\nrun it under systemd, with a listening socket passed in")
		return ExitUsage
	}
	if strings.TrimLeft(fds, "0") != "1" {
		diagnostic(p, fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in")
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
	ln, err := inherit(3)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	if ctx.Err() != nil {
		return ExitSuccess
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.Rand == nil {
		p.Rand = rand.Reader
	}
	p.Rand = &serialRandom{source: p.Rand}
	handle, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: p.Stderr})
	if err != nil {
		if ctx.Err() != nil {
			return ExitSuccess
		}
		diagnostic(p, "cannot open database state/webhooks.db: "+strings.ReplaceAll(err.Error(), "\n", " "))
		return ExitServerFailed
	}
	defer func() { _ = handle.Close() }()
	if ctx.Err() != nil {
		return ExitSuccess
	}
	eventOut := &serialWriter{out: p.Stderr}
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: eventOut, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand})
	emitter := events.New(events.Config{Service: pages.ServiceName, Sink: p.EventSink, Stderr: eventOut, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand, Telemetry: writer, Emits: trail.Emits()})
	abort := func() {
		eventOut.close()
		ended, cancel := context.WithCancel(context.Background())
		cancel()
		emitter.Shutdown(ended)
		writer.Shutdown(ended, "")
	}
	st := store.New(handle, store.Config{Now: p.Now, Rand: p.Rand})
	sweep := sweeper.Start(ctx, sweeper.Config{Store: st, Retention: cfg.Retention(), Now: p.Now, After: p.After})
	sweepStopped := make(chan struct{})
	var stopOnce sync.Once
	stopSweeper := func() { stopOnce.Do(func() { sweep.Stop(); close(sweepStopped) }) }
	stopSweep := context.AfterFunc(ctx, stopSweeper)
	defer stopSweep()
	path, _ := p.LookupEnv("IKIGENBA_SERVICES")
	srv := p.MCP(writer)
	handler := web.Handler(web.Config{Banner: p.Banner, MCP: srv, ServicesPath: path, Store: st, Telemetry: writer, Events: emitter})
	if ctx.Err() != nil {
		stopSweeper()
		abort()
		return ExitSuccess
	}
	address, _ := p.LookupEnv("NOTIFY_SOCKET")
	if ctx.Err() != nil {
		stopSweeper()
		abort()
		return ExitSuccess
	}
	if address != "" {
		if err = notify(address); err != nil {
			stopSweeper()
			abort()
			diagnostic(p, err.Error())
			return ExitServerFailed
		}
	}
	writer.Ready()
	err = server.Serve(ctx, ln, handler, cfg.Drain(), func(deadline context.Context) {
		select {
		case <-sweepStopped:
		case <-deadline.Done():
		}
		_ = emitter.Flush(deadline)
		writer.Shutdown(deadline, context.Cause(ctx).Error())
		emitter.Shutdown(deadline)
	})
	if ctx.Err() == nil {
		go stopSweeper()
		abort()
	}
	eventOut.close()
	if err != nil {
		diagnostic(p, err.Error())
		return ExitServerFailed
	}
	return ExitSuccess
}

func diagnostic(p Process, message string) {
	_, _ = p.Stderr.Write([]byte("webhooks: " + message + "\n"))
}
func inheritListener(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "inherited socket")
	defer func() { _ = f.Close() }()
	ln, err := net.FileListener(f)
	if unix, ok := ln.(*net.UnixListener); ok {
		unix.SetUnlinkOnClose(false)
	}
	return ln, err
}
func notify(address string) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("READY=1"))
	return err
}
