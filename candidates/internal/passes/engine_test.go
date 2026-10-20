package passes_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

type random struct{ n uint64 }

func (r *random) Read(b []byte) (int, error) {
	r.n++
	var block [8]byte
	binary.LittleEndian.PutUint64(block[:], r.n)
	for i := range b {
		b[i] = block[i%8]
	}
	return len(b), nil
}

type request struct{ path, user, email, id string }

// lifecycleSink wakes observers after the capture contains a lifecycle event.
// Waiting tests do not compete with the worker for SQLite read connections.
type lifecycleSink struct {
	capture *events.Capture
	changed chan struct{}
}

func (s lifecycleSink) Deliver(ctx context.Context, event events.Event) error {
	if err := s.capture.Deliver(ctx, event); err != nil {
		return err
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
	return nil
}

type fixture struct {
	sc      *scrapes.Client
	t       *testing.T
	d       *db.DB
	st      *store.Store
	eng     *passes.Engine
	w       *telemetry.Writer
	em      *events.Emitter
	tc      *telemetry.Capture
	ec      *events.Capture
	mu      sync.Mutex
	reqs    []request
	changed chan struct{}
}

func newFixture(t *testing.T, h http.HandlerFunc, available bool) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{t: t, tc: &telemetry.Capture{}, ec: &events.Capture{}, changed: make(chan struct{}, 1)}
	now := func() time.Time { return time.Unix(100000, 0) }
	pause := func(context.Context, time.Duration) {}
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "candidates.db"), Migrations: candidates.Migrations(), Now: now, Service: "candidates"})
	if err != nil {
		t.Fatal(err)
	}
	f.st = store.New(f.d, store.Config{Now: now, Rand: &random{}})
	f.w = telemetry.New(telemetry.Config{Service: "candidates", Sink: f.tc, Now: now, Sleep: pause, Rand: &random{}})
	f.em = events.New(events.Config{Service: "candidates", Sink: lifecycleSink{f.ec, f.changed}, Now: now, Sleep: pause, Rand: &random{}, Emits: trail.Emits()})
	var sc *scrapes.Client
	if available {
		dir, err := os.MkdirTemp("", "pass-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		sock := filepath.Join(dir, "s")
		if h != nil {
			ln, err := net.Listen("unix", sock)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				f.mu.Lock()
				f.reqs = append(f.reqs, request{r.URL.Path, r.Header.Get("X-User-Id"), r.Header.Get("X-User-Email"), r.Header.Get("X-Request-Id")})
				f.mu.Unlock()
				h(w, r)
			})}
			go func() { _ = server.Serve(ln) }()
			t.Cleanup(func() { _ = server.Close() })
		}
		sc = scrapes.NewClient(scrapes.ClientConfig{Service: services.Entry{Name: "scrapes", Socket: sock}, Telemetry: f.w})
	}
	f.sc = sc
	f.eng = passes.New(passes.Config{f.st, sources.New(fmcsa.New()), sc, nil, f.w, f.em, nil, nil})
	t.Cleanup(func() {
		f.eng.Drain(context.Background())
		f.em.Shutdown(context.Background())
		f.w.Shutdown(context.Background(), "test")
		_ = f.d.Close()
	})
	return f
}
func (f *fixture) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.reqs...)
}
func (f *fixture) flush() {
	f.t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	if err := f.em.Flush(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) list() []store.Pass {
	f.t.Helper()
	ps, err := f.st.ListPasses(context.Background(), "", "", 1000)
	if err != nil {
		f.t.Fatal(err)
	}
	return ps
}
func (f *fixture) ended(id string) store.Pass {
	f.t.Helper()
	return f.endedWithin(id, 30*time.Second)
}
func (f *fixture) endedWithin(id string, budget time.Duration) store.Pass {
	f.t.Helper()
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	for {
		for _, event := range f.ec.Events() {
			if event.Name != trail.Finished || event.Attrs["pass"] != id {
				continue
			}
			p, err := f.st.GetPass(context.Background(), id)
			if err != nil {
				f.t.Fatal(err)
			}
			if p.Status == store.StatusRunning {
				f.t.Fatal("ending event preceded stored ending")
			}
			f.flush()
			return p
		}
		select {
		case <-deadline.C:
			f.t.Fatal("pass never ended")
		case <-f.changed:
		}
	}
}

func (f *fixture) delivery(id, run string) events.Outcome {
	return f.eng.Deliver(events.NewContext(identity.NewContext(context.Background(), identity.Caller{RequestID: "delivery_request"}), events.Cause{ID: "evt_0101010101010101", Depth: 2}), events.Delivery{Event: events.Event{ID: id, Name: trail.Accepted, Attrs: events.Attrs{"scraper": "fmcsa", "run": run, "status": "done"}}})
}
func record(w http.ResponseWriter, id string, rows int, status string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "scraper": "fmcsa", "status": status, "started": "2000-01-01T00:00:00Z", "rows": rows, "new": 0, "changed": 0, "unchanged": 0, "gone": 0})
}
func row(w io.Writer, i int) {
	_ = json.NewEncoder(w).Encode(map[string]any{"scraper": "fmcsa", "run": "run_a", "source_id": fmt.Sprint(i), "digest": fmt.Sprint(i), "name": fmt.Sprintf("Business %d", i), "names": []string{}, "collected": "2000-01-01T00:00:00Z", "people": []any{}, "facts": map[string]any{}, "raw": map[string]any{}})
}
func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(30 * time.Second):
		t.Fatal("missing synchronization signal")
	}
}
func assertRefusal(t *testing.T, p store.Pass, err error, word string) {
	t.Helper()
	var rf *passes.Refusal
	if !reflect.DeepEqual(p, store.Pass{}) || !errors.As(err, &rf) || rf.Word != word {
		t.Fatalf("refusal %v %v", p, err)
	}
}

