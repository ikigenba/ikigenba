package smarthttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
)

type requestResult struct{ recorder *httptest.ResponseRecorder }

func startRequest(ctx context.Context, f *fixture, method, path, body string) <-chan requestResult {
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-Request-Id", "queued")
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-"+r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]+"-request")
	}
	done := make(chan requestResult, 1)
	go func() { w := httptest.NewRecorder(); f.handler().ServeHTTP(w, r); done <- requestResult{w} }()
	return done
}

func TestQueuedSelectedGitDisappears(t *testing.T) {
	// R-VB7F-60ZS
	f := setup(t)
	repo := f.create("notes")
	dir := filepath.Join(f.root, "git-path")
	must(t, os.Mkdir(dir, 0700))
	trace := filepath.Join(f.root, "selected-git-trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	must(t, os.WriteFile(trace, nil, 0600))
	alias := filepath.Join(dir, "git")
	must(t, os.Symlink(f.executable, alias))
	g, err := git.Find(dir, func() []string { return append([]string(nil), f.env...) })
	must(t, err)
	f.git = g
	held := queueGrant(f, "other", limits.Fetch, false)
	done := startRequest(deadline(t), f, "GET", "/notes.git/info/refs?service=git-upload-pack", "")
	f.clock.take(t)
	same(t, f.limits.Pressure().Read.Queued, int64(1))
	f.clock.advance(time.Second)
	// Only the test's symlink disappears; the host's git is untouched.
	must(t, os.Remove(alias))
	held.Release()
	w := finishRequest(t, done)
	traceBytes, err := os.ReadFile(filepath.Clean(trace))
	must(t, err)
	backendRan := false
	for line := range strings.SplitSeq(string(traceBytes), "\n") {
		if line == "" {
			continue
		}
		var event struct {
			Event string
			Argv  []string
		}
		must(t, json.Unmarshal([]byte(line), &event))
		if event.Event == "start" && len(event.Argv) > 0 && event.Argv[len(event.Argv)-1] == "http-backend" {
			backendRan = true
		}
	}
	var waits []telemetry.Event
	for _, event := range ownEvents(f.events()) {
		if event.Name == "operation.waited" {
			waits = append(waits, event)
		}
	}
	if backendRan {
		same(t, w.Code, 200)
		same(t, w.Header().Get("Content-Type"), "application/x-git-upload-pack-advertisement")
		if !strings.HasPrefix(w.Body.String(), "001e# service=git-upload-pack\n0000") {
			t.Fatalf("not a git advertisement: %q", w.Body.String())
		}
		same(t, len(waits), 1)
		same(t, waits[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "fetch", "wait_us": int64(time.Second / time.Microsecond)})
	} else {
		same(t, len(waits), 0)
	}
	assertIdle(t, f, repo.ID)
}

func TestRequestCancelledWhileWaitEventIsRecorded(t *testing.T) {
	// R-VB7F-60ZS
	f := setup(t)
	repo := f.create("notes")
	before := f.refs(repo.ID)
	ctx, cancel := context.WithCancel(deadline(t))
	defer cancel()
	var armed atomic.Bool
	f.writer.Shutdown(deadline(t), "replace")
	f.capture = new(telemetry.Capture)
	f.writer = telemetry.New(telemetry.Config{Service: "repos", Sink: f.capture, Now: func() time.Time {
		if armed.Swap(false) {
			cancel()
		}
		return epoch
	}, Rand: new(sequence), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { f.writer.Shutdown(deadline(t), "test") })
	f.limits = limits.New(f.settings, limits.Clock{Now: f.clock.read, After: func(d time.Duration) <-chan time.Time {
		ch := f.clock.after(d)
		if d == time.Duration(f.settings.OperationSeconds)*time.Second {
			armed.Store(true)
		}
		return ch
	}})
	held := queueGrant(f, "other", limits.Push, false)
	rd, wr := io.Pipe()
	t.Cleanup(func() { _ = rd.Close(); _ = wr.Close() })
	body := &observedReadBody{ReadCloser: rd, started: make(chan struct{})}
	r := httptest.NewRequest("POST", "/notes.git/git-receive-pack", body)
	r.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	r = r.WithContext(identity.NewContext(ctx, identity.Caller{UserID: "alice", RequestID: "cancelled-start"}))
	type result struct {
		response *httptest.ResponseRecorder
		aborted  any
	}
	done := make(chan result, 1)
	go func() {
		w := httptest.NewRecorder()
		defer func() { done <- result{w, recover()} }()
		smarthttp.Handler(f.config()).ServeHTTP(w, r)
	}()
	f.clock.take(t)
	f.clock.advance(time.Second)
	held.Release()
	var completed result
	select {
	case completed = <-done:
	case <-deadline(t).Done():
		t.Fatal("startup cancellation left the handler running")
	}
	same(t, errors.Is(ctx.Err(), context.Canceled), true)
	select {
	case <-body.started:
		t.Fatal("pending startup cancellation fed client bytes to git")
	default:
	}
	events := ownEvents(f.events())
	if completed.aborted != nil {
		err, ok := completed.aborted.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("unexpected abort: %v", completed.aborted)
		}
		same(t, len(events), 1)
		same(t, events[0].Name, "operation.waited")
		same(t, events[0].RequestID, "cancelled-start")
		same(t, events[0].User, "alice")
		same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "push", "wait_us": int64(time.Second / time.Microsecond)})
	} else {
		// A refused launch has no wait event; a real launch followed by
		// pending cancellation aborts without feeding the held-open body.
		same(t, completed.response.Code, 500)
		same(t, len(events), 0)
	}
	same(t, f.refs(repo.ID), before)
	f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "fsck")
	assertIdle(t, f, repo.ID)
}

