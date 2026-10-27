package passes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

func parcel(w http.ResponseWriter, i int, county, label string) {
	_ = json.NewEncoder(w).Encode(parcelObject(i, county, label))
}
func parcelObject(i int, county, label string) map[string]any {
	pin := fmt.Sprintf("parcel-%d", i)
	return map[string]any{"scraper": parks.Scraper, "run": "parcel_run", "source_id": pin, "digest": pin, "scraped": "2000-01-01T00:00:00Z", "name": pin, "names": []string{}, "address": map[string]any{"street": fmt.Sprintf("%d Industrial Street", i), "city": "Fixture Town", "zip": "55072", "county": county}, "lat": 45.0 + float64(i)*(500/parks.EarthRadius)*180/3.141592653589793, "lon": -93.0, "people": []any{}, "facts": map[string]any{"state_pin": pin, "use_class": []string{label}, "homestead": "Yes", "emv_bldg": 100}, "raw": map[string]any{}}
}
func parcelRecord(w http.ResponseWriter, n int, status string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "parcel_run", "scraper": parks.Scraper, "status": status, "started": "2000-01-01T00:00:00Z", "rows": n, "new": 0, "changed": 0, "unchanged": 0, "gone": 0})
}
func seedLabel(t *testing.T, f *fixture) (string, string) {
	t.Helper()
	rules, counties, err := f.st.ParkRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, county := range counties {
		if len(rules.Labels[county]) > 0 {
			return county, rules.Labels[county][0]
		}
	}
	t.Fatal("seed label missing")
	return "", ""
}
func parcelDelivery(f *fixture, id string) events.Outcome {
	return f.eng.Deliver(events.NewContext(identity.NewContext(context.Background(), identity.Caller{RequestID: "park_delivery"}), events.Cause{ID: "evt_abababababababab", Depth: 2}), events.Delivery{Event: events.Event{ID: id, Name: trail.Accepted, Attrs: events.Attrs{"scraper": parks.Scraper, "run": "parcel_run", "status": store.StatusDone}}})
}
func assertParkZeroOtherCounts(t *testing.T, p store.Pass) {
	t.Helper()
	if p.Rows != 0 || p.New != 0 || p.Changed != 0 || p.Unchanged != 0 || p.Gone != 0 || p.Created != 0 || p.Linked != 0 || p.Judged != 0 || p.Unranged != 0 || p.Derived != 0 || p.Due != 0 || p.Fetched != 0 || p.HTTPS != 0 || p.HTTP != 0 || p.None != 0 || p.Runs != 0 || p.RunsFailed != 0 || p.Pages != 0 || p.Cost != 0 {
		t.Fatal(p)
	}
}

