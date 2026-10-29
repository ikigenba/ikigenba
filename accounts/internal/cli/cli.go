// Package cli implements accounts' command and injected process seam.
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

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/server"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/web"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Exit codes distinguish success, service failure and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage describes the command to a person.
const Usage string = "Usage: accounts [--version|manifest|--help|db status]\n\nManage the host’s SSH accounts.\nWith no arguments, serve on the socket systemd passes in.\ndb status reports database migrations.\n--version prints the release identity; manifest prints deployment settings.\n--help prints this help.\n\nExit codes: 0 success, 1 server or database failure, 2 usage error.\n"

// Manifest is the deployment contract.
const Manifest = "app = \"accounts\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/accounts.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n"

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

type gatedWriter struct {
	mu     sync.Mutex
	w      io.Writer
	closed bool
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(p), nil
	}
	return w.w.Write(p)
}
func (w *gatedWriter) close() { w.mu.Lock(); defer w.mu.Unlock(); w.closed = true }
func diagnostic(w io.Writer, s string) {
	_, _ = w.Write([]byte("accounts: " + strings.ReplaceAll(s, "\n", " ") + "\n"))
}

// Run executes a command or serves until ctx is cancelled.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
		return command(ctx, p)
	}
	s, err := settings.Read(p.LookupEnv)
	if err != nil {
		_, _ = p.Stderr.Write([]byte("accounts: " + err.Error() + "\n"))
		return ExitUsage
	}
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	digits := fds != ""
	for _, c := range fds {
		if c < '0' || c > '9' {
			digits = false
		}
	}
	n := strings.TrimLeft(fds, "0")
	if pid != strconv.Itoa(p.Pid) || !digits || n == "" {
		_, _ = p.Stderr.Write([]byte("accounts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if n != "1" {
		_, _ = fmt.Fprintf(p.Stderr, "accounts: %s sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n", fds)
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
	stderr := &gatedWriter{w: p.Stderr}
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "accounts.db"), Migrations: accounts.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: stderr})
	if err != nil {
		diagnostic(p.Stderr, "cannot open database state/accounts.db: "+err.Error())
		return ExitServerFailed
	}
	defer func() { _ = d.Close() }()
	rnd := &lockedReader{r: p.Rand}
	st := store.New(d, store.Config{Now: p.Now})
	var h host.Host
	if fields := s.HostFields(); fields != nil {
		h = host.NewCommand(fields)
	} else {
		h = host.NewMemory()
	}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: stderr, Now: p.Now, Sleep: p.Sleep, Rand: rnd})
	path, _ := p.LookupEnv(services.Variable)
	w.Ready()
	hnd := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), ServicesPath: path, Store: st, Telemetry: w, Host: h, Settings: s})
	if socket, _ := p.LookupEnv("NOTIFY_SOCKET"); socket != "" {
		if c, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"}); e == nil {
			_, _ = c.Write([]byte("READY=1"))
			_ = c.Close()
		}
	}
	stopped := false
	err = server.Serve(ctx, ln, hnd, s.Drain(), func(c context.Context) {
		stopped = true
		reason := ""
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		w.Shutdown(c, reason)
	})
	if !stopped {
		c, cancel := context.WithTimeout(context.Background(), s.Drain())
		w.Shutdown(c, "")
		cancel()
	}
	stderr.close()
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
		err := db.Status(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "accounts.db"), Migrations: accounts.Migrations()}, p.Stdout)
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
	_, _ = fmt.Fprintf(p.Stderr, "accounts: unknown %s '%s'\n\nsee 'accounts --help' for usage\n", kind, arg)
	return ExitUsage
}