func TestQueuedSelectedGitRetainedDuringWaitRecord(t *testing.T) {
	// R-VB7F-60ZS
	f := setup(t)
	repo := f.create("notes")
	dir := filepath.Join(f.root, "selected-executable")
	must(t, os.Mkdir(dir, 0700))
	alias := filepath.Join(dir, "git")
	must(t, os.Symlink(f.executable, alias))
	g, err := git.Find(dir, func() []string { return append([]string(nil), f.env...) })
	must(t, err)
	f.git = g
	var armed atomic.Bool
	var removalErr error
	f.writer.Shutdown(deadline(t), "replace")
	f.capture = new(telemetry.Capture)
	f.writer = telemetry.New(telemetry.Config{Service: "repos", Sink: f.capture, Now: func() time.Time {
		if armed.Swap(false) {
			removalErr = os.Remove(alias)
		}
		return epoch
	}, Rand: new(sequence), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { f.writer.Shutdown(deadline(t), "test") })
	f.limits = limits.New(f.settings, limits.Clock{Now: f.clock.read, After: func(d time.Duration) <-chan time.Time {
		ch := f.clock.after(d)
		if d == time.Duration(f.settings.OperationSeconds)*time.Second {
			armed.Store(true)
		}
		return ch
	}})
	held := queueGrant(f, "other", limits.Fetch, false)
	done := startRequest(deadline(t), f, "GET", "/notes.git/info/refs?service=git-upload-pack", "")
	f.clock.take(t)
	f.clock.advance(time.Second)
	held.Release()
	w := finishRequest(t, done)
	must(t, removalErr)
	_, err = os.Stat(alias)
	same(t, errors.Is(err, os.ErrNotExist), true)
	same(t, w.Code, 200)
	same(t, w.Header().Get("Content-Type"), "application/x-git-upload-pack-advertisement")
	if !strings.HasPrefix(w.Body.String(), "001e# service=git-upload-pack\n0000") || !strings.HasSuffix(w.Body.String(), "0000") {
		t.Fatalf("incomplete git advertisement: %q", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), strings.Repeat("0", 40)+" capabilities^{}\x00") {
		t.Fatalf("empty repository capabilities missing: %q", w.Body.String())
	}
	events := ownEvents(f.events())
	same(t, len(events), 1)
	same(t, events[0].Name, "operation.waited")
	same(t, events[0].RequestID, "queued")
	same(t, events[0].User, "alice")
	same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "fetch", "wait_us": int64(time.Second / time.Microsecond)})
	assertIdle(t, f, repo.ID)
}

