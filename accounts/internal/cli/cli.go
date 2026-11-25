// Package cli implements accounts' command and injected process seam.
package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/box"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/awssdk"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/daemonapi"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/server"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/sim"
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
const Usage string = "Usage: accounts [--version|manifest|--help|db status]\n\nManage SSH hosts, accounts and users.\nWith no arguments, serve on the socket systemd passes in.\ndb status reports database migrations.\n--version prints the release identity; manifest prints deployment settings.\n--help prints this help.\n\nExit codes: 0 success, 1 server or database failure, 2 usage error.\n"

// Manifest is the deployment contract.
const Manifest = "app = \"accounts\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = true\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/accounts.db\"\n\n[resources]\nmemory_max = \"256M\"\n\n[home]\ngroup = \"core\"\n"

// NoCloud describes a failure to open the selected cloud.
const NoCloud = "cannot open cloud"

// NginxConf hides the internal event-bus routes from public ingress.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// Process supplies every host-dependent operation used by Run.
type Process struct {
	Args       []string
	LookupEnv  func(string) (string, bool)
	Unsetenv   func(string) error
	Pid        int
	Stdout     io.Writer
	Stderr     io.Writer
	Version    string
	Inherit    func(uintptr) (net.Listener, error)
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration)
	Rand       io.Reader
	Dir        string
	Sink       telemetry.Sink
	Banner     func(page.User) page.Banner
	MCP        func(*telemetry.Writer) *mcp.Server
	After      func(time.Duration) <-chan time.Time
	Executable func() (string, error)
	Cloud      cloud.Opener
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
	return serve(ctx, p, ln, s)
}

