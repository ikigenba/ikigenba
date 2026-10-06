package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
)

type forbiddenEffects struct{}
type forbiddenBus struct{}

func (forbiddenBus) Deliver(context.Context, events.Event) error {
	panic("command delivered bus event")
}

func (forbiddenEffects) Read([]byte) (int, error) {
	panic("command read randomness")
}

func (forbiddenEffects) Deliver(context.Context, telemetry.Event) error {
	panic("command delivered an event")
}

func untouchedProcess(args []string, out, diagnostic io.Writer, dir string) cli.Process {
	return cli.Process{
		Args: args,
		LookupEnv: func(string) (string, bool) {
			panic("command looked up environment")
		},
		Environ: func() []string { panic("command read environment") },
		Unsetenv: func(string) error {
			panic("command unset environment")
		},
		Pid:    123,
		Stdout: out, Stderr: diagnostic,
		Inherit: func(uintptr) (net.Listener, error) {
			panic("command inherited a descriptor")
		},
		Now: func() time.Time { panic("command read clock") },
		Sleep: func(context.Context, time.Duration) {
			panic("command paused")
		},
		After: func(time.Duration) <-chan time.Time {
			panic("command created a timer")
		},
		Rand: forbiddenEffects{}, Dir: dir, Sink: forbiddenEffects{}, EventSink: forbiddenBus{},
		Banner: func(page.User) page.Banner {
			panic("command called banner source")
		},
		MCP: func(*telemetry.Writer) *mcp.Server {
			panic("command created MCP server")
		},
	}
}

// R-D2AA-CMX9 R-DC1H-ESUT
func TestProcessContractAndUntouchedCommandSeams(t *testing.T) {
	api := struct {
		Run func(context.Context, cli.Process) int
	}{Run: cli.Run}
	var out bytes.Buffer
	var diagnostic recordedWrites
	p := untouchedProcess([]string{"--help"}, &out, &diagnostic, t.TempDir())
	if code := api.Run(context.Background(), p); code != cli.ExitSuccess || out.String() != cli.Usage || diagnostic.calls != 0 {
		t.Fatalf("Run = %d, stdout %q, stderr %q", code, out.String(), diagnostic.String())
	}
	assertEmptyDirectory(t, p.Dir)
}

// R-SCSM-XPUS R-Y2L6-GBNK
func TestEmptyArgumentsDispatchToServeAndExitRange(t *testing.T) {
	for _, args := range [][]string{nil, {}} {
		for _, failInherit := range []bool{false, true} {
			var out bytes.Buffer
			var diagnostic recordedWrites
			p := untouchedProcess(args, &out, &diagnostic, t.TempDir())
			p.LookupEnv = func(key string) (string, bool) {
				if failInherit {
					switch key {
					case "LISTEN_PID":
						return "123", true
					case "LISTEN_FDS":
						return "1", true
					}
				}
				return "", false
			}
			p.Unsetenv = nil
			p.Inherit = func(uintptr) (net.Listener, error) {
				return nil, errors.New("cannot inherit test socket")
			}
			code := cli.Run(context.Background(), p)
			want := cli.ExitUsage
			if failInherit {
				want = cli.ExitServerFailed
			}
			if code != want || out.Len() != 0 || diagnostic.calls != 1 || !bytes.HasPrefix(diagnostic.Bytes(), []byte("repos: ")) {
				t.Fatalf("Run(empty, failInherit=%t) = %d, stdout %q, stderr %q in %d writes", failInherit, code, out.String(), diagnostic.String(), diagnostic.calls)
			}
			assertEmptyDirectory(t, p.Dir)
		}
	}
}