func TestQueuedSelectedGitImageRetainedDuringWaitRecord(t *testing.T) {
	// R-VB7F-60ZS
	for _, mode := range []os.FileMode{0500, 0111} {
		t.Run(strconv.FormatUint(uint64(mode), 8), func(t *testing.T) {
			queuedSelectedGitImage(t, mode)
		})
	}
}

func queuedSelectedGitImage(t *testing.T, mode os.FileMode) {
	t.Helper()
	f := setup(t)
	repo := f.create("notes")
	dir := t.TempDir()
	selected := filepath.Join(dir, "git")
	image, err := os.ReadFile(filepath.Clean(f.executable))
	must(t, err)
	imageFile, err := os.OpenFile(filepath.Clean(selected), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	must(t, err)
	_, err = imageFile.Write(image)
	must(t, err)
	must(t, imageFile.Chmod(mode))
	must(t, imageFile.Close())
	g, err := git.Find(dir, func() []string { return append([]string(nil), f.env...) })
	must(t, err)
	f.git = g
	var armed atomic.Bool
	var mutationErr error
	f.writer.Shutdown(deadline(t), "replace")
	f.capture = new(telemetry.Capture)
	f.writer = telemetry.New(telemetry.Config{Service: "repos", Sink: f.capture, Now: func() time.Time {
		if armed.Swap(false) {
			info, statErr := os.Stat(selected)
			switch {
			case statErr != nil:
				mutationErr = statErr
			case info.Mode().Perm() != mode:
				mutationErr = errors.New("selected git mode was not restored before wait recording")
			default:
				mutationErr = os.Chmod(selected, 0600)
			}
		}
		return epoch
	}, Rand: new(sequence), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { f.writer.Shutdown(deadline(t), "test") })
	f.limits = limits.New(f.settings, limits.Clock{Now: f.clock.read, After: func(d time.Duration) <-chan time.Time {
		ch := f.clock.after(d)
		if d == time.Duration(f.settings.OperationSeconds)*time.Second {
			armed.Store(true)
		}
		return ch
	}})
	held := queueGrant(f, "other", limits.Fetch, false)
	done := startRequest(deadline(t), f, "GET", "/notes.git/info/refs?service=git-upload-pack", "")
	f.clock.take(t)
	f.clock.advance(time.Second)
	held.Release()
	w := finishRequest(t, done)
	must(t, mutationErr)
	info, err := os.Stat(selected)
	must(t, err)
	same(t, info.Mode().Perm(), os.FileMode(0600))
	same(t, w.Code, 200)
	same(t, w.Header().Get("Content-Type"), "application/x-git-upload-pack-advertisement")
	if !strings.HasPrefix(w.Body.String(), "001e# service=git-upload-pack\n0000") || !strings.HasSuffix(w.Body.String(), "0000") {
		t.Fatalf("incomplete git advertisement: %q", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), strings.Repeat("0", 40)+" capabilities^{}\x00") {
		t.Fatalf("empty repository capabilities missing: %q", w.Body.String())
	}
	events := ownEvents(f.events())
	same(t, len(events), 1)
	same(t, events[0].Name, "operation.waited")
	same(t, events[0].RequestID, "queued")
	same(t, events[0].User, "alice")
	same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "fetch", "wait_us": int64(time.Second / time.Microsecond)})
	entries, err := os.ReadDir(filepath.Dir(f.store.Dir(repo.ID)))
	must(t, err)
	same(t, len(entries), 1)
	same(t, entries[0].Name(), filepath.Base(f.store.Dir(repo.ID)))
	assertIdle(t, f, repo.ID)
}

