package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
)

// Process carries all process inputs used by Run.
type Process struct {
	Args      []string
	LookupEnv func(key string) (string, bool)
	Environ   func() []string
	Unsetenv  func(key string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Inherit   func(fd uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration)
	After     func(d time.Duration) <-chan time.Time
	Rand      io.Reader
	Dir       string
	Database  string
	Sink      telemetry.Sink
	Banner    func(u page.User) page.Banner
	MCP       func(w *telemetry.Writer) *mcp.Server
}

// Run executes a command or starts the socket-activated service.
func Run(_ context.Context, p Process) int {
	if len(p.Args) != 0 {
		first := p.Args[0]
		known := first == "--version" || first == "manifest" || first == "--help"
		if len(p.Args) == 1 && known {
			product := Usage
			if first == "--version" {
				product = Version + "\n"
			}
			if first == "manifest" {
				product = Manifest
			}
			_, _ = io.WriteString(p.Stdout, product)
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
		_, _ = fmt.Fprintf(p.Stderr, "sites: unknown %s '%s'\n\nsee 'sites --help' for usage\n", kind, arg)
		return ExitUsage
	}
	if _, err := settings.Read(p.LookupEnv); err != nil {
		_, _ = fmt.Fprintf(p.Stderr, "sites: %s\n", err)
		return ExitUsage
	}
	// The serving composition is supplied by the integration phase.
	return ExitServerFailed
}
