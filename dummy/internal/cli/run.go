package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/server"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// Process describes the process state used by Run.
type Process struct {
	Args      []string
	LookupEnv func(key string) (string, bool)
	Unsetenv  func(key string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Inherit   func(fd uintptr) (net.Listener, error)
	Banner    func(u page.User) page.Banner
	MCP       *mcp.Server
	Telemetry *telemetry.Writer
	Gate      *Gate
	Rand      io.Reader
	Dir       string
	Now       func() time.Time
}

// Process exit codes.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage describes the dummy command line.
const Usage = "Usage: dummy [command]\n\nServe the dummy control panel, and its MCP tools at /mcp, on the socket\nsystemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Run runs the dummy command and returns its process exit code.
func Run(ctx context.Context, p Process) int {
	stderr := &lockedWriter{writer: p.Stderr}
	if len(p.Args) > 0 {
		if len(p.Args) == 2 && p.Args[0] == "db" && p.Args[1] == "status" {
			err := db.Status(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "dummy.db"), Migrations: dummy.Migrations()}, p.Stdout)
			if err != nil {
				writeDiagnostic(stderr, "dummy: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n")
				return ExitServerFailed
			}
			return ExitSuccess
		}
		if len(p.Args) == 1 {
			switch p.Args[0] {
			case "--version":
				_, _ = io.WriteString(p.Stdout, Version+"\n")
				return ExitSuccess
			case "manifest":
				_, _ = io.WriteString(p.Stdout, Manifest)
				return ExitSuccess
			case "--help":
				_, _ = io.WriteString(p.Stdout, Usage)
				return ExitSuccess
			}
		}
		arg := p.Args[0]
		if arg == "db" && len(p.Args) > 2 && p.Args[1] == "status" {
			arg = p.Args[2]
		} else if (arg == "--version" || arg == "manifest" || arg == "--help" || arg == "db") && len(p.Args) > 1 {
			arg = p.Args[1]
		}
		kind := "command"
		if strings.HasPrefix(arg, "-") {
			kind = "option"
		}
		writeDiagnostic(stderr, "dummy: unknown "+kind+" '"+arg+"'\n\nsee 'dummy --help' for usage\n")
		return ExitUsage
	}

	drainText, _ := lookup(p.LookupEnv, "DRAIN_SECONDS")
	drain, valid := parseDrain(drainText)
	if !valid {
		writeDiagnostic(stderr, "dummy: DRAIN_SECONDS is '"+drainText+"', not a positive whole number of seconds\n")
		return ExitUsage
	}

	pid, pidSet := lookup(p.LookupEnv, "LISTEN_PID")
	fds, fdsSet := lookup(p.LookupEnv, "LISTEN_FDS")
	const hint = "\n\nrun it under systemd, with a listening socket passed in\n"
	if !pidSet || pid != strconv.Itoa(p.Pid) || !fdsSet || !decimalPositive(fds, false) {
		writeDiagnostic(stderr, "dummy: no socket was passed in"+hint)
		return ExitUsage
	}
	if decimalGreaterThanOne(fds) {
		writeDiagnostic(stderr, "dummy: "+fds+" sockets were passed in, expected 1"+hint)
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
		writeDiagnostic(stderr, "dummy: "+err.Error()+"\n")
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()

	handle, err := db.Open(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "dummy.db"), Migrations: dummy.Migrations(), Now: p.Now})
	if err != nil {
		writeDiagnostic(stderr, "dummy: cannot open database state/dummy.db: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n")
		return ExitServerFailed
	}
	defer func() { _ = handle.Close() }()
	store := widget.NewStore(handle, p.Rand)
	if address, ok := lookup(p.LookupEnv, "NOTIFY_SOCKET"); ok && address != "" {
		if err = notifyReady(address); err != nil {
			writeDiagnostic(stderr, "dummy: "+err.Error()+"\n")
			return ExitServerFailed
		}
	}
	handler := panel.Handler(store, p.Banner, p.MCP, p.Telemetry)
	p.Telemetry.Ready()
	if p.Gate != nil {
		stopWatching := p.Gate.watch(ctx, drain)
		defer stopWatching()
	}
	if err = server.Serve(ctx, ln, handler, drain, func(stopCtx context.Context) {
		if p.Gate != nil {
			if deadline, ok := stopCtx.Deadline(); ok {
				p.Gate.limit(deadline)
			}
			if stopCtx.Err() != nil {
				p.Gate.limit(time.Now())
			}
		}
		p.Telemetry.Shutdown(stopCtx, context.Cause(ctx).Error())
	}); err != nil {
		detail := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
		writeDiagnostic(stderr, "dummy: "+detail+"\n")
		return ExitServerFailed
	}
	return ExitSuccess
}

func lookup(fn func(string) (string, bool), key string) (string, bool) {
	if fn == nil {
		return "", false
	}
	return fn(key)
}

func decimalPositive(s string, noLeadingZero bool) bool {
	if len(s) == 0 || (noLeadingZero && s[0] == '0') {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	for i := range len(s) {
		if s[i] != '0' {
			return true
		}
	}
	return false
}

func decimalGreaterThanOne(s string) bool {
	for i := range len(s) {
		if s[i] != '0' {
			return s[i:] != "1"
		}
	}
	return false
}

func parseDrain(s string) (time.Duration, bool) {
	if s == "" {
		return 5 * time.Second, true
	}
	if !decimalPositive(s, true) {
		return 0, false
	}
	const maxSeconds = math.MaxInt64 / int64(time.Second)
	var seconds int64
	for i := range len(s) {
		digit := int64(s[i] - '0')
		if seconds > (maxSeconds-digit)/10 {
			return time.Duration(math.MaxInt64), true
		}
		seconds = seconds*10 + digit
	}
	return time.Duration(seconds) * time.Second, true
}

func inheritListener(fd uintptr) (net.Listener, error) {
	file := os.NewFile(fd, "inherited listener")
	if file == nil {
		return nil, fmt.Errorf("invalid inherited descriptor %d", fd)
	}
	ln, err := net.FileListener(file)
	_ = file.Close()
	return ln, err
}

func notifyReady(address string) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("READY=1"))
	return err
}

type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

func writeDiagnostic(stderr io.Writer, message string) {
	_, _ = stderr.Write([]byte(message))
}