func TestExecuteOnlySelectedGitClients(t *testing.T) {
	// R-D4P7-M7WP R-D5X3-ZZNE
	f := setup(t)
	repo := f.create("notes")
	selected := filepath.Join(t.TempDir(), "git")
	image, err := os.ReadFile(filepath.Clean(f.executable))
	must(t, err)
	imageFile, err := os.OpenFile(filepath.Clean(selected), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	must(t, err)
	_, err = imageFile.Write(image)
	must(t, err)
	must(t, imageFile.Chmod(0111))
	must(t, imageFile.Close())
	_, err = os.ReadFile(filepath.Clean(selected))
	same(t, errors.Is(err, os.ErrPermission), true)
	f.git, err = git.Find(filepath.Dir(selected), func() []string { return append([]string(nil), f.env...) })
	must(t, err)
	server := f.server()
	work := f.working("source")
	first := f.commit(work, "one")
	f.client(work, "push", server.URL+"/notes.git", "main")
	same(t, strings.TrimSpace(f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "rev-parse", "refs/heads/main")), first)
	for _, version := range []string{"0", "2"} {
		clone := filepath.Join(f.root, "execute-only-clone"+version)
		f.client("", "-c", "protocol.version="+version, "clone", server.URL+"/notes.git", clone)
		same(t, strings.TrimSpace(f.gitRun(clone, "rev-parse", "HEAD")), first)
	}
	second := f.commit(work, "two")
	f.client(work, "push", server.URL+"/notes.git", "main")
	f.client(filepath.Join(f.root, "execute-only-clone2"), "fetch")
	same(t, strings.TrimSpace(f.gitRun(filepath.Join(f.root, "execute-only-clone2"), "rev-parse", "origin/main")), second)
	f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "cat-file", "-e", second)
	info, err := os.Stat(selected)
	must(t, err)
	same(t, info.Mode().Perm(), os.FileMode(0111))
	assertIdle(t, f, repo.ID)
}

func TestReadOnlyRepositoryRootParentFetch(t *testing.T) {
	// R-D4P7-M7WP
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	commit := f.commit(work, "one")
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	parent := filepath.Dir(f.store.Dir(repo.ID))
	parentFile, err := os.Open(filepath.Clean(parent))
	must(t, err)
	must(t, parentFile.Chmod(0500))
	t.Cleanup(func() { must(t, parentFile.Chmod(0700)); must(t, parentFile.Close()) })
	server := f.server()
	for _, version := range []string{"0", "2"} {
		clone := filepath.Join(f.root, "readonly-parent-clone"+version)
		f.client("", "-c", "protocol.version="+version, "clone", server.URL+"/notes.git", clone)
		same(t, strings.TrimSpace(f.gitRun(clone, "rev-parse", "HEAD")), commit)
	}
	same(t, strings.TrimSpace(f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "rev-parse", "refs/heads/main")), commit)
	assertIdle(t, f, repo.ID)
}

type observedReadBody struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (b *observedReadBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	return b.ReadCloser.Read(p)
}

func TestTimeoutEventUsesCallerEnvelopeForEachOperation(t *testing.T) {
	// R-99XD-V3AK
	for _, route := range []string{"git-upload-pack", "git-receive-pack"} {
		t.Run(route, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			rd, wr := io.Pipe()
			t.Cleanup(func() { _ = rd.Close(); _ = wr.Close() })
			body := &observedReadBody{ReadCloser: rd, started: make(chan struct{})}
			r := httptest.NewRequest("POST", "/notes.git/"+route, body)
			r.Header.Set("Content-Type", "application/x-"+route+"-request")
			caller := identity.Caller{UserID: "alice", RequestID: "timeout-envelope"}
			r = r.WithContext(identity.NewContext(deadline(t), caller))
			done := make(chan any, 1)
			go func() {
				defer func() { done <- recover() }()
				smarthttp.Handler(f.config()).ServeHTTP(httptest.NewRecorder(), r)
			}()
			timer := f.clock.take(t)
			ctx := deadline(t)
			select {
			case <-body.started:
			case <-ctx.Done():
				t.Fatal("git never began reading the held request body")
			}
			timer.fire <- epoch
			select {
			case result := <-done:
				err, ok := result.(error)
				if !ok || !errors.Is(err, http.ErrAbortHandler) {
					t.Fatalf("timeout returned without abort: %v", result)
				}
			case <-ctx.Done():
				t.Fatal("timed out handler did not return")
			}
			events := ownEvents(f.events())
			same(t, len(events), 1)
			same(t, events[0].Name, "operation.timed_out")
			same(t, events[0].RequestID, caller.RequestID)
			same(t, events[0].User, caller.UserID)
			op := "fetch"
			if route == "git-receive-pack" {
				op = "push"
			}
			same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": op, "limit": "operation_seconds"})
			assertIdle(t, f, repo.ID)
		})
	}
}
func finishRequest(t *testing.T, done <-chan requestResult) *httptest.ResponseRecorder {
	t.Helper()
	ctx := deadline(t)
	select {
	case result := <-done:
		return result.recorder
	case <-ctx.Done():
		t.Fatal("handler did not return")
		return nil
	}
}