// R-DXL3-JA67 R-E00W-ATNL R-E2GP-2D4Z R-DYSZ-X1WW R-3BZB-PF2P R-3D78-36TE R-3284-N955 R-34NX-ESMJ R-DP1S-UVZC R-33G1-10VU R-2ZSB-VPNR
func TestParksDeliveryDerivesAndDoesNotIngest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var county, label string
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			parcelRecord(w, 3, store.StatusDone)
			return
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		for i := range 3 {
			parcel(w, i, county, label)
		}
	}, true)
	county, label = seedLabel(t, f)
	if got := parcelDelivery(f, "parcel_event"); got != events.OK() {
		t.Fatal(got)
	}
	waitSignal(t, entered)
	ps := f.list()
	if len(ps) != 1 || ps[0].Kind != store.KindParks || ps[0].Origin != store.OriginRun || ps[0].Trigger != "parcel_run" || ps[0].Delivery != "parcel_event" || ps[0].UserID != "" || ps[0].UserEmail != "" {
		t.Fatal(ps)
	}
	if got := parcelDelivery(f, "different_event"); got != events.Skip() {
		t.Fatal(got)
	}
	close(release)
	p := f.ended(ps[0].ID)
	if p.Status != store.StatusDone || p.ParcelRun != "parcel_run" || p.ParcelsRead != 3 || p.ByLabel != 3 || p.ByValue != 0 || p.ByCandidate != 0 || p.ParksCreated != 1 || p.ParksUpdated != 0 || p.ParksGone != 0 || p.ParksGrown != 0 {
		t.Fatal(p)
	}
	assertParkZeroOtherCounts(t, p)
	active, gone, err := f.st.CountParks(ctx)
	if err != nil || active != 1 || gone != 0 {
		t.Fatal(active, gone, err)
	}
	prior, err := f.st.PriorParks(ctx)
	if err != nil || len(prior) != 1 || prior[0].Count != 3 {
		t.Fatal(prior, err)
	}
	parcels, err := f.st.ParkParcels(ctx, prior[0].ID)
	if err != nil || len(parcels) != 3 {
		t.Fatal(parcels, err)
	}
	for _, p := range parcels {
		if p.Rule != parks.RuleLabel {
			t.Fatal(p)
		}
	}
	run, err := f.st.ParksRun(ctx)
	if err != nil || run != "parcel_run" {
		t.Fatal(run, err)
	}
	cs, err := f.st.ParkCountiesRead(ctx)
	if err != nil || !reflect.DeepEqual(cs, []string{county}) {
		t.Fatal(cs, err)
	}
	if len(f.list()) != 1 {
		t.Fatal("parks follow-on", f.list())
	}
	candidates, err := f.st.ListCandidates(ctx, store.CandidateFilter{}, "", "", 10)
	if err != nil || len(candidates) != 0 {
		t.Fatal(candidates, err)
	}
	// Compare every persisted park read with applying the pure plan to the same empty state.
	expected := newFixture(t, nil, false)
	rules, _, err := expected.st.ParkRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kept := []parks.Parcel{}
	for i := range 3 {
		b, err := json.Marshal(parcelObject(i, county, label))
		if err != nil {
			t.Fatal(err)
		}
		var row scrapes.Row
		row.SourceID = fmt.Sprintf("parcel-%d", i)
		if err = json.Unmarshal(b, &row); err != nil {
			t.Fatal(err)
		}
		p := parks.ParcelOf(row)
		p.Rule = parks.Classify(p, rules, nil)
		kept = append(kept, p)
	}
	plan := parks.Match(parks.Clusters(kept, rules), nil)
	recorded, err := expected.st.AddPass(ctx, store.Pass{Kind: store.KindParks, Origin: store.OriginRun, Trigger: "parcel_run", Delivery: "parcel_event"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = expected.st.ApplyParks(ctx, recorded.ID, "parcel_run", []string{county}, plan, store.Counts{ParcelsRead: 3, ByLabel: 3, ParksCreated: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want, got := parkReadSnapshot(t, expected.st), parkReadSnapshot(t, f.st); !reflect.DeepEqual(want, got) {
		t.Fatal(want, got)
	}
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].path != "/runs/parcel_run/record" || reqs[1].path != "/runs/parcel_run/rows" {
		t.Fatal(reqs)
	}
	for _, req := range reqs {
		if req.user != "candidates" || req.email != "" || req.id != "park_delivery" {
			t.Fatal(req)
		}
	}
}

// R-E18S-OLEA R-DNTW-H48N R-E2GP-2D4Z R-E3OL-G4VO R-E4WH-TWMD R-S35A-G1HS
func TestParksDeliveryAndRuleRefusals(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing", "unfinished", "unavailable", "malformed", "nil", "done"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "missing":
					w.WriteHeader(404)
				case "unfinished":
					parcelRecord(w, 0, store.StatusRunning)
				case "unavailable":
					w.WriteHeader(503)
				case "malformed":
					_, _ = w.Write([]byte("{}"))
				default:
					if strings.HasSuffix(r.URL.Path, "/record") {
						parcelRecord(w, 0, store.StatusDone)
					}
				}
			}, mode != "nil")
			got := parcelDelivery(f, "parcel_event")
			if mode == "missing" || mode == "unfinished" {
				if got != events.Skip() || len(f.list()) != 0 {
					t.Fatal(got, f.list())
				}
				return
			}
			if got != events.OK() {
				t.Fatal(got)
			}
			p := f.ended(f.list()[0].ID)
			if mode == "done" && (p.Status != store.StatusDone || p.ParcelRun != "parcel_run") {
				t.Fatal(p)
			}
			if mode != "done" && (p.Status != store.StatusFailed || p.Reason != store.ReasonScrapes || p.ParcelRun != "" || p.ParcelsRead != 0) {
				t.Fatal(p)
			}
			if parcelDelivery(f, "parcel_event") != events.Skip() {
				t.Fatal("redelivery")
			}
		})
	}
	for _, mode := range []string{"nil", "invalid", "busy", "store", "stopping", "nil_invalid", "stopping_nil"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, mode != "nil" && mode != "nil_invalid" && mode != "stopping_nil")
			ctx := context.Background()
			edit := store.ParkRulesEdit{}
			before, cs, err := f.st.ParkRules(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "nil_invalid":
				bad := int64(0)
				edit.Distance = &bad
			case "invalid":
				bad := int64(0)
				edit.Distance = &bad
				fallthrough
			case "busy":
				_, err = f.st.AddPass(ctx, store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "held"})
				if err != nil {
					t.Fatal(err)
				}
			case "store":
				f.d.SetFailing(true)
			case "stopping", "stopping_nil":
				f.eng.Drain(ctx)
			}
			ps := f.list
			if mode == "store" {
				ps = nil
			}
			var old []store.Pass
			if ps != nil {
				old = ps()
			}
			p, err := f.eng.EditParkRules(ctx, identity.Caller{UserID: "owner"}, edit)
			want := map[string]string{"nil": passes.RefusalUnavailable, "invalid": passes.RefusalInvalid, "busy": passes.RefusalBusy, "store": passes.RefusalUnavailable, "stopping": passes.RefusalStopping, "nil_invalid": passes.RefusalUnavailable, "stopping_nil": passes.RefusalStopping}[mode]
			assertRefusal(t, p, err, want)
			f.d.SetFailing(false)
			after, ac, err := f.st.ParkRules(ctx)
			if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(cs, ac) {
				t.Fatal(after, err)
			}
			if ps != nil && !reflect.DeepEqual(old, f.list()) {
				t.Fatal("refused pass")
			}
			f.flush()
			if len(f.ec.Events()) != 0 || len(f.requests()) != 0 {
				t.Fatal("refusal effects")
			}
		})
	}
}

