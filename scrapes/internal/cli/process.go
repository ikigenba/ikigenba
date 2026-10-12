// Package cli runs scrapes through an injected process boundary.
package cli

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Process supplies the process boundary, dependencies, and deterministic sources.
type Process struct {
	Args      []string
	LookupEnv func(string) (string, bool)
	Unsetenv  func(string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	Inherit   func(uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration)
	After     func(time.Duration) <-chan time.Time
	Rand      io.Reader
	Dir       string
	Sink      telemetry.Sink
	EventSink events.Sink
	Banner    func(page.User) page.Banner
	MCP       func(*telemetry.Writer) *mcp.Server
	HTTP      *http.Client
}

type lockedReader struct {
	mu     sync.Mutex
	reader io.Reader
}

func (r *lockedReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reader.Read(p)
}