func TestGrantsKindsLocksDeadlineAndRelease(t *testing.T) {
	// R-8QEZ-QRFG R-9066-SXD0 R-951S-C0BS R-E52N-3LH0
	for _, tc := range []struct {
		method, path string
		op           limits.Op
		lock         bool
	}{
		{"GET", "/notes.git/info/refs?service=git-upload-pack", limits.Fetch, false},
		{"POST", "/notes.git/git-upload-pack", limits.Fetch, false},
		{"GET", "/notes.git/info/refs?service=git-receive-pack", limits.Push, false},
		{"POST", "/notes.git/git-receive-pack", limits.Push, true},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			f.settings.WriteSlots = 2
			operation := make(chan time.Duration, 2)
			resume := make(chan struct{})
			f.limits = limits.New(f.settings, limits.Clock{Now: f.clock.read, After: func(d time.Duration) <-chan time.Time {
				operation <- d
				if d == time.Duration(f.settings.OperationSeconds)*time.Second {
					<-resume
				}
				return make(chan time.Time)
			}})
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader("0000"))
			r.Header.Set("Content-Type", "application/x-"+strings.TrimPrefix(tc.path[strings.LastIndex(tc.path, "/")+1:], "/")+"-request")
			r = r.WithContext(identity.NewContext(deadline(t), identity.Caller{UserID: "alice", RequestID: "kind"}))
			done := make(chan requestResult, 1)
			go func() {
				w := httptest.NewRecorder()
				smarthttp.Handler(f.config()).ServeHTTP(w, r)
				done <- requestResult{w}
			}()
			ctx := deadline(t)
			select {
			case duration := <-operation:
				same(t, duration, time.Duration(f.settings.OperationSeconds)*time.Second)
			case <-ctx.Done():
				t.Fatal("operation did not arm deadline")
			}
			p := f.limits.Pressure()
			if tc.op == limits.Fetch {
				same(t, p.Read.Active, int64(1))
				same(t, p.Write.Active, int64(0))
			} else {
				same(t, p.Read.Active, int64(0))
				same(t, p.Write.Active, int64(1))
			}
			same(t, f.limits.Busy(repo.ID), true)
			// A read grant never conflicts with a write grant's repository lock.
			other := queueGrant(f, repo.ID, opposite(tc.op), false)
			other.Release()
			if tc.op == limits.Push {
				if tc.lock {
					lockCtx, lockCancel := context.WithCancel(deadline(t))
					lockDone := make(chan error, 1)
					go func() {
						g, err := f.limits.Acquire(lockCtx, repo.ID, limits.Push, true)
						if g != nil {
							g.Release()
						}
						lockDone <- err
					}()
					select {
					case d := <-operation:
						same(t, d, time.Duration(f.settings.QueueSeconds)*time.Second)
					case <-ctx.Done():
						t.Fatal("push did not hold repository lock")
					}
					lockCancel()
					select {
					case err := <-lockDone:
						if err == nil {
							t.Fatal("conflicting push was granted")
						}
					case <-ctx.Done():
						t.Fatal("lock waiter did not return")
					}
				} else {
					g := queueGrant(f, repo.ID, limits.Push, true)
					g.Release()
				}
			}
			before := f.clock.count()
			close(resume)
			w := finishRequest(t, done)
			same(t, w.Code, 200)
			same(t, f.clock.count(), before)
			assertIdle(t, f, repo.ID)
		})
	}
}
func opposite(op limits.Op) limits.Op {
	if op == limits.Fetch {
		return limits.Push
	}
	return limits.Fetch
}