// R-E64E-7OD2 R-EENO-W2JX R-EDFS-IAT8 R-DP1S-UVZC R-3EF4-GYK3 R-3108-9HEG
func TestParkRulesPassOutlivesCallerAndUsesItsIdentity(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			parcelRecord(w, 0, store.StatusDone)
		}
	}, true)
	ctx, cancel := context.WithCancel(context.Background())
	c := identity.Caller{UserID: "park_owner", Email: "owner@example.test", RequestID: "rules_request"}
	n := int64(2)
	p, err := f.eng.EditParkRules(ctx, c, store.ParkRulesEdit{MinParcels: &n})
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	f.flush()
	if p.Kind != store.KindParks || p.Origin != store.OriginRules || p.Trigger != store.TriggerParkRules || p.UserID != c.UserID || p.UserEmail != c.Email || p.RequestID != c.RequestID || p.Status != store.StatusRunning {
		t.Fatal(p)
	}
	r, _, err := f.st.ParkRules(context.Background())
	if err != nil || r.MinParcels != n {
		t.Fatal(r, err)
	}
	if len(f.ec.Events()) != 1 || f.ec.Events()[0].Name != trail.Started {
		t.Fatal(f.ec.Events())
	}
	busy, err := f.eng.EditParkRules(ctx, c, store.ParkRulesEdit{})
	assertRefusal(t, busy, err, passes.RefusalBusy)
	cancel()
	close(release)
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.ParcelRun != "parcel_run" {
		t.Fatal(p)
	}
	for _, r := range f.requests() {
		if r.user != c.UserID || r.email != c.Email || r.id != c.RequestID {
			t.Fatal(r)
		}
	}
}

