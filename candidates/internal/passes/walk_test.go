package passes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

func seedWalkParks(t *testing.T, f *fixture, n int) []store.Park {
	t.Helper()
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "walk-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	plan := parks.Plan{}
	for i := range n {
		plan.News = append(plan.News, parks.New{Number: int64(i + 1), Cluster: parks.Cluster{County: "Fixture", Town: "Fixture", Lat: 45 + float64(i), Lon: -93, Radius: 100, Parcels: []parks.Parcel{}}})
	}
	if _, err := f.st.ApplyParks(t.Context(), p.ID, "walk-fixture", []string{"Fixture"}, plan, store.Counts{ParksCreated: int64(n)}); err != nil {
		t.Fatal(err)
	}
	ks, err := f.st.ListParks(t.Context(), store.ParkFilter{Walked: parks.WalkedAll, Gone: parks.GoneAll}, parks.SortParcels, "", "", 100)
	if err != nil || len(ks) != n {
		t.Fatal(ks, err)
	}
	return ks
}
func walkClient(rt enrichTransport) *places.Client {
	return &places.Client{HTTP: &http.Client{Transport: rt}, Key: "walk-test-key", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
}
func placesResponse(r *http.Request, status, n int, base string) *http.Response {
	var body string
	if status != 200 {
		body = `{"error":{"status":"PERMISSION_DENIED"}}`
	} else {
		ps := make([]map[string]any, n)
		for i := range n {
			ps[i] = map[string]any{"id": fmt.Sprintf("%s-%d", base, i), "displayName": map[string]string{"text": fmt.Sprintf("Business %s %d", base, i)}, "formattedAddress": fmt.Sprintf("%d Fixture Street, Fixture, MN 55072", i)}
		}
		b, _ := json.Marshal(map[string]any{"places": ps})
		body = string(b)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}
}
func configureWalk(f *fixture, cl *places.Client) {
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Places: cl})
}
func parkIDs(ks []store.Park) []string {
	ids := make([]string, len(ks))
	for i, k := range ks {
		ids[i] = k.ID
	}
	return ids
}
func setWalkCeiling(t *testing.T, f *fixture, n int64) {
	t.Helper()
	p, err := f.st.SetParkRules(t.Context(), store.ParkRulesEdit{WalkCeiling: &n}, store.Pass{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.FailPass(t.Context(), p.ID, store.ReasonInterrupted); err != nil {
		t.Fatal(err)
	}
}

func walkToolClient(t *testing.T, f *fixture) *mcp.Client {
	t.Helper()
	srv := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: f.w})
	tools.Register(srv, tools.Config{Passes: f.eng, Store: f.st})
	host := httptest.NewServer(identity.Require(srv))
	t.Cleanup(host.Close)
	// Cancel held Places requests before closing a possibly blocked tool handler.
	t.Cleanup(func() { f.eng.Drain(context.Background()) })
	return mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: host.Client()})
}

