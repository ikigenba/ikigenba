package trail_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func writer(t *testing.T, sink telemetry.Sink) *telemetry.Writer {
	t.Helper()
	w := telemetry.New(telemetry.Config{Service: "accounts", Sink: sink, Stderr: io.Discard,
		Now: func() time.Time { return time.Unix(123, 0) }, Rand: rand.NewChaCha8([32]byte{1}),
	})
	t.Cleanup(func() { w.Shutdown(context.Background(), "done") })
	return w
}

// R-9NTT-PE98 R-9P1Q-35ZX R-E3OL-H7GN R-E64E-8QY1 R-0XAC-XLB8 R-1RN7-D10W
func TestWireConstants(t *testing.T) {
	const (
		_ string = trail.EventChangeFinished
		_ string = trail.ResultOK + trail.ResultRefused + trail.ResultFailed
		_ string = trail.EventOperationStarted + trail.EventOperationFinished + trail.EventHostChanged + trail.EventAccountChanged
		_ string = trail.EventHostSilent + trail.EventHostSynced + trail.EventHostApplyFailed + trail.EventResourceUnexplained
		_ string = trail.AccountRemoved + trail.ResourceInstance + trail.ResourceVolume + trail.ResourceAddress
		_ string = trail.EventInviteCreated + trail.EventInviteRevoked + trail.EventInviteUsed + trail.EventAdminChanged
		_ string = trail.EventVolumeChanged
	)
	got := []string{
		trail.EventChangeFinished, trail.ResultOK, trail.ResultRefused, trail.ResultFailed,
		trail.EventOperationStarted, trail.EventOperationFinished, trail.EventHostChanged, trail.EventAccountChanged,
		trail.EventHostSilent, trail.EventHostSynced, trail.EventHostApplyFailed, trail.EventResourceUnexplained,
		trail.AccountRemoved, trail.ResourceInstance, trail.ResourceVolume, trail.ResourceAddress,
		trail.EventInviteCreated, trail.EventInviteRevoked, trail.EventInviteUsed, trail.EventAdminChanged,
		trail.EventVolumeChanged,
	}
	want := []string{
		"change.finished", "ok", "refused", "failed",
		"operation.started", "operation.finished", "host.changed", "account.changed",
		"host.silent", "host.synced", "host.apply_failed", "resource.unexplained",
		"removed", "instance", "volume", "address",
		"invite.created", "invite.revoked", "invite.used", "admin.changed",
		"volume.changed",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire constants %v, want %v", got, want)
	}
}

// R-E7CA-MIOQ R-EENO-X54W
func TestResult(t *testing.T) {
	refusal, unrelated := errors.New("refusal"), errors.New("unrelated")
	cases := []struct {
		err      error
		refusals []error
		want     string
	}{
		{nil, []error{refusal, nil}, trail.ResultOK},
		{fmt.Errorf("wrapper: %w", refusal), []error{unrelated, refusal}, trail.ResultRefused},
		{unrelated, []error{refusal}, trail.ResultFailed},
		{refusal, nil, trail.ResultFailed},
	}
	for _, c := range cases {
		if got := trail.Result(c.err, c.refusals...); got != c.want {
			t.Errorf("Result(%v, %v) = %q, want %q", c.err, c.refusals, got, c.want)
		}
	}
}

type notice struct {
	name string
	attr telemetry.Attrs
	send func(context.Context, *telemetry.Writer)
}

