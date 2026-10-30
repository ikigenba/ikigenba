package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

// R-QBW0-YDSC R-Q9G8-6UAY R-QAO4-KM1N
func TestMembersResultShapes(t *testing.T) {
	type resultWord string
	var words = []resultWord{ResultAdded, ResultMember, ResultUnknown}
	equal(t, words, []resultWord{"added", "member", "unknown"})
	for _, skipped := range [][]Skip{nil, {}} {
		out := outputValue(t, MemberResult{"chosen", ResultAdded, skipped}, []string{"candidate", "result", "skipped"})
		equal(t, string(out["skipped"]), "[]")
	}
	for _, results := range [][]MemberResult{nil, {}} {
		out := outputValue(t, MembersAdd{results}, []string{"results"})
		equal(t, string(out["results"]), "[]")
	}
}

// R-Q88B-T2K9 R-QD3X-C5J1 R-QEBT-PX9Q R-QFJQ-3P0F
func TestMembersRegisteredTypes(t *testing.T) {
	f := setup(t)
	f.start()
	k := f.create("mail")
	// Compare to a registration deriving both schemas from the declared types.
	sample := mcp.NewServer(mcp.ServerConfig{Name: "sample", Telemetry: f.cfg.Telemetry})
	mcp.AddTool(sample, mcp.Tool[MembersArgs, MembersAdd]{Name: "sample", Description: DescribeAddMembers, Effect: mcp.Additive, Handler: func(context.Context, identity.Caller, MembersArgs) (MembersAdd, error) { return MembersAdd{}, nil }})
	hs := httptest.NewServer(events.Middleware(identity.Require(sample)))
	defer hs.Close()
	reference := *f
	reference.client = mcp.NewClient(mcp.ClientConfig{Endpoint: hs.URL})
	derived, err := reference.client.ListTools(context.Background(), testCaller)
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.client.ListTools(context.Background(), testCaller)
	if err != nil {
		t.Fatal(err)
	}
	info := list[7]
	equal(t, info.Name, "add_members")
	equal(t, info.InputSchema, derived[0].InputSchema)
	equal(t, info.OutputSchema, derived[0].OutputSchema)
	var schema struct {
		Properties map[string]struct {
			Type  string
			Items struct{ Type string }
		}
		Required []string
	}
	if err := json.Unmarshal(info.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	equal(t, schema.Properties["candidates"].Type, "array")
	equal(t, schema.Properties["candidates"].Items.Type, "string")
	for _, args := range []any{
		map[string]any{"campaign": k.ID, "candidates": []string{}, "unknown": true},
		map[string]any{"campaign": 1, "candidates": []string{}},
		map[string]any{"campaign": k.ID, "candidates": "wrong"},
		map[string]any{"campaign": k.ID, "candidates": []any{id(1), 1}},
		map[string]any{"campaign": k.ID, "candidates": nil},
		map[string]any{"campaign": k.ID},
	} {
		_, refusal := f.call("add_members", args)
		_, expected := reference.call("sample", args)
		if refusal == "" {
			t.Fatalf("accepted invalid args %#v", args)
		}
		equal(t, refusal, expected)
	}
	equal(t, decode[MembersAdd](t, f.ok("add_members", MembersArgs{k.ID, []string{}})), MembersAdd{[]MemberResult{}})
}

// R-QHZI-V8HT R-QMV4-EBGL R-QGRM-HGR4
func TestMembersCampaignPrecedenceAndNoNeeded(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{id(1): business(1)}}
	f.sibling(s, "candidates")
	art := &sibling{artifact: "fixture-file"}
	f.sibling(art, "artifacts")
	f.start()
	k := f.create("mail")
	equal(t, decode[MembersAdd](t, f.ok("add_members", MembersArgs{k.ID, []string{}})), MembersAdd{[]MemberResult{}})
	deleted := f.create("mail")
	f.ok("delete_campaign", DeleteArgs{deleted.ID})
	for _, campaign := range []string{"bad", "camp_ffffffffffffffff", deleted.ID} {
		for _, ids := range [][]string{{}, {id(1)}} {
			f.refuse("add_members", MembersArgs{campaign, ids}, RefusalNotFound, RefuseNotFound)
		}
	}
	equal(t, len(s.calls()), 0)
	f.flush()
	equal(t, len(f.bus.Events()), 0)
	equal(t, len(art.calls()), 0)
	nilFixture := setup(t)
	nilFixture.start()
	nilCampaign := nilFixture.create("mail")
	nilFixture.refuse("add_members", MembersArgs{nilCampaign.ID, []string{id(1)}}, RefusalUnavailable, RefuseCandidates)
	equal(t, decode[MembersAdd](t, nilFixture.ok("add_members", MembersArgs{nilCampaign.ID, []string{"bad", "bad", "invalid"}})), MembersAdd{[]MemberResult{{"bad", ResultUnknown, []Skip{}}, {"invalid", ResultUnknown, []Skip{}}}})
	// A member brought in from a prospect list keeps both that origin and its pieces.
	row, added, err := nilFixture.cfg.Store.AddMember(context.Background(), store.MemberInput{Campaign: nilCampaign.ID, Candidate: id(1), ProspectList: "pl_1111111111111111", Channels: []string{"mail"}, UserID: testCaller.UserID, UserEmail: testCaller.Email})
	if err != nil || !added {
		t.Fatalf("member fixture %v %v", added, err)
	}
	equal(t, decode[MembersAdd](t, nilFixture.ok("add_members", MembersArgs{nilCampaign.ID, []string{id(1), id(1), "bad"}})), MembersAdd{[]MemberResult{{id(1), ResultMember, []Skip{}}, {"bad", ResultUnknown, []Skip{}}}})
	equal(t, memberRow(t, nilFixture, nilCampaign.ID, id(1)), row)
	nilFixture.flush()
	equal(t, len(nilFixture.bus.Events()), 0)
	equal(t, len(changeTrail(nilFixture.log.Events())), 0)
}