// R-Y65D-MTWQ
func TestWalkToolAnswersWhileNearbyRequestsRemainOpen(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"one", "several", "again", "walked", "no_key", "unknown", "malformed", "gone", "empty", "too_many", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			parkCount := 2
			if mode == "too_many" {
				parkCount = passes.MaxWalkParks + 1
			}
			ks := seedWalkParks(t, f, parkCount)
			ids := []string{ks[0].ID}
			again := mode == "again"
			if again || mode == "walked" {
				prior, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.st.RecordSweep(t.Context(), store.Sweep{Pass: prior.ID, Park: ids[0], Circles: 1, Requests: 1, Cost: places.PriceNanos}); err != nil {
					t.Fatal(err)
				}
				if _, err = f.st.FinishWalk(t.Context(), prior.ID, store.Counts{}, false); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "several":
				ids = parkIDs(ks)
			case "unknown":
				ids = []string{"park_0123456789abcdef"}
			case "malformed":
				ids = []string{"unknown-park"}
			case "gone":
				p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "gone-fixture"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.st.ApplyParks(t.Context(), p.ID, "gone-fixture", nil, parks.Plan{Gone: ids}, store.Counts{ParksGone: 1}); err != nil {
					t.Fatal(err)
				}
			case "empty":
				ids = []string{}
			case "too_many":
				ids = parkIDs(ks)
			case "duplicate":
				ids = []string{ids[0], ids[0]}
			}
			var mu sync.Mutex
			var requests []string
			entered := make(chan struct{}, 1)
			cl := walkClient(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				requests = append(requests, r.URL.String())
				mu.Unlock()
				if r.URL.String() != places.NearbyURL {
					return nil, errors.New("unexpected Places request")
				}
				select {
				case entered <- struct{}{}:
				default:
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			if mode == "no_key" {
				cl = nil
			}
			configureWalk(f, cl)
			beforePasses := f.list()
			beforeWalks := make([][]store.ParkWalk, len(ks))
			beforeParks := make([]store.Park, len(ks))
			for i, k := range ks {
				var err error
				beforeWalks[i], err = f.st.ParkWalks(t.Context(), k.ID)
				if err != nil {
					t.Fatal(err)
				}
				beforeParks[i], err = f.st.GetPark(t.Context(), k.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			client := walkToolClient(t, f)
			args, err := json.Marshal(tools.WalkArgs{Parks: ids, Again: again})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			result, err := client.CallTool(ctx, identity.Caller{UserID: "owner"}, "walk", args)
			if err != nil {
				t.Fatal("walk did not answer while Places requests were held open:", err)
			}
			if mode == "one" || mode == "several" || again {
				if result.IsError() {
					t.Fatal("walk refused:", result)
				}
				raw, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				var wire struct {
					Structured passes.Record `json:"structuredContent"`
					Content    []struct{ Type, Text string }
				}
				if err = json.Unmarshal(raw, &wire); err != nil {
					t.Fatal(err)
				}
				waitSignal(t, entered)
				p, err := f.st.GetPass(t.Context(), wire.Structured.ID)
				if err != nil || p.Status != store.StatusRunning || wire.Structured != passes.RecordOf(p) {
					t.Fatal(wire.Structured, p, err)
				}
				want, err := json.Marshal(passes.RecordOf(p))
				if err != nil || len(wire.Content) != 1 || wire.Content[0].Type != "text" || wire.Content[0].Text != string(want) {
					t.Fatal(wire.Content, err)
				}
				return
			}
			if !result.IsError() {
				t.Fatal("walk accepted invalid selection:", result)
			}
			f.eng.Drain(context.Background())
			mu.Lock()
			made := append([]string(nil), requests...)
			mu.Unlock()
			for _, url := range made {
				if url == places.NearbyURL {
					t.Fatal("refused walk sent a Nearby request")
				}
			}
			if !reflect.DeepEqual(f.list(), beforePasses) {
				t.Fatal("refused walk changed passes")
			}
			for i, k := range ks {
				ws, err := f.st.ParkWalks(t.Context(), k.ID)
				if err != nil || !reflect.DeepEqual(ws, beforeWalks[i]) {
					t.Fatal("refused walk changed park walks:", k.ID, ws, err)
				}
				p, err := f.st.GetPark(t.Context(), k.ID)
				if err != nil || !reflect.DeepEqual(p, beforeParks[i]) {
					t.Fatal("refused walk changed park:", k.ID, p, err)
				}
			}
		})
	}
}

// R-Q59C-61OK
func TestWalkConstants(t *testing.T) {
	if passes.MaxWalkParks != 50 {
		t.Fatal(passes.MaxWalkParks)
	}
	seen := map[string]bool{passes.NoRouteKey: true}
	for _, s := range []string{passes.NoPlacesKey, passes.ParkGone, passes.AlreadyWalked} {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal(s)
		}
		seen[s] = true
	}
}