func TestQueueRejectionsAndDrain(t *testing.T) {
	// R-V7JQ-0PRP R-V8RM-EHIE R-V9ZI-S993 R-99XD-V3AK
	for _, kind := range []string{"queue_length", "queue_seconds", "draining"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			held := queueGrant(f, repo.ID, limits.Fetch, false)
			defer held.Release()
			ctx, cancel := context.WithCancel(deadline(t))
			defer cancel()
			waiting := startRequest(ctx, f, "GET", "/notes.git/info/refs?service=git-upload-pack", "")
			timer := f.clock.take(t)
			same(t, timer.duration, time.Duration(f.settings.QueueSeconds)*time.Second)
			same(t, f.limits.Pressure().Read.Queued, int64(1))
			before := len(f.events())
			var w *httptest.ResponseRecorder
			switch kind {
			case "queue_length":
				w = f.request("GET", "/notes.git/info/refs?service=git-upload-pack", nil)
			case "queue_seconds":
				timer.fire <- epoch
				w = finishRequest(t, waiting)
			case "draining":
				f.limits.Drain()
				w = finishRequest(t, waiting)
			}
			message := smarthttp.TooBusy + "\n"
			if kind == "draining" {
				message = smarthttp.Stopping + "\n"
			}
			outcome(t, w, 503, message)
			same(t, w.Header().Get("Retry-After"), strconv.FormatInt(f.settings.QueueSeconds, 10))
			events := ownEvents(f.events()[before:])
			same(t, len(events), 1)
			same(t, events[0].Name, "operation.rejected")
			same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "fetch", "limit": kind})
			same(t, events[0].User, "alice")
			requestID := "queued"
			if kind == "queue_length" {
				requestID = "request"
			}
			same(t, events[0].RequestID, requestID)
			if kind == "queue_length" {
				cancel()
				finishRequest(t, waiting)
			}
			held.Release()
			assertIdle(t, f, repo.ID)
			if kind == "draining" {
				before = len(f.events())
				w = f.request("GET", "/notes.git/info/refs?service=git-receive-pack", nil)
				outcome(t, w, 503, message)
				e := ownEvents(f.events()[before:])
				same(t, len(e), 1)
				same(t, e[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "push", "limit": "draining"})
			}
		})
	}
}

func TestQueuedContextCancellation(t *testing.T) {
	// R-92LZ-KGUE R-951S-C0BS
	f := setup(t)
	repo := f.create("notes")
	held := queueGrant(f, repo.ID, limits.Fetch, false)
	ctx, cancel := context.WithCancel(deadline(t))
	done := startRequest(ctx, f, "GET", "/notes.git/info/refs?service=git-upload-pack", "")
	f.clock.take(t)
	cancel()
	finishRequest(t, done)
	same(t, f.limits.Pressure().Read.Queued, int64(0))
	same(t, len(ownEvents(f.events())), 0)
	same(t, len(f.clock.timers), 0)
	held.Release()
	assertIdle(t, f, repo.ID)
}

func TestWaitAndRepositoryReresolution(t *testing.T) {
	// R-V3W0-VEJM R-VB7F-60ZS R-951S-C0BS R-99XD-V3AK
	for _, change := range []string{"rename", "delete", "close"} {
		for _, method := range []string{"GET", "POST"} {
			t.Run(change+method, func(t *testing.T) {
				f := setup(t)
				repo := f.create("notes")
				op, path, contentType, body := limits.Fetch, "/notes.git/info/refs?service=git-upload-pack", "application/x-git-upload-pack-advertisement", ""
				if method == "POST" {
					op, path, contentType, body = limits.Push, "/notes.git/git-receive-pack", "application/x-git-receive-pack-result", "0000"
				}
				held := queueGrant(f, "other", op, false)
				done := startRequest(deadline(t), f, method, path, body)
				f.clock.take(t)
				f.clock.advance(1234 * time.Microsecond)
				switch change {
				case "rename":
					_, err := f.store.Rename(deadline(t), repo.ID, "renamed")
					must(t, err)
				case "delete":
					must(t, f.store.Delete(deadline(t), repo.ID))
				case "close":
					f.db.SetFailing(true)
				}
				held.Release()
				w := finishRequest(t, done)
				switch change {
				case "rename":
					same(t, w.Code, 200)
					same(t, w.Header().Get("Content-Type"), contentType)
				case "delete":
					outcome(t, w, 404, smarthttp.RepoNotFound+"\n")
				case "close":
					outcome(t, w, 500, smarthttp.Unreachable+"\n")
				}
				events := ownEvents(f.events())
				if change == "rename" {
					same(t, len(events), 1)
					same(t, events[0].Name, "operation.waited")
					same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": string(op), "wait_us": int64(1234)})
					same(t, events[0].User, "alice")
					same(t, events[0].RequestID, "queued")
					same(t, len(f.clock.timers), 1)
				} else {
					same(t, len(events), 0)
					same(t, len(f.clock.timers), 0)
				}
				assertIdle(t, f, repo.ID)
			})
		}
	}
}

