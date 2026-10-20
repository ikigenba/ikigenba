package passes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

func campaignCandidate(t *testing.T, f *fixture, key string) string {
	t.Helper()
	_, ids, err := f.st.Attach(t.Context(), "campaign_seed", false, []store.Observation{{Scraper: "fmcsa", SourceID: key, Digest: key, Reading: sources.Reading{Name: key, NameKey: key}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	return ids[0]
}
func campaignDelivery(name, candidate string, at time.Time) events.Delivery {
	return events.Delivery{Event: events.Event{Name: name, Time: at, Attrs: events.Attrs{"campaign": "campaign_fixture", "candidate": candidate, "prospect_list": "pl_fixture", "reason": "private reason", "piece": 17, "channel": nil, "how": false}}}
}
func campaignUses(t *testing.T, f *fixture, id string) []store.CampaignUse {
	t.Helper()
	uses, err := f.st.CampaignUses(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return uses
}

// R-V6ZM-A3XT R-UJTJ-0GUM R-UTKQ-2MS6 R-ZSTY-AWS9 R-5C1K-ZR28 R-RAR6-AHNY
// R-59LS-87KU R-V4JT-IKGF R-4XES-EI5W
func TestCampaignDeliveriesConvergeWithoutPasses(t *testing.T) {
	orders := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	names := []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered}
	stamp := time.Date(2026, 2, 3, 4, 5, 6, 789, time.FixedZone("offset", 3600))
	for _, order := range orders {
		for _, duplicate := range []bool{false, true} {
			t.Run(fmt.Sprint(order, duplicate), func(t *testing.T) {
				f := newFixture(t, nil, false)
				var pf *promptFake
				if duplicate {
					f.eng.Drain(t.Context())
					client, fake := fakePrompts(t, f, func(string, map[string]json.RawMessage) (any, bool, int) { return nil, false, 500 })
					pf = fake
					f.eng = passes.New(passes.Config{Store: f.st, Telemetry: f.w, Events: f.em, Prompts: client})
				}
				id := campaignCandidate(t, f, "business")
				before, err := f.st.GetCandidate(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				rejects, err := f.st.Rejects(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				var direct events.Handler = f.eng.DeliverCampaign
				want := store.CampaignUse{Candidate: id, Campaign: "campaign_fixture"}
				for _, i := range order {
					ctx := context.Background()
					if duplicate {
						ctx = events.NewContext(identity.NewContext(ctx, identity.Caller{UserID: "owner", RequestID: "request"}), events.Cause{ID: "evt_0101010101010101", Depth: 1})
					}
					d := campaignDelivery(names[i], id, stamp.Add(time.Duration(i)*time.Hour))
					switch i {
					case 0:
						want.Added = stamp.UTC().Truncate(time.Second)
						want.ProspectList = "pl_fixture"
					case 1:
						want.Rejected = stamp.Add(time.Hour).UTC().Truncate(time.Second)
						want.Reason = "private reason"
					case 2:
						want.Delivered = stamp.Add(2 * time.Hour).UTC().Truncate(time.Second)
					}

					if out := direct(ctx, d); out != events.OK() {
						t.Fatal(out)
					}
					if got := campaignUses(t, f, id); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
						t.Fatalf("after %s: %#v want %#v", names[i], got, want)
					}
					if duplicate {
						if out := f.eng.Handlers()[names[i]](ctx, d); out != events.OK() {
							t.Fatal(out)
						}
						if got := campaignUses(t, f, id); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
							t.Fatalf("after duplicate %s: %#v want %#v", names[i], got, want)
						}
					}
				}
				uses := campaignUses(t, f, id)
				if len(uses) != 1 {
					t.Fatal(uses)
				}
				u := uses[0]
				if u.Campaign != "campaign_fixture" || u.ProspectList != "pl_fixture" || u.Reason != "private reason" || !u.Added.Equal(stamp.UTC().Truncate(time.Second)) || !u.Rejected.Equal(stamp.Add(time.Hour).UTC().Truncate(time.Second)) || !u.Delivered.Equal(stamp.Add(2*time.Hour).UTC().Truncate(time.Second)) {
					t.Fatal(u)
				}
				after, err := f.st.GetCandidate(t.Context(), id)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal(after, err)
				}
				afterRejects, err := f.st.Rejects(t.Context(), id)
				if err != nil || !reflect.DeepEqual(rejects, afterRejects) {
					t.Fatal(afterRejects, err)
				}
				if len(f.list()) != 0 || len(f.requests()) != 0 {
					t.Fatal("unexpected work")
				}
				f.flush()
				if len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
					t.Fatal("unexpected events")
				}
				if pf != nil && len(pf.snapshot()) != 0 {
					t.Fatal("unexpected prompts call")
				}
			})
		}
	}
}

// R-1TZX-959X R-52AD-XL4O R-58DV-UFU5 R-4ZUL-61NA R-59LS-87KU
func TestCampaignClassificationFailureAndDrain(t *testing.T) {
	f := newFixture(t, nil, false)
	id := campaignCandidate(t, f, "business")
	stamp := time.Unix(100, 0)
	valid := campaignDelivery(trail.AcceptedAdded, id, stamp)
	variants := []events.Delivery{campaignDelivery("scrape.finished", id, stamp), campaignDelivery(trail.AcceptedAdded, id, time.Time{}), campaignDelivery(trail.AcceptedAdded, "missing", stamp)}
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered} {
		variants = append(variants, campaignDelivery(name, "missing", stamp))
		missing := campaignDelivery(name, id, stamp)
		missing.Event.Attrs = nil
		variants = append(variants, missing)
		for _, key := range []string{"candidate", "campaign"} {
			for _, v := range []any{nil, "", 42} {
				d := campaignDelivery(name, id, stamp)
				d.Event.Attrs[key] = v
				variants = append(variants, d)
			}
		}
	}
	for _, d := range variants {
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.Skip() {
			t.Fatal(d, out)
		}
	}
	f.d.SetFailing(true)
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered} {
		if out := f.eng.DeliverCampaign(t.Context(), campaignDelivery(name, id, stamp)); out != events.Fail(store.Unreachable) {
			t.Fatal(out)
		}
	}
	f.d.SetFailing(false)
	if len(campaignUses(t, f, id)) != 0 {
		t.Fatal("failure wrote uses")
	}
	if out := f.eng.DeliverCampaign(t.Context(), valid); out != events.OK() {
		t.Fatal(out)
	}
	before := campaignUses(t, f, id)
	f.eng.Drain(t.Context())
	for _, d := range append(variants, valid) {
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.Fail(passes.Stopping) {
			t.Fatal(out)
		}
	}
	if !reflect.DeepEqual(before, campaignUses(t, f, id)) || len(f.list()) != 0 || len(f.requests()) != 0 {
		t.Fatal("side effects")
	}
	f.flush()
	if len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
		t.Fatal("unexpected events")
	}
}

