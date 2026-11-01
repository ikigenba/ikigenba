package cli

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/server"
	"github.com/ikigenba/ikigenba/sessions/internal/settings"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/trail"
	"github.com/ikigenba/ikigenba/sessions/internal/web"
)

// outputGate closes the writer's diagnostic stream without closing the process's.
type outputGate struct {
	mu     sync.Mutex
	dst    io.Writer
	closed bool
}

func (g *outputGate) Write(b []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return len(b), nil
	}
	return g.dst.Write(b)
}
func (g *outputGate) close() { g.mu.Lock(); defer g.mu.Unlock(); g.closed = true }

func serve(ctx context.Context, p Process, s settings.Settings) int {
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	count, err := strconv.ParseUint(fds, 10, 64)
	digits := fds != ""
	for _, c := range fds {
		digits = digits && c >= '0' && c <= '9'
	}
	if pid != strconv.Itoa(p.Pid) || !digits || (err == nil && count == 0) {
		return socketUsage(p, "no socket was passed in")
	}
	if err != nil || count != 1 {
		return socketUsage(p, fds+" sockets were passed in, expected 1")
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	ln, err := p.Inherit(3)
	if err != nil {
		return runtimeError(p, err.Error())
	}
	defer func() { _ = ln.Close() }()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "sessions.db"), Migrations: sessions.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: p.Stderr})
	if err != nil {
		return runtimeError(p, "cannot open database state/sessions.db: "+err.Error())
	}
	defer func() { _ = d.Close() }()
	path, _ := p.LookupEnv("IKIGENBA_SERVICES")
	g := &outputGate{dst: p.Stderr}
	defer g.close()
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: g, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand})
	w.Ready()
	st := store.New(d, store.Config{Now: p.Now})
	n, err := st.FinishRefresh(ctx)
	trail.Refresh(context.Background(), w, n, trail.Result(err))
	if err != nil {
		deadline, cancel := context.WithTimeout(context.Background(), s.Drain())
		defer cancel()
		w.Shutdown(deadline, store.Unreachable)
		g.close()
		return runtimeError(p, store.Unreachable)
	}
	hd := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), ServicesPath: path, Store: st, Telemetry: w, Settings: s})
	notify, _ := p.LookupEnv("NOTIFY_SOCKET")
	if notify != "" {
		address := &net.UnixAddr{Name: notify, Net: "unixgram"}
		if c, e := net.DialUnix("unixgram", nil, address); e == nil {
			_, _ = c.Write([]byte("READY=1"))
			_ = c.Close()
		}
	}
	err = server.Serve(ctx, ln, hd, s.Drain(), func(c context.Context) {
		reason := "serving failed"
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		w.Shutdown(c, reason)
	})
	g.close()
	if err != nil {
		return runtimeError(p, err.Error())
	}
	return ExitSuccess
}
func socketUsage(p Process, text string) int {
	_, _ = p.Stderr.Write([]byte("sessions: " + text + "\n\nrun it under systemd, with a listening socket passed in\n"))
	return ExitUsage
}
func runtimeError(p Process, text string) int {
	_, _ = p.Stderr.Write([]byte("sessions: " + strings.ReplaceAll(text, "\n", " ") + "\n"))
	return ExitServerFailed
}
