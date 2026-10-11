// Package cli implements the process seam and command dispatch.
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

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/server"
	"github.com/ikigenba/ikigenba/connections/internal/settings"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
	"github.com/ikigenba/ikigenba/connections/internal/web"
)

// Process carries every dependency consumed by Run.
type Process struct {
	Args      []string
	LookupEnv func(key string) (string, bool)
	Unsetenv  func(key string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	Inherit   func(fd uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration)
	Rand      io.Reader
	Dir       string
	Sink      telemetry.Sink
	EventSink events.Sink
	Banner    func(u page.User) page.Banner
	MCP       func(w *telemetry.Writer) *mcp.Server
	Providers []providers.Provider
	HTTP      *http.Client
}

// Exit codes shared by all command paths.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage is the command help.
const Usage = "Usage: connections [command]\n\nAccounts at outside providers, connected for the suite's connectors: a\nperson connects one at /<provider>/connect, siblings on the host get\naccess tokens at /token/<provider>, with MCP tools at /mcp and a page of\nconnections at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest declares the host integration.
const Manifest = "app = \"connections\"\ndescription = \"Accounts at outside providers, connected for the suite's connectors\"\ndefault = false\nmcp = true\nguests = false\nsecrets = [\"GOOGLE_CLIENT_ID\", \"GOOGLE_CLIENT_SECRET\"]\n\n[database]\nengine = \"sqlite\"\npath = \"state/connections.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n"

// NginxConf protects private endpoints at the public name.
const NginxConf = "location ^~ /token/ { return 404; }\nlocation = /events { return 404; }\nlocation = /declarations { return 404; }\n"

type lockedReader struct {
	mu sync.Mutex
	r  io.Reader
}

func (r *lockedReader) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Read(b)
}
func diagnostic(p Process, s string) { _, _ = io.WriteString(p.Stderr, "connections: "+s+"\n") }
func flatten(e error) string         { return strings.ReplaceAll(e.Error(), "\n", " ") }
func command(ctx context.Context, p Process) int {
	a := p.Args
	if len(a) == 1 {
		switch a[0] {
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
	if len(a) == 2 && a[0] == "db" && a[1] == "status" {
		if e := db.Status(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "connections.db"), Migrations: connections.Migrations()}, p.Stdout); e != nil {
			diagnostic(p, flatten(e))
			return ExitServerFailed
		}
		return ExitSuccess
	}
	arg := a[0]
	switch arg {
	case "--version", "manifest", "--help":
		arg = a[1]
	case "db":
		if len(a) > 1 {
			arg = a[1]
			if arg == "status" && len(a) > 2 {
				arg = a[2]
			}
		}
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	diagnostic(p, "unknown "+kind+" '"+arg+"'\n\nsee 'connections --help' for usage")
	return ExitUsage
}

// Run dispatches commands or serves the listener supplied by the process.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
		return command(ctx, p)
	}
	if p.Providers == nil {
		p.Providers = providers.Table()
	}
	names := make([]string, 0, 2*len(p.Providers))
	for _, entry := range p.Providers {
		names = append(names, entry.ClientIDVar, entry.ClientSecretVar)
	}
	secrets, err := settings.Secrets(p.LookupEnv, names)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitUsage
	}
	cfg, err := settings.Read(p.LookupEnv)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitUsage
	}
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	n, e := strconv.Atoi(fds)
	if pid != strconv.Itoa(p.Pid) || e != nil || n < 1 {
		diagnostic(p, "no socket was passed in\n\nrun it under systemd, with a listening socket passed in")
		return ExitUsage
	}
	if n > 1 {
		diagnostic(p, fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in")
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	if p.Inherit == nil {
		p.Inherit = inherit
	}
	ln, err := p.Inherit(3)
	if err != nil {
		diagnostic(p, flatten(err))
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.Rand == nil {
		p.Rand = rand.Reader
	}
	p.Rand = &lockedReader{r: p.Rand}
	if p.HTTP == nil {
		p.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "connections.db"), Migrations: connections.Migrations(), Now: p.Now, Service: "connections", Stderr: p.Stderr})
	if err != nil {
		diagnostic(p, "cannot open database state/connections.db: "+flatten(err))
		return ExitServerFailed
	}
	defer func() { _ = d.Close() }()
	s := store.New(d, store.Config{Now: p.Now, Rand: p.Rand})
	_ = s.PurgeExpiredPending(ctx)
	w := telemetry.New(telemetry.Config{Service: "connections", Version: p.Version, Sink: p.Sink, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand})
	emitter := events.New(events.Config{Service: "connections", Sink: p.EventSink, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand, Telemetry: w, Emits: trail.Emits()})
	clients := make([]providers.Client, 0, len(p.Providers))
	for _, entry := range p.Providers {
		clients = append(clients, providers.Client{Provider: entry, ID: secrets[entry.ClientIDVar], Secret: secrets[entry.ClientSecretVar], HTTP: p.HTTP, Now: p.Now})
	}
	servicesPath, _ := p.LookupEnv("IKIGENBA_SERVICES")
	var mc *mcp.Server
	if p.MCP != nil {
		mc = p.MCP(w)
	} else {
		mc = mcp.NewServer(mcp.ServerConfig{Name: "connections", Version: p.Version, Telemetry: w})
	}
	h := web.Handler(web.Config{Banner: p.Banner, MCP: mc, ServicesPath: servicesPath, CallbackURL: cfg.CallbackURL, Clients: clients, Store: s, Telemetry: w, Events: emitter, Now: p.Now, Rand: p.Rand})
	w.Ready()
	emitter.Ready()
	notify, _ := p.LookupEnv("NOTIFY_SOCKET")
	ready(notify)
	err = server.Serve(ctx, ln, h, cfg.Drain(), func(drain context.Context) {
		emitter.Shutdown(drain)
		reason := "server stopped"
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		w.Shutdown(drain, reason)
	})
	if err != nil {
		diagnostic(p, flatten(err))
		return ExitServerFailed
	}
	return ExitSuccess
}
func inherit(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "listener")
	defer func() { _ = f.Close() }()
	return net.FileListener(f)
}
func ready(path string) {
	if path == "" {
		return
	}
	if strings.HasPrefix(path, "@") {
		path = "\x00" + path[1:]
	}
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()
	_, _ = c.Write([]byte("READY=1"))
}
