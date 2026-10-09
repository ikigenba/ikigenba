// Package cli runs home's command and socket-activated service.
package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home/internal/pages"
	"github.com/ikigenba/ikigenba/home/internal/server"
)

// Command products and exit codes define the public CLI contract.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
	Usage            = "Usage: home [command]\n\nServe the space's front door, a page of every service at /, on the\nsocket systemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	Manifest         = "app = \"home\"\ndescription = \"" + pages.Description + "\"\ndefault = true\nmcp = false\nguests = false\nsecrets = []\n\n[resources]\nmemory_max = \"128M\"\n"
)

// Process supplies the process resources and environment used by Run.
type Process struct {
	Args      []string
	LookupEnv func(string) (string, bool)
	Unsetenv  func(string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	Inherit   func(uintptr) (net.Listener, error)
	Banner    func(page.User) page.Banner
	Sink      telemetry.Sink
}

type guardedWriter struct {
	mu     sync.Mutex
	dst    io.Writer
	closed bool
}

func (w *guardedWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.dst == nil {
		return len(b), nil
	}
	return w.dst.Write(b)
}
func (w *guardedWriter) close() { w.mu.Lock(); defer w.mu.Unlock(); w.closed = true }

// Run executes a command or serves the inherited socket until cancellation.
func Run(ctx context.Context, p Process) int {
	stderr := &guardedWriter{dst: p.Stderr}
	defer stderr.close()
	diagnostic := func(code int, line string) int { _, _ = stderr.Write([]byte("home: " + line + "\n")); return code }
	if len(p.Args) > 0 {
		first := p.Args[0]
		known := first == "--version" || first == "--help" || first == "manifest"
		if known && len(p.Args) == 1 {
			product := Usage
			switch first {
			case "--version":
				product = p.Version + "\n"
			case "manifest":
				product = Manifest
			}
			if p.Stdout != nil {
				_, _ = io.WriteString(p.Stdout, product)
			}
			return ExitSuccess
		}
		arg := first
		if known {
			arg = p.Args[1]
		}
		kind := "command"
		if strings.HasPrefix(arg, "-") {
			kind = "option"
		}
		return diagnostic(ExitUsage, "unknown "+kind+" '"+arg+"'\n\nsee 'home --help' for usage")
	}
	drainValue, present := p.LookupEnv("DRAIN_SECONDS")
	drain := 5 * time.Second
	if present && drainValue != "" {
		if !digits(drainValue) || drainValue[0] == '0' {
			return diagnostic(ExitUsage, "DRAIN_SECONDS is '"+drainValue+"', not a positive whole number of seconds")
		}
		seconds, err := strconv.ParseUint(drainValue, 10, 64)
		if err != nil || seconds > uint64(math.MaxInt64/int64(time.Second)) {
			drain = time.Duration(math.MaxInt64)
		} else {
			drain = time.Duration(seconds) * time.Second
		}
	}
	pid, pidOK := p.LookupEnv("LISTEN_PID")
	fds, fdsOK := p.LookupEnv("LISTEN_FDS")
	countText := strings.TrimLeft(fds, "0")
	if !pidOK || pid != strconv.Itoa(p.Pid) || !fdsOK || !digits(fds) || countText == "" {
		return diagnostic(ExitUsage, "no socket was passed in\n\nrun it under systemd, with a listening socket passed in")
	}
	if countText != "1" {
		return diagnostic(ExitUsage, fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in")
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
		return diagnostic(ExitServerFailed, err.Error())
	}
	defer func() { _ = ln.Close() }()
	if ctx.Err() != nil {
		return ExitSuccess
	}
	address, notifyPresent := p.LookupEnv("NOTIFY_SOCKET")
	if ctx.Err() != nil {
		return ExitSuccess
	}
	if notifyPresent && address != "" {
		conn, notifyErr := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
		if notifyErr == nil {
			_, notifyErr = conn.Write([]byte("READY=1"))
			_ = conn.Close()
		}
		if notifyErr != nil {
			return diagnostic(ExitServerFailed, notifyErr.Error())
		}
	}
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: p.Sink, Stderr: stderr})
	writer.Ready()
	path, pathPresent := p.LookupEnv("IKIGENBA_SERVICES")
	if !pathPresent {
		path = ""
	}
	h := pages.Handler(pages.Config{Banner: p.Banner, ServicesPath: path, Telemetry: writer})
	stopped := false
	stop := func(deadline context.Context) { stopped = true; writer.Shutdown(deadline, context.Cause(ctx).Error()) }
	err = server.Serve(ctx, ln, h, drain, stop)
	if !stopped {
		deadline, cancel := context.WithTimeout(context.Background(), drain)
		writer.Shutdown(deadline, "serve failed")
		cancel()
	}
	if err != nil {
		return diagnostic(ExitServerFailed, err.Error())
	}
	return ExitSuccess
}
func digits(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func inheritListener(fd uintptr) (net.Listener, error) {
	file := os.NewFile(fd, fmt.Sprintf("listener-%d", fd))
	if file == nil {
		return nil, fmt.Errorf("cannot take file descriptor %d", fd)
	}
	defer func() { _ = file.Close() }()
	return net.FileListener(file)
}
