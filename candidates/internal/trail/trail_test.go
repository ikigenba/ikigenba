package trail_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// R-775C-V5DE R-WLNU-3JBB
func TestDeclarations(t *testing.T) {
	if trail.Started != "pass.started" || trail.Finished != "pass.finished" || trail.Accepted != "scrape.finished" || trail.AcceptedJudge != "cron.judge_candidates.fired" || trail.AcceptedAdded != "campaign.member.added" || trail.AcceptedRejected != "campaign.member.rejected" || trail.AcceptedDelivered != "campaign.piece.delivered" {
		t.Fatal("names")
	}
	want := []events.Emission{{Event: "pass.started", Attrs: []string{"pass", "kind", "origin", "trigger", "target", "enrich_kind"}}, {Event: "pass.finished", Attrs: []string{"pass", "kind", "origin", "trigger", "target", "enrich_kind", "status", "reason", "parcel_run", "rows", "new", "changed", "unchanged", "gone", "created", "linked", "judged", "unranged", "derived", "due", "fetched", "https", "http", "none", "runs", "runs_failed", "pages", "cost", "parcels_read", "by_label", "by_value", "by_candidate", "parks_created", "parks_updated", "parks_gone", "parks_grown", "parks_walked", "circles", "requests", "found", "new_candidates", "search_different", "search_stands", "search_none"}}}
	a, b := trail.Emits(), trail.Emits()
	if !reflect.DeepEqual(a, want) || !reflect.DeepEqual(b, want) {
		t.Fatalf("declarations %v", a)
	}
	a[0].Event = "changed"
	a[0].Attrs[0] = "changed"
	a[1].Attrs[0] = "changed"
	if !reflect.DeepEqual(b, want) || !reflect.DeepEqual(trail.Emits(), want) {
		t.Fatal("shared storage")
	}
}

// R-WPBJ-8UJE R-ZUG2-AZCG R-ZVNY-OR35
func TestRecord(t *testing.T) {
	t.Setenv("IKIGENBA_SERVICES", "")
	tc, ec := &telemetry.Capture{}, &events.Capture{}
	now := func() time.Time { return time.Unix(100, 0) }
	pause := func(context.Context, time.Duration) {}
	w := telemetry.New(telemetry.Config{Service: "candidates", Sink: tc, Now: now, Sleep: pause, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 256))})
	em := events.New(events.Config{Service: "candidates", Sink: ec, Now: now, Sleep: pause, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 256)), Emits: trail.Emits()})
	defer w.Shutdown(context.Background(), "test")
	defer em.Shutdown(context.Background())
	p := store.Pass{ID: "pass_fixture", Trigger: "run_fixture", Status: store.StatusDone, RequestID: "saved_request", UserID: "saved_user", Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Derived: 15, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, Target: "pl_fixture", EnrichKind: "site_text", Runs: 16, RunsFailed: 17, Pages: 18, Cost: 19, ParcelRun: "parcel_fixture", ParcelsRead: 20, ByLabel: 21, ByValue: 22, ByCandidate: 23, ParksCreated: 24, ParksUpdated: 25, ParksGone: 26, ParksGrown: 27, ParksWalked: 28, Circles: 29, Requests: 30, Found: 31, NewCandidates: 32, SearchDifferent: 33, SearchStands: 34, SearchNone: 35}
	ctx := events.NewContext(identity.NewContext(context.Background(), identity.Caller{UserID: "other", RequestID: "other"}), events.Cause{ID: "evt_0303030303030303", Depth: 4})
	trail.Record(ctx, w, em, trail.Started, p)
	trail.Record(ctx, w, em, trail.Finished, p)
	trail.Record(ctx, w, em, trail.Accepted, p)
	trail.Record(ctx, w, em, trail.AcceptedJudge, p)
	trail.Record(ctx, w, em, "other", p)
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := em.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts, es := tc.Events(), ec.Events()
	if len(ts) != 2 || len(es) != 2 {
		t.Fatalf("counts %d %d", len(ts), len(es))
	}
	want := []telemetry.Attrs{{"pass": p.ID, "kind": p.Kind, "origin": p.Origin, "trigger": p.Trigger, "target": p.Target, "enrich_kind": p.EnrichKind}, {"pass": p.ID, "kind": p.Kind, "origin": p.Origin, "trigger": p.Trigger, "target": p.Target, "enrich_kind": p.EnrichKind, "status": p.Status, "reason": p.Reason, "rows": int64(1), "new": int64(2), "changed": int64(3), "unchanged": int64(4), "gone": int64(5), "created": int64(6), "linked": int64(7), "judged": int64(8), "unranged": int64(9), "derived": int64(15), "due": int64(10), "fetched": int64(11), "https": int64(12), "http": int64(13), "none": int64(14), "runs": int64(16), "runs_failed": int64(17), "pages": int64(18), "cost": int64(19), "parcel_run": p.ParcelRun, "parcels_read": int64(20), "by_label": int64(21), "by_value": int64(22), "by_candidate": int64(23), "parks_created": int64(24), "parks_updated": int64(25), "parks_gone": int64(26), "parks_grown": int64(27), "parks_walked": int64(28), "circles": int64(29), "requests": int64(30), "found": int64(31), "new_candidates": int64(32), "search_different": int64(33), "search_stands": int64(34), "search_none": int64(35)}}
	for i := range ts {
		if ts[i].Name != []string{trail.Started, trail.Finished}[i] || es[i].Name != ts[i].Name || ts[i].RequestID != p.RequestID || ts[i].User != p.UserID || es[i].RequestID != p.RequestID || es[i].User != p.UserID || !reflect.DeepEqual(ts[i].Attrs, want[i]) || !reflect.DeepEqual(es[i].Attrs, events.Attrs(want[i])) || es[i].Cause != "evt_0303030303030303" || es[i].Depth != 5 {
			t.Fatalf("events %#v %#v", ts[i], es[i])
		}
	}
	trail.Record(context.Background(), w, em, trail.Started, p)
	_ = w.Flush(context.Background())
	_ = em.Flush(context.Background())
	last := ec.Events()[2]
	if last.Cause != "" || last.Depth != 0 || last.User != p.UserID {
		t.Fatal(last)
	}
	if strings.Contains(trail.Started, "\n") {
		t.Fatal("unexpected newline")
	}
}
