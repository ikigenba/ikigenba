// Package cli runs the gateway through an injected process seam.
package cli

import (
	"context"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	appkitmcp "github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
	"github.com/ikigenba/ikigenba/mcp/internal/server"
)

// Version is the release identity shared by every gateway surface.
var Version = "v0.1.1"

// Manifest is the platform application declaration.
const Manifest = "app = \"mcp\"\ndescription = \"Connect AI assistants to your services\"\ndefault = false\nmcp = false\nsecrets = []\n"

// Usage is the command's complete help text.
const Usage = "Usage: mcp [command]\n\nServe the MCP gateway at /mcp, and its connect page at /, on the socket\nsystemd passes in. With no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"

// Process supplies the process state and gateway construction inputs.
type Process struct {
	Args      []string
	LookupEnv func(key string) (string, bool)
	Unsetenv  func(key string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Inherit   func(fd uintptr) (net.Listener, error)
	Banner    func(u page.User) page.Banner
	MCP       *appkitmcp.Server
}

// Exit codes distinguish successful work, host failure, and usage failure.
const (
	ExitSuccess = iota
	ExitServerFailed
	ExitUsage
)

type runWriter struct {
	mu     sync.Mutex
	dst    io.Writer
	closed bool
}

func (w *runWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.dst == nil {
		return len(b), nil
	}
	return w.dst.Write(b)
}
func (w *runWriter) close() { w.mu.Lock(); w.closed = true; w.mu.Unlock() }

// Run handles commands or serves and drains the inherited socket.
func Run(ctx context.Context, p Process) int {
	stderr := &runWriter{dst: p.Stderr}
	defer stderr.close()
	diagnostic := func(text string, code int) int { _, _ = stderr.Write([]byte("mcp: " + text + "\n")); return code }
	if len(p.Args) > 0 {
		arg := p.Args[0]
		product := ""
		switch arg {
		case "--version":
			product = Version + "\n"
		case "manifest":
			product = Manifest
		case "--help":
			product = Usage
		}
		if product != "" && len(p.Args) == 1 {
			if p.Stdout != nil {
				_, _ = io.WriteString(p.Stdout, product)
			}
			return ExitSuccess
		}
		if product != "" {
			arg = p.Args[1]
		}
		kind := "command"
		if strings.HasPrefix(arg, "-") {
			kind = "option"
		}
		return diagnostic("unknown "+kind+" '"+arg+"'\n\nsee 'mcp --help' for usage", ExitUsage)
	}
	lookup := func(key string) (string, bool) {
		if p.LookupEnv == nil {
			return "", false
		}
		return p.LookupEnv(key)
	}
	drain := 5 * time.Second
	if v, ok := lookup("DRAIN_SECONDS"); ok && v != "" {
		if !digits(v) || v[0] == '0' {
			return diagnostic("DRAIN_SECONDS is '"+v+"', not a positive whole number of seconds", ExitUsage)
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n > uint64(math.MaxInt64/int64(time.Second)) {
			drain = time.Duration(math.MaxInt64)
		} else {
			drain = time.Duration(n) * time.Second
		}
	}
	pid, pidOK := lookup("LISTEN_PID")
	fds, fdsOK := lookup("LISTEN_FDS")
	count, err := strconv.ParseUint(fds, 10, 64)
	if !pidOK || pid != strconv.Itoa(p.Pid) || !fdsOK || !digits(fds) || (err == nil && count == 0) {
		return diagnostic("no socket was passed in\n\nrun it under systemd, with a listening socket passed in", ExitUsage)
	}
	if err != nil || count != 1 {
		return diagnostic(fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in", ExitUsage)
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	inherit := p.Inherit
	if inherit == nil {
		inherit = fileListener
	}
	ln, err := inherit(3)
	if err != nil {
		return diagnostic(err.Error(), ExitServerFailed)
	}
	servicesPath, _ := lookup("IKIGENBA_SERVICES")
	if address, ok := lookup("NOTIFY_SOCKET"); ok && address != "" {
		if err = notify(address); err != nil {
			_ = ln.Close()
			return diagnostic(err.Error(), ExitServerFailed)
		}
	}
	handler := gateway.Handler(gateway.Config{Banner: p.Banner, MCP: p.MCP, ServicesPath: servicesPath, Stderr: stderr})
	if err = server.Serve(ctx, ln, handler, drain); err != nil {
		return diagnostic(err.Error(), ExitServerFailed)
	}
	return ExitSuccess
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
func fileListener(fd uintptr) (net.Listener, error) {
	file := os.NewFile(fd, "inherited socket")
	ln, err := net.FileListener(file)
	_ = file.Close()
	return ln, err
}
func notify(address string) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("READY=1"))
	return err
}