// R-ODFH-87V1
func TestWalkRefusalsLeaveNoWork(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"stopping", "no_key", "empty", "too_many", "duplicate", "malformed", "missing", "gone", "walked", "busy", "store"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			ks := seedWalkParks(t, f, 2)
			ids := parkIDs(ks)
			var requests atomic.Int64
			cl := walkClient(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				return placesResponse(r, 200, 1, "fixture"), nil
			})
			want, text := passes.RefusalInvalid, ""
			switch mode {
			case "no_key":
				cl = nil
				text = passes.NoPlacesKey
			case "empty":
				ids = nil
			case "too_many":
				ids = make([]string, passes.MaxWalkParks+1)
			case "duplicate":
				ids = []string{ids[0], ids[0]}
			case "malformed":
				ids = []string{"not-a-park"}
				want = passes.RefusalNotFound
			case "missing":
				ids = []string{"park_0123456789abcdef"}
				want = passes.RefusalNotFound
			case "gone":
				p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "gone-fixture"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.st.ApplyParks(t.Context(), p.ID, "gone-fixture", nil, parks.Plan{Gone: ids}, store.Counts{ParksGone: 2}); err != nil {
					t.Fatal(err)
				}
				text = passes.ParkGone + " " + strings.Join(ids, ", ")
			case "walked":
				p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.st.RecordSweep(t.Context(), store.Sweep{Pass: p.ID, Park: ids[1], Circles: 1, Requests: 1, Cost: places.PriceNanos}); err != nil {
					t.Fatal(err)
				}
				if _, err = f.st.FinishWalk(t.Context(), p.ID, store.Counts{}, false); err != nil {
					t.Fatal(err)
				}
				text = passes.AlreadyWalked + " " + ids[1]
			case "busy":
				_, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
				want = passes.RefusalBusy
			}
			configureWalk(f, cl)
			if mode == "stopping" {
				f.eng.Drain(context.Background())
				want = passes.RefusalStopping
			}
			before := f.list()
			if mode == "store" {
				f.d.SetFailing(true)
			}
			p, err := f.eng.Walk(t.Context(), identity.Caller{UserID: "owner"}, ids, false)
			f.d.SetFailing(false)
			if mode == "store" {
				if p.ID != "" || !errors.Is(err, store.ErrUnreachable) {
					t.Fatal(p, err)
				}
			} else {
				assertRefusal(t, p, err, want)
				var r *passes.Refusal
				if !errors.As(err, &r) || r.Text != text {
					t.Fatal(err, text)
				}
			}
			if !reflect.DeepEqual(f.list(), before) || requests.Load() != 0 {
				t.Fatal("refusal changed store or sent request")
			}
			for _, k := range ks {
				ws, err := f.st.ParkWalks(t.Context(), k.ID)
				if err != nil || mode != "walked" && len(ws) != 0 {
					t.Fatal(ws, err)
				}
			}
			if len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
				t.Fatal("refusal emitted event")
			}
		})
	}
}