func notices(tool, host, account, user, ip, result, op, kind, from, to, status, reason, resource, data string, n int64) []notice {
	return []notice{
		{trail.EventChangeFinished, telemetry.Attrs{"tool": tool, "host": host, "account": account, "user": user, "ip": ip, "result": result},
			func(ctx context.Context, w *telemetry.Writer) {
				trail.Change(ctx, w, tool, host, account, user, ip, result)
			}},
		{trail.EventOperationStarted, telemetry.Attrs{"operation": op, "kind": kind, "host": host, "account": account, "data": data},
			func(ctx context.Context, w *telemetry.Writer) {
				trail.OperationStarted(ctx, w, op, kind, host, account, data)
			}},
		{trail.EventOperationFinished, telemetry.Attrs{"operation": op, "kind": kind, "host": host, "account": account, "status": status, "reason": reason, "data": data},
			func(ctx context.Context, w *telemetry.Writer) {
				trail.OperationFinished(ctx, w, op, kind, host, account, status, reason, data)
			}},
		{trail.EventHostChanged, telemetry.Attrs{"host": host, "from": from, "to": to, "operation": op},
			func(ctx context.Context, w *telemetry.Writer) { trail.HostChanged(ctx, w, host, from, to, op) }},
		{trail.EventAccountChanged, telemetry.Attrs{"account": account, "host": host, "from": from, "to": to},
			func(ctx context.Context, w *telemetry.Writer) { trail.AccountChanged(ctx, w, account, host, from, to) }},
		{trail.EventHostSilent, telemetry.Attrs{"host": host, "applied": n},
			func(ctx context.Context, w *telemetry.Writer) { trail.HostSilent(ctx, w, host, n) }},
		{trail.EventHostSynced, telemetry.Attrs{"host": host, "applied": n},
			func(ctx context.Context, w *telemetry.Writer) { trail.HostSynced(ctx, w, host, n) }},
		{trail.EventHostApplyFailed, telemetry.Attrs{"host": host, "attempted": n},
			func(ctx context.Context, w *telemetry.Writer) { trail.HostApplyFailed(ctx, w, host, n) }},
		{trail.EventResourceUnexplained, telemetry.Attrs{"resource": resource, "kind": kind, "host": host},
			func(ctx context.Context, w *telemetry.Writer) { trail.Unexplained(ctx, w, resource, kind, host) }},
		{trail.EventInviteCreated, telemetry.Attrs{"invite": op, "account": account, "host": host, "admin": n > 0},
			func(ctx context.Context, w *telemetry.Writer) { trail.InviteCreated(ctx, w, op, account, host, n > 0) }},
		{trail.EventInviteRevoked, telemetry.Attrs{"invite": op, "account": account, "host": host},
			func(ctx context.Context, w *telemetry.Writer) { trail.InviteRevoked(ctx, w, op, account, host) }},
		{trail.EventInviteUsed, telemetry.Attrs{"invite": op, "account": account, "host": host, "user": user, "admin": n > 0},
			func(ctx context.Context, w *telemetry.Writer) {
				trail.InviteUsed(ctx, w, op, account, host, user, n > 0)
			}},
		{trail.EventAdminChanged, telemetry.Attrs{"account": account, "host": host, "user": user, "admin": n > 0},
			func(ctx context.Context, w *telemetry.Writer) { trail.AdminChanged(ctx, w, account, host, user, n > 0) }},
		{trail.EventVolumeChanged, telemetry.Attrs{"volume": resource, "host": host, "from": from, "to": to, "reason": reason},
			func(ctx context.Context, w *telemetry.Writer) {
				trail.VolumeChanged(ctx, w, resource, host, from, to, reason)
			}},
	}
}

func example(n int64) []notice {
	return notices("allow_ip", "host01", "acme", "daffy", "192.0.2.0/24", trail.ResultOK, "op_1", "delete", "deleting", "kept", "done", "reason", "vol-1", "keep", n)
}

func checkEvent(t *testing.T, e telemetry.Event, want notice, c identity.Caller) {
	t.Helper()
	if e.Name != want.name || !reflect.DeepEqual(e.Attrs, want.attr) || e.User != c.UserID || e.RequestID != c.RequestID {
		t.Errorf("event %+v, want %s %v under caller %q and request %q", e, want.name, want.attr, c.UserID, c.RequestID)
	}
}

// R-I7QS-EGN0 R-1HW0-AV3C R-E9S3-E264 R-0YI9-BD1X R-1SV3-QSRL
var (
	_ func(context.Context, *telemetry.Writer, string, string, string, string, string, string)         = trail.Change
	_ func(context.Context, *telemetry.Writer, string, string, string, string, string)                 = trail.OperationStarted
	_ func(context.Context, *telemetry.Writer, string, string, string, string, string, string, string) = trail.OperationFinished
	_ func(context.Context, *telemetry.Writer, string, string, string, string)                         = trail.HostChanged
	_ func(context.Context, *telemetry.Writer, string, string, string, string)                         = trail.AccountChanged
	_ func(context.Context, *telemetry.Writer, string, int64)                                          = trail.HostSilent
	_ func(context.Context, *telemetry.Writer, string, int64)                                          = trail.HostSynced
	_ func(context.Context, *telemetry.Writer, string, int64)                                          = trail.HostApplyFailed
	_ func(context.Context, *telemetry.Writer, string, string, string)                                 = trail.Unexplained
	_ func(context.Context, *telemetry.Writer, string, string, string, bool)                           = trail.InviteCreated
	_ func(context.Context, *telemetry.Writer, string, string, string)                                 = trail.InviteRevoked
	_ func(context.Context, *telemetry.Writer, string, string, string, string, bool)                   = trail.InviteUsed
	_ func(context.Context, *telemetry.Writer, string, string, string, bool)                           = trail.AdminChanged
	_ func(context.Context, *telemetry.Writer, string, string, string, string, string)                 = trail.VolumeChanged
)

