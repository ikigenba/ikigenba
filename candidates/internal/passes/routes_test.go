package passes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func routeAfter(time.Duration) <-chan time.Time { return make(chan time.Time) }

func routeFixture(t *testing.T, transport enrichTransport) (*fixture, *routes.Client) {
	t.Helper()
	f := newFixture(t, nil, false)
	f.eng.Drain(t.Context())
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(100000, 0) }, Rand: &random{}, Origin: "Owner origin"})
	client := &routes.Client{HTTP: &http.Client{Transport: transport}, Key: "fixture-key", After: routeAfter}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(), Routes: client, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) {
		t.Error("route pass fetched website")
		return nil, errors.New("website")
	})}, After: routeAfter}, Telemetry: f.w, Events: f.em, After: routeAfter})
	return f, client
}

func routeCandidates(t *testing.T, f *fixture, n int, empty bool) ([]string, string) {
	t.Helper()
	obs := make([]store.Observation, n)
	for i := range n {
		street := fmt.Sprintf("%d Main St", i+1)
		if empty && i == n-1 {
			street = ""
		}
		obs[i] = store.Observation{Scraper: "fmcsa", SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: fmt.Sprintf("Route business %d", i), NameKey: fmt.Sprintf("route%d", i), Street: street, City: "Hinckley", State: "MN", Zip: "55037"}}
	}
	_, ids, err := f.st.Attach(t.Context(), "fixture", false, obs)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.st.JudgeAll(t.Context(), "", 10000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	pl, err := f.st.CreateProspectList(t.Context(), store.ProspectList{Name: "Route targets", Size: int64(n), CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	return ids, pl.ID
}

func routeEnded(t *testing.T, f *fixture, id string) store.Pass {
	t.Helper()
	for {
		for _, event := range f.ec.Events() {
			if event.Name == trail.Finished && event.Attrs["pass"] == id {
				p, err := f.st.GetPass(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
		}
		<-f.changed
	}
}

func routeAnswer(r *http.Request) (*http.Response, error) {
	body := `{"routes":[{"duration":"4200s","distanceMeters":16093}]}`
	if r.URL.Host == "maps.googleapis.com" {
		body = `{"status":"OK","results":[{"formatted_address":"Placed street","geometry":{"location_type":"ROOFTOP","location":{"lat":46,"lng":-92}}}]}`
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
}

// R-O2VG-U1G7 R-K51J-USRD R-K69G-8KI2 R-K7HC-MC8R R-K9X5-DVQ5 R-KB51-RNGU R-KESQ-WYOX R-PO6Q-T9AU
func TestRouteTargetsResultsCountsAndRepeat(t *testing.T) {
	t.Parallel()
	if passes.RoutesInFlight != 4 || passes.NoRouteKey == "" || strings.Contains(passes.NoRouteKey, "\n") || passes.NoRouteKey == passes.NoPrompt {
		t.Fatal("route scheduling/copy")
	}
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var mu sync.Mutex
			requests := 0
			transport := enrichTransport(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				requests++
				mu.Unlock()
				if fail {
					return nil, errors.New("fixture failure")
				}
				return routeAnswer(r)
			})
			f, client := routeFixture(t, transport)
			ids, target := routeCandidates(t, f, 4, true)
			read, err := f.st.ReadProspectList(t.Context(), target)
			if err != nil || len(read.Members) != 4 {
				t.Fatal(read, err)
			}
			reg := enrichers.New(enrichers.Config{Routes: client})
			en, _ := reg.Get(enrichers.KindDriveRoute)
			router := en.(enrichers.Router)
			expected := map[string]enrichers.Result{}
			inputs := map[string]string{}
			want := store.Counts{Due: 3}
			for _, id := range ids[:3] {
				s, err := f.st.Subject(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				res, tally := router.Route(t.Context(), s)
				expected[id] = res
				inputs[id] = router.Inputs(s)
				want.Runs += tally.Sent
				want.RunsFailed += tally.Failed
				want.Cost += tally.CostNanos
			}
			mu.Lock()
			requests = 0
			mu.Unlock()
			caller := identity.Caller{UserID: "owner", RequestID: "route-request"}
			p, err := f.eng.EnrichTarget(t.Context(), caller, enrichers.KindDriveRoute, target, false)
			if err != nil {
				t.Fatal(err)
			}
			if p.Status != store.StatusRunning || p.Target != target || p.EnrichKind != enrichers.KindDriveRoute || p.UserID != caller.UserID {
				t.Fatal(p)
			}
			p = routeEnded(t, f, p.ID)
			got := store.Counts{Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Judged: p.Judged, Unranged: p.Unranged, Derived: p.Derived, Due: p.Due, Fetched: p.Fetched, HTTPS: p.HTTPS, HTTP: p.HTTP, None: p.None, Runs: p.Runs, RunsFailed: p.RunsFailed, Pages: p.Pages, Cost: p.Cost}
			mu.Lock()
			sent := requests
			mu.Unlock()
			if p.Status != store.StatusDone || got != want || int64(sent) != p.Runs {
				t.Fatal(p, got, want, sent)
			}
			for _, id := range ids {
				es, err := f.st.Enrichments(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				res, selected := expected[id]
				if !selected {
					if len(es) != 0 {
						t.Fatal(es)
					}
					continue
				}
				if len(es) != 1 || es[0].Kind != enrichers.KindDriveRoute || es[0].Inputs != inputs[id] || es[0].Source != res.Source || es[0].Cost != res.Cost || !reflect.DeepEqual(es[0].Found, res.Found) || !es[0].At.Equal(time.Unix(100000, 0)) {
					t.Fatal(es, res)
				}
				var found enrichers.RouteFound
				if err = json.Unmarshal(es[0].Found, &found); err != nil || found.Failed() != fail {
					t.Fatal(found, err)
				}
			}
			for _, other := range f.list() {
				if other.Trigger == p.ID {
					t.Fatal("route follow-on", other)
				}
			}
			second, err := f.eng.EnrichTarget(t.Context(), caller, enrichers.KindDriveRoute, target, false)
			if err != nil {
				t.Fatal(err)
			}
			second = routeEnded(t, f, second.ID)
			if second.Status != store.StatusDone || second.Due+second.Runs+second.RunsFailed+second.Cost != 0 {
				t.Fatal(second)
			}
			mu.Lock()
			after := requests
			mu.Unlock()
			if after != sent {
				t.Fatal("repeat requested route", sent, after)
			}
			forced, err := f.eng.EnrichTarget(t.Context(), caller, enrichers.KindDriveRoute, ids[0], true)
			if err != nil {
				t.Fatal(err)
			}
			forced = routeEnded(t, f, forced.ID)
			if forced.Due != 1 || forced.Runs != want.Runs/3 {
				t.Fatal(forced)
			}
		})
	}
}

// R-K8P9-03ZG R-2YKF-HXX2
func TestRouteWorkersAdvanceAndDrainCancels(t *testing.T) {
	t.Parallel()
	type held struct {
		r       *http.Request
		release chan struct{}
	}
	started := make(chan held, 20)
	var mu sync.Mutex
	active, peak, sent := 0, 0, 0
	f, _ := routeFixture(t, enrichTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		active++
		sent++
		peak = max(peak, active)
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		h := held{r, make(chan struct{})}
		started <- h
		select {
		case <-h.release:
			return nil, errors.New("released")
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}))
	ids, target := routeCandidates(t, f, 10, false)
	p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindDriveRoute, target, false)
	if err != nil {
		t.Fatal(err)
	}
	helds := make([]held, 0, 4)
	for range passes.RoutesInFlight {
		helds = append(helds, <-started)
	}
	select {
	case extra := <-started:
		t.Fatal("too many requests", extra.r.URL)
	default:
	}
	close(helds[0].release)
	replacement := <-started
	select {
	case extra := <-started:
		t.Fatal("too many replacements", extra.r.URL)
	default:
	}
	f.eng.Drain(context.Background())
	mu.Lock()
	gotPeak, gotSent, left := peak, sent, active
	mu.Unlock()
	if gotPeak != passes.RoutesInFlight || gotSent != passes.RoutesInFlight+1 || left != 0 {
		t.Fatal("routing concurrency", gotPeak, gotSent, left)
	}
	p = routeEnded(t, f, p.ID)
	if p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted {
		t.Fatal(p)
	}
	for _, h := range append(helds[1:], replacement) {
		select {
		case <-h.r.Context().Done():
		default:
			t.Fatal("request survived drain")
		}
	}
	total := 0
	for _, id := range ids {
		es, err := f.st.Enrichments(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		total += len(es)
	}
	if total != 1 {
		t.Fatal("interrupted route recorded a result", total)
	}
	ds, err := f.st.DueAmong(t.Context(), enrichers.KindDriveRoute, ids, false)
	if err != nil || len(ds) != 9 {
		t.Fatal(ds, err)
	}
}

// R-JYY1-XY1W R-JWI9-6EKI R-JVAC-SMTT R-K1DU-PHJA
func TestRouteRefusalsPreserveRecords(t *testing.T) {
	for _, mode := range []string{"no-key", "no-http", "target", "kind", "busy", "store", "stopping", "corpus"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			f, client := routeFixture(t, enrichTransport(func(r *http.Request) (*http.Response, error) { requests++; return routeAnswer(r) }))
			ids, _ := routeCandidates(t, f, 1, false)
			target, kind := ids[0], enrichers.KindDriveRoute
			want := passes.RefusalInvalid
			text := ""
			switch mode {
			case "no-key":
				client = nil
				text = passes.NoRouteKey
			case "no-http":
				client.HTTP = nil
				want = passes.RefusalUnavailable
			case "target":
				target = "bad"
				want = passes.RefusalNotFound
			case "kind":
				kind = ""
			case "busy":
				want = passes.RefusalBusy
				_, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: target, EnrichKind: enrichers.KindLinkFacts, UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
			case "stopping":
				want = passes.RefusalStopping
			}
			f.eng.Drain(t.Context())
			f.eng = passes.New(passes.Config{Store: f.st, Routes: client, Telemetry: f.w, Events: f.em, After: routeAfter})
			if mode == "stopping" {
				f.eng.Drain(t.Context())
			}
			snapshot := func() []byte {
				es, err := f.st.Enrichments(t.Context(), ids[0])
				if err != nil {
					t.Fatal(err)
				}
				facts, err := f.st.GetFacts(t.Context(), ids[0])
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					t.Fatal(err)
				}
				b, err := json.Marshal([]any{f.list(), es, facts})
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			before := snapshot()
			if mode == "store" {
				f.d.SetFailing(true)
			}
			var p store.Pass
			var err error
			if mode == "corpus" {
				p, err = f.eng.Enrich(t.Context(), identity.Caller{UserID: "owner"}, kind)
			} else {
				p, err = f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, kind, target, false)
			}
			if mode == "store" {
				f.d.SetFailing(false)
				var rf *passes.Refusal
				if !errors.Is(err, store.ErrUnreachable) || errors.As(err, &rf) || p != (store.Pass{}) {
					t.Fatal(p, err)
				}
			} else {
				var rf *passes.Refusal
				if !errors.As(err, &rf) || rf.Word != want || rf.Text != text || p != (store.Pass{}) {
					t.Fatal(p, err, want, text)
				}
			}
			f.flush()
			if requests != 0 || string(before) != string(snapshot()) || len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
				t.Fatal("refused call changed records or contacted services", requests)
			}
		})
	}
}

