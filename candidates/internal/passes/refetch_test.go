package passes_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Refetch tests clone immutable bytes, never a shared running database.
type refetchTemplate struct {
	database   []byte
	migrations fs.FS
}

var legacyRefetchTemplate = sync.OnceValues(prepareRefetchTemplate)

func prepareRefetchTemplate() (out refetchTemplate, err error) {
	old := fstest.MapFS{}
	files, err := fs.ReadDir(candidates.Migrations(), ".")
	if err != nil {
		return out, err
	}
	for _, file := range files {
		if file.Name() >= "0027_page_reasons.sql" {
			continue
		}
		body, e := fs.ReadFile(candidates.Migrations(), file.Name())
		if e != nil {
			return out, e
		}
		old[file.Name()] = &fstest.MapFile{Data: body}
	}
	dir, err := os.MkdirTemp("", "scratch.")
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	path := filepath.Join(dir, "legacy.db")
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: old, Now: func() time.Time { return time.Unix(100000, 0) }})
	if err != nil {
		return out, err
	}
	if err = d.Close(); err != nil {
		return out, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return out, err
	}
	out.database, err = root.ReadFile("legacy.db")
	out.migrations = old
	return out, errors.Join(err, root.Close())
}

// Only the upgrade migration creates work entries. Seed the earlier schema
// through its published rollback seam, then open the current migrations.
func refetchFixture(t *testing.T, n int, handlers ...http.HandlerFunc) (*fixture, []store.Page) {
	t.Helper()
	var handler http.HandlerFunc
	if len(handlers) != 0 {
		handler = handlers[0]
	}
	f := newFixture(t, handler, handler != nil)
	initial := f.d
	t.Cleanup(func() { _ = initial.Close() })
	template, err := legacyRefetchTemplate()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	f.path = filepath.Join(dir, "legacy.db")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeErr := root.WriteFile("legacy.db", template.database, 0600)
	if err = errors.Join(writeErr, root.Close()); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Unix(100000, 0) }
	previous, err := db.Open(t.Context(), db.Config{Path: f.path, Migrations: template.migrations, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	pages := make([]store.Page, n)
	err = previous.Write(t.Context(), func(tx *sql.Tx) error {
		for i := range n {
			id := fmt.Sprintf("cand_%016x", i+1)
			if _, e := tx.ExecContext(t.Context(), `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated,name_source,name_record,address_source,address_record,phone_source,phone_record,email_source,email_record,other_names,other_addresses,other_phones,other_emails) VALUES (?,'active','','old','["old"]','old','','','','','','','','[]','','[]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','','','','','','','','','[]','[]','[]','[]')`, id); e != nil {
				return e
			}
			p := store.Page{Candidate: id, URL: fmt.Sprintf("https://repair.test/%d", i), Title: fmt.Sprintf("old title %d", i), Text: fmt.Sprintf("old text %d", i), Size: website.BodyCap, Fetched: now().UTC(), UserID: fmt.Sprintf("owner%d", i), UserEmail: fmt.Sprintf("owner%d@repair.test", i)}
			result, e := tx.ExecContext(t.Context(), `INSERT INTO pages(candidate,url,title,text,size,fetched,user_id,user_email) VALUES(?,?,?,?,?,?,?,?)`, p.Candidate, p.URL, p.Title, p.Text, p.Size, p.Fetched.Format(time.RFC3339), p.UserID, p.UserEmail)
			if e != nil {
				return e
			}
			p.Number, e = result.LastInsertId()
			if e != nil {
				return e
			}
			pages[i] = p
		}
		return nil
	})
	if e := previous.Close(); err == nil {
		err = e
	}
	if err != nil {
		t.Fatal(err)
	}
	f.d, err = db.Open(t.Context(), db.Config{Path: f.path, Migrations: candidates.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	f.st = store.New(f.d, store.Config{Now: now, Rand: &random{}})
	return f, pages
}
func refetchEngine(f *fixture, transport enrichTransport, after func(time.Duration) <-chan time.Time, out io.Writer) {
	var checker *website.Checker
	if transport != nil {
		checker = &website.Checker{Client: &http.Client{Transport: transport}, After: fastAfter}
	}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Website: checker, After: after, Stderr: out})
}
func repairResponse(r *http.Request, kind string) (*http.Response, error) {
	body := "<title>Replacement title</title><main><p>" + strings.Repeat("readable content ", website.MinChars) + "</p></main>"
	status := 200
	headers := http.Header{"Content-Type": {"text/html"}}
	switch kind {
	case website.ReasonScripted:
		body = "<title>Scripted title</title><script>application()</script>"
	case website.ReasonRedirect:
		body = "<title>Frame title</title><iframe src='/view'></iframe>"
	case website.ReasonEmpty:
		body = "<title>Empty title</title><p>tiny</p>"
	case "missing":
		status = 404
	case "error":
		return nil, errors.New("repair transport failed")
	case "offsite":
		if r.URL.Host == "repair.test" {
			status = 301
			headers.Set("Location", "https://elsewhere.test/landing")
		}
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
func refetchPending(t *testing.T, f *fixture) []store.Page {
	t.Helper()
	ps, err := f.st.PendingRefetches(t.Context(), 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}
func requireOriginalPageCopies(t *testing.T, f *fixture, pages []store.Page) {
	t.Helper()
	for _, p := range pages {
		got, e := f.st.CountPages(t.Context(), p.Candidate)
		if e != nil || got.Copies != 1 {
			t.Fatal(p.Candidate, got, e)
		}
	}
}

// R-XAON-TY2L R-XD4G-LHJZ R-XFK9-D11D
func TestRefetchWalksEveryBatchInOrder(t *testing.T) {
	// Keep this full corpus ahead of the package's parallel pass deadlines.
	f, pages := refetchFixture(t, 2*passes.BatchSize+1)
	requested := []string{}
	refetchEngine(f, func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		return repairResponse(r, "")
	}, fastAfter, nil)
	waitSignal(t, f.eng.StartRefetch(t.Context()))
	if len(requested) != len(pages) {
		t.Fatal(len(requested), len(pages))
	}
	if len(refetchPending(t, f)) != 0 {
		t.Fatal("entries not settled")
	}
	for i, p := range pages {
		if requested[i] != p.URL {
			t.Fatal(i, requested[i], p.URL)
		}
		listed, e := f.st.Pages(t.Context(), p.Candidate)
		if e != nil || len(listed) != 1 || listed[0].Number <= p.Number {
			t.Fatal(listed, e)
		}
		got, e := f.st.GetPage(t.Context(), p.Candidate, listed[0].Number)
		want := (&website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) { return repairResponse(r, "") })}, After: fastAfter}).Get(t.Context(), p.URL)
		if e != nil || got.URL != p.URL || got.Candidate != p.Candidate || got.UserID != p.UserID || got.UserEmail != p.UserEmail || got.Title != want.Title || got.Text != want.Text || got.Size != want.Size || got.Reason != want.Reason {
			t.Fatal(got, want, e)
		}
		old, e := f.st.GetPage(t.Context(), p.Candidate, p.Number)
		if e != nil || !reflect.DeepEqual(old, p) {
			t.Fatal(old, p, e)
		}
	}
}

