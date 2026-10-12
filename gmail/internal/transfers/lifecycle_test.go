package transfers

import (
	"context"

	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
)

func lifeWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal("lifecycle deadline exceeded")
	}
}
func lifePull(t *testing.T, f *workFixture, c identity.Caller, path string) store.Transfer {
	t.Helper()
	tr, err := f.e.Pull(context.Background(), c, PullRequest{Message: "message", Attachment: "2", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}
func lifeSaved(t *testing.T, f *workFixture, tr store.Transfer) store.Transfer {
	t.Helper()
	saved, err := f.st.GetTransfer(context.Background(), tr.UserID, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
func lifeFlush(t *testing.T, f *workFixture) {
	t.Helper()
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.emitter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func lifeGatePuts(f *workFixture, gates map[string]chan struct{}, entered chan<- string) {
	f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPut {
			return false
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return true
		}
		p := strings.TrimPrefix(r.URL.Path, "/files/")
		entered <- p
		select {
		case <-gates[p]:
			workMetadata(w, p, int64(len(b)))
		case <-r.Context().Done():
		}
		return true
	}
}
func lifeReceive(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("start %s; want %s", got, want)
		}
	case <-ctx.Done():
		t.Fatal("no work started")
	}
}

// R-J33G-5ZW5 R-J4BC-JRMU R-J96Y-2ULM
func TestLifecycleGlobalFIFOAndEventOrder(t *testing.T) {
	f := newWorkFixture(t)
	f.e.cfg.MaxActive = 1
	f.e.cfg.MaxQueued = 2
	gates := map[string]chan struct{}{"a": make(chan struct{}), "b": make(chan struct{}), "c": make(chan struct{})}
	entered := make(chan string, 3)
	lifeGatePuts(f, gates, entered)
	callerA := identity.Caller{UserID: "owner-a", Email: "owner-a@fixture.invalid", RequestID: "request-a"}
	ctx := events.NewContext(context.Background(), events.Cause{ID: "evt_0123456789abcdef", Depth: 2})
	a, err := f.e.Pull(ctx, callerA, PullRequest{Message: "message", Attachment: "2", Path: "a"})
	if err != nil {
		t.Fatal(err)
	}
	lifeReceive(t, entered, "a")
	b := lifePull(t, f, identity.Caller{UserID: "owner-b", RequestID: "request-b"}, "b")
	c := lifePull(t, f, identity.Caller{UserID: "owner-c", RequestID: "request-c"}, "c")
	if a.Status != store.StatusRunning || b.Status != store.StatusQueued || c.Status != store.StatusQueued {
		t.Fatal(a, b, c)
	}
	close(gates["a"])
	lifeReceive(t, entered, "b")
	if lifeSaved(t, f, a).Status != store.StatusDone || lifeSaved(t, f, b).Status != store.StatusRunning || lifeSaved(t, f, c).Status != store.StatusQueued {
		t.Fatal("wrong FIFO states")
	}
	close(gates["b"])
	lifeReceive(t, entered, "c")
	close(gates["c"])
	f.e.Drain(context.Background())
	lifeFlush(t, f)
	want := []string{trail.TransferStarted + ":" + a.ID, trail.TransferFinished + ":" + a.ID, trail.TransferStarted + ":" + b.ID, trail.TransferFinished + ":" + b.ID, trail.TransferStarted + ":" + c.ID, trail.TransferFinished + ":" + c.ID}
	var got []string
	for _, ev := range f.capture.Events() {
		if ev.Name != trail.TransferStarted && ev.Name != trail.TransferFinished {
			continue
		}
		id, _ := ev.Attrs["transfer"].(string)
		got = append(got, ev.Name+":"+id)
		tr := map[string]store.Transfer{a.ID: a, b.ID: b, c.ID: c}[id]
		saved := lifeSaved(t, f, tr)
		if ev.User != tr.UserID || ev.RequestID != tr.RequestID {
			t.Fatal(ev)
		}
		if ev.Name == trail.TransferFinished && (ev.Attrs["status"] != saved.Status || ev.Attrs["bytes"] != saved.Bytes || ev.Attrs["reason"] != saved.Reason) {
			t.Fatal("ending differs", ev, saved)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	var bus []string
	for _, ev := range f.bus.Events() {
		bus = append(bus, ev.Name+":"+ev.Attrs["transfer"].(string))
		if ev.Attrs["transfer"] == a.ID && (ev.Cause != "evt_0123456789abcdef" || ev.Depth != 3 || ev.User != a.UserID || ev.RequestID != a.RequestID) {
			t.Fatal(ev)
		}
	}
	if !reflect.DeepEqual(bus, want) {
		t.Fatal(bus, want)
	}
}

// R-J5J8-XJDJ R-J33G-5ZW5
func TestLifecycleDraftEligibilityPassesBlockedPush(t *testing.T) {
	f := newWorkFixture(t)
	f.e.cfg.MaxActive = 2
	d := workDraft(t, f, nil)
	uploads := make(chan int, 2)
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	var n atomic.Int32
	f.gmailHook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/session" {
			return false
		}
		_, _ = io.Copy(io.Discard, r.Body)
		i := int(n.Add(1)) - 1
		uploads <- i
		select {
		case <-gates[i]:
			_, _ = io.WriteString(w, `{"id":"remote-draft","message":{"id":"updated-message","threadId":"updated-thread"}}`)
		case <-r.Context().Done():
		}
		return true
	}
	entered := make(chan string, 2)
	pullGates := map[string]chan struct{}{"q": make(chan struct{}), "r": make(chan struct{})}
	lifeGatePuts(f, pullGates, entered)
	p1 := workPush(t, f, d)
	workAwait(t, func() bool { return n.Load() == 1 })
	p2 := workPush(t, f, d)
	q := lifePull(t, f, testCaller, "q")
	lifeReceive(t, entered, "q")
	if p1.Status != store.StatusRunning || p2.Status != store.StatusQueued || q.Status != store.StatusRunning {
		t.Fatal(p1, p2, q)
	}
	close(pullGates["q"])
	workEnd(t, f, q)
	if lifeSaved(t, f, p2).Status != store.StatusQueued {
		t.Fatal("second push passed first")
	}
	r := lifePull(t, f, testCaller, "r")
	lifeReceive(t, entered, "r")
	if r.Status != store.StatusRunning {
		t.Fatal(r)
	}
	close(pullGates["r"])
	workEnd(t, f, r)
	close(gates[0])
	workAwait(t, func() bool { return n.Load() == 2 })
	if lifeSaved(t, f, p1).Status != store.StatusDone || lifeSaved(t, f, p2).Status != store.StatusRunning {
		t.Fatal("push order")
	}
	close(gates[1])
	f.e.Drain(context.Background())
	saved, err := f.st.GetDraft(context.Background(), d.UserID, d.ID)
	if err != nil || len(saved.Attachments) != 2 {
		t.Fatal(saved, err)
	}
	lifeFlush(t, f)
	var changes []string
	for _, ev := range f.bus.Events() {
		id := ev.Attrs["transfer"].(string)
		if id == p1.ID || id == p2.ID {
			changes = append(changes, ev.Name+":"+id)
		}
	}
	want := []string{trail.TransferStarted + ":" + p1.ID, trail.TransferFinished + ":" + p1.ID, trail.TransferStarted + ":" + p2.ID, trail.TransferFinished + ":" + p2.ID}
	if !reflect.DeepEqual(changes, want) {
		t.Fatal(changes)
	}
}

// R-J7Z1-P2UX
func TestLifecycleConcurrentQueueBound(t *testing.T) {
	f := newWorkFixture(t)
	f.e.cfg.MaxActive = 1
	f.e.cfg.MaxQueued = 2
	hold := make(chan struct{})
	entered := make(chan struct{})
	heads := make(chan struct{}, 20)
	releaseHeads := make(chan struct{})
	f.artifactHook = func(_ http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/files/burst"):
			heads <- struct{}{}
			<-releaseHeads
			return false
		case r.Method == http.MethodPut:
			close(entered)
			select {
			case <-hold:
			case <-r.Context().Done():
			}
			return true
		}
		return false
	}
	first := lifePull(t, f, testCaller, "running")
	lifeWait(t, entered)
	type result struct {
		tr  store.Transfer
		err error
	}
	results := make(chan result, 20)
	for i := range 20 {
		go func(i int) {
			tr, err := f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Attachment: "2", Path: "burst" + jsonNumber(int64(i))})
			results <- result{tr, err}
		}(i)
	}
	for range 20 {
		lifeWait(t, heads)
	}
	close(releaseHeads)
	queued, refused := 0, 0
	for range 20 {
		res := <-results
		if res.err == nil {
			if res.tr.Status != store.StatusQueued {
				t.Fatal(res)
			}
			queued++
		} else {
			var r *Refusal
			if !errors.As(res.err, &r) || r.Word != RefusalQueueFull {
				t.Fatal(res.err)
			}
			refused++
		}
	}
	if queued != 2 || refused != 18 {
		t.Fatal(queued, refused)
	}
	unfinished, err := f.st.UnfinishedTransfers(context.Background())
	if err != nil || len(unfinished) != 3 {
		t.Fatal(unfinished, err)
	}
	if lifeSaved(t, f, first).Status != store.StatusRunning {
		t.Fatal("slot lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.e.Drain(ctx)
}

// R-J6R5-BB48
func TestLifecycleFailedStartPassesNextEligible(t *testing.T) {
	f := newWorkFixture(t)
	f.e.cfg.MaxActive = 1
	entered := make(chan string, 3)
	gates := map[string]chan struct{}{"a": make(chan struct{}), "c": make(chan struct{})}
	lifeGatePuts(f, gates, entered)
	a := lifePull(t, f, testCaller, "a")
	lifeReceive(t, entered, "a")
	b := lifePull(t, f, testCaller, "b")
	c := lifePull(t, f, testCaller, "c")
	ended, err := f.st.FinishTransfer(context.Background(), b.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
	if err != nil {
		t.Fatal(err)
	}
	close(gates["a"])
	lifeReceive(t, entered, "c")
	if lifeSaved(t, f, a).Status != store.StatusDone || lifeSaved(t, f, b) != ended || lifeSaved(t, f, c).Status != store.StatusRunning {
		t.Fatal("failed start did not pass slot")
	}
	close(gates["c"])
	f.e.Drain(context.Background())
	lifeFlush(t, f)
	for _, ev := range f.bus.Events() {
		if ev.Attrs["transfer"] == b.ID {
			t.Fatal("event for failed start", ev)
		}
	}
	if err := f.e.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// R-JAEU-GMCB
func TestLifecycleUnrecordedFinishReleasesSlot(t *testing.T) {
	f := newWorkFixture(t)
	f.e.cfg.MaxActive = 1
	entered := make(chan string, 1)
	gates := map[string]chan struct{}{"a": make(chan struct{})}
	lifeGatePuts(f, gates, entered)
	a := lifePull(t, f, testCaller, "a")
	lifeReceive(t, entered, "a")
	f.db.SetFailing(true)
	close(gates["a"])
	done := make(chan struct{})
	go func() { f.e.Drain(context.Background()); close(done) }()
	lifeWait(t, done)
	f.db.SetFailing(false)
	if lifeSaved(t, f, a).Status != store.StatusRunning {
		t.Fatal("ending unexpectedly recorded")
	}
	lifeFlush(t, f)
	for _, ev := range f.bus.Events() {
		if ev.Name == trail.TransferFinished {
			t.Fatal("event for failed ending")
		}
	}
	for _, ev := range f.capture.Events() {
		if ev.Name == trail.TransferFinished {
			t.Fatal("trail for failed ending")
		}
	}
}

// R-JBMQ-UE30
func TestLifecycleRecoverSettlesAllAndStoreFailures(t *testing.T) {
	f := newWorkFixture(t)
	var records []store.Transfer
	for _, status := range []string{store.StatusRunning, store.StatusQueued} {
		tr, err := f.st.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: status, Message: "message", Path: status, UserID: status, UserEmail: status + "@fixture.invalid", RequestID: status + "-request"})
		if err != nil {
			t.Fatal(err)
		}
		if status == store.StatusRunning {
			tr, err = f.st.SetTransferBytes(context.Background(), tr.ID, 3)
			if err != nil {
				t.Fatal(err)
			}
		}
		records = append(records, tr)
	}
	ctx := events.NewContext(context.Background(), events.Cause{ID: "evt_abcdef0123456789", Depth: 4})
	if err := f.e.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	lifeFlush(t, f)
	if len(f.bus.Events()) != 2 {
		t.Fatal(f.bus.Events())
	}
	telemetryFinishes := 0
	for _, ev := range f.capture.Events() {
		if ev.Name == trail.TransferFinished {
			telemetryFinishes++
			found := false
			for _, tr := range records {
				if ev.Attrs["transfer"] == tr.ID {
					found = true
					if ev.User != tr.UserID || ev.RequestID != tr.RequestID || ev.Attrs["bytes"] != tr.Bytes || ev.Attrs["reason"] != store.ReasonInterrupted || ev.Attrs["status"] != store.StatusFailed {
						t.Fatal(ev)
					}
				}
			}
			if !found {
				t.Fatal(ev)
			}
		}
	}
	if telemetryFinishes != 2 {
		t.Fatal(telemetryFinishes)
	}
	for _, tr := range records {
		got := lifeSaved(t, f, tr)
		if got.Status != store.StatusFailed || got.Reason != store.ReasonInterrupted || got.Bytes != tr.Bytes {
			t.Fatal(got)
		}
		found := false
		for _, ev := range f.bus.Events() {
			if ev.Attrs["transfer"] == tr.ID {
				found = true
				if ev.Name != trail.TransferFinished || ev.User != tr.UserID || ev.RequestID != tr.RequestID || ev.Cause != "" || ev.Depth != 0 || ev.Attrs["bytes"] != tr.Bytes {
					t.Fatal(ev)
				}
			}
		}
		if !found {
			t.Fatal("missing recovery event")
		}
	}
	f.db.SetFailing(true)
	if err := f.e.Recover(context.Background()); !errors.Is(err, store.ErrUnreachable) {
		t.Fatal(err)
	}
	f.db.SetFailing(false)
}

// R-JCUN-85TP R-JE2J-LXKE R-JGIC-DH1S
func TestLifecycleDrainSettlesQueueAndLetsRunningFinish(t *testing.T) {
	f := newWorkFixture(t)
	f.e.cfg.MaxActive = 1
	entered := make(chan string, 1)
	gates := map[string]chan struct{}{"a": make(chan struct{})}
	lifeGatePuts(f, gates, entered)
	a := lifePull(t, f, testCaller, "a")
	lifeReceive(t, entered, "a")
	b := lifePull(t, f, testCaller, "b")
	done := make(chan struct{})
	go func() { f.e.Drain(context.Background()); close(done) }()
	workAwait(t, func() bool { return lifeSaved(t, f, b).Status == store.StatusFailed })
	got := lifeSaved(t, f, b)
	if got.Reason != store.ReasonInterrupted || got.Bytes != 0 {
		t.Fatal(got)
	}
	select {
	case <-done:
		t.Fatal("drain returned while running")
	default:
	}
	if lifeSaved(t, f, a).Status != store.StatusRunning {
		t.Fatal("running work was cut off")
	}
	close(gates["a"])
	lifeWait(t, done)
	if lifeSaved(t, f, a).Status != store.StatusDone {
		t.Fatal("running did not finish")
	}
	lifeFlush(t, f)
	counts := map[string]int{}
	for _, ev := range f.bus.Events() {
		counts[ev.Name+":"+ev.Attrs["transfer"].(string)]++
	}
	if counts[trail.TransferStarted+":"+b.ID] != 0 || counts[trail.TransferFinished+":"+b.ID] != 1 || counts[trail.TransferStarted+":"+a.ID] != 1 || counts[trail.TransferFinished+":"+a.ID] != 1 {
		t.Fatal(counts)
	}
	unfinished, err := f.st.UnfinishedTransfers(context.Background())
	if err != nil || len(unfinished) != 0 {
		t.Fatal(unfinished, err)
	}
}

// R-JFAF-ZPB3 R-JGIC-DH1S
func TestLifecycleDrainCancelsEveryExchange(t *testing.T) {
	for _, target := range []string{"token", "put", "get", "upload"} {
		t.Run(target, func(t *testing.T) {
			f := newWorkFixture(t)
			entered, canceled := make(chan struct{}), make(chan struct{})
			hold := func(r *http.Request) { close(entered); <-r.Context().Done(); close(canceled) }
			switch target {
			case "token":
				f.tokenHook = func(_ http.ResponseWriter, r *http.Request, n int) bool {
					if n != 2 {
						return false
					}
					hold(r)
					return true
				}
			case "put":
				f.artifactHook = func(_ http.ResponseWriter, r *http.Request) bool {
					if r.Method != http.MethodPut {
						return false
					}
					_, _ = io.Copy(io.Discard, r.Body)
					hold(r)
					return true
				}
			case "get":
				f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
					if r.Method != http.MethodGet {
						return false
					}
					workFileHeaders(w, 100)
					_, _ = io.WriteString(w, "first")
					w.(http.Flusher).Flush()
					hold(r)
					return true
				}
			case "upload":
				f.gmailHook = func(_ http.ResponseWriter, r *http.Request) bool {
					if r.URL.Path != "/session" {
						return false
					}
					_, _ = io.Copy(io.Discard, r.Body)
					hold(r)
					return true
				}
			}
			var tr store.Transfer
			var draft store.Draft
			if target == "get" || target == "upload" {
				draft = workDraft(t, f, nil)
				tr = workPush(t, f, draft)
			} else {
				tr = workPull(context.Background(), t, f, "2")
			}
			lifeWait(t, entered)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { f.e.Drain(ctx); close(done) }()
			cancel()
			lifeWait(t, done)
			lifeWait(t, canceled)
			got := lifeSaved(t, f, tr)
			if got.Status != store.StatusFailed || got.Reason != store.ReasonInterrupted {
				t.Fatal(got)
			}
			if target == "token" && got.Bytes != 0 {
				t.Fatal(got)
			}
			if target == "put" && got.Bytes != int64(len(f.payload)) {
				t.Fatal(got)
			}
			if target == "upload" && got.Bytes != int64(len(f.files["new.bin"])) {
				t.Fatal(got)
			}
			if draft.ID != "" {
				saved, err := f.st.GetDraft(context.Background(), draft.UserID, draft.ID)
				if err != nil || len(saved.Attachments) != 0 || saved.Message != draft.Message {
					t.Fatal(saved, err)
				}
			}
			lifeFlush(t, f)
			finishes := 0
			for _, ev := range f.bus.Events() {
				if ev.Name == trail.TransferFinished {
					finishes++
					if ev.Attrs["status"] != got.Status || ev.Attrs["reason"] != got.Reason || ev.Attrs["bytes"] != got.Bytes {
						t.Fatal(ev)
					}
				}
			}
			if finishes != 1 {
				t.Fatal(finishes)
			}
		})
	}
}