func TestWaitEventBeforeFetchAndWithinRequestDuration(t *testing.T) {
	// R-VB7F-60ZS R-5H86-W3QH R-99XD-V3AK
	f := setup(t)
	repo := f.create("notes")
	// This test gives middleware and limits the same monotonic time source.
	f.writer.Shutdown(deadline(t), "replace")
	f.capture = new(telemetry.Capture)
	f.writer = telemetry.New(telemetry.Config{Service: "repos", Sink: f.capture, Now: f.clock.read, Rand: new(sequence)})
	t.Cleanup(func() { f.writer.Shutdown(deadline(t), "test") })
	held := queueGrant(f, repo.ID, limits.Fetch, false)
	done := startRequest(deadline(t), f, "POST", "/notes.git/git-upload-pack", "0000")
	f.clock.take(t)
	f.clock.advance(3 * time.Second)
	held.Release()
	w := finishRequest(t, done)
	same(t, w.Code, 200)
	events := f.events()
	waitIndex, fetchIndex := -1, -1
	var waitUS, durationUS int64
	for i, e := range events {
		switch e.Name {
		case "operation.waited":
			waitIndex = i
			waitUS = e.Attrs["wait_us"].(int64)
			same(t, e.RequestID, "queued")
			same(t, e.User, "alice")
			same(t, e.Attrs["repo"], repo.ID)
		case "repo.fetched":
			fetchIndex = i
		case "request.finished":
			durationUS = e.Attrs["duration_us"].(int64)
		}
	}
	if waitIndex < 0 || fetchIndex <= waitIndex {
		t.Fatalf("wrong event order %v", events)
	}
	same(t, waitUS, int64(3*time.Second/time.Microsecond))
	if waitUS > durationUS {
		t.Fatalf("wait %d exceeds request %d", waitUS, durationUS)
	}
}

func TestSizePreflightBeforeFullWriteQueueAndDrain(t *testing.T) {
	// R-V53X-96AB R-V9ZI-S993
	f := setup(t)
	repo := f.create("notes")
	f.settings.RepoMaxBytes = 1
	f.resetLimits()
	held := queueGrant(f, "other", limits.Push, false)
	defer held.Release()
	waiting := make(chan error, 1)
	go func() {
		g, err := f.limits.Acquire(deadline(t), "third", limits.Push, false)
		if g != nil {
			g.Release()
		}
		waiting <- err
	}()
	f.clock.take(t)
	same(t, f.limits.Pressure().Write.Queued, int64(1))
	for _, state := range []string{"full", "draining"} {
		if state == "draining" {
			f.limits.Drain()
			ctx := deadline(t)
			select {
			case err := <-waiting:
				if err == nil {
					t.Fatal("drained waiter was granted")
				}
			case <-ctx.Done():
				t.Fatal("drained waiter did not return")
			}
		}
		before := len(f.events())
		for _, path := range []string{"/notes.git/info/refs?service=git-receive-pack", "/notes.git/git-receive-pack"} {
			method := "GET"
			if strings.HasSuffix(path, "/git-receive-pack") {
				method = "POST"
			}
			w := f.request(method, path, strings.NewReader("0000"))
			outcome(t, w, 507, fmt.Sprintf(smarthttp.AtSizeLimit, 1)+"\n")
			same(t, w.Header().Get("Retry-After"), "")
		}
		events := ownEvents(f.events()[before:])
		same(t, len(events), 2)
		for _, event := range events {
			same(t, event.Name, "operation.rejected")
			same(t, event.Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "push", "limit": "repo_max_bytes"})
		}
		same(t, len(f.clock.timers), 0)
	}
}