// R-QKFB-MRZ7 R-QO30-S37A R-QEBT-PX9Q R-QFJQ-3P0F R-QQIT-JMOO R-QSYM-B662
func TestMembersDistinctOrderEligibilityAndTrail(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{}}
	for n := 1; n <= 5; n++ {
		s.businesses[id(n)] = business(n)
	}
	s.businesses[id(1)]["email"] = ""
	for _, field := range []string{"street", "city", "state", "zip", "email"} {
		s.businesses[id(2)][field] = ""
	}
	s.businesses[id(3)]["status"] = "merged"
	s.businesses[id(3)]["merged_into"] = id(99)
	f.sibling(s, "candidates")
	art := &sibling{artifact: "fixture-file"}
	f.sibling(art, "artifacts")
	f.start()
	k := f.create("mail", "email")
	present := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(5)}))
	before := memberRow(t, f, k.ID, id(5))
	calls := len(s.calls())
	out := decode[MembersAdd](t, f.ok("add_members", MembersArgs{k.ID, []string{id(1), "bad", id(2), id(1), id(9), id(5), id(3), id(2)}}))
	want := MembersAdd{[]MemberResult{{id(1), ResultAdded, []Skip{{"email", SkipNoEmail}}}, {"bad", ResultUnknown, []Skip{}}, {id(2), ResultAdded, []Skip{{"mail", SkipNoAddress}, {"email", SkipNoEmail}}}, {id(9), ResultUnknown, []Skip{}}, {id(5), ResultMember, []Skip{}}, {id(3), ResultAdded, []Skip{}}}}
	equal(t, out, want)
	equal(t, memberRow(t, f, k.ID, id(5)), before)
	equal(t, present.Member.ProspectList, "")
	for _, n := range []int{1, 2, 3} {
		row := memberRow(t, f, k.ID, id(n))
		equal(t, row.Member.ProspectList, "")
		equal(t, row.Member.AddedUserID, testCaller.UserID)
		equal(t, row.Member.AddedUserEmail, testCaller.Email)
	}
	equal(t, len(memberRow(t, f, k.ID, id(1)).Pieces), 1)
	equal(t, len(memberRow(t, f, k.ID, id(2)).Pieces), 0)
	if _, err := f.cfg.Store.GetMember(context.Background(), k.ID, id(9)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown member: %v", err)
	}
	req := s.calls()[calls:]
	equal(t, len(req), 4)
	for i, n := range []int{1, 2, 9, 3} {
		equal(t, req[i].Tool, "candidate")
		equal(t, req[i].Args, map[string]string{"candidate": id(n)})
		equal(t, req[i].User, testCaller.UserID)
		equal(t, req[i].Email, testCaller.Email)
	}
	expected := setup(t)
	ctx := identity.NewContext(context.Background(), testCaller)
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 1})
	for _, n := range []int{5, 1, 2, 3} {
		trail.MemberAdded(ctx, expected.cfg.Telemetry, expected.cfg.Events, k.ID, id(n), "")
	}
	f.flush()
	expected.flush()
	equal(t, f.bus.Events(), expected.bus.Events())
	equal(t, changeTrail(f.log.Events()), changeTrail(expected.log.Events()))
	calls = len(s.calls())
	repeated := decode[MembersAdd](t, f.ok("add_members", MembersArgs{k.ID, []string{id(1), id(2), id(3)}}))
	equal(t, repeated, MembersAdd{[]MemberResult{{id(1), ResultMember, []Skip{}}, {id(2), ResultMember, []Skip{}}, {id(3), ResultMember, []Skip{}}}})
	equal(t, len(s.calls()), calls)
	equal(t, len(art.calls()), 0)
	f.flush()
	equal(t, f.bus.Events(), expected.bus.Events())
	equal(t, changeTrail(f.log.Events()), changeTrail(expected.log.Events()))
}