// R-XBWK-7PTA R-XECC-Z9AO R-XO3K-1F88
func TestRefetchReturnsBeforeFetchAndPacesAfterIt(t *testing.T) {
	t.Parallel()
	f, pages := refetchFixture(t, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	requests := make(chan string, 2)
	timers := make(chan chan time.Time, 2)
	refetchEngine(f, func(r *http.Request) (*http.Response, error) {
		requests <- r.URL.String()
		if r.URL.String() == pages[0].URL {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return repairResponse(r, "")
	}, func(d time.Duration) <-chan time.Time {
		if d != website.Interval {
			t.Errorf("unexpected timer %v", d)
		}
		c := make(chan time.Time, 1)
		timers <- c
		return c
	}, nil)
	done := f.eng.StartRefetch(t.Context())
	assertOpen(t, done)
	waitSignal(t, entered)
	assertClosed(t, f.eng.StartRefetch(t.Context()))
	assertOpen(t, done)
	if got := <-requests; got != pages[0].URL {
		t.Fatal(got)
	}
	select {
	case <-timers:
		t.Fatal("pace began before fetch ended")
	default:
	}
	unlock()
	var timer chan time.Time
	select {
	case timer = <-timers:
	case <-time.After(5 * time.Second):
		t.Fatal("no pace timer")
	}
	select {
	case got := <-requests:
		t.Fatal("unpaced request", got)
	default:
	}
	timer <- time.Time{}
	waitSignal(t, done)
	if got := <-requests; got != pages[1].URL {
		t.Fatal(got)
	}
	select {
	case got := <-requests:
		t.Fatal("duplicate request", got)
	default:
	}
}

// R-XI02-4KIR R-XJ7Y-IC9G R-XKFU-W405 R-XSZ5-KI70
func TestRefetchOutcomesDiagnosticAndReopenedStart(t *testing.T) {
	t.Parallel()
	for _, n := range []int{15, 16} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Parallel()
			f, pages := refetchFixture(t, n)
			kinds := []string{"", website.ReasonScripted, website.ReasonScripted, website.ReasonRedirect, website.ReasonRedirect, website.ReasonRedirect, website.ReasonEmpty, website.ReasonEmpty, website.ReasonEmpty, website.ReasonEmpty, "missing", "missing", "error", "offsite", "merge"}
			if n == 16 {
				kinds = append(kinds, "")
			}
			kindsByURL := map[string]string{}
			for i, p := range pages {
				kindsByURL[p.URL] = kinds[i]
			}
			requested := []string{}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unlock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unlock)
			writes := [][]byte{}
			refetchEngine(f, func(r *http.Request) (*http.Response, error) {
				requested = append(requested, r.URL.String())
				kind := kindsByURL[r.URL.String()]
				if kind == "merge" {
					_, _, e := f.st.Decide(r.Context(), store.Ruling{Kind: store.KindMerge, Candidate: pages[0].Candidate, Other: pages[14].Candidate, Why: "same business", UserID: "ruler"})
					if e != nil {
						t.Error(e)
					}
				}
				return repairResponse(r, kind)
			}, fastAfter, fillWriter(func(b []byte) (int, error) {
				writes = append(writes, bytes.Clone(b))
				close(entered)
				<-release
				return len(b), nil
			}))
			done := f.eng.StartRefetch(t.Context())
			waitSignal(t, entered)
			assertOpen(t, done)
			unlock()
			waitSignal(t, done)
			wantLine := fmt.Sprintf("candidates: pages refetched: %d read, 2 scripted, 3 redirect, 4 empty, 5 failed\n", n-14)
			if len(writes) != 1 || string(writes[0]) != wantLine {
				t.Fatal(writes)
			}
			if len(requested) != n+1 || len(refetchPending(t, f)) != 0 {
				t.Fatal(requested, refetchPending(t, f))
			}
			if n == 16 && requested[len(requested)-1] != pages[15].URL {
				t.Fatal("entry after merged candidate was not fetched", requested)
			}
			for i, p := range pages {
				if i == 14 {
					p.Candidate = pages[0].Candidate
				}
				listed, e := f.st.Pages(t.Context(), p.Candidate)
				if e != nil {
					t.Fatal(e)
				}
				var got store.Page
				for _, q := range listed {
					if q.URL == p.URL {
						got, e = f.st.GetPage(t.Context(), p.Candidate, q.Number)
					}
				}
				if e != nil || got.Number == 0 {
					t.Fatal(got, e)
				}
				if i >= 10 && i < 15 {
					if got.Number != p.Number {
						t.Fatal("failed entry added a copy", got, p)
					}
				} else {
					expected := (&website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) { return repairResponse(r, kinds[i]) })}, After: fastAfter}).Get(t.Context(), p.URL)
					if got.Number <= pages[len(pages)-1].Number || got.URL != p.URL || got.UserID != p.UserID || got.UserEmail != p.UserEmail || got.Title != expected.Title || got.Text != expected.Text || got.Reason != expected.Reason || got.Size != expected.Size {
						t.Fatal(got, expected)
					}
					if i > 0 && i < 10 && got.Text != "" {
						t.Fatal(got)
					}
				}
				old, e := f.st.GetPage(t.Context(), p.Candidate, p.Number)
				if e != nil || !reflect.DeepEqual(old, p) {
					t.Fatal(old, p, e)
				}
			}
			later, e := db.Open(t.Context(), db.Config{Path: f.path, Migrations: candidates.Migrations(), Now: func() time.Time { return time.Unix(100000, 0) }})
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = later.Close() }()
			st := store.New(later, store.Config{})
			before := make(map[string][]store.Page)
			for _, p := range pages {
				if p.Candidate != pages[14].Candidate {
					before[p.Candidate], e = st.Pages(t.Context(), p.Candidate)
					if e != nil {
						t.Fatal(e)
					}
				}
			}
			var zero bytes.Buffer
			e2 := passes.New(passes.Config{Store: st, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
				t.Errorf("second start requested %s", r.URL)
				return repairResponse(r, "")
			})}, After: fastAfter}, After: func(d time.Duration) <-chan time.Time {
				t.Errorf("empty refetch timer %v", d)
				return make(chan time.Time)
			}, Stderr: &zero})
			waitSignal(t, e2.StartRefetch(t.Context()))
			e2.Drain(t.Context())
			if zero.String() != "candidates: pages refetched: 0 read, 0 scripted, 0 redirect, 0 empty, 0 failed\n" {
				t.Fatal(zero.String())
			}
			for id, want := range before {
				got, e := st.Pages(t.Context(), id)
				if e != nil || !reflect.DeepEqual(got, want) {
					t.Fatal(got, want, e)
				}
			}

		})
	}
}