// R-18E0-JLHM R-HT9C-7JEF R-BJYG-BQS8 R-UF5P-F79H R-YC9K-YJVL R-YDHH-CBMA
func TestWalkAccountingAndFollowOn(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"done", "exact_ceiling", "cut", "cut_after_first", "first_failure", "second_failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			ks := seedWalkParks(t, f, 3)
			ceiling := int64(10)
			if mode == "exact_ceiling" || mode == "cut" || mode == "cut_after_first" {
				setWalkCeiling(t, f, ceiling)
			}
			var made atomic.Int64
			configureWalk(f, walkClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != places.NearbyURL {
					t.Error(r.URL)
				}
				i := made.Add(1)
				switch mode {
				case "first_failure":
					return placesResponse(r, 403, 0, ""), nil
				case "second_failure":
					if i == 2 {
						return placesResponse(r, 500, 0, ""), nil
					}
				case "exact_ceiling":
					if i == 1 || i == 6 {
						return placesResponse(r, 200, places.MaxResults, fmt.Sprint(i)), nil
					}
					return placesResponse(r, 200, 0, ""), nil
				case "cut_after_first":
					if i == 1 || i >= 6 {
						return placesResponse(r, 200, places.MaxResults, fmt.Sprint(i)), nil
					}
					return placesResponse(r, 200, 0, ""), nil
				case "cut":
					return placesResponse(r, 200, places.MaxResults, fmt.Sprint(i)), nil
				}
				return placesResponse(r, 200, 1, fmt.Sprint(i)), nil
			}))
			c := identity.Caller{UserID: "walk-owner", Email: "walk@example.test", RequestID: "walk-request"}
			p, err := f.eng.Walk(t.Context(), c, parkIDs(ks), false)
			if err != nil {
				t.Fatal(err)
			}
			if p.Kind != store.KindWalk || p.Origin != store.OriginWalk || p.Trigger != store.TriggerWalk || p.Status != store.StatusRunning || p.UserID != c.UserID || p.UserEmail != c.Email || p.RequestID != c.RequestID || p.Target != "" || p.EnrichKind != "" || p.Scraper != "" || p.Delivery != "" {
				t.Fatal(p)
			}
			end := f.ended(p.ID)
			wantRequests, wantCircles, wantParks := int64(3), int64(3), int64(3)
			reason := ""
			cut := false
			switch mode {
			case "exact_ceiling":
				wantRequests, wantCircles, wantParks, cut = 10, 10, 2, true
			case "cut_after_first":
				wantRequests, wantCircles, wantParks, cut = 10, 10, 2, true
			case "cut":
				wantRequests, wantCircles, wantParks, cut = 10, 10, 1, true
			case "first_failure":
				wantRequests, wantCircles, wantParks, reason = 1, 0, 0, store.ReasonPlaces
			case "second_failure":
				wantRequests, wantCircles, wantParks, reason = 2, 1, 1, store.ReasonPlaces
			}
			if end.Requests != wantRequests || made.Load() != wantRequests || end.Circles != wantCircles || end.ParksWalked != wantParks || end.Cost != wantCircles*places.PriceNanos || end.Reason != reason || end.CeilingReached != cut {
				t.Fatal(end, made.Load())
			}
			if reason == "" && end.Status != store.StatusDone || reason != "" && end.Status != store.StatusFailed {
				t.Fatal(end)
			}
			ws, err := f.st.PassWalks(t.Context(), p.ID)
			if err != nil || len(ws) != int(wantParks) {
				t.Fatal(ws, err)
			}
			var found, created int64
			for i, w := range ws {
				if w.Park != ks[i].ID {
					t.Fatal(ws, ks)
				}
				found += w.Found
				created += w.NewCandidates
			}
			if end.Found != found || end.NewCandidates != created {
				t.Fatal(end, ws)
			}
			if mode == "done" {
				en := waitFollow(t, f, p.ID)
				if en.Kind != store.KindEnrich || en.Origin != store.OriginPass || en.RequestID != p.RequestID || en.Derived == 0 {
					t.Fatal(en)
				}
				j := waitFollow(t, f, en.ID)
				if j.Kind != store.KindJudge || j.Judged == 0 {
					t.Fatal(j)
				}
			}
			if reason != "" {
				f.eng.Drain(context.Background())
				for _, v := range f.list() {
					if v.Trigger == p.ID {
						t.Fatal("failed walk followed", v)
					}
				}
			}
			f.flush()
			started, finished := 0, 0
			for _, ev := range f.ec.Events() {
				if ev.Attrs["pass"] != p.ID {
					continue
				}
				if ev.Name == trail.Started {
					started++
				}
				if ev.Name == trail.Finished {
					finished++
				}
				if ev.User != c.UserID || ev.RequestID != c.RequestID {
					t.Fatal(ev)
				}
			}
			if started != 1 || finished != 1 {
				t.Fatal(started, finished)
			}
		})
	}
}

