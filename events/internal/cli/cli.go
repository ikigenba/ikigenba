// Package cli runs events commands and wires the serving process.
package cli

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/declarations"
	"github.com/ikigenba/ikigenba/events/internal/delivery"
	"github.com/ikigenba/ikigenba/events/internal/pages"
	"github.com/ikigenba/ikigenba/events/internal/server"
	"github.com/ikigenba/ikigenba/events/internal/settings"
	"github.com/ikigenba/ikigenba/events/internal/store"
	"github.com/ikigenba/ikigenba/events/internal/tools"
	"github.com/ikigenba/ikigenba/events/internal/web"
)

// Command constants define products and exit statuses.
const (
	ExitSuccess = 0
	ExitFailure = 1
	ExitUsage   = 2
	Usage       = "Usage: events [command]\n\nServe the suite's internal event bus: emit at /emit, MCP tools at /mcp, and a\nlanding page at /, on the socket systemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	Manifest    = "app = \"events\"\ndescription = \"The suite's internal event bus\"\ndefault = false\nmcp = true\nsecrets = []\n\n[env]\nEVENTS_DEPTH_MAX = \"8\"\nEVENTS_DELIVERY_TIMEOUT_SECONDS = \"5\"\nEVENTS_DELIVERY_ATTEMPTS = \"10\"\nEVENTS_INFLIGHT_MAX = \"4\"\nEVENTS_RETENTION_DAYS = \"2\"\nEVENTS_DECLARATIONS_SECONDS = \"60\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/events.db\"\n"
	NginxConf   = "location = /emit { return 404; }\n"
)

// Process supplies the process resources and deterministic scheduling hooks.
type Process struct {
	Args         []string
	LookupEnv    func(key string) (string, bool)
	Unsetenv     func(key string) error
	Pid          int
	Stdout       io.Writer
	Stderr       io.Writer
	Version      string
	Banner       func(u page.User) page.Banner
	Inherit      func(fd uintptr) (net.Listener, error)
	Now          func() time.Time
	Sleep        func(ctx context.Context, d time.Duration)
	SweepAfter   func(d time.Duration) <-chan time.Time
	RefreshAfter func(d time.Duration) <-chan time.Time
	AskAfter     func(d time.Duration) <-chan time.Time
	TimeoutAfter func(d time.Duration) <-chan time.Time
	BackoffAfter func(d time.Duration) <-chan time.Time
	Rand         io.Reader
	Dir          string
	Sink         telemetry.Sink
}

