package cli

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Process supplies the process state and runtime hooks used by Run.
type Process struct {
	Args               []string
	LookupEnv          func(string) (string, bool)
	Environ            func() []string
	Unsetenv           func(string) error
	Pid                int
	Stdout, Stderr     io.Writer
	Inherit            func(uintptr) (net.Listener, error)
	Now                func() time.Time
	Sleep              func(context.Context, time.Duration)
	After, ScriptAfter func(time.Duration) <-chan time.Time
	Rand               io.Reader
	Dir                string
	Sink               telemetry.Sink
	Banner             func(page.User) page.Banner
	MCP                func(*telemetry.Writer) *mcp.Server
}
