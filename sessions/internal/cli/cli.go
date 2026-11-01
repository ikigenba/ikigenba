// Package cli implements the service's commands and process seam.
package cli

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/settings"
)

// Exit codes distinguish success, runtime failure and invalid invocation.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage is the service's help text.
const Usage = `Usage: sessions [--version|manifest|--help|db status]

Keeps the business's record of agent sessions.

With no arguments, serve on the inherited listening socket.
  --version  Print the release identity.
  manifest   Print the deployment manifest.
  --help     Print this help.
  db status  Report applied and pending database migrations.

Exit codes: 0 success; 1 server or database failure; 2 invalid usage or settings.
`

// Manifest is the deployment manifest printed by the manifest command.
const Manifest = "app = \"sessions\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/sessions.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n"

// NginxConf hides the suite's internal bus paths at the public host.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// Process carries the process boundary and runtime dependencies.
type Process struct {
	Args      []string
	LookupEnv func(key string) (string, bool)
	Unsetenv  func(key string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	Inherit   func(fd uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration)
	Rand      io.Reader
	Dir       string
	Sink      telemetry.Sink
	Banner    func(u page.User) page.Banner
	MCP       func(w *telemetry.Writer) *mcp.Server
}

// Run executes a command or serves until the context is cancelled.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) == 0 {
		s, err := settings.Read(p.LookupEnv)
		if err != nil {
			_, _ = p.Stderr.Write([]byte("sessions: " + err.Error() + "\n"))
			return ExitUsage
		}
		return serve(ctx, p, s)
	}
	if len(p.Args) == 1 {
		var product string
		switch p.Args[0] {
		case "--version":
			product = p.Version + "\n"
		case "manifest":
			product = Manifest
		case "--help":
			product = Usage
		default:
			return usageError(p, p.Args[0])
		}
		_, _ = io.WriteString(p.Stdout, product)
		return ExitSuccess
	}
	if p.Args[0] == "db" && p.Args[1] == "status" && len(p.Args) == 2 {
		err := db.Status(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "sessions.db"), Migrations: sessions.Migrations()}, p.Stdout)
		if err != nil {
			_, _ = p.Stderr.Write([]byte("sessions: " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"))
			return ExitServerFailed
		}
		return ExitSuccess
	}
	arg := p.Args[0]
	switch arg {
	case "db":
		arg = p.Args[1]
		if arg == "status" {
			arg = p.Args[2]
		}
	case "--version", "manifest", "--help":
		arg = p.Args[1]
	}
	return usageError(p, arg)
}

func usageError(p Process, arg string) int {
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = p.Stderr.Write([]byte("sessions: unknown " + kind + " '" + arg + "'\n\nsee 'sessions --help' for usage\n"))
	return ExitUsage
}