// R-SBQH-7IGY R-84KF-D8N0 R-85SB-R0DP R-SE69-Z1YC R-SFE6-CTP1
func TestRecordContract(t *testing.T) {
	if passes.BatchSize != 500 || passes.Busy == "" || passes.Stopping == "" || passes.Busy == passes.Stopping || strings.Contains(passes.Busy+passes.Stopping, "\n") {
		t.Fatal("constants")
	}
	for _, word := range []string{passes.RefusalNotFound, passes.RefusalInvalid, passes.RefusalBusy, passes.RefusalStopping, passes.RefusalUnavailable} {
		if (&passes.Refusal{Word: word}).Error() != word {
			t.Fatal(word)
		}
	}
	if !reflect.DeepEqual([]string{passes.RefusalNotFound, passes.RefusalInvalid, passes.RefusalBusy, passes.RefusalStopping, passes.RefusalUnavailable}, []string{"not_found", "invalid", "busy", "stopping", "unavailable"}) {
		t.Fatal("word")
	}
	start := time.Date(2001, 2, 3, 4, 5, 6, 0, time.FixedZone("east", 3600))
	p := store.Pass{ID: "id", Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, Status: store.StatusDone, Reason: "reason", Started: start, Finished: start.Add(time.Hour), Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Derived: 15, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, UserID: "user", UserEmail: "email"}
	r := passes.RecordOf(p)
	want := passes.Record{"id", store.KindRebuild, store.OriginRebuild, store.TriggerRebuild, "", "", store.StatusDone, "reason", start.UTC().Format(time.RFC3339), p.Finished.UTC().Format(time.RFC3339), 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 10, 11, 12, 13, 14, 0, 0, 0, 0, "user", "email"}
	if r != want {
		t.Fatalf("%#v", r)
	}
	got, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"id":"id","kind":"rebuild","origin":"rebuild","trigger":"rebuild","status":"done","reason":"reason","started":"2001-02-03T03:05:06Z","finished":"2001-02-03T04:05:06Z","rows":1,"new":2,"changed":3,"unchanged":4,"gone":5,"created":6,"linked":7,"judged":8,"unranged":9,"derived":15,"due":10,"fetched":11,"https":12,"http":13,"none":14,"runs":0,"runs_failed":0,"pages":0,"cost":0,"user":"user","email":"email"}`
	if string(got) != expected {
		t.Fatalf("%s", got)
	}
	p.Origin = store.OriginEnrich
	if got := passes.RecordOf(p); got.User != p.UserID || got.Email != p.UserEmail {
		t.Fatal(got)
	}
	p.Trigger = "run"
	p.Origin = store.OriginRun
	p.Finished = time.Time{}
	p.Reason = ""
	r = passes.RecordOf(p)
	if r.Finished != "" || r.User != "" || r.Email != "" {
		t.Fatal(r)
	}
	got, _ = json.Marshal(r)
	if bytes.Contains(got, []byte(`"finished"`)) || bytes.Contains(got, []byte(`"reason"`)) || bytes.Contains(got, []byte(`"user"`)) || bytes.Contains(got, []byte(`"email"`)) {
		t.Fatal(string(got))
	}
}

// R-YGG6-MDXZ R-SHTZ-4D6F R-YIVZ-DXFD R-YOZH-AS4U
func TestRefusals(t *testing.T) {
	for _, mode := range []string{"unavailable", "busy", "stopping", "store"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, mode != "unavailable")
			before := f.list()
			switch mode {
			case "busy":
				_, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
				before = f.list()
			case "stopping":
				f.eng.Drain(context.Background())
			case "store":
				f.d.SetFailing(true)
			}
			p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "user"})
			if mode == "store" {
				var rf *passes.Refusal
				if !errors.Is(err, store.ErrUnreachable) || errors.As(err, &rf) || p.ID != "" {
					t.Fatal(p, err)
				}
				f.d.SetFailing(false)
			} else {
				assertRefusal(t, p, err, mode)
			}
			if !reflect.DeepEqual(f.list(), before) || len(f.requests()) != 0 {
				t.Fatal("refusal side effects")
			}
			f.flush()
			if len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
				t.Fatal("refusal events")
			}
			if mode == "stopping" {
				if out := f.delivery("event", "run_a"); out != events.Fail(passes.Stopping) {
					t.Fatal(out)
				}
			}
		})
	}
}

// R-4XES-EI5W R-5I52-WLRP R-YMJO-J8NG R-ZT3Z-F1TD R-75XG-HDMP
func TestSkippedAndHandlers(t *testing.T) {
	f := newFixture(t, nil, false)
	handlers := f.eng.Handlers()
	if len(handlers) != 5 || handlers[trail.Accepted] == nil || handlers[trail.AcceptedJudge] == nil {
		t.Fatal(handlers)
	}
	variants := []events.Event{{Name: trail.AcceptedJudge}, {Name: "cron.other.fired", ID: "e"}, {ID: "e", Name: "scrape.started"}, {ID: "e", Name: trail.Accepted, Attrs: events.Attrs{"scraper": "fmcsa", "run": "run_a", "status": "failed"}}, {ID: "e", Name: trail.Accepted, Attrs: events.Attrs{"scraper": "other", "run": "run_a", "status": "done"}}, {ID: "e", Name: trail.Accepted, Attrs: events.Attrs{"scraper": "fmcsa", "status": "done"}}, {Name: trail.Accepted, Attrs: events.Attrs{"scraper": "fmcsa", "run": "run_a", "status": "done"}}, {ID: "e", Name: trail.Accepted, Attrs: events.Attrs{"scraper": "fmcsa", "run": "rebuild", "status": "done"}}, {ID: "e", Name: trail.Accepted, Attrs: events.Attrs{"scraper": 17, "run": "run_a", "status": "done"}}}
	for _, event := range variants {
		d := events.Delivery{Event: event}
		if f.eng.Deliver(context.Background(), d) != events.Skip() || handlers[trail.Accepted](context.Background(), d) != events.Skip() {
			t.Fatal(event)
		}
	}
	if len(f.list()) != 0 || len(f.requests()) != 0 {
		t.Fatal("skip side effects")
	}
	f.flush()
	if len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
		t.Fatal("skip events")
	}
	r := httptest.NewRecorder()
	events.DeclarationsHandler(f.em, passes.New(passes.Config{Sources: sources.New(fmcsa.New(), ppp.New())}).Handlers()).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/declarations", nil))
	want := `{"emits":[{"event":"pass.finished","attrs":["pass","kind","origin","trigger","target","enrich_kind","status","reason","rows","new","changed","unchanged","gone","created","linked","judged","rescored","unranged","derived","due","fetched","https","http","none","runs","runs_failed","pages","cost"]},{"event":"pass.started","attrs":["pass","kind","origin","trigger","target","enrich_kind"]}],"accepts":["campaign.member.added","campaign.member.rejected","campaign.piece.delivered","cron.judge_candidates.fired","scrape.finished"]}`
	if r.Code != 200 || r.Body.String() != want {
		t.Fatal(r.Code, r.Body.String())
	}
}

// R-YRFA-2BM8 R-JSZX-9PKC R-SYWK-H5K5 R-YSN6-G3CX
func TestDeliveryRecordAndRedelivery(t *testing.T) {
	for _, mode := range []string{"missing", "unfinished", "unavailable", "malformed", "nil", "done"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/rows") {
					return
				}
				switch mode {
				case "missing":
					w.WriteHeader(404)
				case "unfinished":
					record(w, "run_a", 0, "running")
				case "unavailable":
					w.WriteHeader(503)
				case "malformed":
					_, _ = io.WriteString(w, "{}")
				default:
					record(w, "run_a", 0, "done")
				}
			}, mode != "nil")
			got := f.delivery("event", "run_a")
			if mode == "missing" || mode == "unfinished" {
				if got != events.Skip() || len(f.list()) != 0 {
					t.Fatal(got, f.list())
				}
				return
			}
			if got != events.OK() {
				t.Fatal(got)
			}
			var ps []store.Pass
			for _, v := range f.list() {
				if v.Trigger == "run_a" {
					ps = append(ps, v)
				}
			}
			if len(ps) != 1 {
				t.Fatal(ps)
			}
			p := f.ended(ps[0].ID)
			want := store.StatusFailed
			if mode == "done" {
				want = store.StatusDone
			}
			if p.Trigger != "run_a" || p.Delivery != "event" || p.UserID != "" || p.RequestID != "delivery_request" || p.Status != want || p.UserEmail != "" {
				t.Fatal(p)
			}
			if want == store.StatusFailed && (p.Reason != store.ReasonScrapes || p.Rows != 0) {
				t.Fatal(p)
			}
			reqs := f.requests()
			if mode != "nil" {
				if reqs[0] != (request{"/runs/run_a/record", "candidates", "", "delivery_request"}) {
					t.Fatal(reqs)
				}
			}
			if f.delivery("event", "run_other") != events.Skip() || f.delivery("other", "run_a") != events.Skip() {
				t.Fatal("redelivery")
			}
			if !reflect.DeepEqual(reqs, f.requests()) {
				t.Fatal("redelivery read")
			}
			f.flush()
			n := 0
			for _, event := range f.ec.Events() {
				if event.Attrs["pass"] == p.ID {
					n++
				}
			}
			if n != 2 {
				t.Fatal(f.ec.Events())
			}
			f.d.SetFailing(true)
			if f.delivery("fresh", "fresh") != events.Fail(store.Unreachable) {
				t.Fatal("failed lookup")
			}
			f.d.SetFailing(false)
		})
	}
}

// R-ZPGA-9QLA R-YWAV-LEL0 R-YXIR-Z6BP R-2P2D-KR58 R-ZDDG-Y6YQ
func TestRebuildBackgroundIdentityAndEvents(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			close(entered)
			select {
			case <-release:
				record(w, "run_a", 1, "done")
			case <-r.Context().Done():
			}
			return
		}
		row(w, 1)
	}, true)
	ctx, cancel := context.WithCancel(events.NewContext(context.Background(), events.Cause{ID: "evt_0202020202020202", Depth: 3}))
	caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "request"}
	p, err := f.eng.Rebuild(ctx, caller)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	if p.Kind != store.KindRebuild || p.Origin != store.OriginRebuild || p.Status != store.StatusRunning || p.Trigger != store.TriggerRebuild || p.UserID != caller.UserID || p.UserEmail != caller.Email || p.RequestID != caller.RequestID || p.Delivery != "" {
		t.Fatal(p)
	}
	stored, err := f.st.GetPass(context.Background(), p.ID)
	if err != nil || stored.Status != store.StatusRunning {
		t.Fatal(stored, err)
	}
	f.flush()
	if len(f.ec.Events()) != 1 || f.ec.Events()[0].Name != trail.Started {
		t.Fatal(f.ec.Events())
	}
	cancel()
	close(release)
	end := f.ended(p.ID)
	if end.Status != store.StatusDone {
		t.Fatal(end)
	}
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].path != "/scrapers/fmcsa/current" || reqs[1].path != "/runs/run_a/rows" {
		t.Fatal(reqs)
	}
	for _, r := range reqs {
		if r.user != caller.UserID || r.email != caller.Email || r.id != caller.RequestID {
			t.Fatal(r)
		}
	}
	f.flush()
	ts, es := f.tc.Events(), f.ec.Events()
	if len(es) != 2 {
		t.Fatal(es)
	}
	for i, event := range es {
		if event.RequestID != caller.RequestID || event.User != caller.UserID || event.Cause != "evt_0202020202020202" || event.Depth != 4 || event.Attrs["pass"] != p.ID {
			t.Fatal(event)
		}
		if (i == 0 && event.Name != trail.Started) || (i == 1 && event.Name != trail.Finished) {
			t.Fatal(es)
		}
	}
	var lifecycle []telemetry.Event
	for _, event := range ts {
		if event.Name == trail.Started || event.Name == trail.Finished {
			lifecycle = append(lifecycle, event)
		}
	}
	if len(lifecycle) != 2 {
		t.Fatal(ts)
	}
	for i := range es {
		if es[i].Name != lifecycle[i].Name || !reflect.DeepEqual(es[i].Attrs, events.Attrs(lifecycle[i].Attrs)) {
			t.Fatal("different events")
		}
	}
	attrs := es[1].Attrs
	if attrs["status"] != end.Status || attrs["rows"] != end.Rows || attrs["new"] != end.New || attrs["changed"] != end.Changed || attrs["unchanged"] != end.Unchanged || attrs["gone"] != end.Gone || attrs["created"] != end.Created || attrs["linked"] != end.Linked || attrs["judged"] != end.Judged || attrs["unranged"] != end.Unranged || attrs["reason"] != end.Reason {
		t.Fatal(attrs, end)
	}
}

// R-2TXZ-3U40 R-2SQ2-Q2DB R-2P2D-KR58 R-SWGR-PM2R R-Z3M9-W116 R-GU9F-TVQY
func TestResolutionAndRebuild(t *testing.T) {
	var stateMu sync.Mutex
	rows := 3
	runID := "run_a"
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		n, id := rows, runID
		stateMu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/record") || strings.HasSuffix(r.URL.Path, "/current") {
			record(w, id, n, "done")
			return
		}
		for i := 1; i <= n; i++ {
			row(w, i)
		}
	}, true)
	if out := f.delivery("e1", "run_a"); out != events.OK() {
		t.Fatal(out)
	}
	var initial store.Pass
	for _, v := range f.list() {
		if v.Trigger == "run_a" {
			initial = v
		}
	}
	first := f.ended(initial.ID)
	settleCostChain(t, f, first.Trigger, time.Now())
	if first.Status != store.StatusDone || first.Rows != 3 || first.New != 3 || first.Changed != 0 || first.Unchanged != 0 || first.Gone != 0 || first.Created+first.Linked != 3 || first.Judged != 0 || first.Unranged != 0 || first.Derived+first.Due+first.Fetched+first.HTTPS+first.HTTP+first.None+first.Runs+first.RunsFailed+first.Pages+first.Cost != 0 {
		t.Fatal(first)
	}
	cs, err := f.st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
		links, err := f.st.Links(context.Background(), c.ID)
		if err != nil || len(links) != 1 {
			t.Fatal(links, err)
		}
		l := links[0]
		if l.Run != "run_a" || l.Gone || l.Scraper != "fmcsa" || l.Digest != l.SourceID || l.Reading.Name != fmt.Sprintf("Business %s", l.SourceID) {
			t.Fatal(l)
		}
	}
	ruling, _, err := f.st.Decide(context.Background(), store.Ruling{Kind: store.KindInclude, Candidate: ids[0], Why: "test choice", UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	stateMu.Lock()
	rows = 2
	runID = "run_b"
	stateMu.Unlock()
	if f.delivery("e2", "run_b") != events.OK() {
		t.Fatal("second delivery")
	}
	var ing store.Pass
	for _, v := range f.list() {
		if v.Trigger == "run_b" {
			ing = v
		}
	}
	second := f.ended(ing.ID)
	settleCostChain(t, f, second.Trigger, time.Now())
	if second.Status != store.StatusDone || second.Gone != 1 || second.Unchanged != 2 {
		t.Fatal(second)
	}
	var gone int
	for _, id := range ids {
		ls, _ := f.st.Links(context.Background(), id)
		if ls[0].Gone {
			gone++
		}
	}
	if gone != 1 {
		t.Fatal("missing gone evidence")
	}
	stateMu.Lock()
	rows = 3
	runID = "run_c"
	stateMu.Unlock()
	p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	end := f.ended(p.ID)
	if end.Status != store.StatusDone || end.Created != 0 || end.Changed != 3 || end.Rows != 3 {
		t.Fatal(end)
	}
	for _, id := range ids {
		c, err := f.st.GetCandidate(context.Background(), id)
		if err != nil || c.ID != id {
			t.Fatal(c, err)
		}
		ls, _ := f.st.Links(context.Background(), id)
		if ls[0].Gone || ls[0].Run != "run_c" {
			t.Fatal(ls)
		}
		a, err := f.st.GetAssessment(context.Background(), id)
		if err != nil || a.Calculated.Before(p.Started) {
			t.Fatal(a, err)
		}
	}
	rs, err := f.st.Rulings(context.Background(), ids[0])
	if err != nil || len(rs) != 1 || !reflect.DeepEqual(rs[0], ruling) {
		t.Fatal(rs, err)
	}
}

// R-Z4U6-9SRV R-SF2K-0S3I R-Z9PR-SVQN R-2WDR-VDLE
func TestBatchStreamingAndFailureKeepsEvidence(t *testing.T) {
	released, firstBatch := make(chan struct{}), make(chan struct{})
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			record(w, "run_a", passes.BatchSize+2, "done")
			return
		}
		for i := 0; i < passes.BatchSize; i++ {
			row(w, i)
		}
		w.(http.Flusher).Flush()
		close(firstBatch)
		select {
		case <-released:
			row(w, passes.BatchSize)
			_, _ = io.WriteString(w, "{broken")
		case <-r.Context().Done():
		}
	}, true)
	seed := store.Observation{Scraper: "fmcsa", SourceID: "prior", Digest: "prior", Reading: sources.Reading{Name: "Previous", NameKey: "previous"}}
	_, priorIDs, err := f.st.Attach(context.Background(), "prior_run", false, []store.Observation{seed})
	if err != nil {
		t.Fatal(err)
	}
	if f.delivery("batch_event", "run_a") != events.OK() {
		t.Fatal("delivery")
	}
	p := f.list()[0]
	waitSignal(t, firstBatch)
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		cs, err := f.st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 1000)
		if err != nil {
			t.Fatal(err)
		}
		if len(cs) == passes.BatchSize+1 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("batch not attached before stream resumed")
		default:
			runtime.Gosched()
		}
	}
	still, _ := f.st.GetPass(context.Background(), p.ID)
	if still.Status != store.StatusRunning {
		t.Fatal(still)
	}
	close(released)
	end := f.ended(p.ID)
	if end.Status != store.StatusFailed || end.Reason != store.ReasonScrapes || failedCounts(end) != (store.Counts{}) {
		t.Fatal(end)
	}
	ls, err := f.st.Links(context.Background(), priorIDs[0])
	if err != nil || ls[0].Gone {
		t.Fatal(ls, err)
	}
	cs, _ := f.st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 1000)
	seen := 0
	for _, c := range cs {
		ls, _ := f.st.Links(context.Background(), c.ID)
		for _, l := range ls {
			if l.SourceID != "prior" {
				seen++
				if l.Run != "run_a" || l.Gone || l.Digest != l.SourceID {
					t.Fatal(l)
				}
			}
		}
	}
	if seen < passes.BatchSize {
		t.Fatal(seen)
	}
}

// R-Z9PR-SVQN R-SF2K-0S3I R-2WDR-VDLE
func TestFailedReads(t *testing.T) {
	for _, mode := range []string{"short", "500", "malformed", "not_done", "missing_current", "dead"} {
		t.Run(mode, func(t *testing.T) {
			var h http.HandlerFunc
			if mode != "dead" {
				h = func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/current") {
						switch mode {
						case "missing_current":
							w.WriteHeader(404)
						case "not_done":
							record(w, "run_a", 3, "running")
						default:
							record(w, "run_a", 3, "done")
						}
						return
					}
					switch mode {
					case "500":
						w.WriteHeader(500)
					case "malformed":
						_, _ = io.WriteString(w, "{}\n")
					default:
						row(w, 1)
						row(w, 2)
					}
				}
			}
			f := newFixture(t, h, true)
			_, ids, err := f.st.Attach(context.Background(), "prior", false, []store.Observation{{Scraper: "fmcsa", SourceID: "third", Digest: "third", Reading: sources.Reading{Name: "prior"}}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
			if err != nil {
				t.Fatal(err)
			}
			end := f.ended(p.ID)
			if mode == "missing_current" {
				if end.Status != store.StatusDone || end.Rows != 0 || end.Judged != 1 {
					t.Fatal(end)
				}
			} else {
				if end.Status != store.StatusFailed || end.Reason != store.ReasonScrapes || failedCounts(end) != (store.Counts{}) {
					t.Fatal(end)
				}
			}
			ls, _ := f.st.Links(context.Background(), ids[0])
			if ls[0].Gone {
				t.Fatal(ls)
			}
		})
	}
}

// R-T9VN-X38E R-ZH16-3I6T R-2WDR-VDLE R-YOZH-AS4U
func TestDrainCancelsReadsAndPendingDelivery(t *testing.T) {
	for _, which := range []string{"rows", "record"} {
		t.Run(which, func(t *testing.T) {
			entered := make(chan struct{})
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/current") {
					record(w, "run_a", 0, "done")
					return
				}
				if which == "rows" && strings.HasSuffix(r.URL.Path, "/record") {
					record(w, "run_a", 0, "done")
					return
				}
				close(entered)
				<-r.Context().Done()
			}, true)
			delivered := make(chan events.Outcome, 1)
			var p store.Pass
			if which == "rows" {
				var err error
				p, err = f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				go func() { delivered <- f.delivery("pending", "run_a") }()
			}
			waitSignal(t, entered)
			drained := make(chan struct{})
			go func() { f.eng.Drain(context.Background()); close(drained) }()
			waitSignal(t, drained)
			if which == "rows" {
				p = f.ended(p.ID)
				if p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted || failedCounts(p) != (store.Counts{}) {
					t.Fatal(p)
				}
				f.flush()
				if len(f.ec.Events()) != 2 || f.ec.Events()[1].Name != trail.Finished {
					t.Fatal(f.ec.Events())
				}
			} else {
				select {
				case out := <-delivered:
					if out != events.Fail(passes.Stopping) {
						t.Fatal(out)
					}
				case <-time.After(30 * time.Second):
					t.Fatal("pending record read not cancelled")
				}
				if len(f.list()) != 0 {
					t.Fatal("recorded after drain")
				}
			}
			rp, err := f.eng.Rebuild(context.Background(), identity.Caller{})
			assertRefusal(t, rp, err, passes.RefusalStopping)
			if f.delivery("post", "run_b") != events.Fail(passes.Stopping) {
				t.Fatal("delivery after drain")
			}
		})
	}
}

// R-ZELD-BYPF R-ZC5K-KF81 R-ZH16-3I6T
func TestRecoverAndUnrecordableEnding(t *testing.T) {
	f := newFixture(t, nil, false)
	original, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "old_run", Delivery: "old_event", UserID: "old_user", RequestID: "old_request"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := events.NewContext(context.Background(), events.Cause{ID: "evt_0404040404040404", Depth: 2})
	if err = f.eng.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := f.st.GetPass(context.Background(), original.ID)
	if err != nil || p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted {
		t.Fatal(p, err)
	}
	f.flush()
	es := f.ec.Events()
	if len(es) != 1 || es[0].Name != trail.Finished || es[0].Cause != "" || es[0].Depth != 0 || es[0].User != original.UserID || es[0].RequestID != original.RequestID || len(f.requests()) != 0 {
		t.Fatal(es)
	}
	f.d.SetFailing(true)
	if err = f.eng.Recover(context.Background()); !errors.Is(err, store.ErrUnreachable) {
		t.Fatal(err)
	}
	f.d.SetFailing(false)
	t.Run("ending", func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/current") {
				close(entered)
				select {
				case <-release:
					record(w, "run_a", 0, "done")
				case <-r.Context().Done():
				}
				return
			}
		}, true)
		p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		waitSignal(t, entered)
		f.d.SetFailing(true)
		close(release)
		done := make(chan struct{})
		go func() { f.eng.Drain(context.Background()); close(done) }()
		waitSignal(t, done)
		f.d.SetFailing(false)
		stored, err := f.st.GetPass(context.Background(), p.ID)
		if err != nil || stored.Status != store.StatusRunning {
			t.Fatal(stored, err)
		}
		f.flush()
		if len(f.ec.Events()) != 1 || f.ec.Events()[0].Name != trail.Started {
			t.Fatal(f.ec.Events())
		}
		if err = f.eng.Recover(context.Background()); err != nil {
			t.Fatal(err)
		}
		if f.ended(p.ID).Reason != store.ReasonInterrupted {
			t.Fatal("not recovered")
		}
	})
}

// R-TB3K-AUZ3 R-YGG6-MDXZ R-SHTZ-4D6F R-SYWK-H5K5
func TestConcurrentStarts(t *testing.T) {
	entered := make(chan struct{}, 32)
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			record(w, "other", 0, "done")
			return
		}
		entered <- struct{}{}
		<-r.Context().Done()
	}, true)
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			_, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			var rf *passes.Refusal
			if !errors.As(err, &rf) || rf.Word != passes.RefusalBusy {
				t.Fatal(err)
			}
		}
	}
	if successes != 1 || len(f.list()) != 1 {
		t.Fatal(successes, f.list())
	}
	waitSignal(t, entered)
	if f.delivery("race_event", "other") != events.OK() {
		t.Fatal("independent ingest refused")
	}
	wg.Go(func() { f.eng.Drain(context.Background()) })
	wg.Go(func() { _ = f.eng.Recover(context.Background()) })
	wg.Go(func() { _, _ = f.eng.Rebuild(context.Background(), identity.Caller{}) })
	wg.Go(func() { _ = f.delivery("after", "another") })
	wg.Wait()
	if ps := f.list(); len(ps) != 2 || ps[0].Status != store.StatusFailed || ps[0].Reason != store.ReasonInterrupted || ps[1].Status != store.StatusFailed || ps[1].Reason != store.ReasonInterrupted {
		t.Fatal(ps)
	}
}

type failCandidateRandom struct{ first bool }

func (r *failCandidateRandom) Read(b []byte) (int, error) {
	if r.first {
		return 0, io.ErrUnexpectedEOF
	}
	r.first = true
	for i := range b {
		b[i] = 0x5a
	}
	return len(b), nil
}

// R-2WDR-VDLE
func TestStoreFailureReason(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			record(w, "run_a", 1, "done")
			return
		}
		row(w, 1)
	}, true)
	f.eng.Drain(context.Background())
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(100000, 0) }, Rand: &failCandidateRandom{}})
	f.eng = passes.New(passes.Config{f.st, sources.New(fmcsa.New()), f.sc, nil, f.w, f.em, nil, nil})
	p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusFailed || p.Reason != store.ReasonStore || failedCounts(p) != (store.Counts{}) {
		t.Fatal(p)
	}
}

// R-SHTZ-4D6F
func TestRefusalPrecedence(t *testing.T) {
	f := newFixture(t, nil, false)
	_, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "existing", Delivery: "existing_event"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.eng.Rebuild(context.Background(), identity.Caller{})
	assertRefusal(t, p, err, passes.RefusalUnavailable)
	f.eng.Drain(context.Background())
	p, err = f.eng.Rebuild(context.Background(), identity.Caller{})
	assertRefusal(t, p, err, passes.RefusalStopping)
	if len(f.list()) != 1 || len(f.requests()) != 0 {
		t.Fatal("refusal effects")
	}
	f.flush()
	if len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
		t.Fatal("refusal events")
	}
}

// R-YWAV-LEL0 R-YXIR-Z6BP R-SYWK-H5K5
func TestDeliveryOutlivesStartingContext(t *testing.T) {
	for _, alreadyCancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(alreadyCancelled), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/record") {
					record(w, "run_a", 1, "done")
					return
				}
				close(entered)
				select {
				case <-release:
					row(w, 1)
				case <-r.Context().Done():
				}
			}, true)
			ctx, cancel := context.WithCancel(identity.NewContext(context.Background(), identity.Caller{RequestID: "delivery_request"}))
			defer cancel()
			if alreadyCancelled {
				cancel()
			}
			out := f.eng.Deliver(ctx, events.Delivery{Event: events.Event{ID: "evt_0505050505050505", Name: trail.Accepted, Attrs: events.Attrs{"scraper": "fmcsa", "run": "run_a", "status": "done"}}})
			if out != events.OK() {
				t.Fatal(out)
			}
			waitSignal(t, entered)
			p := f.list()[0]
			if p.Status != store.StatusRunning {
				t.Fatal(p)
			}
			cancel()
			close(release)
			if end := f.ended(p.ID); end.Status != store.StatusDone {
				t.Fatal(end)
			}
			reqs := f.requests()
			if len(reqs) != 2 || reqs[0].path != "/runs/run_a/record" || reqs[1].path != "/runs/run_a/rows" {
				t.Fatal(reqs)
			}
			for _, r := range reqs {
				if r.user != "candidates" || r.email != "" || r.id != "delivery_request" {
					t.Fatal(r)
				}
			}
		})
	}
}

// R-ZH16-3I6T
func TestDrainDeadlineWhileEndingHeld(t *testing.T) {
	entered := make(chan struct{})
	f := newFixture(t, func(_ http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }, true)
	f.eng.Drain(context.Background())
	ending, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var calls atomic.Int64
	f.st = store.New(f.d, store.Config{Now: func() time.Time {
		if calls.Add(1) == 2 {
			close(ending)
			<-release
		}
		return time.Unix(100000, 0)
	}, Rand: &random{}})
	f.eng = passes.New(passes.Config{f.st, sources.New(fmcsa.New()), f.sc, nil, f.w, f.em, nil, nil})
	p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drained := make(chan struct{})
	go func() { f.eng.Drain(ctx); close(drained) }()
	waitSignal(t, ending)
	select {
	case <-drained:
		t.Fatal("drain returned before ending or deadline")
	default:
	}
	cancel()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("drain exceeded cancelled deadline")
	}
	releaseOnce.Do(func() { close(release) })
	if end := f.ended(p.ID); end.Reason != store.ReasonInterrupted {
		t.Fatal(end)
	}
}

// R-ZELD-BYPF R-TB3K-AUZ3
func TestRecoverSettlesActivePass(t *testing.T) {
	entered := make(chan struct{})
	f := newFixture(t, func(_ http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }, true)
	p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner", RequestID: "request"})
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	before := f.requests()
	if err = f.eng.Recover(events.NewContext(context.Background(), events.Cause{ID: "evt_0606060606060606", Depth: 2})); err != nil {
		t.Fatal(err)
	}
	stored, err := f.st.GetPass(context.Background(), p.ID)
	if err != nil || stored.Status != store.StatusFailed || stored.Reason != store.ReasonInterrupted {
		t.Fatal(stored, err)
	}
	if !reflect.DeepEqual(before, f.requests()) {
		t.Fatal("recover read scrapes")
	}
	f.eng.Drain(context.Background())
	f.flush()
	es := f.ec.Events()
	if len(es) != 2 || es[1].Name != trail.Finished || es[1].Cause != "" || es[1].Depth != 0 {
		t.Fatal(es)
	}
}

func failedCounts(p store.Pass) store.Counts {
	return store.Counts{Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Judged: p.Judged, Unranged: p.Unranged, Derived: p.Derived, Due: p.Due, Fetched: p.Fetched, HTTPS: p.HTTPS, HTTP: p.HTTP, None: p.None}
}