// R-XLNR-9VQU R-XKFU-W405
func TestRefetchStoreFailureAndNilDiagnostics(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"failure", "failure nil", "success nil"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, pages := refetchFixture(t, 1)
			var writes [][]byte
			var out io.Writer
			if mode != "success nil" {
				f.d.SetFailing(true)
			}
			expected := f.d.Write(t.Context(), func(*sql.Tx) error { return nil })
			if mode == "failure" {
				out = fillWriter(func(b []byte) (int, error) {
					writes = append(writes, bytes.Clone(b))
					f.d.SetFailing(false)
					text := string(b)
					if !strings.HasPrefix(text, "candidates: pages refetch failed\n\n") || strings.Count(text, "\n") != 3 || !strings.HasSuffix(text, "\n") || !strings.Contains(text, strings.ReplaceAll(expected.Error(), "\n", " ")) {
						t.Error(text, expected)
					}
					return len(b), nil
				})
			}
			refetchEngine(f, func(r *http.Request) (*http.Response, error) {
				if mode != "success nil" {
					t.Error("failed store fetched")
				}
				return repairResponse(r, "")
			}, fastAfter, out)
			waitSignal(t, f.eng.StartRefetch(t.Context()))
			f.d.SetFailing(false)
			if mode == "failure" && len(writes) != 1 {
				t.Fatal(writes)
			}
			if mode != "success nil" {
				if !reflect.DeepEqual(refetchPending(t, f), pages) {
					t.Fatal(refetchPending(t, f), pages)
				}
				requireOriginalPageCopies(t, f, pages)
			}
		})
	}
}

