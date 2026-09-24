// Package cli owns the run seam: Process and Run.
package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/server"
	"github.com/ikigenba/ikigenba/auth/internal/store"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

// Process carries the invocation and its external inputs.
type Process struct {
	Args       []string
	LookupEnv  func(key string) (string, bool)
	Unsetenv   func(key string) error
	Pid        int
	Stdout     io.Writer
	Stderr     io.Writer
	Inherit    func(fd uintptr) (net.Listener, error)
	Now        func() time.Time
	Rand       io.Reader
	OIDCIssuer string
	DBSource   string
}

const usageText = `Usage: auth [command]

Serve the auth service on the socket systemd passes in. With no command,
serve.

Commands:
  manifest   print the app manifest

Options:
  --help      print this help
  --version   print the version

Exit codes:
  0  success
  1  the server failed
  2  usage error
`

const manifestText = `app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
`

const socketHint = "\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3001 auth'\n"

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(b)
}

// Run executes one invocation and returns its exit status.
func Run(ctx context.Context, p Process) int {
	stderr := &lockedWriter{w: p.Stderr}
	if len(p.Args) != 0 {
		args := p.Args
		switch args[0] {
		case "--version", "--help", "manifest":
			if len(args) == 1 {
				switch args[0] {
				case "--version":
					_, _ = fmt.Fprintln(p.Stdout, version.Version)
				case "--help":
					_, _ = io.WriteString(p.Stdout, usageText)
				case "manifest":
					_, _ = io.WriteString(p.Stdout, manifestText)
				}
				return 0
			}
			args = args[1:]
		}
		kind := "unknown command"
		if strings.HasPrefix(args[0], "--") {
			kind = "unknown option"
		}
		_, _ = fmt.Fprintf(stderr, "auth: %s '%s'\n\nsee 'auth --help' for usage\n", kind, args[0])
		return 2
	}
	return serve(ctx, p, stderr)
}

func serve(ctx context.Context, p Process, stderr io.Writer) int {
	lookup := p.LookupEnv
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}
	settings := [3]string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "WORKSPACE_DOMAIN"}
	values := [3]string{}
	for i, key := range settings {
		v, ok := lookup(key)
		if !ok || v == "" {
			_, _ = fmt.Fprintf(stderr, "auth: %s is not set\n", key)
			return 2
		}
		values[i] = v
	}
	drain := 5 * time.Second
	if v, ok := lookup("DRAIN_SECONDS"); ok && v != "" {
		if !positiveDecimal(v) {
			_, _ = fmt.Fprintf(stderr, "auth: DRAIN_SECONDS is '%s', not a positive whole number of seconds\n", v)
			return 2
		}
		drain = drainDuration(v)
	}
	pid, pidOK := lookup("LISTEN_PID")
	fds, fdsOK := lookup("LISTEN_FDS")
	if !pidOK || pid != strconv.Itoa(p.Pid) || !fdsOK || !decimal(fds) || strings.TrimLeft(fds, "0") == "" {
		_, _ = io.WriteString(stderr, "auth: no socket was passed in"+socketHint)
		return 2
	}
	if strings.TrimLeft(fds, "0") != "1" {
		_, _ = fmt.Fprintf(stderr, "auth: %s sockets were passed in, expected 1%s", fds, socketHint)
		return 2
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	ln, err := inherit(p)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "auth: %s\n", err)
		return 1
	}
	defer func() { _ = ln.Close() }()
	st, err := store.Open(p.DBSource, p.Rand)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "auth: cannot open database %s: %s\n", p.DBSource, err)
		return 1
	}
	defer func() { _ = st.Close() }()
	client := google.NewClient(values[0], values[1], values[2], p.OIDCIssuer)
	h := server.New(server.Config{Store: st, Google: client, Now: p.Now, Rand: p.Rand, Stderr: stderr, WorkspaceDomain: values[2]})
	if addr, ok := lookup("NOTIFY_SOCKET"); ok && addr != "" {
		if err := notify(addr); err != nil {
			_, _ = fmt.Fprintf(stderr, "auth: %s\n", err)
			return 1
		}
	}
	if err := server.Serve(ctx, ln, h, drain); err != nil {
		_, _ = fmt.Fprintf(stderr, "auth: %s\n", err)
		return 1
	}
	return 0
}

func positiveDecimal(v string) bool {
	return len(v) > 0 && v[0] >= '1' && v[0] <= '9' && decimal(v)
}

func drainDuration(v string) time.Duration {
	const maxSeconds = int64(^uint64(0)>>1) / int64(time.Second)
	maxText := strconv.FormatInt(maxSeconds, 10)
	if len(v) > len(maxText) || len(v) == len(maxText) && v > maxText {
		return time.Duration(1<<63 - 1)
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return time.Duration(n) * time.Second
}

func decimal(v string) bool {
	if v == "" {
		return false
	}
	for i := range len(v) {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

func inherit(p Process) (net.Listener, error) {
	if p.Inherit != nil {
		return p.Inherit(3)
	}
	file := os.NewFile(3, "systemd socket")
	if file == nil {
		return nil, fmt.Errorf("file descriptor 3 is unavailable")
	}
	ln, err := net.FileListener(file)
	_ = file.Close()
	if unix, ok := ln.(*net.UnixListener); ok {
		unix.SetUnlinkOnClose(false)
	}
	return ln, err
}

func notify(addr string) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("READY=1"))
	return err
}
