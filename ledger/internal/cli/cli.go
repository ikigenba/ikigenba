// Package cli implements ledger' command and injected process seam.
package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/pages"
	"github.com/ikigenba/ikigenba/ledger/internal/server"
	"github.com/ikigenba/ikigenba/ledger/internal/settings"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
	"github.com/ikigenba/ikigenba/ledger/internal/web"
)

// Exit codes distinguish success, service failure and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage describes the command to a person.
const Usage string = "Usage: ledger [--version|manifest|--help|db status]\n\nKeep the business's books.\nWith no arguments, serve on the socket systemd passes in.\ndb status reports database migrations.\n--version prints the release identity; manifest prints deployment settings.\n--help prints this help.\n\nExit codes: 0 success, 1 server or database failure, 2 usage error.\n"

// Manifest is the deployment contract.
const Manifest = "app = \"ledger\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/ledger.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"application\"\n"

// NginxConf hides the internal event-bus routes from public ingress.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// Process supplies every host-dependent operation used by Run.
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
	Rand      io.Reader
	Dir       string
	Sink      telemetry.Sink
	EventSink events.Sink
	Banner    func(page.User) page.Banner
	MCP       func(*telemetry.Writer) *mcp.Server
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

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
func diagnostic(w io.Writer, s string) {
	_, _ = w.Write([]byte("ledger: " + strings.ReplaceAll(s, "\n", " ") + "\n"))
}

// Run executes a command or serves until ctx is cancelled.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
		return command(ctx, p)
	}
	s, err := settings.Read(p.LookupEnv)
	if err != nil {
		_, _ = p.Stderr.Write([]byte("ledger: " + err.Error() + "\n"))
		return ExitUsage
	}
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	n, _ := strconv.Atoi(fds)
	if pid != strconv.Itoa(p.Pid) || n < 1 {
		_, _ = p.Stderr.Write([]byte("ledger: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if n > 1 {
		_, _ = fmt.Fprintf(p.Stderr, "ledger: %s sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n", fds)
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	ln, err := p.Inherit(3)
	if err != nil {
		diagnostic(p.Stderr, err.Error())
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	stderr := &lockedWriter{w: p.Stderr}
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "ledger.db"), Migrations: ledger.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: stderr})
	if err != nil {
		diagnostic(p.Stderr, "cannot open database state/ledger.db: "+err.Error())
		return ExitServerFailed
	}
	defer func() { _ = d.Close() }()
	rnd := &lockedReader{r: p.Rand}
	st := store.New(d, store.Config{Now: p.Now, Rand: rnd})
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: stderr, Now: p.Now, Sleep: p.Sleep, Rand: rnd})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: p.EventSink, Stderr: stderr, Now: p.Now, Sleep: p.Sleep, Rand: rnd, Telemetry: w, Emits: trail.Emits()})
	path, _ := p.LookupEnv(services.Variable)
	var ac *artifacts.Client
	if list, e := services.Read(path); e == nil {
		if entry, ok := list.Find("artifacts"); ok {
			ac = artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
		}
	}
	w.Ready()
	h := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), ServicesPath: path, Store: st, Telemetry: w, Events: em, Artifacts: ac, Settings: s})
	if socket, _ := p.LookupEnv("NOTIFY_SOCKET"); socket != "" {
		if c, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"}); e == nil {
			_, _ = c.Write([]byte("READY=1"))
			_ = c.Close()
		}
	}
	err = server.Serve(ctx, ln, h, s.Drain(), func(c context.Context) {
		_ = em.Flush(c)
		reason := ""
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		w.Shutdown(c, reason)
		em.Shutdown(c)
	})
	if err != nil {
		diagnostic(p.Stderr, err.Error())
		return ExitServerFailed
	}
	return ExitSuccess
}
func command(ctx context.Context, p Process) int {
	if len(p.Args) == 1 {
		switch p.Args[0] {
		case "--version":
			_, _ = fmt.Fprintln(p.Stdout, p.Version)
			return ExitSuccess
		case "manifest":
			_, _ = io.WriteString(p.Stdout, Manifest)
			return ExitSuccess
		case "--help":
			_, _ = io.WriteString(p.Stdout, Usage)
			return ExitSuccess
		}
	}
	if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
		err := db.Status(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "ledger.db"), Migrations: ledger.Migrations()}, p.Stdout)
		if err != nil {
			diagnostic(p.Stderr, err.Error())
			return ExitServerFailed
		}
		return ExitSuccess
	}
	arg := p.Args[0]
	if len(p.Args) > 1 {
		switch arg {
		case "db":
			arg = p.Args[1]
			if arg == "status" && len(p.Args) > 2 {
				arg = p.Args[2]
			}
		case "--version", "manifest", "--help":
			arg = p.Args[1]
		}
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = fmt.Fprintf(p.Stderr, "ledger: unknown %s '%s'\n\nsee 'ledger --help' for usage\n", kind, arg)
	return ExitUsage
}