// R-KCCY-5F7J
func TestOtherPassKindsNeverRoute(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, true)
	f.eng.Drain(t.Context())
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(100000, 0) }, Rand: &random{}, Origin: "Owner origin"})
	ids, _ := routeCandidates(t, f, 1, false)
	requests := 0
	client := &routes.Client{HTTP: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) { requests++; return routeAnswer(r) })}, Key: "fixture-key", After: routeAfter}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Routes: client, Telemetry: f.w, Events: f.em, After: routeAfter})
	p, err := f.eng.Enrich(t.Context(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	p = routeEnded(t, f, p.ID)
	if p.Status != store.StatusDone {
		t.Fatal(p)
	}
	waitFollow(t, f, p.ID)
	p, err = f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if p = routeEnded(t, f, p.ID); p.Status != store.StatusDone {
		t.Fatal(p)
	}
	p, err = f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if p = routeEnded(t, f, p.ID); p.Status != store.StatusDone {
		t.Fatal(p)
	}
	p, err = f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindDriveMinutes, ids[0], true)
	if err != nil {
		t.Fatal(err)
	}
	p = routeEnded(t, f, p.ID)
	if p.Status != store.StatusDone {
		t.Fatal(p)
	}
	es, err := f.st.Enrichments(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range es {
		if en.Kind == enrichers.KindDriveRoute {
			t.Fatal(en)
		}
	}
	if requests != 0 {
		t.Fatal(requests)
	}
}
