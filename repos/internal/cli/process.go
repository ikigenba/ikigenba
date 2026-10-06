// Package cli implements repos' command and process seams.
package cli

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Process supplies the arguments, resources and effects of a repos run.
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
	Sink      telemetry.Sink
	EventSink events.Sink
	Banner    func(u page.User) page.Banner
	MCP       func(w *telemetry.Writer) *mcp.Server
}