// R-XMVN-NNHJ R-XVEY-C1OE R-XO3K-1F88
func TestRefetchCutOffDuringFetchAndPace(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel fetch", "drain fetch", "drain pace"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, pages := refetchFixture(t, 2)
			entered := make(chan struct{})
			cancelled, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unlock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unlock)
			var requests atomic.Int64
			var output bytes.Buffer
			refetchEngine(f, func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				if mode != "drain pace" {
					close(entered)
					<-r.Context().Done()
					if mode == "drain fetch" {
						close(cancelled)
						<-release
					}
					return nil, r.Context().Err()
				}
				return repairResponse(r, "")
			}, func(d time.Duration) <-chan time.Time {
				if d != website.Interval {
					t.Error(d)
				}
				close(entered)
				return make(chan time.Time)
			}, &output)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := f.eng.StartRefetch(ctx)
			waitSignal(t, entered)
			if mode == "cancel fetch" {
				cancel()
			} else {
				drained := make(chan struct{})
				go func() { f.eng.Drain(context.Background()); close(drained) }()
				if mode == "drain fetch" {
					waitSignal(t, cancelled)
					assertOpen(t, drained)
					assertOpen(t, done)
					unlock()
				}
				waitFillDrain(t, drained)
				assertClosed(t, f.eng.StartRefetch(t.Context()))
			}
			waitSignal(t, done)
			pending := refetchPending(t, f)
			want := pages
			if mode == "drain pace" {
				want = pages[1:]
			}
			if requests.Load() != 1 || output.Len() != 0 || !reflect.DeepEqual(pending, want) {
				t.Fatal(requests.Load(), output.String(), pending, want)
			}
			requireOriginalPageCopies(t, f, want)
		})
	}
}

