package cli

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/server"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

type lockedOutput struct {
	mu     sync.Mutex
	writer io.Writer
	closed bool
}

func (w *lockedOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.writer == nil {
		return len(b), nil
	}
	return w.writer.Write(b)
}
func (w *lockedOutput) close() { w.mu.Lock(); defer w.mu.Unlock(); w.closed = true }

func duration(value string, fallback, unit time.Duration) (time.Duration, bool) {
	if value == "" {
		return fallback, true
	}
	if value[0] == '0' {
		return 0, false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n > math.MaxInt64/int64(unit) {
		return time.Duration(math.MaxInt64), true
	}
	return time.Duration(n) * unit, true
}
func pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
func inherit(fd uintptr) (net.Listener, error) {
	f := os.NewFile(fd, "inherited socket")
	if f == nil {
		return nil, errors.New("invalid inherited descriptor")
	}
	defer func() { _ = f.Close() }()
	return net.FileListener(f)
}
func notify(address string) error {
	if address == "" {
		return nil
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("READY=1"))
	return err
}

func serve(ctx context.Context, p Process) int {
	out := &lockedOutput{writer: p.Stderr}
	defer out.close()
	lookup := p.LookupEnv
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}
	get := func(key string) string {
		value, ok := lookup(key)
		if !ok {
			return ""
		}
		return value
	}
	drainValue := get("DRAIN_SECONDS")
	drain, ok := duration(drainValue, 5*time.Second, time.Second)
	if !ok {
		diagnostic(out, "DRAIN_SECONDS is '"+drainValue+"', not a positive whole number of seconds\n")
		return ExitUsage
	}
	retentionValue := get("RETENTION_DAYS")
	retention, ok := duration(retentionValue, 15*24*time.Hour, 24*time.Hour)
	if !ok {
		diagnostic(out, "RETENTION_DAYS is '"+retentionValue+"', not a positive whole number of days\n")
		return ExitUsage
	}
	pid := get("LISTEN_PID")
	fds := get("LISTEN_FDS")
	digits := fds != ""
	for _, c := range fds {
		if c < '0' || c > '9' {
			digits = false
		}
	}
	count := strings.TrimLeft(fds, "0")
	if pid != strconv.Itoa(p.Pid) || !digits || count == "" {
		diagnostic(out, "no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n")
		return ExitUsage
	}
	if count != "1" {
		diagnostic(out, fds+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n")
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	take := p.Inherit
	if take == nil {
		take = inherit
	}
	ln, err := take(3)
	if err != nil {
		diagnostic(out, err.Error()+"\n")
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	db, err := store.Open(p.DBSource)
	if err != nil {
		diagnostic(out, "cannot open database "+p.DBSource+": "+err.Error()+"\n")
		return ExitServerFailed
	}
	defer func() { _ = db.Close() }()
	now := p.Now
	if now == nil {
		now = time.Now
	}
	sleep := p.Sleep
	if sleep == nil {
		sleep = pause
	}
	_ = db.Sweep(ctx, now().Add(-retention))
	if err = notify(get("NOTIFY_SOCKET")); err != nil {
		diagnostic(out, err.Error()+"\n")
		return ExitServerFailed
	}
	writer := telemetry.New(telemetry.Config{Service: web.ServiceName, Version: Version, Sink: db, Stderr: out, Now: now, Sleep: sleep, Rand: p.Rand})
	writer.Ready()
	_ = writer.Flush(ctx)
	srv := p.MCP(writer)
	handler := web.Handler(web.Config{Banner: p.Banner, MCP: srv, ServicesPath: get("IKIGENBA_SERVICES"), Store: db, Telemetry: writer})
	sweepCtx, cancelSweep := context.WithCancel(ctx)
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		for sweepCtx.Err() == nil {
			sleep(sweepCtx, time.Hour)
			if sweepCtx.Err() != nil {
				return
			}
			_ = db.Sweep(sweepCtx, now().Add(-retention))
		}
	}()
	err = server.Serve(ctx, ln, handler, drain, func(stopCtx context.Context) { writer.Shutdown(stopCtx, context.Cause(ctx).Error()) })
	cancelSweep()
	<-sweepDone
	if err != nil {
		// An accept failure has no stop callback; terminate the writer before reporting it.
		if ctx.Err() == nil {
			ended, cancel := context.WithCancel(context.Background())
			cancel()
			writer.Shutdown(ended, err.Error())
		}
		diagnostic(out, err.Error()+"\n")
		return ExitServerFailed
	}
	return ExitSuccess
}