// Run handles a command or serves the inherited listening socket.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
		return command(ctx, p)
	}
	s, err := settings.Read(p.LookupEnv)
	if err != nil {
		diagnostic(p.Stderr, err.Error())
		return ExitUsage
	}
	pid, havePID := p.LookupEnv("LISTEN_PID")
	fds, haveFDS := p.LookupEnv("LISTEN_FDS")
	if !havePID || !haveFDS || !digits(pid) || !digits(fds) || !samePID(pid, p.Pid) || allZero(fds) {
		diagnostic(p.Stderr, "no socket was passed in\n\nrun it under systemd, with a listening socket passed in")
		return ExitUsage
	}
	count, err := strconv.ParseUint(fds, 10, 64)
	if err != nil || count != 1 {
		diagnostic(p.Stderr, fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in")
		return ExitUsage
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
	listener, err := inherit(3)
	if err != nil {
		diagnostic(p.Stderr, err.Error())
		return ExitFailure
	}
	defer func() { _ = listener.Close() }()
	d, err := db.Open(ctx, db.Config{Path: databasePath(p), Migrations: events.Migrations(), Now: p.Now, Service: appEvents.ServiceName, Stderr: p.Stderr})
	if err != nil {
		diagnostic(p.Stderr, "cannot open database state/events.db: "+strings.ReplaceAll(err.Error(), "\n", " "))
		return ExitFailure
	}
	defer func() { _ = d.Close() }()
	servicePath, haveServices := p.LookupEnv("IKIGENBA_SERVICES")
	if !haveServices {
		servicePath = ""
	}
	writer := telemetry.New(telemetry.Config{Service: appEvents.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: p.Rand})
	var decl *declarations.Declarations
	st := store.New(d, store.Config{Now: p.Now, DepthMax: s.DepthMax, Ask: func(ctx context.Context, service string) { decl.Ask(ctx, service) }, Telemetry: writer})
	decl = declarations.New(declarations.Config{Store: st, Services: servicePath, Telemetry: writer, AskAfter: p.AskAfter})
	srv := mcp.NewServer(mcp.ServerConfig{Name: appEvents.ServiceName, Version: p.Version, Telemetry: writer, Instructions: func(context.Context) string { return instructions(servicePath) }})
	tools.Register(srv, tools.Config{Store: st, Telemetry: writer})
	pg := pages.New(pages.Config{Banner: p.Banner, ServicesPath: servicePath, Store: st})
	handler := web.Handler(web.Config{Pages: pg, MCP: srv, Sink: st, Telemetry: writer})
	loop := delivery.New(delivery.Config{Store: st, Services: servicePath, Telemetry: writer, Settings: s, TimeoutAfter: p.TimeoutAfter, BackoffAfter: p.BackoffAfter})
	notify, haveNotify := p.LookupEnv("NOTIFY_SOCKET")
	if !haveNotify {
		notify = ""
	}
	err = server.Run(ctx, server.Config{Listener: listener, Handler: handler, Store: st, Declarations: decl, Delivery: loop, Telemetry: writer, NotifySocket: notify, Retention: s.Retention(), Refresh: s.DeclarationsInterval(), Drain: s.Drain(), Now: p.Now, SweepAfter: p.SweepAfter, RefreshAfter: p.RefreshAfter})
	if err != nil {
		diagnostic(p.Stderr, err.Error())
		return ExitFailure
	}
	return ExitSuccess
}

func databasePath(p Process) string    { return filepath.Join(p.Dir, "state", "events.db") }
func diagnostic(w io.Writer, s string) { _, _ = io.WriteString(w, "events: "+s+"\n") }
func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func allZero(s string) bool { return strings.TrimLeft(s, "0") == "" }
func samePID(s string, pid int) bool {
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && pid >= 0 && n == uint64(pid)
}
func inherited(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "events-listener")
	defer func() { _ = f.Close() }()
	return net.FileListener(f)
}
func instructions(path string) string {
	if path == "" {
		return ""
	}
	list, err := services.Read(path)
	if err != nil {
		return ""
	}
	e, ok := list.Find(appEvents.ServiceName)
	if !ok {
		return ""
	}
	return e.Description
}
func command(ctx context.Context, p Process) int {
	if len(p.Args) == 1 {
		var value string
		switch p.Args[0] {
		case "--version":
			value = p.Version + "\n"
		case "manifest":
			value = Manifest
		case "--help":
			value = Usage
		}
		if value != "" {
			_, _ = io.WriteString(p.Stdout, value)
			return ExitSuccess
		}
	}
	if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
		if err := db.Status(ctx, db.Config{Path: databasePath(p), Migrations: events.Migrations()}, p.Stdout); err != nil {
			diagnostic(p.Stderr, err.Error())
			return ExitFailure
		}
		return ExitSuccess
	}
	arg := p.Args[0]
	switch arg {
	case "--version", "manifest", "--help":
		if len(p.Args) > 1 {
			arg = p.Args[1]
		}
	case "db":
		if len(p.Args) > 1 {
			arg = p.Args[1]
			if arg == "status" && len(p.Args) > 2 {
				arg = p.Args[2]
			}
		}
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	diagnostic(p.Stderr, "unknown "+kind+" '"+arg+"'\n\nsee 'events --help' for usage")
	return ExitUsage
}