// R-XPBG-F6YX
func TestRefetchWithoutCheckerDoesNothing(t *testing.T) {
	t.Parallel()
	f, pages := refetchFixture(t, 2)
	var output bytes.Buffer
	refetchEngine(f, nil, func(time.Duration) <-chan time.Time { t.Error("nil checker waited"); return nil }, &output)
	assertClosed(t, f.eng.StartRefetch(t.Context()))
	if output.Len() != 0 || !reflect.DeepEqual(refetchPending(t, f), pages) {
		t.Fatal(output.String(), refetchPending(t, f))
	}
	requireOriginalPageCopies(t, f, pages)
}

// R-XQJC-SYPM
func TestRefetchLeavesPassesFactsAndEnrichmentsAlone(t *testing.T) {
	t.Parallel()
	f, pages := refetchFixture(t, 3, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("refetch contacted scrapes %s", r.URL)
		w.WriteHeader(500)
	})
	pc, pf := fakePrompts(t, f, func(name string, _ map[string]json.RawMessage) (any, bool, int) {
		t.Errorf("refetch called prompts %s", name)
		return nil, false, 500
	})
	rejectOther := enrichTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("refetch contacted another service %s", r.URL)
		return nil, errors.New("unexpected service")
	})
	for _, p := range pages {
		found, e := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: p.URL})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: p.Candidate, Kind: sources.WebsiteCheck, Inputs: p.URL, Found: found}}); e != nil {
			t.Fatal(e)
		}
	}
	kinds := map[string]string{pages[0].URL: "", pages[1].URL: website.ReasonScripted, pages[2].URL: "missing"}
	enrichments := map[string][]store.Enrichment{}
	facts := map[string]store.Facts{}
	factErrors := map[string]error{}
	for _, p := range pages {
		var e error
		enrichments[p.Candidate], e = f.st.Enrichments(t.Context(), p.Candidate)
		if e != nil {
			t.Fatal(e)
		}
		facts[p.Candidate], factErrors[p.Candidate] = f.st.GetFacts(t.Context(), p.Candidate)
	}
	before := f.list()
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Prompts: pc, Telemetry: f.w, Events: f.em, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) { return repairResponse(r, kinds[r.URL.String()]) })}, After: fastAfter}, Routes: &routes.Client{HTTP: &http.Client{Transport: rejectOther}, Key: "refetch-test-key", After: fastAfter}, Places: walkClient(rejectOther), After: fastAfter})
	waitSignal(t, f.eng.StartRefetch(t.Context()))
	f.flush()
	if !reflect.DeepEqual(f.list(), before) || len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 || len(f.requests()) != 0 || len(pf.snapshot()) != 0 {
		t.Fatal("refetch changed passes or emitted events")
	}
	for _, p := range pages {
		es, e := f.st.Enrichments(t.Context(), p.Candidate)
		if e != nil || !reflect.DeepEqual(es, enrichments[p.Candidate]) {
			t.Fatal(es, e)
		}
		fact, e := f.st.GetFacts(t.Context(), p.Candidate)
		if !errors.Is(e, factErrors[p.Candidate]) || !reflect.DeepEqual(fact, facts[p.Candidate]) {
			t.Fatal(fact, e)
		}
	}
}