func serve(ctx context.Context, p Process, ln net.Listener, s settings.Settings) int {
	if p.After == nil {
		p.After = time.After
	}
	if p.Sleep == nil {
		p.Sleep = sleep
	}
	stderr := &gatedWriter{w: p.Stderr}
	defer stderr.close()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "accounts.db"), Migrations: accounts.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: stderr})
	if err != nil {
		diagnostic(p.Stderr, "cannot open database state/accounts.db: "+err.Error())
		return ExitServerFailed
	}
	defer func() { _ = d.Close() }()
	rnd := &lockedReader{r: p.Rand}
	st := store.New(d, store.Config{Now: p.Now, Rand: rnd})
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: stderr, Now: p.Now, Sleep: p.Sleep, Rand: rnd})
	writer.Ready()

	path, _ := p.LookupEnv(services.Variable)
	binding := func() (string, error) { return s.Binding(path) }
	accountsURL := func() (string, error) { return s.AccountsURL(path) }
	hosts := s.HostsInUse()
	c, mc, err := openCloud(ctx, p, s, rnd)
	if err != nil {
		diagnostic(stderr, NoCloud+": "+err.Error())
		c, mc, hosts = nil, nil, settings.Hosts{}
	}
	configured := c != nil
	binary := ""
	if configured {
		binary = daemonBinary(p.Executable)
	}

	verifier := hostauth.NewVerifier(hostauth.VerifierConfig{Region: hosts.Region, Role: hosts.InstanceProfile, Binding: binding, STS: c, Now: p.Now})
	central, user, admin := p.MCP(writer), p.MCP(writer), p.MCP(writer)
	boxConfig := box.Config{Store: st, Telemetry: writer, AccountsURL: accountsURL}
	box.RegisterUser(user, boxConfig)
	box.RegisterAdmin(admin, boxConfig)
	// A cancelled start still needs a handler so the ordinary shutdown path
	// can close the writer. An interrupted digest simply disables the download.
	daemonConfig := daemonapi.Config{Store: st, Verifier: verifier, Binary: binary, Telemetry: writer, Now: p.Now, Configured: configured, After: p.After, BoxUser: user, BoxAdmin: admin}
	ds, err := daemonapi.New(ctx, daemonConfig)
	if err != nil {
		daemonConfig.Binary = ""
		ds, _ = daemonapi.New(context.Background(), daemonConfig)
	}
	engine := ops.New(ops.Config{Store: st, Cloud: c, Hosts: hosts, AccountsURL: accountsURL, Telemetry: writer, Now: p.Now, After: p.After})
	handler := web.Handler(web.Config{Banner: p.Banner, MCP: central, ServicesPath: path, Store: st, Cloud: c, Hosts: hosts, Configured: configured, Wake: engine.Wake, Daemon: ds, Telemetry: writer, Settings: s})
	notifyReady(p.LookupEnv)

	work, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	// Keep the serving context live until long polls can exit. The engine's
	// cloud calls lose their context immediately when the process is cancelled.
	serving, stopServing := context.WithCancel(context.WithoutCancel(ctx))
	defer stopServing()
	detach := context.AfterFunc(ctx, func() {
		ds.Stop()
		stopServing()
	})
	defer detach()
	var background sync.WaitGroup
	if configured {
		background.Go(func() { _ = engine.Run(work) })
	}
	if mc != nil {
		b, _ := binding()
		simulator := sim.New(sim.Config{
			Cloud: mc, Handler: handler, Binding: b, Digest: ds.Digest, Now: p.Now, Sleep: p.Sleep, After: p.After,
			BanFile: filepath.Join(p.Dir, "state", "sandbox-bans"),
			BoxDir:  filepath.Join(p.Dir, "state", "box"),
		})
		background.Go(func() { _ = simulator.Run(work) })
	}
	backgroundDone := make(chan struct{})
	go func() {
		background.Wait()
		close(backgroundDone)
	}()
	stop := func(drain context.Context) {
		cancelWork()
		ds.Stop()
		httpDrained := drain.Err() == nil
		select {
		case <-backgroundDone:
		case <-drain.Done():
		}
		if httpDrained && drain.Err() != nil {
			// A cloud call can ignore cancellation. Leave a bounded window for
			// an already-drained server to deliver its stopping event.
			var cancel context.CancelFunc
			drain, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
		}
		reason := ""
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		writer.Shutdown(drain, reason)
	}
	stopped := false
	err = server.Serve(serving, ln, handler, s.Drain(), func(drain context.Context) {
		stopped = true
		stop(drain)
	})
	if !stopped {
		drain, cancel := context.WithTimeout(context.Background(), s.Drain())
		stop(drain)
		cancel()
	}
	stderr.close()
	if err != nil {
		diagnostic(p.Stderr, err.Error())
		return ExitServerFailed
	}
	return ExitSuccess
}

func openCloud(ctx context.Context, p Process, s settings.Settings, rnd io.Reader) (cloud.Cloud, *memory.Cloud, error) {
	switch s.Cloud() {
	case settings.ModeAWS:
		open := p.Cloud
		if open == nil {
			open = awssdk.Open
		}
		c, err := open(ctx, s.Hosts.Region)
		return c, nil, err
	case settings.ModeSandbox:
		c, err := memory.New(memory.Config{Path: filepath.Join(p.Dir, "state", "sandbox-cloud.json"), Now: p.Now, Rand: rnd, Hosts: s.HostsInUse()})
		if err != nil {
			return nil, nil, err
		}
		return c, c, nil
	default:
		return nil, nil, nil
	}
}

func daemonBinary(executable func() (string, error)) string {
	if executable == nil {
		executable = os.Executable
	}
	path, err := executable()
	if err != nil {
		return ""
	}
	bin, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(path), "accountsd"))
	if err != nil {
		return ""
	}
	return bin
}

func sleep(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func notifyReady(lookup func(string) (string, bool)) {
	socket, _ := lookup("NOTIFY_SOCKET")
	if socket == "" {
		return
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err == nil {
		_, _ = conn.Write([]byte("READY=1"))
		_ = conn.Close()
	}
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
