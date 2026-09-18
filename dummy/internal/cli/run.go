package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

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

// Usage describes the dummy command line.
const Usage = "Usage: dummy [command]\n\nServe the Dummy page at 127.0.0.1:$PORT. With no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"

// Run runs the dummy command and returns its process exit code.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
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
		if arg == "--version" || arg == "manifest" || arg == "--help" {
			arg = p.Args[1]
		}
		kind := "command"
		if strings.HasPrefix(arg, "-") {
			kind = "option"
		}
		_, _ = io.WriteString(p.Stderr, "dummy: unknown "+kind+" '"+arg+"'\n\nsee 'dummy --help' for usage\n")
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
