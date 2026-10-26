package cli_test

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-GPDH-384Z R-XQKF-BRHF
func TestRunPrunesEveryPageBeforeReadyAndAtSweep(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	h.env["CANDIDATES_KEEP_PASSES"] = "1"
	d, _ := runStore(t, h.p.Dir, h.p.Now)
	st := store.New(d, store.Config{Now: h.p.Now, Rand: &secrecyBytes{}})
	seed := func() []string {
		t.Helper()
		ids := []string{}
		for i := 0; i < 2*passes.BatchSize+2; i++ {
			p, err := st.AddPass(t.Context(), store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, p.ID)
		}
		return ids
	}
	assert := func(ids []string) {
		t.Helper()
		for i, id := range ids {
			r, _ := h.call(identity.Caller{UserID: "reader"}, "pass", map[string]any{"pass": id})
			if i == len(ids)-1 {
				if r.IsError() {
					t.Fatal("newest pass removed", id)
				}
			} else {
				runAssertNotFound(t, r)
			}
		}
	}
	before := seed()
	h.start()
	assert(before)
	timer := runTimer(t, h)
	later := seed()
	timer <- h.p.Now()
	runTimer(t, h)
	assert(later)
	h.stop()
}

// R-U6CJ-6RQQ
func TestRunPassRetriesBusyWriteWithProcessSleep(t *testing.T) {
	held := make(chan struct{})
	fake := &runFake{rows: []map[string]any{runRow()}, hold: held, arrived: make(chan struct{}, 1)}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, fake)}))
	pauses := make(chan time.Duration, 1)
	release := make(chan struct{})
	h.p.Sleep = func(_ context.Context, d time.Duration) { pauses <- d; close(release) }
	h.start()
	c := identity.Caller{UserID: "owner"}
	_, v := h.call(c, "ingest", map[string]any{})
	id := runText(v, "id")
	<-fake.arrived
	d, _ := runStore(t, h.p.Dir, h.p.Now)
	locked := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- d.Write(context.Background(), func(*sql.Tx) error { close(locked); <-release; return nil })
	}()
	<-locked
	close(held)
	got := <-pauses
	if got != store.WriteBackoff {
		t.Fatal(got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p := h.waitPass(c, id)
	if runText(p, "status") != store.StatusDone {
		t.Fatal(p)
	}
	h.stop()
}

type failNextBytes struct {
	next  atomic.Bool
	bytes secrecyBytes
}

func (r *failNextBytes) Read(p []byte) (int, error) {
	if r.next.Swap(false) {
		return 0, io.ErrUnexpectedEOF
	}
	return r.bytes.Read(p)
}

// R-KDES-2YHL R-HMAR-F15Q
func TestRunForwardsStoreFailureDiagnostic(t *testing.T) {
	rnd := &failNextBytes{}
	fake := &runFake{rows: []map[string]any{runRow()}}
	socket := runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/rows") {
			rnd.next.Store(true)
		}
		fake.ServeHTTP(w, r)
	}))
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": socket}))
	h.p.Rand = rnd
	h.start()
	c := identity.Caller{UserID: "owner", RequestID: "failure-request"}
	_, v := h.call(c, "ingest", map[string]any{})
	id := runText(v, "id")
	p := h.waitPass(c, id)
	if runText(p, "status") != store.StatusFailed || runText(p, "reason") != store.ReasonStore {
		t.Fatal(p)
	}
	h.stop()
	text := h.err.String()
	if h.out.String() != "" || h.err.writes != 1 || !strings.HasPrefix(text, "candidates: pass "+id+" failed: store\n\n") || strings.Count(text, "\n") != 3 || !strings.Contains(text, store.ErrUnreachable.Error()) {
		t.Fatal(text, h.err.writes)
	}
}