// R-E8K6-Z7UG R-38BM-K3UM R-39JI-XVLB R-3ARF-BNC0 R-EENO-W2JX R-EDFS-IAT8
func TestStartParksCurrentRunAndNoops(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"nil", "missing", "unfinished", "malformed", "busy", "same", "done", "stopping", "store"} {
		t.Run(mode, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			failStore := func() {}
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/current") {
					switch mode {
					case "missing":
						w.WriteHeader(404)
					case "unfinished":
						parcelRecord(w, 0, store.StatusRunning)
					case "malformed":
						_, _ = w.Write([]byte("{}"))
					default:
						if mode == "store" {
							failStore()
						}
						parcelRecord(w, 0, store.StatusDone)
					}
					return
				}
				once.Do(func() { close(entered) })
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}, mode != "nil")
			failStore = func() { f.d.SetFailing(true) }
			if mode == "stopping" {
				f.eng.Drain(context.Background())
			}
			if mode == "busy" {
				_, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "same" {
				p, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "parcel_run"})
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.st.ApplyParks(context.Background(), p.ID, "parcel_run", nil, parks.Plan{}, store.Counts{})
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, ok := f.eng.StartParks(ctx, "start_request")
			f.d.SetFailing(false)
			if mode == "store" && (len(f.requests()) != 1 || len(f.list()) != 0) {
				t.Fatal(f.requests(), f.list())
			}
			if mode != "done" {
				if ok || p != (store.Pass{}) {
					t.Fatal(p, ok)
				}
				f.flush()
				if len(f.ec.Events()) != 0 {
					t.Fatal(f.ec.Events())
				}
				return
			}
			if !ok || p.Kind != store.KindParks || p.Origin != store.OriginStart || p.Trigger != "parcel_run" || p.RequestID != "start_request" || p.UserID != "" || p.UserEmail != "" || p.Delivery != "" || p.Status != store.StatusRunning {
				t.Fatal(p, ok)
			}
			waitSignal(t, entered)
			cancel()
			f.flush()
			if len(f.ec.Events()) != 1 {
				t.Fatal(f.ec.Events())
			}
			close(release)
			if ended := f.ended(p.ID); ended.Status != store.StatusDone {
				t.Fatal(ended)
			}
			for _, r := range f.requests() {
				if r.user != "candidates" || r.email != "" || r.id != "start_request" {
					t.Fatal(r)
				}
			}
		})
	}
}

