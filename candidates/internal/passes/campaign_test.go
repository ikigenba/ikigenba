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
	return events.Delivery{Event: events.Event{Name: name, Time: at, Attrs: events.Attrs{"campaign": "campaign_fixture", "candidate": candidate, "prospect_list": "pl_fixture", "reason": "private reason", "piece": 17, "channel": nil, "how": false, "at": at.Format(time.RFC3339Nano)}}}
}
func campaignUses(t *testing.T, f *fixture, id string) []store.CampaignUse {
	t.Helper()
	uses, err := f.st.CampaignUses(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return uses
}

// R-TLNW-TU9B R-0EOZ-5U1B R-TI07-OJ18 R-TJ84-2ARX R-TKG0-G2IM R-TO3P-LDQP R-TMVT-7M00
// R-59LS-87KU R-V4JT-IKGF R-TFKE-WZJU
func TestCampaignDeliveriesConvergeWithoutPasses(t *testing.T) {
	var orders [][]int
	for a := range 4 {
		for b := range 4 {
			for c := range 4 {
				for d := range 4 {
					if a != b && a != c && a != d && b != c && b != d && c != d {
						orders = append(orders, []int{a, b, c, d})
					}
				}
			}
		}
	}
	names := []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered, trail.AcceptedOpened}
	stamp := time.Date(2026, 2, 3, 4, 5, 6, 789, time.FixedZone("offset", 3600))
	for _, order := range orders {
		for _, duplicate := range []bool{false, true} {
			t.Run(fmt.Sprint(order, duplicate), func(t *testing.T) {
				f := newFixture(t, nil, duplicate)
				var pf *promptFake
				if duplicate {
					f.eng.Drain(t.Context())
					client, fake := fakePrompts(t, f, func(string, map[string]json.RawMessage) (any, bool, int) { return nil, false, 500 })
					pf = fake
					f.eng = passes.New(passes.Config{Store: f.st, Telemetry: f.w, Events: f.em, Scrapes: f.sc, Prompts: client})
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
					case 3:
						want.Opened = stamp.Add(3 * time.Hour).UTC().Truncate(time.Second)
					}

					deliver := f.eng.DeliverCampaign
					if duplicate {
						deliver = f.eng.Handlers()[names[i]]
					}
					if out := deliver(ctx, d); out != events.OK() {
						t.Fatal(out)
					}
					if got := campaignUses(t, f, id); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
						t.Fatalf("after %s: %#v want %#v", names[i], got, want)
					}
					if duplicate {
						if out := f.eng.DeliverCampaign(ctx, d); out != events.OK() {
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
				if u.Campaign != "campaign_fixture" || u.ProspectList != "pl_fixture" || u.Reason != "private reason" || !u.Added.Equal(stamp.UTC().Truncate(time.Second)) || !u.Rejected.Equal(stamp.Add(time.Hour).UTC().Truncate(time.Second)) || !u.Delivered.Equal(stamp.Add(2*time.Hour).UTC().Truncate(time.Second)) || !u.Opened.Equal(stamp.Add(3*time.Hour).UTC().Truncate(time.Second)) {
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

// R-TGSB-ARAJ R-52AD-XL4O R-58DV-UFU5 R-4ZUL-61NA R-59LS-87KU
func TestCampaignClassificationFailureAndDrain(t *testing.T) {
	f := newFixture(t, nil, false)
	id := campaignCandidate(t, f, "business")
	stamp := time.Unix(100, 0)
	valid := campaignDelivery(trail.AcceptedAdded, id, stamp)
	variants := []events.Delivery{campaignDelivery("scrape.finished", id, stamp), campaignDelivery(trail.AcceptedAdded, id, time.Time{}), campaignDelivery(trail.AcceptedAdded, "missing", stamp)}
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered, trail.AcceptedOpened} {
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
	zeroOpened := campaignDelivery(trail.AcceptedOpened, id, stamp)
	zeroOpened.Event.Attrs["at"] = "0001-01-01T00:00:00Z"
	variants = append(variants, zeroOpened)
	for _, d := range variants {
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.Skip() {
			t.Fatal(d, out)
		}
	}
	f.d.SetFailing(true)
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered, trail.AcceptedOpened} {
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

// R-OL77-OUCE R-TI07-OJ18 R-TJ84-2ARX R-TKG0-G2IM
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
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered, trail.AcceptedOpened} {
		d := campaignDelivery(name, id, stamp)
		d.Event.ID = "same-id"
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
			t.Fatal(out)
		}
	}
	if len(campaignUses(t, f, id)) != 0 || len(campaignUses(t, f, survivor)) != 1 {
		t.Fatal("merge destination")
	}
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered, trail.AcceptedOpened} {
		d := campaignDelivery(name, id, stamp.Add(time.Hour))
		d.Event.Attrs["reason"] = "later"
		d.Event.Attrs["prospect_list"] = "later"
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
			t.Fatal(out)
		}
	}
	u := campaignUses(t, f, survivor)[0]
	if !u.Added.Equal(stamp) || !u.Rejected.Equal(stamp) || u.Reason != "private reason" || u.ProspectList != "pl_fixture" || !u.Delivered.Equal(stamp.Add(time.Hour)) || !u.Opened.Equal(stamp) {
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

// R-0EOZ-5U1B R-V4JT-IKGF
func TestCampaignOptionalAttributesAndProspectListState(t *testing.T) {
	f := newFixture(t, nil, false)
	id := campaignCandidate(t, f, "business")
	co, err := f.st.CreateProspectList(t.Context(), store.ProspectList{Name: "campaign prospect list", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.st.JudgeAll(t.Context(), "", 10000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
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

// R-0EOZ-5U1B R-TLNW-TU9B
func TestCampaignOpenedTimeParsingAndFallback(t *testing.T) {
	busTime := time.Date(2026, 10, 2, 3, 4, 5, 987, time.FixedZone("bus-offset", 7200))
	visitTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		at      any
		present bool
		want    time.Time
	}{
		{"utc", "2026-10-01T12:00:00Z", true, visitTime},
		{"offset", "2026-10-01T07:00:00-05:00", true, visitTime},
		{"empty", "", true, busTime},
		{"invalid", "yesterday", true, busTime},
		{"number", 17, true, busTime},
		{"absent", nil, false, busTime},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil, false)
			id := campaignCandidate(t, f, "business")
			other := campaignCandidate(t, f, "untouched")
			d := campaignDelivery(trail.AcceptedOpened, id, busTime)
			if tt.present {
				d.Event.Attrs["at"] = tt.at
			} else {
				delete(d.Event.Attrs, "at")
			}
			if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
				t.Fatal(out)
			}
			want := store.CampaignUse{Candidate: id, Campaign: "campaign_fixture", Opened: tt.want.UTC().Truncate(time.Second)}
			if got := campaignUses(t, f, id); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
				t.Fatal(got, want)
			}
			if len(campaignUses(t, f, other)) != 0 {
				t.Fatal("changed another candidate")
			}
		})
	}
}

// R-TLNW-TU9B R-TKG0-G2IM
func TestCampaignOpenedKeepsEarliestAndPreservesOtherHistory(t *testing.T) {
	f := newFixture(t, nil, false)
	id := campaignCandidate(t, f, "business")
	stamp := time.Date(2026, 10, 1, 12, 0, 0, 123, time.FixedZone("offset", -18000))
	for _, name := range []string{trail.AcceptedAdded, trail.AcceptedRejected, trail.AcceptedDelivered} {
		if out := f.eng.DeliverCampaign(t.Context(), campaignDelivery(name, id, stamp)); out != events.OK() {
			t.Fatal(out)
		}
	}
	before := campaignUses(t, f, id)[0]
	for _, offset := range []time.Duration{0, 0, time.Hour, -time.Hour} {
		d := campaignDelivery(trail.AcceptedOpened, id, stamp.Add(48*time.Hour))
		d.Event.Attrs["at"] = stamp.Add(offset).Format(time.RFC3339Nano)
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
			t.Fatal(out)
		}
		want := before
		want.Opened = stamp.UTC().Truncate(time.Second)
		if offset < 0 {
			want.Opened = want.Opened.Add(offset)
		}
		if got := campaignUses(t, f, id); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Fatal(got, want)
		}
	}
	before = campaignUses(t, f, id)[0]
	for _, offset := range []time.Duration{time.Hour, -time.Hour} {
		d := campaignDelivery(trail.AcceptedDelivered, id, stamp.Add(offset))
		d.Event.Attrs["piece"], d.Event.Attrs["channel"], d.Event.Attrs["how"] = nil, 23, "anything"
		if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
			t.Fatal(out)
		}
		want := before
		want.Delivered = stamp.Add(time.Hour).UTC().Truncate(time.Second)
		if got := campaignUses(t, f, id); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Fatal(got, want)
		}
	}
}
