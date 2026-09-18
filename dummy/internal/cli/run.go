package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/ikigenba/ikigenba/dummy/internal/server"
)

// Process describes the process state used by Run.
type Process struct {
	Args      []string
	LookupEnv func(key string) (string, bool)
	Stdout    io.Writer
	Stderr    io.Writer
	Listen    func(network, address string) (net.Listener, error)
	Listening func(addr net.Addr)
}

// Process exit codes.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Run runs the dummy command and returns its process exit code.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) == 1 && p.Args[0] == "--version" {
		_, _ = fmt.Fprintln(p.Stdout, Version)
		return ExitSuccess
	}
	if len(p.Args) != 0 {
		_, _ = fmt.Fprintln(p.Stderr, "usage: dummy [--version]")
		return ExitUsage
	}

	port, ok := lookupPort(p.LookupEnv)
	if !ok {
		_, _ = fmt.Fprintln(p.Stderr, "PORT must be an integer from 1 through 65535")
		return ExitUsage
	}

	listen := p.Listen
	if listen == nil {
		listen = net.Listen
	}
	ln, err := listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		_, _ = fmt.Fprintf(p.Stderr, "listen: %v\n", err)
		return ExitServerFailed
	}
	if p.Listening != nil {
		p.Listening(ln.Addr())
	}
	if err = server.Serve(ctx, ln, nil); err != nil {
		_, _ = fmt.Fprintf(p.Stderr, "serve: %v\n", err)
		return ExitServerFailed
	}
	return ExitSuccess
}

func lookupPort(lookupEnv func(string) (string, bool)) (string, bool) {
	if lookupEnv == nil {
		return "", false
	}
	port, ok := lookupEnv("PORT")
	if !ok {
		return "", false
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", false
	}
	return port, true
}