// R-EIBE-1DS0 R-373Q-6C3X R-3108-9HEG
func TestParksFailureLeavesPriorParks(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"short", "drain", "no_current"} {
		t.Run(mode, func(t *testing.T) {
			var county, label string
			entered := make(chan struct{})
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/record") {
					parcelRecord(w, 3, store.StatusDone)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/current") {
					w.WriteHeader(404)
					return
				}
				parcel(w, 0, county, label)
				w.(http.Flusher).Flush()
				close(entered)
				switch mode {
				case "drain":
					<-r.Context().Done()
				default:
				}
			}, true)
			county, label = seedLabel(t, f)
			ctx := context.Background()
			held, err := f.st.AddPass(ctx, store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "prior_run"})
			if err != nil {
				t.Fatal(err)
			}
			ps := []parks.Parcel{}
			for i := range 3 {
				lat, lon := 45.0+float64(i)*0.0001, -93.0
				ps = append(ps, parks.Parcel{PIN: fmt.Sprintf("old-%d", i), County: county, City: "Old Town", Lat: &lat, Lon: &lon, Rule: parks.RuleLabel})
			}
			_, err = f.st.ApplyParks(ctx, held.ID, "prior_run", []string{county}, parks.Match(parks.Clusters(ps, parks.Rules{Distance: 600, MinParcels: 3}), nil), store.Counts{})
			if err != nil {
				t.Fatal(err)
			}
			before := parkReadSnapshot(t, f.st)
			if mode == "no_current" {
				p, err := f.eng.EditParkRules(ctx, identity.Caller{UserID: "owner"}, store.ParkRulesEdit{})
				if err != nil {
					t.Fatal(err)
				}
				end := f.ended(p.ID)
				if end.Reason != store.ReasonScrapes {
					t.Fatal(end)
				}
				assertParkZeroOtherCounts(t, end)
				if end.ParcelsRead != 0 || end.ByLabel != 0 || end.ByValue != 0 || end.ByCandidate != 0 || end.ParksCreated != 0 || end.ParksUpdated != 0 || end.ParksGone != 0 || end.ParksGrown != 0 {
					t.Fatal(end)
				}
			} else {
				if got := parcelDelivery(f, "new_event"); got != events.OK() {
					t.Fatal(got)
				}
				waitSignal(t, entered)
				if mode == "drain" {
					f.eng.Drain(ctx)
				}

				var p store.Pass
				for _, x := range f.list() {
					if x.Trigger == "parcel_run" {
						p = x
					}
				}
				end := f.ended(p.ID)
				assertParkZeroOtherCounts(t, end)
				if end.Status != store.StatusFailed || end.ParcelRun != "" || end.ParcelsRead != 0 || end.ByLabel != 0 || end.ByValue != 0 || end.ByCandidate != 0 || end.ParksCreated != 0 || end.ParksUpdated != 0 || end.ParksGone != 0 || end.ParksGrown != 0 {
					t.Fatal(end)
				}
			}
			after := parkReadSnapshot(t, f.st)
			if !reflect.DeepEqual(before, after) {
				t.Fatal(before, after)
			}
			run, err := f.st.ParksRun(ctx)
			if err != nil || run != "prior_run" {
				t.Fatal(run, err)
			}
		})
	}
}

// R-EJJA-F5IP
func TestParksStreamingHeapDoesNotRetainRejectedRows(t *testing.T) {
	var baseline, final uint64
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			parcelRecord(w, 100000, store.StatusDone)
			return
		}
		type owner struct {
			Name string `json:"name"`
			Role string `json:"role"`
		}
		type row struct {
			Scraper  string   `json:"scraper"`
			Run      string   `json:"run"`
			SourceID string   `json:"source_id"`
			Digest   string   `json:"digest"`
			Name     string   `json:"name"`
			Scraped  string   `json:"scraped"`
			Names    []string `json:"names"`
			People   []owner  `json:"people"`
			Facts    struct{} `json:"facts"`
			Raw      struct{} `json:"raw"`
		}
		encoder := json.NewEncoder(w)
		for i := range 100000 {
			x := row{Scraper: parks.Scraper, Run: "parcel_run", SourceID: fmt.Sprintf("%0100d", i), Digest: "fixture", Name: "parcel", Scraped: "2000-01-01T00:00:00Z", Names: []string{}, People: []owner{{Name: strings.Repeat("X", 900), Role: parks.OwnerRole}}}
			_ = encoder.Encode(x)
			if i == 999 || i == 99999 {
				w.(http.Flusher).Flush()
				runtime.GC()
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				if i == 999 {
					baseline = m.HeapAlloc
				} else {
					final = m.HeapAlloc
				}
			}
		}
	}, true)
	runtime.GC()
	if got := parcelDelivery(f, "heap_event"); got != events.OK() {
		t.Fatal(got)
	}
	id := f.list()[0].ID
	var end store.Pass
