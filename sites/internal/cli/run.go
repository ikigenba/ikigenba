package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"

	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites"
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
	Version   string
	Inherit   func(fd uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration)
	After     func(d time.Duration) <-chan time.Time
	Rand      io.Reader
	Dir       string
	Sink      telemetry.Sink
	Banner    func(u page.User) page.Banner
	MCP       func(w *telemetry.Writer) *mcp.Server
}

// Run executes a command or starts the socket-activated service.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) != 0 {
		first := p.Args[0]
		known := first == "--version" || first == "manifest" || first == "--help" || first == "db"
		if first == "db" && len(p.Args) == 2 && p.Args[1] == "status" {
			err := db.Status(context.WithoutCancel(ctx), db.Config{Path: filepath.Join(p.Dir, "state", "sites.db"), Migrations: sites.Migrations()}, p.Stdout)
			if err != nil {
				_, _ = p.Stderr.Write([]byte("sites: " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"))
				return ExitServerFailed
			}
			return ExitSuccess
		}
		if len(p.Args) == 1 && known && first != "db" {
			product := Usage
			if first == "--version" {
				product = p.Version + "\n"
			}
			if first == "manifest" {
				product = Manifest
			}
			_, _ = io.WriteString(p.Stdout, product)
			return ExitSuccess
		}
		arg := first
		if known && len(p.Args) > 1 {
			arg = p.Args[1]
			if first == "db" && arg == "status" {
				arg = p.Args[2]
			}
		}
		kind := "command"
		if strings.HasPrefix(arg, "-") {
			kind = "option"
		}
		_, _ = fmt.Fprintf(p.Stderr, "sites: unknown %s '%s'\n\nsee 'sites --help' for usage\n", kind, arg)
		return ExitUsage
	}
	return runServe(ctx, p)
}