// R-UF5P-F79H R-HUH8-LB54 R-OJIZ-52KI
func TestWalkOutlivesCallerConcurrentAdmissionAndDrain(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ks := seedWalkParks(t, f, 2)
	entered := make(chan struct{})
	admitted := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(admitted) })
	var made atomic.Int64
	configureWalk(f, walkClient(func(r *http.Request) (*http.Response, error) {
		i := made.Add(1)
		if i == 1 {
			<-admitted
		}
		if i == 8 {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		n := 0
		if i == 1 || i == 6 {
			n = places.MaxResults
		}
		return placesResponse(r, 200, n, fmt.Sprint(i)), nil
	}))
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	const n = 12
	results := make(chan struct {
		p   store.Pass
		err error
	}, n)
	gate := make(chan struct{})
	for range n {
		wg.Go(func() {
			<-gate
			p, err := f.eng.Walk(ctx, identity.Caller{UserID: "owner"}, parkIDs(ks), false)
			results <- struct {
				p   store.Pass
				err error
			}{p, err}
		})
	}
	close(gate)
	wg.Wait()
	releaseOnce.Do(func() { close(admitted) })
	close(results)
	var winner store.Pass
	for r := range results {
		if r.err == nil {
			if winner.ID != "" {
				t.Fatal("two walk starts")
			}
			winner = r.p
		} else {
			assertRefusal(t, r.p, r.err, passes.RefusalBusy)
		}
	}
	if winner.ID == "" {
		t.Fatal("no admitted walk")
	}
	cancel()
	waitSignal(t, entered)
	f.eng.Drain(context.Background())
	end := f.ended(winner.ID)
	if end.Status != store.StatusFailed || end.Reason != store.ReasonInterrupted || end.ParksWalked != 1 || end.Circles != 7 || end.Requests != 8 || end.Cost != 7*places.PriceNanos || end.CeilingReached {
		t.Fatal(end)
	}
	ws, err := f.st.PassWalks(t.Context(), winner.ID)
	if err != nil || len(ws) != 1 {
		t.Fatal(ws, err)
	}
	p, err := f.eng.Walk(t.Context(), identity.Caller{}, nil, false)
	assertRefusal(t, p, err, passes.RefusalStopping)
}

// R-ZCUK-FO4F R-ZAER-O4N1 R-Z96V-ACWC
func TestWalkDiagnosticsPrecedeEnding(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"places", "store", "nil"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			ks := seedWalkParks(t, f, 1)
			writer := &diagnosticWriter{restore: func() { f.d.SetFailing(false) }}
			cfg := passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Telemetry: f.w, Events: f.em, Stderr: writer}
			cfg.Places = walkClient(func(r *http.Request) (*http.Response, error) {
				if mode == "store" {
					f.d.SetFailing(true)
					return placesResponse(r, 200, 1, "store"), nil
				}
				return placesResponse(r, 403, 0, ""), nil
			})
			if mode == "nil" {
				cfg.Stderr = nil
			}
			f.eng = passes.New(cfg)
			p, err := f.eng.Walk(t.Context(), identity.Caller{UserID: "owner"}, parkIDs(ks), false)
			if err != nil {
				t.Fatal(err)
			}
			end := f.ended(p.ID)
			reason := store.ReasonPlaces
			if mode == "store" {
				reason = store.ReasonStore
			}
			if end.Reason != reason || end.Requests != 1 || end.ParksWalked != 0 {
				t.Fatal(end)
			}
			if mode == "nil" {
				if len(writer.writes) != 0 {
					t.Fatal(writer.writes)
				}
				return
			}
			if len(writer.writes) != 1 {
				t.Fatal(writer.writes)
			}
			text := string(writer.writes[0])
			if mode == "places" && text != "candidates: pass "+p.ID+" failed: places\n\n"+places.FailStatus+" PERMISSION_DENIED\n" {
				t.Fatal(text)
			}
			if !strings.HasPrefix(text, "candidates: pass "+p.ID+" failed: "+reason+"\n\n") || strings.Count(text, "\n") != 3 || strings.Contains(text, cfg.Places.Key) || mode == "places" && !strings.Contains(text, "PERMISSION_DENIED") {
				t.Fatal(text)
			}
		})
	}
}

