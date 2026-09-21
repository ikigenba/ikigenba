// Package cli owns the run seam: Process and Run.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/server"
	"github.com/ikigenba/ikigenba/auth/internal/store"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

// Process carries every input Run would otherwise read from the ambient world.
type Process struct {
	Args       []string
	Getenv     func(string) string
	Stdout     io.Writer
	Stderr     io.Writer
	Now        func() time.Time
	Rand       io.Reader
	OIDCIssuer string
	DBSource   string
}

const usageText = `Usage: auth [command]

Serve the auth service at 127.0.0.1:$PORT. With no command, serve.

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
port = 3001
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
`

// Run executes one invocation and returns the process exit status.
func Run(p Process) int {
	args := p.Args
	if len(args) > 0 {
		switch args[0] {
		case "--version":
			if len(args) != 1 {
				return usageError(p, "unknown command", args[1])
			}
			_, _ = fmt.Fprintln(p.Stdout, version.Version)
			return 0
		case "--help":
			if len(args) != 1 {
				return usageError(p, "unknown command", args[1])
			}
			_, _ = io.WriteString(p.Stdout, usageText)
			return 0
		case "manifest":
			if len(args) != 1 {
				return usageError(p, "unknown command", args[1])
			}
			_, _ = io.WriteString(p.Stdout, manifestText)
			return 0
		default:
			if strings.HasPrefix(args[0], "--") {
				return usageError(p, "unknown option", args[0])
			}
			return usageError(p, "unknown command", args[0])
		}
	}
	return serve(p)
}

func usageError(p Process, kind, value string) int {
	_, _ = fmt.Fprintf(p.Stderr, "auth: %s '%s'\n\nsee 'auth --help' for usage\n", kind, value)
	return 2
}

func serve(p Process) int {
	getenv := p.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	port, code, ok := readConfig(p, getenv)
	if !ok {
		return code
	}
	st, err := store.Open(p.DBSource, p.Rand)
	if err != nil {
		_, _ = fmt.Fprintf(p.Stderr, "auth: cannot open database %s: %s\n", p.DBSource, err.Error())
		return 1
	}
	defer func() { _ = st.Close() }()

	client := google.NewClient(
		getenv("GOOGLE_CLIENT_ID"),
		getenv("GOOGLE_CLIENT_SECRET"),
		getenv("WORKSPACE_DOMAIN"),
		p.OIDCIssuer,
	)
	srv := server.New(server.Config{
		Store:           st,
		Google:          client,
		Now:             p.Now,
		Rand:            p.Rand,
		Stderr:          p.Stderr,
		WorkspaceDomain: getenv("WORKSPACE_DOMAIN"),
	})

	addr := net.JoinHostPort("127.0.0.1", port)
	errc := make(chan error, 1)
	go func() {
		errc <- srv.Serve(addr)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case err := <-errc:
		if err == nil {
			return 0
		}
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "listen" && isAddrInUse(err) {
			_, _ = fmt.Fprintf(p.Stderr, "auth: listen tcp %s: bind: address already in use\n", addr)
			return 1
		}
		_, _ = fmt.Fprintf(p.Stderr, "auth: %s\n", err.Error())
		return 1
	case <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-errc
		return 0
	}
}

func readConfig(p Process, getenv func(string) string) (string, int, bool) {
	port := getenv("PORT")
	if port == "" {
		_, _ = io.WriteString(p.Stderr, "auth: PORT is not set\n")
		return "", 2, false
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		_, _ = fmt.Fprintf(p.Stderr, "auth: PORT is '%s', not a port number\n", port)
		return "", 2, false
	}
	for _, name := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "WORKSPACE_DOMAIN"} {
		if getenv(name) == "" {
			_, _ = fmt.Fprintf(p.Stderr, "auth: %s is not set\n", name)
			return "", 2, false
		}
	}
	return port, 0, true
}

func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