// R-XRR9-6QGB
func TestRefetchAdmitsJudgeAndFillAndRunsBesideFill(t *testing.T) {
	t.Parallel()
	f, _ := refetchFixture(t, 1)
	entered := make(chan struct{})
	refetchEngine(f, func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}, fastAfter, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := f.eng.StartRefetch(ctx)
	waitSignal(t, entered)
	p, e := f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"})
	if e != nil {
		t.Fatal("refetch blocked judge", e)
	}
	f.ended(p.ID)
	// Drain joins the worker's settled marker after its recorded ending.
	f.eng.Drain(t.Context())
	waitSignal(t, done)
	// A fill blocked on the database can coexist with a new engine's refetch.
	asleep, unlock := holdFillWriter(t, f)
	entered = make(chan struct{})
	refetchEngine(f, func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}, fastAfter, nil)
	filled := f.eng.StartFill(t.Context())
	waitSignal(t, asleep)
	done = f.eng.StartRefetch(t.Context())
	waitSignal(t, entered)
	assertOpen(t, filled)
	assertOpen(t, done)
	drained := make(chan struct{})
	go func() { f.eng.Drain(context.Background()); close(drained) }()
	waitFillDrain(t, drained)
	waitSignal(t, filled)
	waitSignal(t, done)
	unlock()
	// Conversely, an already running refetch never bars StartFill.
	f2, _ := refetchFixture(t, 1)
	asleep, unlock = holdFillWriter(t, f2)
	entered = make(chan struct{})
	refetchEngine(f2, func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}, fastAfter, nil)
	done = f2.eng.StartRefetch(t.Context())
	waitSignal(t, entered)
	filled = f2.eng.StartFill(t.Context())
	waitSignal(t, asleep)
	assertOpen(t, filled)
	drained = make(chan struct{})
	go func() { f2.eng.Drain(context.Background()); close(drained) }()
	waitFillDrain(t, drained)
	waitSignal(t, filled)
	waitSignal(t, done)
	unlock()
}

// R-XU71-Y9XP
func TestConcurrentRefetchAdmissionAndEngineMethods(t *testing.T) {
	t.Parallel()
	f, pages := refetchFixture(t, 1)
	entered := make(chan struct{})
	var requests atomic.Int64
	refetchEngine(f, func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(entered)
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	}, fastAfter, nil)
	const racers = 12
	start := make(chan struct{})
	answers := make(chan (<-chan struct{}), racers)
	var wg sync.WaitGroup
	for range racers {
		wg.Go(func() { <-start; answers <- f.eng.StartRefetch(t.Context()) })
	}
	close(start)
	wg.Wait()
	close(answers)
	var first <-chan struct{}
	for ch := range answers {
		select {
		case <-ch:
		default:
			if first != nil {
				t.Fatal("two refetches began")
			}
			first = ch
		}
	}
	if first == nil {
		t.Fatal("no refetch began")
	}
	waitSignal(t, entered)
	caller := identity.Caller{UserID: "owner"}
	for _, call := range []func(){
		func() { _, _ = f.eng.Ingest(t.Context(), caller, "") },
		func() { _, _ = f.eng.Judge(t.Context(), caller) },
		func() { _, _ = f.eng.Enrich(t.Context(), caller, "") },
		func() {
			_, _ = f.eng.EnrichTarget(t.Context(), caller, enrichers.KindLinkFacts, pages[0].Candidate, false)
		},
		func() { _, _ = f.eng.EditParkRules(t.Context(), caller, store.ParkRulesEdit{}) },
		func() { _, _ = f.eng.Walk(t.Context(), caller, nil, false) },
		func() { _, _ = f.eng.StartParks(t.Context(), "concurrent-start") },
		func() { _ = f.eng.Deliver(t.Context(), judgeDelivery("concurrent-refetch")) },
		func() {
			_ = f.eng.DeliverCampaign(t.Context(), campaignDelivery(trail.AcceptedAdded, pages[0].Candidate, time.Unix(100000, 0)))
		},
		func() { _ = f.eng.Recover(t.Context()) },
		func() { waitSignal(t, f.eng.StartFill(t.Context())) },
		func() { assertClosed(t, f.eng.StartRefetch(t.Context())) },
		func() { f.eng.Drain(context.Background()) },
	} {
		wg.Go(call)
	}
	wg.Wait()
	drained := make(chan struct{})
	go func() { f.eng.Drain(context.Background()); close(drained) }()
	waitFillDrain(t, drained)
	waitSignal(t, first)
	if requests.Load() != 1 {
		t.Fatal(requests.Load())
	}
}

