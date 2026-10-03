// Package cli implements telemetry's command and injected process lifecycle.
package cli

import (
	"context"
	"io"
	"net"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Version is the deployed release identifier.
var Version = "v0.1.1"

// Manifest describes the application's host configuration.
const Manifest = "app = \"telemetry\"\ndescription = \"The suite's trail of events\"\ndefault = false\nmcp = true\nsecrets = []\n\n[env]\nRETENTION_DAYS = \"15\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/telemetry.db\"\n"

// NginxConf prevents public access to the sibling ingest endpoint.
const NginxConf = "location = /ingest { return 404; }\n"

// Usage is the command's complete help product.
const Usage = "Usage: telemetry [command]\n\nServe the suite's trail of events: ingest at /ingest, MCP tools at /mcp, and\na landing page at /, on the socket systemd passes in. With no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"

// Exit codes distinguish success, runtime failures, and invalid invocations.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Process supplies the process inputs and constructors that Run uses.
type Process struct {
	Args      []string
	LookupEnv func(string) (string, bool)
	Unsetenv  func(string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Inherit   func(uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration)
	Rand      io.Reader
	DBSource  string
	Banner    func(page.User) page.Banner
	MCP       func(*telemetry.Writer) *mcp.Server
}

// Run handles one invocation and returns its exit status.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) == 0 {
		return serve(ctx, p)
	}
	arg := p.Args[0]
	recognized := arg == "--version" || arg == "manifest" || arg == "--help"
	if recognized && len(p.Args) == 1 {
		product := Usage
		switch arg {
		case "--version":
			product = Version + "\n"
		case "manifest":
			product = Manifest
		}
		if p.Stdout != nil {
			_, _ = io.WriteString(p.Stdout, product)
		}
		return ExitSuccess
	}
	if recognized {
		arg = p.Args[1]
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	diagnostic(p.Stderr, "unknown "+kind+" '"+arg+"'\n\nsee 'telemetry --help' for usage\n")
	return ExitUsage
}

func diagnostic(w io.Writer, text string) {
	if w != nil {
		_, _ = w.Write([]byte("telemetry: " + text))
	}
}
