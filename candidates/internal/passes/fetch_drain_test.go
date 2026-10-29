package passes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-Q3XB-GX8X R-373Q-6C3X R-YJKZ-96BR
func TestDrainReleasesCompletedChecksWaitingToPersist(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ctx := context.Background()
	ids := seedEmails(t, f, website.Concurrency, func(i int) string {
		name := "candidate" + strconv.Itoa(i)
		return name + "@" + name + ".test"
	})
	found, err := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://old.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[0], Kind: sources.WebsiteCheck, Inputs: "old.test", Source: "old.test", Found: found}}); err != nil {
		t.Fatal(err)
	}
	before, err := f.st.Enrichments(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	releaseWrite := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseWrite)
	var calls atomic.Int64
	now := func() time.Time {
		// The pass's admission reads first; its first result write reads next.
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		return time.Unix(100000, 0)
	}
	f.st = store.New(f.d, store.Config{Now: now, Rand: &random{}})
	completed := make(chan struct{})
	var requests atomic.Int64
	checker := &website.Checker{After: fastAfter, Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == website.Concurrency {
			close(completed)
		}
		return pageResponse(r), nil
	})}}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Website: checker, Telemetry: f.w, Events: f.em})
	p, err := f.eng.Enrich(ctx, identity.Caller{UserID: "owner"}, sources.WebsiteCheck)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	waitSignal(t, completed)
	drainCtx, cancel := context.WithCancel(ctx)
	cancel()
	f.eng.Drain(drainCtx)
	releaseWrite()
	p = f.ended(p.ID)
	if p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted {
		t.Fatal(p)
	}
	for _, id := range ids {
		es, err := f.st.Enrichments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if id == ids[0] {
			if !reflect.DeepEqual(es, before) {
				t.Fatal(es, before)
			}
		} else if len(es) != 0 {
			t.Fatal(es)
		}
	}
}