// R-XRR9-6QGB
func TestRefetchDoesNotBlockAnyPassOrCampaignDelivery(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"ingest", "judge", "enrich", "target", "rules", "walk", "start parks", "deliver ingest", "deliver judge", "deliver parks", "campaign"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, pages := refetchFixture(t, 1, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "mn_parcels") || strings.Contains(r.URL.Path, "parcel_run") {
					if strings.HasSuffix(r.URL.Path, "/current") || strings.HasSuffix(r.URL.Path, "/record") {
						parcelRecord(w, 0, store.StatusDone)
					}
				} else if strings.HasSuffix(r.URL.Path, "/current") || strings.HasSuffix(r.URL.Path, "/record") {
					record(w, "run_a", 0, store.StatusDone)
				}
			})
			parks := seedWalkParks(t, f, 1)
			entered := make(chan struct{})
			checker := &website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
				close(entered)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}, After: fastAfter}
			f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Website: checker, Telemetry: f.w, Events: f.em, After: fastAfter, Places: walkClient(func(r *http.Request) (*http.Response, error) { return placesResponse(r, 200, 0, ""), nil })})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := f.eng.StartRefetch(ctx)
			waitSignal(t, entered)
			caller := identity.Caller{UserID: "owner"}
			var p store.Pass
			var err error
			switch mode {
			case "ingest":
				p, err = f.eng.Ingest(t.Context(), caller, "fmcsa")
			case "judge":
				p, err = f.eng.Judge(t.Context(), caller)
			case "enrich":
				p, err = f.eng.Enrich(t.Context(), caller, "")
			case "target":
				p, err = f.eng.EnrichTarget(t.Context(), caller, enrichers.KindLinkFacts, pages[0].Candidate, false)
			case "rules":
				p, err = f.eng.EditParkRules(t.Context(), caller, store.ParkRulesEdit{})
			case "walk":
				p, err = f.eng.Walk(t.Context(), caller, parkIDs(parks), false)
			case "start parks":
				var ok bool
				p, ok = f.eng.StartParks(t.Context(), "refetch-start")
				if !ok {
					t.Fatal("refetch barred parks start")
				}
			case "deliver ingest":
				if got := f.delivery("refetch_ingest", "run_a"); got != events.OK() {
					t.Fatal(got)
				}
			case "deliver judge":
				if got := f.eng.Deliver(t.Context(), judgeDelivery("refetch_judge")); got != events.OK() {
					t.Fatal(got)
				}
			case "deliver parks":
				if got := parcelDelivery(f, "refetch_parks"); got != events.OK() {
					t.Fatal(got)
				}
			case "campaign":
				if got := f.eng.DeliverCampaign(t.Context(), campaignDelivery(trail.AcceptedAdded, pages[0].Candidate, time.Unix(100000, 0))); got != events.OK() {
					t.Fatal(got)
				}
				if len(campaignUses(t, f, pages[0].Candidate)) != 1 {
					t.Fatal("campaign use not recorded")
				}
			}
			if err != nil {
				t.Fatal("refetch barred pass", err)
			}
			if mode != "campaign" {
				if p.ID == "" {
					for _, q := range f.list() {
						if q.ID != parks[0].ID && q.Kind != store.KindWalk && q.Trigger != "walk-fixture" {
							p = q
							break
						}
					}
				}
				if p.ID == "" {
					t.Fatal("no pass recorded")
				}
				f.ended(p.ID)
			}
			assertOpen(t, done)
			cancel()
			waitSignal(t, done)
		})
	}
}

