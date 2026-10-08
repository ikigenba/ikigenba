package cli_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/cron/internal/cli"
)

// R-BYCF-VSOL R-BZKC-9KFA
func TestProcessAndRunDeclarations(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := cli.Process{Args: []string{}, LookupEnv: func(key string) (string, bool) {
		return map[string]string{"LISTEN_PID": "41", "LISTEN_FDS": "1"}[key], key == "LISTEN_PID" || key == "LISTEN_FDS"
	}, Unsetenv: func(string) error { return nil }, Pid: 41, Stdout: &stdout, Stderr: &stderr, Version: "display", Inherit: func(fd uintptr) (net.Listener, error) {
		_ = fd
		return nil, errors.New("injected listener failure")
	}, Now: func() time.Time { return time.Date(2026, 10, 5, 9, 32, 0, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) {}, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: bytes.NewReader(nil), Dir: t.TempDir(), Sink: &telemetry.Capture{}, EventSink: &events.Capture{}, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { return nil }}
	_ = func(run func(context.Context, cli.Process) int) int {
		return run(context.Background(), p)
	}(cli.Run)
}