// R-OFV9-ZRCF R-OH36-DJ34 R-ZBMO-1WDQ
func TestOtherEngineMethodsNeverWalk(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 4 {
			t.Errorf("unexpected scrapes path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if parts[1] == "runs" && parts[3] == "rows" {
			return
		}
		run, scraper := "", ""
		switch {
		case parts[1] == "scrapers" && parts[3] == "current":
			scraper = parts[2]
			run = "current-" + scraper
		case parts[1] == "runs" && parts[3] == "record":
			run = parts[2]
			scraper = strings.TrimPrefix(run, "run-")
		default:
			t.Errorf("unexpected scrapes path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": run, "scraper": scraper, "status": store.StatusDone, "started": "2000-01-01T00:00:00Z", "rows": 0, "new": 0, "changed": 0, "unchanged": 0, "gone": 0})
	}, true)
	var requests atomic.Int64
	cl := walkClient(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return placesResponse(r, 200, 1, "unwanted"), nil
	})
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New(), ppp.New(), form5500.New(), oshaita.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Places: cl})
	c := identity.Caller{UserID: "owner"}
	p, started := f.eng.StartParks(t.Context(), "start")
	if !started || p.Kind != store.KindParks || f.ended(p.ID).Status != store.StatusDone {
		t.Fatal(p, started)
	}
	for _, name := range []string{fmcsa.Name, ppp.Name, form5500.Name, oshaita.Name, parks.Scraper} {
		delivery := "event-" + name
		out := f.eng.Deliver(t.Context(), events.Delivery{Event: events.Event{ID: delivery, Name: trail.Accepted, Attrs: events.Attrs{"scraper": name, "run": "run-" + name, "status": store.StatusDone}}})
		if out != events.OK() {
			t.Fatal(name, out)
		}
		var delivered store.Pass
		for _, v := range f.list() {
			if v.Delivery == delivery {
				if delivered.ID != "" {
					t.Fatal("duplicate delivery pass", name)
				}
				delivered = v
			}
		}
		wantKind := store.KindIngest
		if name == parks.Scraper {
			wantKind = store.KindParks
		}
		if delivered.ID == "" || delivered.Kind != wantKind || delivered.Trigger != "run-"+name {
			t.Fatal(name, delivered)
		}
		if end := f.ended(delivered.ID); end.Status != store.StatusDone {
			t.Fatal(name, end)
		}
		if name != parks.Scraper {
			if en := waitFollow(t, f, delivered.ID); en.Kind != store.KindEnrich || en.Status != store.StatusDone {
				t.Fatal(name, en)
			}
		}
	}
	p, err := f.eng.Ingest(t.Context(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if end := f.ended(p.ID); end.Status != store.StatusDone {
		t.Fatal(end)
	}
	p, err = f.eng.Judge(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	f.ended(p.ID)
	p, err = f.eng.Enrich(t.Context(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	f.ended(p.ID)
	p, err = f.eng.EditParkRules(t.Context(), c, store.ParkRulesEdit{})
	if err != nil {
		t.Fatal(err)
	}
	if end := f.ended(p.ID); end.Status != store.StatusDone {
		t.Fatal(end)
	}
	ids := seedTarget(t, f, 1)
	p, err = f.eng.EnrichTarget(t.Context(), c, enrichers.KindLinkFacts, ids[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if end := f.ended(p.ID); end.Derived > 0 {
		waitFollow(t, f, p.ID)
	}
	if err = f.eng.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if out := f.eng.Deliver(t.Context(), events.Delivery{Event: events.Event{ID: "daily-judge", Name: trail.AcceptedJudge}}); out != events.OK() {
		t.Fatal(out)
	}
	for _, v := range f.list() {
		if v.Delivery == "daily-judge" {
			f.ended(v.ID)
		}
	}
	f.eng.DeliverCampaign(t.Context(), events.Delivery{Event: events.Event{Name: trail.AcceptedAdded}})
	f.eng.Drain(context.Background())
	if requests.Load() != 0 {
		t.Fatal(requests.Load())
	}
	for _, p := range f.list() {
		if p.Kind == store.KindWalk {
			t.Fatal(p)
		}
	}
}

// R-SOKW-PW7E R-8WDK-MWRK R-SS8L-V7FH
func TestWalkRecordEveryFieldAndOrigin(t *testing.T) {
	start := time.Date(2004, 3, 2, 1, 0, 0, 0, time.FixedZone("fixture", 3600))
	p := store.Pass{ID: "pass", Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, Target: "target", EnrichKind: "kind", Scraper: "scraper", ParcelRun: "parcel", Status: store.StatusDone, Reason: "reason", Started: start, Finished: start.Add(time.Hour), Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Derived: 10, Due: 11, Fetched: 12, HTTPS: 13, HTTP: 14, None: 15, Runs: 16, RunsFailed: 17, Pages: 18, Cost: 19, ParcelsRead: 20, ByLabel: 21, ByValue: 22, ByCandidate: 23, ParksCreated: 24, ParksUpdated: 25, ParksGone: 26, ParksGrown: 27, ParksWalked: 28, Circles: 29, Requests: 30, Found: 31, NewCandidates: 32, SearchDifferent: 33, SearchStands: 34, SearchNone: 35, CeilingReached: true, UserID: "owner", UserEmail: "owner@example.test"}
	want := passes.Record{"pass", store.KindWalk, store.OriginWalk, store.TriggerWalk, "target", "kind", "scraper", "parcel", store.StatusDone, "reason", start.UTC().Format(time.RFC3339), p.Finished.UTC().Format(time.RFC3339), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, true, "owner", "owner@example.test"}
	if got := passes.RecordOf(p); got != want {
		t.Fatal(got, want)
	}
	raw, err := json.Marshal(want)
	expected := `{"id":"pass","kind":"walk","origin":"walk","trigger":"walk","target":"target","enrich_kind":"kind","scraper":"scraper","parcel_run":"parcel","status":"done","reason":"reason","started":"2004-03-02T00:00:00Z","finished":"2004-03-02T01:00:00Z","rows":1,"new":2,"changed":3,"unchanged":4,"gone":5,"created":6,"linked":7,"judged":8,"unranged":9,"derived":10,"due":11,"fetched":12,"https":13,"http":14,"none":15,"runs":16,"runs_failed":17,"pages":18,"cost":19,"parcels_read":20,"by_label":21,"by_value":22,"by_candidate":23,"parks_created":24,"parks_updated":25,"parks_gone":26,"parks_grown":27,"parks_walked":28,"circles":29,"requests":30,"found":31,"new_candidates":32,"search_different":33,"search_stands":34,"search_none":35,"ceiling_reached":true,"user":"owner","email":"owner@example.test"}`
	if err != nil || string(raw) != expected {
		t.Fatal(string(raw), err)
	}
	for _, origin := range []string{store.OriginIngest, store.OriginJudge, store.OriginEnrich, store.OriginRules, store.OriginWalk, store.OriginRun, store.OriginCron, store.OriginPass, store.OriginStart} {
		p.Origin = origin
		p.Finished = time.Time{}
		got := passes.RecordOf(p)
		hand := origin == store.OriginIngest || origin == store.OriginJudge || origin == store.OriginEnrich || origin == store.OriginRules || origin == store.OriginWalk
		if got.Finished != "" || hand && (got.User != p.UserID || got.Email != p.UserEmail) || !hand && (got.User != "" || got.Email != "") {
			t.Fatal(got)
		}
	}
}

// R-18E0-JLHM R-HT9C-7JEF
func TestWalkKeepsCeilingReadDuringAdmission(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ks := seedWalkParks(t, f, 1)
	setWalkCeiling(t, f, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var made atomic.Int64
	configureWalk(f, walkClient(func(r *http.Request) (*http.Response, error) {
		if made.Add(1) == 1 {
			close(entered)
			<-release
		}
		return placesResponse(r, 200, places.MaxResults, "same"), nil
	}))
	p, err := f.eng.Walk(t.Context(), identity.Caller{UserID: "owner"}, parkIDs(ks), false)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	setWalkCeiling(t, f, 100)
	once.Do(func() { close(release) })
	end := f.ended(p.ID)
	if end.Status != store.StatusDone || end.Requests != 2 || !end.CeilingReached || made.Load() != 2 {
		t.Fatal(end, made.Load())
	}
}

// R-UF5P-F79H
func TestWalkUnrecordableEndingEmitsNoFinishAndDrains(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ks := seedWalkParks(t, f, 1)
	written := make(chan struct{})
	writer := &diagnosticWriter{restore: func() { close(written) }}
	cl := walkClient(func(r *http.Request) (*http.Response, error) {
		f.d.SetFailing(true)
		return placesResponse(r, 200, 1, "unrecorded"), nil
	})
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Telemetry: f.w, Events: f.em, Places: cl, Stderr: writer})
	p, err := f.eng.Walk(t.Context(), identity.Caller{UserID: "owner"}, parkIDs(ks), false)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, written)
	f.eng.Drain(context.Background())
	f.d.SetFailing(false)
	saved, err := f.st.GetPass(t.Context(), p.ID)
	if err != nil || saved.Status != store.StatusRunning {
		t.Fatal(saved, err)
	}
	f.flush()
	for _, ev := range f.ec.Events() {
		if ev.Attrs["pass"] == p.ID && ev.Name == trail.Finished {
			t.Fatal(ev)
		}
	}
}

// R-ODFH-87V1 R-UF5P-F79H
func TestWalkAgainAndStartedBeforeReturn(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ks := seedWalkParks(t, f, 1)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	configureWalk(f, walkClient(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return placesResponse(r, 200, 1, "again"), nil
	}))
	prior, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.RecordSweep(t.Context(), store.Sweep{Pass: prior.ID, Park: ks[0].ID, Circles: 1, Requests: 1, Cost: places.PriceNanos}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.FinishWalk(t.Context(), prior.ID, store.Counts{}, false); err != nil {
		t.Fatal(err)
	}
	c := identity.Caller{UserID: "owner", RequestID: "again-request"}
	p, err := f.eng.Walk(t.Context(), c, parkIDs(ks), true)
	if err != nil {
		t.Fatal(err)
	}
	f.flush()
	for _, sink := range []string{"telemetry", "bus"} {
		count := 0
		if sink == "telemetry" {
			for _, ev := range f.tc.Events() {
				if ev.Attrs["pass"] == p.ID {
					if ev.Name != trail.Started || ev.User != c.UserID || ev.RequestID != c.RequestID {
						t.Fatal(ev)
					}
					count++
				}
			}
		} else {
			for _, ev := range f.ec.Events() {
				if ev.Attrs["pass"] == p.ID {
					if ev.Name != trail.Started || ev.User != c.UserID || ev.RequestID != c.RequestID {
						t.Fatal(ev)
					}
					count++
				}
			}
		}
		if count != 1 {
			t.Fatal(sink, count)
		}
	}
	waitSignal(t, entered)
	once.Do(func() { close(release) })
	end := f.ended(p.ID)
	if end.Status != store.StatusDone || end.ParksWalked != 1 {
		t.Fatal(end)
	}
	ws, err := f.st.ParkWalks(t.Context(), ks[0].ID)
	if err != nil || len(ws) != 2 {
		t.Fatal(ws, err)
	}
	f.flush()
	for _, events := range [][]string{func() []string {
		var names []string
		for _, ev := range f.tc.Events() {
			if ev.Attrs["pass"] == p.ID {
				names = append(names, ev.Name)
			}
		}
		return names
	}(), func() []string {
		var names []string
		for _, ev := range f.ec.Events() {
			if ev.Attrs["pass"] == p.ID {
				names = append(names, ev.Name)
			}
		}
		return names
	}()} {
		if !reflect.DeepEqual(events, []string{trail.Started, trail.Finished}) {
			t.Fatal(events)
		}
	}
}
