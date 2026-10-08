package cli_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/cli"
)

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Fatal("unexpected random read")
	return 0, io.EOF
}

type forbiddenSink struct{ t *testing.T }

func (s forbiddenSink) Deliver(context.Context, telemetry.Event) error {
	s.t.Fatal("unexpected sink delivery")
	return nil
}
func guardedProcess(t *testing.T) cli.Process {
	t.Helper()
	timer := func(time.Duration) <-chan time.Time { t.Fatal("unexpected timer"); return nil }
	return cli.Process{Version: "seam-display", LookupEnv: func(string) (string, bool) { t.Fatal("unexpected environment read"); return "", false }, Unsetenv: func(string) error { t.Fatal("unexpected environment unset"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("unexpected inherited listener"); return nil, nil }, Now: func() time.Time { t.Fatal("unexpected clock"); return time.Time{} }, Sleep: func(context.Context, time.Duration) { t.Fatal("unexpected pause") }, Rand: forbiddenReader{t}, Sink: forbiddenSink{t}, SweepAfter: timer, RefreshAfter: timer, AskAfter: timer, TimeoutAfter: timer, BackoffAfter: timer}
}