// R-1J3W-OMU1 R-1KBT-2EKQ R-0ZQ5-P4SM R-1NZI-7PST
func TestEachEvent(t *testing.T) {
	caller := identity.Caller{UserID: "caller", RequestID: "request", Email: "private@example.test"}
	for _, c := range []identity.Caller{{}, {RequestID: "anonymous-request"}, caller} {
		for _, cancelled := range []bool{false, true} {
			ctx := context.Background()
			if c.RequestID != "" {
				ctx = identity.NewContext(ctx, c)
			}
			ctx, cancel := context.WithCancel(ctx)
			if cancelled {
				cancel()
			}
			capture := &telemetry.Capture{}
			w := writer(t, capture)
			want := append(example(7), example(-2)...)
			want = append(want, notices("", "", "", "", "", "", "", "", "", "", "", "", "", "", 0)...)
			for _, n := range want {
				n.send(ctx, w)
			}
			cancel()
			if err := w.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := capture.Events()
			if len(got) != len(want) {
				t.Fatalf("captured %d events, want %d", len(got), len(want))
			}
			for i, e := range got {
				checkEvent(t, e, want[i], c)
			}
		}
	}
}

type blockedSink struct {
	telemetry.Capture
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedSink) Deliver(ctx context.Context, ev telemetry.Event) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.Capture.Deliver(ctx, ev)
}

// R-9V58-00PE R-9WD4-DSG3 R-1LJP-G6BF R-1NZI-7PST
func TestNoticeQueuesWithoutDelivery(t *testing.T) {
	const primingEvent, laterEvent = "request.started", "tool.called"
	for _, n := range example(7) {
		t.Run(n.name, func(t *testing.T) {
			sink := &blockedSink{entered: make(chan struct{}), release: make(chan struct{})}
			w := writer(t, sink)
			release := sync.OnceFunc(func() { close(sink.release) })
			defer release()
			w.Emit(context.Background(), primingEvent, nil)
			select {
			case <-sink.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("priming delivery never started")
			}
			done := make(chan struct{})
			go func() { n.send(context.Background(), w); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("notice waited for delivery")
			}
			w.Emit(context.Background(), laterEvent, nil)
			release()
			if err := w.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := sink.Events()
			if len(got) != 3 || got[0].Name != primingEvent || got[1].Name != n.name || got[2].Name != laterEvent {
				t.Fatalf("unexpected order %v", got)
			}
		})
	}
}

// R-1MRL-TY24 R-1NZI-7PST
func TestConcurrentNotices(t *testing.T) {
	const callers = 32
	capture := &telemetry.Capture{}
	w := writer(t, capture)
	var wg sync.WaitGroup
	start := make(chan struct{})
	want := map[string]notice{}
	for i := range callers {
		id := fmt.Sprint(i)
		c := identity.Caller{UserID: id, RequestID: "request-" + id}
		ctx := identity.NewContext(context.Background(), c)
		for _, n := range notices("tool"+id, "host"+id, "account"+id, "user"+id, "ip"+id, id, id, id, id, id, id, id, id, "data"+id, int64(i)) {
			want[id+"/"+n.name] = n
			wg.Go(func() {
				<-start
				n.send(ctx, w)
			})
		}
	}
	close(start)
	wg.Wait()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range capture.Events() {
		key := e.User + "/" + e.Name
		n, found := want[key]
		if !found {
			t.Fatalf("unexpected or duplicate event %+v", e)
		}
		checkEvent(t, e, n, identity.Caller{UserID: e.User, RequestID: "request-" + e.User})
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing %d events", len(want))
	}
}