// R-JIY5-50J6
func TestLifecycleConcurrentMethods(t *testing.T) {
	f := newWorkFixture(t)
	d := workDraft(t, f, nil)
	gate := make(chan struct{})
	entered := make(chan struct{}, 32)
	f.artifactHook = func(_ http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPut {
			return false
		}
		entered <- struct{}{}
		select {
		case <-gate:
		case <-r.Context().Done():
		}
		return true
	}
	first := workPull(context.Background(), t, f, "2")
	lifeWait(t, entered)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 80)
	for range 10 {
		wg.Add(4)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Attachment: "2", Path: "pulled.bin"})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := f.e.Push(context.Background(), testCaller, PushRequest{Path: "new.bin", Draft: d.ID})
			errs <- err
		}()
		go func() { defer wg.Done(); <-start; _, err := f.e.Token(context.Background(), testCaller); errs <- err }()
		go func() {
			defer wg.Done()
			<-start
			_, err := Token(context.Background(), f.e.cfg.Connections, f.e.ConnectURL(), testCaller)
			errs <- err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f.e.Drain(ctx)
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil {
			continue
		}
		var r *Refusal
		if !errors.As(err, &r) && !errors.Is(err, store.ErrUnreachable) {
			t.Fatal(err)
		}
	}
	got := lifeSaved(t, f, first)
	if got.Status != store.StatusFailed || got.Reason != store.ReasonInterrupted {
		t.Fatal(got)
	}
	unfinished, err := f.st.UnfinishedTransfers(context.Background())
	if err != nil || len(unfinished) != 0 {
		t.Fatal(unfinished, err)
	}
}

// R-JBMQ-UE30
func TestLifecycleRecoverWriteFailureEmitsNothing(t *testing.T) {
	f := newWorkFixture(t)
	tr, err := f.st.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusQueued, Message: "message", Path: "path", UserID: testCaller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := f.e.cfg
	cfg.Store = store.New(f.db, store.Config{Now: func() time.Time { cancel(); return fClock() }})
	engine := New(cfg)
	if err := engine.Recover(ctx); !errors.Is(err, store.ErrUnreachable) {
		t.Fatal(err)
	}
	if got := lifeSaved(t, f, tr); got.Status != store.StatusQueued {
		t.Fatal("failed write changed record", got)
	}
	lifeFlush(t, f)
	if len(f.bus.Events()) != 0 {
		t.Fatal("event for failed recovery", f.bus.Events())
	}
	for _, ev := range f.capture.Events() {
		if ev.Name == trail.TransferFinished {
			t.Fatal("trail for failed recovery", ev)
		}
	}
}
