// Package cli implements gdrive's process boundary.
package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
	"github.com/ikigenba/ikigenba/gdrive/internal/server"
	"github.com/ikigenba/ikigenba/gdrive/internal/settings"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/trail"
	"github.com/ikigenba/ikigenba/gdrive/internal/transfers"
	"github.com/ikigenba/ikigenba/gdrive/internal/urls"
	"github.com/ikigenba/ikigenba/gdrive/internal/web"
)

// Process supplies the process and network dependencies.
type Process struct {
	Args      []string
	LookupEnv func(string) (string, bool)
	Unsetenv  func(string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	Inherit   func(uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration)
	After     func(time.Duration) <-chan time.Time
	Rand      io.Reader
	Dir       string
	Sink      telemetry.Sink
	EventSink events.Sink
	Banner    func(page.User) page.Banner
	MCP       func(*telemetry.Writer) *mcp.Server
	HTTP      *http.Client
}
type lockedReader struct {
	mu sync.Mutex
	r  io.Reader
}

func (r *lockedReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Read(p)
}
func diagnostic(p Process, s string) {
	_, _ = p.Stderr.Write([]byte("gdrive: " + strings.ReplaceAll(s, "\n", " ") + "\n"))
}

// Run executes a command or serves on the inherited listener.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
		return command(p)
	}
	s, err := settings.Read(p.LookupEnv)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitUsage
	}
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	n, _ := strconv.ParseUint(fds, 10, 64)
	activationPID, pidErr := strconv.Atoi(pid)
	if pidErr != nil || activationPID != p.Pid || n == 0 {
		_, _ = p.Stderr.Write([]byte("gdrive: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if n != 1 {
		_, _ = p.Stderr.Write([]byte("gdrive: " + fds + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, k := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(k)
		}
	}
	if p.Inherit == nil {
		p.Inherit = func(fd uintptr) (net.Listener, error) {
			f := os.NewFile(fd, "gdrive-listener")
			defer func() { _ = f.Close() }()
			return net.FileListener(f)
		}
	}
	ln, err := p.Inherit(3)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	if p.Now == nil {
		p.Now = time.Now
	}
	rawNow := p.Now
	var clockMu sync.Mutex
	p.Now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return rawNow() }
	if p.Rand == nil {
		p.Rand = rand.Reader
	}
	p.Rand = &lockedReader{r: p.Rand}
	if p.After == nil {
		p.After = time.After
	}
	if p.HTTP == nil {
		p.HTTP = DefaultHTTP()
	}
	handle, err := db.Open(context.WithoutCancel(ctx), db.Config{Path: filepath.Join(p.Dir, "state", "gdrive.db"), Migrations: gdrive.Migrations(), Now: p.Now})
	if err != nil {
		diagnostic(p, "cannot open database state/gdrive.db: "+err.Error())
		return ExitServerFailed
	}
	defer func() { _ = handle.Close() }()
	if p.Sink == nil {
		p.Sink = telemetry.NewSocketSink()
	}
	if p.EventSink == nil {
		p.EventSink = events.NewSocketSink()
	}
	gate := &deliveryGate{}
	p.Sink = gatedTelemetry{gate, p.Sink}
	p.EventSink = gatedEvents{gate, p.EventSink}
	p.Stderr = &lockedWriter{writer: p.Stderr}
	w := telemetry.New(telemetry.Config{Service: "gdrive", Version: p.Version, Sink: p.Sink, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand})
	em := events.New(events.Config{Service: "gdrive", Sink: p.EventSink, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand, Telemetry: w, Emits: trail.Emits()})
	st := store.New(handle, store.Config{Now: p.Now, Rand: p.Rand})
	path, _ := p.LookupEnv("IKIGENBA_SERVICES")
	list, _ := services.Read(path)
	var ac *artifacts.Client
	var cc *connections.Client
	var connect string
	if entry, ok := list.Find("artifacts"); ok {
		ac = artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
	}
	if entry, ok := list.Find("connections"); ok {
		cc = connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w})
		connect = entry.URL
	}
	dc := drive.NewClient(drive.Config{HTTP: p.HTTP, Telemetry: w})
	eng := transfers.New(transfers.Config{Store: st, Drive: dc, Artifacts: ac, Connections: cc, Telemetry: w, Events: em, ConnectURL: urls.Connect(urls.Base(connect)), MaxBytes: s.TransferMaxBytes, MaxActive: s.TransferMaxActive, MaxQueued: s.TransferMaxQueued, KeepDays: s.TransferKeepDays, KeepCount: s.TransferKeepCount, Now: p.Now})
	w.Ready()
	if err = eng.Recover(context.WithoutCancel(ctx)); err != nil {
		gate.abort()
		em.Shutdown(context.Background())
		w.Shutdown(context.Background(), "")
		diagnostic(p, store.Unreachable)
		return ExitServerFailed
	}
	_ = eng.Prune(context.WithoutCancel(ctx))
	h := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), ServicesPath: path, Store: st, Transfers: eng, Drive: dc, Telemetry: w, Events: em})
	pending := &requests{changed: make(chan struct{})}
	original := h
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pending.begin()
		defer pending.end()
		original.ServeHTTP(w, r)
	})
	notify, _ := p.LookupEnv("NOTIFY_SOCKET")
	if notify != "" {
		if strings.HasPrefix(notify, "@") {
			notify = "\x00" + notify[1:]
		}
		c, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: notify, Net: "unixgram"})
		if e == nil {
			_, _ = c.Write([]byte("READY=1"))
			_ = c.Close()
		}
	}
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		for {
			if ctx.Err() != nil {
				return
			}
			timer := p.After(SweepInterval)
			select {
			case <-ctx.Done():
				return
			case <-timer:
				if ctx.Err() != nil {
					return
				}
				_ = eng.Prune(context.WithoutCancel(ctx))
			}
		}
	}()
	err = server.Serve(ctx, ln, h, s.Drain(), func(dctx context.Context) {
		gate.stop(dctx)
		<-sweepDone
		eng.Drain(dctx)
		pending.wait(dctx)
		_ = em.Flush(dctx)
		reason := ""
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		w.Shutdown(dctx, reason)
		em.Shutdown(dctx)
	})
	if err != nil {
		diagnostic(p, err.Error())
		return ExitServerFailed
	}
	return ExitSuccess
}
func command(p Process) int {
	a := p.Args
	if len(a) == 1 {
		switch a[0] {
		case "--version":
			_, _ = io.WriteString(p.Stdout, p.Version+"\n")
			return ExitSuccess
		case "manifest":
			_, _ = io.WriteString(p.Stdout, Manifest)
			return ExitSuccess
		case "--help":
			_, _ = io.WriteString(p.Stdout, Usage)
			return ExitSuccess
		}
	}
	if len(a) == 2 && a[0] == "db" && a[1] == "status" {
		if err := db.Status(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "gdrive.db"), Migrations: gdrive.Migrations()}, p.Stdout); err != nil {
			diagnostic(p, err.Error())
			return ExitServerFailed
		}
		return ExitSuccess
	}
	arg := a[0]
	if arg == "db" && len(a) > 2 && a[1] == "status" {
		arg = a[2]
	} else if len(a) > 1 && (arg == "db" || arg == "--help" || arg == "--version" || arg == "manifest") {
		arg = a[1]
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = fmt.Fprintf(p.Stderr, "gdrive: unknown %s '%s'\n\nsee 'gdrive --help' for usage\n", kind, arg)
	return ExitUsage
}