waitForEnding:
	for {
		for _, event := range f.ec.Events() {
			if event.Name == trail.Finished && event.Attrs["pass"] == id {
				var err error
				end, err = f.st.GetPass(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				break waitForEnding
			}
		}
		<-f.changed
	}
	f.flush()
	if end.Status != store.StatusDone || end.ParcelsRead != 100000 || end.ByLabel != 0 || end.ByValue != 0 || end.ByCandidate != 0 {
		t.Fatal(end)
	}
	if final > baseline && final-baseline >= 4194304 {
		t.Fatalf("heap grew %d bytes", final-baseline)
	}
}

// R-M78O-D1KG R-DK67-BT0K R-DMM0-3CHY
func TestParksRecordCarriesEveryCountAndRulesStarter(t *testing.T) {
	t.Parallel()
	p := store.Pass{Kind: store.KindParks, Origin: store.OriginRules, ParcelRun: "parcels", ParcelsRead: 1, ByLabel: 2, ByValue: 3, ByCandidate: 4, ParksCreated: 5, ParksUpdated: 6, ParksGone: 7, ParksGrown: 8, UserID: "owner", UserEmail: "owner@example.test"}
	r := passes.RecordOf(p)
	if r.ParcelRun != p.ParcelRun || r.ParcelsRead != 1 || r.ByLabel != 2 || r.ByValue != 3 || r.ByCandidate != 4 || r.ParksCreated != 5 || r.ParksUpdated != 6 || r.ParksGone != 7 || r.ParksGrown != 8 || r.User != p.UserID || r.Email != p.UserEmail {
		t.Fatal(r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"id":"","kind":"parks","origin":"rules","trigger":"","parcel_run":"parcels","status":"","started":"0001-01-01T00:00:00Z","rows":0,"new":0,"changed":0,"unchanged":0,"gone":0,"created":0,"linked":0,"judged":0,"unranged":0,"derived":0,"due":0,"fetched":0,"https":0,"http":0,"none":0,"runs":0,"runs_failed":0,"pages":0,"cost":0,"parcels_read":1,"by_label":2,"by_value":3,"by_candidate":4,"parks_created":5,"parks_updated":6,"parks_gone":7,"parks_grown":8,"user":"owner","email":"owner@example.test"}`
	if string(b) != expected {
		t.Fatal(string(b))
	}
	p.Origin = store.OriginStart
	r = passes.RecordOf(p)
	if r.User != "" || r.Email != "" {
		t.Fatal(r)
	}

}

type parkRestoreWriter struct {
	f      *fixture
	writes [][]byte
}

func (w *parkRestoreWriter) Write(b []byte) (int, error) {
	w.writes = append(w.writes, append([]byte(nil), b...))
	w.f.d.SetFailing(false)
	return len(b), nil
}

// R-3108-9HEG R-2UWQ-CMOZ
func TestParksStoreFailureRestoresForFailedEnding(t *testing.T) {
	t.Parallel()
	var f *fixture
	f = newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			parcelRecord(w, 0, store.StatusDone)
			return
		}
		f.d.SetFailing(true)
	}, true)
	f.eng.Drain(context.Background())
	out := &parkRestoreWriter{f: f}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Stderr: out})
	if got := parcelDelivery(f, "store_event"); got != events.OK() {
		t.Fatal(got)
	}
	end := f.ended(f.list()[0].ID)
	if end.Status != store.StatusFailed || end.Reason != store.ReasonStore || end.ParcelRun != "" || end.ParcelsRead != 0 || end.ByLabel != 0 || end.ByValue != 0 || end.ByCandidate != 0 || end.ParksCreated != 0 || end.ParksUpdated != 0 || end.ParksGone != 0 || end.ParksGrown != 0 {
		t.Fatal(end)
	}
	assertParkZeroOtherCounts(t, end)
	if len(out.writes) != 1 || !strings.HasPrefix(string(out.writes[0]), "candidates: pass "+end.ID+" failed: store\n\n") || !strings.HasSuffix(string(out.writes[0]), "\n") {
		t.Fatal(out.writes)
	}
}

var _ io.Writer = (*parkRestoreWriter)(nil)