// R-XLNR-9VQU
func TestRefetchStoreFailureAfterFetchStopsTheWalk(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"", "missing"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, pages := refetchFixture(t, 2)
			requests := 0
			writes := 0
			refetchEngine(f, func(r *http.Request) (*http.Response, error) {
				requests++
				f.d.SetFailing(true)
				return repairResponse(r, kind)
			}, fastAfter, fillWriter(func(b []byte) (int, error) {
				writes++
				f.d.SetFailing(false)
				if !strings.HasPrefix(string(b), "candidates: pages refetch failed\n\n") {
					t.Error(string(b))
				}
				return len(b), nil
			}))
			waitSignal(t, f.eng.StartRefetch(t.Context()))
			if requests != 1 || writes != 1 || !reflect.DeepEqual(refetchPending(t, f), pages) {
				t.Fatal(requests, writes, refetchPending(t, f))
			}
			requireOriginalPageCopies(t, f, pages)
		})
	}
}

// R-XMVN-NNHJ
func TestRefetchCancellationDuringStoreCall(t *testing.T) {
	t.Parallel()
	f, pages := refetchFixture(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	var requests atomic.Int64
	var calls atomic.Int64
	f.st = store.New(f.d, store.Config{Now: func() time.Time { calls.Add(1); cancel(); return time.Unix(100000, 0) }})
	refetchEngine(f, func(r *http.Request) (*http.Response, error) { requests.Add(1); return repairResponse(r, "") }, fastAfter, &output)
	waitSignal(t, f.eng.StartRefetch(ctx))
	if calls.Load() != 1 || requests.Load() != 1 || output.Len() != 0 || !reflect.DeepEqual(refetchPending(t, f), pages) {
		t.Fatal(calls.Load(), requests.Load(), output.String(), refetchPending(t, f))
	}
	requireOriginalPageCopies(t, f, pages)
}

// R-XKFU-W405 R-XLNR-9VQU
func TestRefetchEndsBeforeHandingOverItsDiagnostic(t *testing.T) {
	t.Parallel()
	f, _ := refetchFixture(t, 1)
	firstEntered, secondEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	var writes atomic.Int64
	refetchEngine(f, func(r *http.Request) (*http.Response, error) { return repairResponse(r, "") }, fastAfter, fillWriter(func(b []byte) (int, error) {
		switch writes.Add(1) {
		case 1:
			if string(b) != "candidates: pages refetched: 1 read, 0 scripted, 0 redirect, 0 empty, 0 failed\n" {
				t.Error(string(b))
			}
			close(firstEntered)
			<-release
		case 2:
			if !strings.HasPrefix(string(b), "candidates: pages refetch failed\n\n") {
				t.Error(string(b))
			}
			close(secondEntered)
		default:
			t.Error("unexpected diagnostic", string(b))
		}
		return len(b), nil
	}))
	first := f.eng.StartRefetch(t.Context())
	waitSignal(t, firstEntered)
	assertOpen(t, first)
	f.d.SetFailing(true)
	second := f.eng.StartRefetch(t.Context())
	waitSignal(t, secondEntered)
	waitSignal(t, second)
	f.d.SetFailing(false)
	assertOpen(t, first)
	unlock()
	waitSignal(t, first)
	if writes.Load() != 2 {
		t.Fatal(writes.Load())
	}
}
