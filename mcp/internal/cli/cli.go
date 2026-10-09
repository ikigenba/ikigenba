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
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
	"github.com/ikigenba/ikigenba/mcp/internal/server"
)

// Manifest is the platform application declaration.
const Manifest = "app = \"mcp\"\ndescription = \"" + gateway.Description + "\"\ndefault = false\nmcp = false\nguests = true\nsecrets = []\n\n[resources]\nmemory_max = \"128M\"\n"

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
	Version   string
	Inherit   func(fd uintptr) (net.Listener, error)
	Banner    func(u page.User) page.Banner
	MCP       func(*telemetry.Writer) *appkitmcp.Server
	Sink      telemetry.Sink
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
			product = p.Version + "\n"
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
	sink := p.Sink
	if sink == nil {
		sink = telemetry.NewSocketSink()
	}
	gate := &drainSink{sink: sink}
	writer := telemetry.New(telemetry.Config{Service: gateway.ServiceName, Version: p.Version, Sink: gate, Stderr: stderr})
	handler := gateway.Handler(gateway.Config{Banner: p.Banner, MCP: p.MCP(writer), ServicesPath: servicesPath, Telemetry: writer})
	writer.Ready()
	serving, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	var failure error
	var stopMu sync.Mutex
	stop := func(err error, failed bool) {
		stopMu.Lock()
		defer stopMu.Unlock()
		if serving.Err() != nil {
			return
		}
		if failed {
			failure = err
		}
		gate.stop(drain)
		cancel(err)
	}
	stopped := context.AfterFunc(ctx, func() { stop(context.Cause(ctx), false) })
	defer stopped()
	if ctx.Err() != nil {
		stop(context.Cause(ctx), false)
	}
	listener := &runListener{Listener: ln, ctx: ctx, stop: stop}
	err = server.Serve(serving, listener, handler, drain)
	stopMu.Lock()
	acceptErr := failure
	stopMu.Unlock()
	reason := "failed"
	if acceptErr == nil && context.Cause(ctx) != nil {
		reason = context.Cause(ctx).Error()
	}
	shutdown, done := context.WithDeadline(context.Background(), gate.deadline())
	writer.Shutdown(shutdown, reason)
	done()
	if acceptErr != nil {
		stderr.final([]byte("mcp: " + acceptErr.Error() + "\n"))
		return ExitServerFailed
	}
	if err != nil {
		stderr.final([]byte("mcp: " + err.Error() + "\n"))
		return ExitServerFailed
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

// drainSink refuses to begin delivery after the shared drain deadline.
type drainSink struct {
	mu    sync.Mutex
	sink  telemetry.Sink
	until time.Time
}

func (s *drainSink) stop(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.until.IsZero() {
		s.until = time.Now().Add(d)
	}
}
func (s *drainSink) deadline() time.Time { s.mu.Lock(); defer s.mu.Unlock(); return s.until }
func (s *drainSink) Deliver(ctx context.Context, e telemetry.Event) error {
	s.mu.Lock()
	until := s.until
	if !until.IsZero() && !time.Now().Before(until) {
		s.mu.Unlock()
		return telemetry.ErrRejected
	}
	s.mu.Unlock()
	return s.sink.Deliver(ctx, e)
}

type runListener struct {
	net.Listener
	ctx  context.Context
	stop func(error, bool)
}

func (l *runListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err == nil {
			return conn, nil
		}
		if l.ctx.Err() != nil {
			return nil, err
		}
		if temporaryAccept(err) {
			continue
		}
		l.stop(err, true)
		return nil, err
	}
}
func (w *runWriter) final(b []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed && w.dst != nil {
		_, _ = w.dst.Write(b)
	}
	w.closed = true
}

func temporaryAccept(err error) bool {
	if temporary, ok := err.(interface{ Temporary() bool }); ok && temporary.Temporary() {
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return temporaryAccept(wrapped.Unwrap())
	}
	if wrapped, ok := err.(interface{ Unwrap() []error }); ok {
		for _, nested := range wrapped.Unwrap() {
			if temporaryAccept(nested) {
				return true
			}
		}
	}

	return false
}
