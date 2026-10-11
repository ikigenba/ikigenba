// Package cli provides the injectable artifacts process.
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
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
	"github.com/ikigenba/ikigenba/artifacts/internal/server"
	"github.com/ikigenba/ikigenba/artifacts/internal/settings"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/sweeper"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
	"github.com/ikigenba/ikigenba/artifacts/internal/web"
)

// Exit codes distinguish success, server failure and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage is the command help.
const Usage = "Usage: artifacts [command]\n\nBinary files agents and services share, each put and fetched by path at\n/files/<path>, with MCP tools at /mcp and a page of artifacts at /, on\nthe socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest describes the app to its host.
const Manifest = "app = \"artifacts\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[env]\nARTIFACT_MAX_BYTES = \"1073741824\"\nIMPORT_SECONDS = \"600\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/artifacts.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n"

// NginxConf configures streaming transfers at the public name.
const NginxConf = "client_max_body_size    0;\nproxy_request_buffering off;\nproxy_http_version      1.1;\nproxy_buffering         on;\nproxy_read_timeout      3600s;\nproxy_send_timeout      3600s;\nlocation = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// Process supplies the runtime resources and deterministic seams.
type Process struct {
	Args        []string
	LookupEnv   func(string) (string, bool)
	Unsetenv    func(string) error
	Pid         int
	Stdout      io.Writer
	Stderr      io.Writer
	Version     string
	Inherit     func(uintptr) (net.Listener, error)
	Now         func() time.Time
	Sleep       func(context.Context, time.Duration)
	After       func(time.Duration) <-chan time.Time
	ImportAfter func(time.Duration) <-chan time.Time
	Rand        io.Reader
	Dir         string
	Sink        telemetry.Sink
	EventSink   events.Sink
	Banner      func(page.User) page.Banner
	MCP         func(*telemetry.Writer) *mcp.Server
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

// lifecycleOutput prevents terminated request goroutines writing after Run returns.
type lifecycleOutput struct {
	mu     sync.Mutex
	writer io.Writer
	closed bool
}

func (o *lifecycleOutput) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return len(b), nil
	}
	return o.writer.Write(b)
}
func (o *lifecycleOutput) finish(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	if line != "" {
		_, _ = o.writer.Write([]byte("artifacts: " + strings.ReplaceAll(line, "\n", " ") + "\n"))
	}
}
func diagnostic(p Process, line string) int {
	_, _ = p.Stderr.Write([]byte("artifacts: " + strings.ReplaceAll(line, "\n", " ") + "\n"))
	return ExitServerFailed
}

// Run executes a command or serves until its context is canceled.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
		return command(ctx, p)
	}
	s, err := settings.Read(p.LookupEnv)
	if err != nil {
		_, _ = p.Stderr.Write([]byte("artifacts: " + err.Error() + "\n"))
		return ExitUsage
	}
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	n, _ := strconv.ParseUint(fds, 10, 64)
	if pid != strconv.Itoa(p.Pid) || n == 0 {
		_, _ = p.Stderr.Write([]byte("artifacts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if n != 1 {
		_, _ = p.Stderr.Write([]byte("artifacts: " + fds + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	if p.Inherit == nil {
		p.Inherit = func(fd uintptr) (net.Listener, error) {
			f := os.NewFile(fd, "listener")
			defer func() { _ = f.Close() }()
			return net.FileListener(f)
		}
	}
	ln, err := p.Inherit(3)
	if err != nil {
		return diagnostic(p, err.Error())
	}
	defer func() { _ = ln.Close() }()
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.After == nil {
		p.After = time.After
	}
	if p.ImportAfter == nil {
		p.ImportAfter = time.After
	}
	if p.Rand == nil {
		p.Rand = rand.Reader
	}
	p.Rand = &lockedReader{r: p.Rand}
	if p.Sink == nil {
		p.Sink = telemetry.NewSocketSink()
	}
	if p.EventSink == nil {
		p.EventSink = events.NewSocketSink()
	}
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "artifacts.db"), Migrations: artifacts.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: p.Stderr})
	if err != nil {
		return diagnostic(p, "cannot open database state/artifacts.db: "+err.Error())
	}
	defer func() { _ = d.Close() }()
	blobs := filepath.Join(p.Dir, "state", "blobs")
	if err = os.MkdirAll(blobs, 0700); err != nil {
		return diagnostic(p, "cannot create directory state/blobs: "+err.Error())
	}
	st := store.New(d, blobs, store.Config{Now: p.Now, Rand: p.Rand})
	gone, err := st.Reconcile(ctx)
	if err != nil {
		return diagnostic(p, store.Unreachable)
	}
	output := &lifecycleOutput{writer: p.Stderr}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: output, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: p.EventSink, Stderr: output, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand, Telemetry: w, Emits: trail.Emits()})
	w.Ready()
	for _, a := range gone {
		trail.Orphan(identity.NewContext(context.Background(), identity.Caller{UserID: a.OwnerID, Email: a.OwnerEmail}), w, a)
	}
	sw := sweeper.Start(ctx, sweeper.Config{Store: st, Now: p.Now, After: p.After, Telemetry: w, Events: em})
	path, _ := p.LookupEnv("IKIGENBA_SERVICES")
	h := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), ServicesPath: path, Store: st, Telemetry: w, Events: em, Settings: s, Now: p.Now, ImportAfter: p.ImportAfter})
	notify, _ := p.LookupEnv("NOTIFY_SOCKET")
	if notify != "" {
		if strings.HasPrefix(notify, "@") {
			notify = "\x00" + notify[1:]
		}
		conn, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: notify, Net: "unixgram"})
		if e == nil {
			_, _ = conn.Write([]byte("READY=1"))
			_ = conn.Close()
		}
	}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			sw.Stop()
		case <-stopped:
		}
	}()
	err = server.Serve(ctx, ln, h, s.Drain(), func(drain context.Context) {
		sw.Stop()
		_ = em.Flush(drain)
		reason := ""
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		w.Shutdown(drain, reason)
		em.Shutdown(drain)
	})
	close(stopped)
	sw.Stop()
	if err != nil {
		output.finish(err.Error())
		return ExitServerFailed
	}
	output.finish("")
	return ExitSuccess
}
func command(ctx context.Context, p Process) int {
	if len(p.Args) == 1 {
		var output string
		switch p.Args[0] {
		case "--version":
			output = p.Version + "\n"
		case "manifest":
			output = Manifest
		case "--help":
			output = Usage
		}
		if output != "" {
			_, _ = p.Stdout.Write([]byte(output))
			return ExitSuccess
		}
	}
	if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
		err := db.Status(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "artifacts.db"), Migrations: artifacts.Migrations()}, p.Stdout)
		if err != nil {
			return diagnostic(p, err.Error())
		}
		return ExitSuccess
	}
	arg := p.Args[0]
	switch arg {
	case "--version", "manifest", "--help":
		arg = p.Args[1]
	case "db":
		if len(p.Args) > 1 {
			arg = p.Args[1]
			if arg == "status" {
				arg = p.Args[2]
			}
		}
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = p.Stderr.Write([]byte("artifacts: unknown " + kind + " '" + arg + "'\n\nsee 'artifacts --help' for usage\n"))
	return ExitUsage
}