// R-QLN8-0JPW R-QGRM-HGR4
func TestMembersPreflightRefusalsAddNothing(t *testing.T) {
	for _, failure := range []string{"unavailable: failed", "stopping: failed", "arbitrary: failed", "http"} {
		t.Run(failure, func(t *testing.T) {
			f := setup(t)
			s := &sibling{businesses: map[string]map[string]any{}, candidateFailures: map[string]string{}, candidateStatuses: map[string]int{}}
			ids := []string{}
			for n := 1; n <= 5; n++ {
				ids = append(ids, id(n))
				s.businesses[id(n)] = business(n)
			}
			if failure == "http" {
				s.candidateStatuses[id(4)] = 503
			} else {
				s.candidateFailures[id(4)] = failure
			}
			f.sibling(s, "candidates")
			f.start()
			k := f.create("mail")
			f.refuse("add_members", MembersArgs{k.ID, ids}, RefusalUnavailable, RefuseCandidates)
			read := decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID}))
			equal(t, len(read.Members), 0)
			f.flush()
			equal(t, len(f.bus.Events()), 0)
			equal(t, len(changeTrail(f.log.Events())), 0)
			s.mu.Lock()
			clear(s.candidateFailures)
			clear(s.candidateStatuses)
			s.mu.Unlock()
			count := len(s.calls())
			out := decode[MembersAdd](t, f.ok("add_members", MembersArgs{k.ID, ids}))
			equal(t, len(s.calls())-count, 5)
			for i, r := range out.Results {
				equal(t, r, MemberResult{ids[i], ResultAdded, []Skip{}})
			}
		})
	}
}

// R-QPAX-5UXZ R-QQIT-JMOO
func TestMembersStorePartialFailureAndRetry(t *testing.T) {
	f := setup(t)
	limited := &boundedReader{remaining: 56}
	f.cfg.Store = store.New(f.d, store.Config{Now: fixtureTime, Rand: limited})
	s := &sibling{businesses: map[string]map[string]any{}}
	ids := []string{}
	for n := 1; n <= 5; n++ {
		ids = append(ids, id(n))
		s.businesses[id(n)] = business(n)
	}
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail")
	f.refuse("add_members", MembersArgs{k.ID, ids}, RefusalUnavailable, store.Unreachable)
	read := decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID}))
	equal(t, len(read.Members), 2)
	for i, m := range read.Members {
		equal(t, m.Candidate, ids[i])
		equal(t, len(m.Pieces), 1)
	}
	f.flush()
	equal(t, len(f.bus.Events()), 2)
	equal(t, len(changeTrail(f.log.Events())), 2)
	limited.remaining = 10000
	out := decode[MembersAdd](t, f.ok("add_members", MembersArgs{k.ID, ids}))
	for i, r := range out.Results {
		result := ResultAdded
		if i < 2 {
			result = ResultMember
		}
		equal(t, r, MemberResult{ids[i], result, []Skip{}})
	}
	equal(t, len(s.calls()), 8)
	f.flush()
	equal(t, len(f.bus.Events()), 5)
	equal(t, len(changeTrail(f.log.Events())), 5)
}

// R-QO30-S37A R-QQIT-JMOO
func TestMembersConcurrentAddOnce(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{}}
	ids := []string{}
	for n := 1; n <= 3; n++ {
		ids = append(ids, id(n))
		s.businesses[id(n)] = business(n)
	}
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail")
	var wg sync.WaitGroup
	outs := make([]MembersAdd, 2)
	start := make(chan struct{})
	for i := range outs {
		wg.Go(func() { <-start; outs[i] = decode[MembersAdd](t, f.ok("add_members", MembersArgs{k.ID, ids})) })
	}
	close(start)
	wg.Wait()
	for i, id := range ids {
		equal(t, outs[0].Results[i].Candidate, id)
		equal(t, outs[1].Results[i].Candidate, id)
		results := []string{outs[0].Results[i].Result, outs[1].Results[i].Result}
		if !reflect.DeepEqual(results, []string{ResultAdded, ResultMember}) && !reflect.DeepEqual(results, []string{ResultMember, ResultAdded}) {
			t.Fatal(results)
		}
	}
	f.flush()
	equal(t, len(f.bus.Events()), 3)
	equal(t, len(changeTrail(f.log.Events())), 3)
	seen := map[string]bool{}
	for _, event := range f.bus.Events() {
		candidate := event.Attrs["candidate"].(string)
		if seen[candidate] {
			t.Fatal("duplicate event")
		}
		seen[candidate] = true
		equal(t, event.Name, trail.EventMemberAdded)
	}
}
