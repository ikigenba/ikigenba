package passes_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-2G9X-RDSN R-3284-N955
func TestJudgeWalkUsesPassTimeAcrossPages(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	var clock atomic.Int64
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(100000+clock.Add(1), 0) }, Rand: &random{}})
	ids := seedEmails(t, f, 2*passes.BatchSize+1, func(int) string { return "" })
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Telemetry: f.w, Events: f.em})
	p, err := f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Judged != int64(len(ids)) {
		t.Fatal(p)
	}
	for _, id := range ids {
		j, err := f.st.GetJudgments(t.Context(), id)
		if err != nil || !j.Calculated.Equal(p.Started) {
			t.Fatal(id, j, err, p.Started)
		}
	}
}

// R-W9OP-JMRU
func TestWebsiteWalkContinuesAcrossEmptyDuePages(t *testing.T) {
	f := newFixture(t, nil, false)
	ids := seedEmails(t, f, 2*passes.BatchSize, func(int) string { return "ops@late.test" })
	sort.Strings(ids)
	found, err := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://late.test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[:len(ids)-1] {
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "late.test", Source: "late.test", Found: found}}); err != nil {
			t.Fatal(err)
		}
	}
	f.checker(func(r *http.Request) (*http.Response, error) { return pageResponse(r), nil })
	p := f.enrich(sources.WebsiteCheck)
	if p.Due != 1 || p.HTTPS != 1 || p.Fetched != 1 {
		t.Fatal(p)
	}
	es, err := f.st.Enrichments(t.Context(), ids[len(ids)-1])
	if err != nil || len(es) != 1 {
		t.Fatal(es, err)
	}
}

type diagnosticWriter struct {
	writes  [][]byte
	restore func()
}

func (w *diagnosticWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, append([]byte(nil), p...))
	w.restore()
	return len(p), nil
}

// R-2UWQ-CMOZ R-3EF4-GYK3
func TestStoreFailureDiagnosticPrecedesEnding(t *testing.T) {
	t.Parallel()

	var f *fixture
	var failingText string
	f = newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			record(w, "diagnostic-run", 1, store.StatusDone)
			return
		}
		f.d.SetFailing(true)
		err := f.d.Write(t.Context(), func(*sql.Tx) error { return nil })
		if err != nil {
			failingText = err.Error()
		}
		row(w, 1)
	}, true)
	writer := &diagnosticWriter{restore: func() { f.d.SetFailing(false) }}
	cfg := passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em}
	cfg.Stderr = writer
	f.eng = passes.New(cfg)
	p, err := f.eng.Ingest(context.Background(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusFailed || p.Reason != store.ReasonStore || failedCounts(p) != (store.Counts{}) || len(writer.writes) != 1 {
		t.Fatal(p, writer.writes)
	}
	text := string(writer.writes[0])
	prefix := "candidates: pass " + p.ID + " failed: store\n\n"
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 3 || !strings.Contains(text, strings.ReplaceAll(failingText, "\n", " ")) {
		t.Fatal(text)
	}
}

// R-3284-N955 R-2HHU-55JC
func TestIngestMarksGoneAcrossAllPages(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			record(w, "run_a", 1, store.StatusDone)
			return
		}
		row(w, 2*passes.BatchSize+10)
	}, true)
	ids := seedEmails(t, f, 2*passes.BatchSize+1, func(int) string { return "" })
	if got := f.delivery("gone-event", "run_a"); got != events.OK() {
		t.Fatal(got)
	}
	var p store.Pass
	for _, v := range f.list() {
		if v.Trigger == "run_a" {
			p = v
		}
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Gone != int64(len(ids)) {
		t.Fatal(p)
	}
	for _, id := range ids {
		ls, err := f.st.Links(t.Context(), id)
		if err != nil || len(ls) != 1 || !ls[0].Gone {
			t.Fatal(id, ls, err)
		}
	}
}

// R-2UWQ-CMOZ
func TestStoreDiagnosticSurvivesUnrecordableEnding(t *testing.T) {
	t.Parallel()
	var f *fixture
	f = newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			record(w, "unrecorded-run", 1, store.StatusDone)
			return
		}
		f.d.SetFailing(true)
		row(w, 1)
	}, true)
	written := make(chan struct{})
	writer := &diagnosticWriter{restore: func() { close(written) }}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Stderr: writer})
	p, err := f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	<-written
	f.eng.Drain(context.Background())
	f.d.SetFailing(false)
	saved, err := f.st.GetPass(t.Context(), p.ID)
	if err != nil || saved.Status != store.StatusRunning || len(writer.writes) != 1 || !strings.HasPrefix(string(writer.writes[0]), "candidates: pass "+p.ID+" failed: store\n\n") {
		t.Fatal(saved, err, writer.writes)
	}
}

// R-2UWQ-CMOZ
func TestNonStoreEndingsLeaveDiagnosticWriterSilent(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"done", "scrapes", "interrupted", "prompts"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/current") {
					if mode == "scrapes" {
						w.WriteHeader(500)
						return
					}
					record(w, "silent-run", 1, store.StatusDone)
					return
				}
				if mode == "interrupted" {
					close(entered)
					<-r.Context().Done()
					return
				}
				row(w, 1)
			}, true)
			writer := &diagnosticWriter{restore: func() {}}
			cfg := passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Stderr: writer}
			var target string
			if mode == "prompts" {
				target = seedTarget(t, f, 1)[0]
				pc, _ := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
					if name == "result" {
						return nil, false, 500
					}
					return promptAnswer(name, args)
				})
				cfg.Prompts = pc
				cfg.After = func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- time.Time{}; return ch }
			}
			f.eng = passes.New(cfg)
			var p store.Pass
			var err error
			if mode == "prompts" {
				p, err = f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindSiteText, target, false)
			} else {
				p, err = f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "interrupted" {
				<-entered
				f.eng.Drain(context.Background())
			}
			p = f.ended(p.ID)
			want := ""
			if mode != "done" {
				want = mode
			}
			if p.Reason != want || len(writer.writes) != 0 {
				t.Fatal(p, writer.writes)
			}
		})
	}
}
