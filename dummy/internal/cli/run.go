package cli

import (
	"context"
	"fmt"
	"io"
	"net"
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

var (
	serve         = server.Serve
	serverHandler = server.Handler
)

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
		writeDiagnostic(p.Stderr, "dummy: unknown "+kind+" '"+arg+"'\n\nsee 'dummy --help' for usage\n")
		return ExitUsage
	}

	port, ok := lookupPort(p.LookupEnv)
	if !ok {
		if port == "" {
			writeDiagnostic(p.Stderr, "dummy: PORT is not set\n")
		} else {
			writeDiagnostic(p.Stderr, "dummy: PORT is '"+port+"', not a port number\n")
		}
		return ExitUsage
	}

	listen := p.Listen
	if listen == nil {
		listen = net.Listen
	}
	ln, err := listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		writeDiagnostic(p.Stderr, fmt.Sprintf("dummy: listen: %v\n", err))
		return ExitServerFailed
	}
	if p.Listening != nil {
		p.Listening(ln.Addr())
	}
	if err = serve(ctx, ln, serverHandler()); err != nil {
		writeDiagnostic(p.Stderr, fmt.Sprintf("dummy: serve: %v\n", err))
		return ExitServerFailed
	}
	return ExitSuccess
}

func lookupPort(lookupEnv func(string) (string, bool)) (string, bool) {
	if lookupEnv == nil {
		return "", false
	}
	port, ok := lookupEnv("PORT")
	if !ok || port == "" {
		return "", false
	}
	if !isPortNumber(port) {
		return port, false
	}
	return port, true
}

func isPortNumber(port string) bool {
	if len(port) < 1 || len(port) > 5 || port[0] == '0' {
		return false
	}
	number := 0
	for index := range len(port) {
		if port[index] < '0' || port[index] > '9' {
			return false
		}
		number = number*10 + int(port[index]-'0')
	}
	return number <= 65535
}

func writeDiagnostic(stderr io.Writer, message string) {
	_, _ = stderr.Write([]byte(message))
}