// R-OL77-OUCE R-UJTJ-0GUM R-UTKQ-2MS6 R-ZSTY-AWS9
func TestCampaignFollowsMergeAndIgnoresEventID(t *testing.T) {
	f := newFixture(t, nil, false)
	id := campaignCandidate(t, f, "first")
	survivor := campaignCandidate(t, f, "second")
	if _, _, err := f.st.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: id, Why: "same business", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}

	endpoint := campaignCandidate(t, f, "third")
	if _, _, err := f.st.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: endpoint, Other: survivor, Why: "same endpoint", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	survivor = endpoint
	stamp := time.Unix(100, 0)
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered} {
		d := campaignDelivery(name, id, stamp)
		d.Event.ID = "same-id"
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
			t.Fatal(out)
		}
	}
	if len(campaignUses(t, f, id)) != 0 || len(campaignUses(t, f, survivor)) != 1 {
		t.Fatal("merge destination")
	}
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered} {
		d := campaignDelivery(name, id, stamp.Add(time.Hour))
		d.Event.Attrs["reason"] = "later"
		d.Event.Attrs["prospect_list"] = "later"
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
			t.Fatal(out)
		}
	}
	u := campaignUses(t, f, survivor)[0]
	if !u.Added.Equal(stamp) || !u.Rejected.Equal(stamp) || u.Reason != "private reason" || u.ProspectList != "pl_fixture" || !u.Delivered.Equal(stamp.Add(time.Hour)) {
		t.Fatal(u)
	}
	d := campaignDelivery(trail.AcceptedDelivered, id, stamp.Add(-time.Hour))
	if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
		t.Fatal(out)
	}
	if !reflect.DeepEqual(u, campaignUses(t, f, survivor)[0]) {
		t.Fatal("older delivery changed use")
	}
	d = campaignDelivery(trail.AcceptedAdded, id, stamp)
	d.Event.ID = "same-id"
	d.Event.Attrs["campaign"] = "another_campaign"
	if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() || len(campaignUses(t, f, survivor)) != 2 {
		t.Fatal(out)
	}
}

// R-29ZB-D6WK
func TestConcurrentCampaignDeliveries(t *testing.T) {
	entered := make(chan struct{}, 8)
	f := newFixture(t, func(_ http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }, true)
	id := campaignCandidate(t, f, "business")
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			d := campaignDelivery(trail.AcceptedAdded, id, time.Unix(int64(100+i), 123))
			if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
				t.Error(out)
			}
		})
	}
	wg.Wait()
	u := campaignUses(t, f, id)
	if len(u) != 1 || !u[0].Added.Equal(time.Unix(100, 0)) {
		t.Fatal(u)
	}
	if _, err := f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, ""); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	for i := range 32 {
		wg.Go(func() {
			_ = f.eng.DeliverCampaign(t.Context(), campaignDelivery(trail.AcceptedDelivered, id, time.Unix(int64(200+i), 0)))
		})
	}
	wg.Go(func() { _ = f.eng.Deliver(t.Context(), events.Delivery{}) })
	wg.Go(func() { _, _ = f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "") })
	wg.Go(func() { _, _ = f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"}) })
	wg.Go(func() { f.eng.Drain(t.Context()) })
	wg.Wait()
}

// R-V6ZM-A3XT R-V4JT-IKGF
func TestCampaignOptionalAttributesAndProspectListState(t *testing.T) {
	f := newFixture(t, nil, false)
	id := campaignCandidate(t, f, "business")
	co, err := f.st.CreateProspectList(t.Context(), store.ProspectList{Name: "campaign prospect list", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.JudgeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := f.st.ReadProspectList(t.Context(), co.ID)
	if err != nil {
		t.Fatal(err)
	}
	candidateBefore, err := f.st.GetCandidate(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected} {
		d := campaignDelivery(name, id, time.Unix(100, 0))
		d.Event.Attrs["prospect_list"] = 7
		delete(d.Event.Attrs, "reason")
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
			t.Fatal(out)
		}
	}
	u := campaignUses(t, f, id)
	if len(u) != 1 || u[0].ProspectList != "" || u[0].Reason != "" {
		t.Fatal(u)
	}
	after, err := f.st.ReadProspectList(t.Context(), co.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(after, err)
	}
	candidateAfter, err := f.st.GetCandidate(t.Context(), id)
	if err != nil || !reflect.DeepEqual(candidateBefore, candidateAfter) {
		t.Fatal(candidateAfter, err)
	}
}
