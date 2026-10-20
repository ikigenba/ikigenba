package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

func equal[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
func timeText(v time.Time) string {
	if v.IsZero() {
		return ""
	}
	return v.UTC().Format(time.RFC3339)
}
func expectedCampaign(c store.Campaign) Campaign {
	return Campaign{c.ID, c.Name, c.Description, c.Channels, c.Destination, timeText(c.Created), c.CreatedUserID, c.CreatedUserEmail, c.Deleted}
}
func expectedRecord(p store.Piece) PieceRecord {
	return PieceRecord{p.ID, p.Campaign, p.Candidate, p.Channel, p.State, timeText(p.Created), timeText(p.Delivered), p.DeliveredUserID, p.DeliveredUserEmail, p.How, timeText(p.Dropped), p.DroppedUserID, p.DroppedUserEmail}
}
func expectedMember(r store.MemberRow) Member {
	ps := []Piece{}
	for _, r := range r.Pieces {
		p := r.Piece
		e := r.Engagement
		ps = append(ps, Piece{p.ID, p.Campaign, p.Candidate, p.Channel, p.State, timeText(p.Created), timeText(p.Delivered), p.DeliveredUserID, p.DeliveredUserEmail, p.How, timeText(p.Dropped), p.DroppedUserID, p.DroppedUserEmail, e.Opens, timeText(e.First), timeText(e.Last)})
	}
	m := r.Member
	e := r.Engagement
	return Member{m.Campaign, m.Candidate, m.Cohort, timeText(m.Added), m.AddedUserID, m.AddedUserEmail, m.Rejected, m.RejectedUserID, m.RejectedUserEmail, timeText(m.RejectedAt), m.Why, e.Opened, e.Opens, timeText(e.First), timeText(e.Last), ps}
}
func memberRow(t *testing.T, f *fixture, c, m string) store.MemberRow {
	t.Helper()
	r, e := f.cfg.Store.GetMember(context.Background(), c, m)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func pieceRow(t *testing.T, f *fixture, p string) store.PieceRow {
	t.Helper()
	r, e := f.cfg.Store.GetPiece(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func expectBusiness(n int) Business {
	return Business{fmt.Sprintf("business %d", n), []string{"alias"}, "street", "city", "state", "zip", "county", "phone", "mgreenly+fake01@gmail.com", []Person{{"person", "owner"}}}
}

// R-T621-QVNT R-T79Y-4NEI R-GKP2-TSCU R-T8HU-IF57 R-TDDG-1I3Z
// R-U2ZC-2OOK R-U478-GGF9 R-TZBM-XDGH R-GQSK-QN2B R-GS0H-4ET0 R-GT8D-I6JP
func TestCompleteAnswersVisitsAndTrail(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{id(1): business(1), id(2): business(2)}}
	art := &sibling{artifact: "file-id"}
	f.sibling(s, "candidates")
	f.sibling(art, "artifacts")
	f.start()
	k := f.create("mail", "email")
	stored, storedErr := f.cfg.Store.GetCampaign(context.Background(), k.ID)
	if storedErr != nil {
		t.Fatal(storedErr)
	}
	equal(t, k, expectedCampaign(stored))
	a := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(1)}))
	equal(t, a.Member, expectedMember(memberRow(t, f, k.ID, id(1))))
	mail, email := a.Member.Pieces[0].ID, a.Member.Pieces[1].ID
	*f.at = fixtureTime().Add(time.Second)
	delivery := f.ok("deliver", PieceArgs{mail})
	equal(t, decode[PieceChange](t, delivery), PieceChange{expectedRecord(pieceRow(t, f, mail).Piece), true})
	var recordFields map[string]json.RawMessage
	if e := json.Unmarshal(delivery["piece"], &recordFields); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"opens", "first", "last"} {
		if _, ok := recordFields[key]; ok {
			t.Fatal("change engagement", key)
		}
	}
	*f.at = fixtureTime().Add(2 * time.Second)
	equal(t, decode[PieceChange](t, f.ok("drop", PieceArgs{email})), PieceChange{expectedRecord(pieceRow(t, f, email).Piece), true})
	*f.at = fixtureTime().Add(3 * time.Second)
	f.ok("reject", RejectArgs{k.ID, id(1), "fixture why"})
	*f.at = fixtureTime().Add(4 * time.Second)
	f.ok("add_member", MemberArgs{k.ID, id(2)})
	visits := []struct {
		piece   string
		seconds int
		input   store.VisitInput
	}{{mail, 8, store.VisitInput{UserAgent: "ua-one", Referrer: "ref-one", Address: "address-one"}}, {mail, 5, store.VisitInput{UserAgent: "ua-two", Referrer: "ref-two", Address: "address-two"}}, {email, 12, store.VisitInput{UserAgent: "ua-three", Referrer: "ref-three", Address: "address-three"}}}
	for _, v := range visits {
		*f.at = fixtureTime().Add(time.Duration(v.seconds) * time.Second)
		p := pieceRow(t, f, v.piece).Piece
		if _, e := f.cfg.Store.Visit(context.Background(), p.Token, v.input); e != nil {
			t.Fatal(e)
		}
	}
	first, last := timeText(fixtureTime().Add(5*time.Second)), timeText(fixtureTime().Add(8*time.Second))
	limit := 1
	e := decode[Engagement](t, f.ok("engagement", EngagementArgs{Piece: mail, Limit: &limit}))
	wantVisits := []Visit{{first, "ua-two", "ref-two", "address-two"}}
	if e.Next == nil {
		t.Fatal("no continuation")
	}
	nextPage := *e.Next
	var seq int64
	if err := (paging.Token{Tool: "engagement"}).Decode(nextPage, &seq); err != nil || seq != 2 {
		t.Fatalf("visit token %d %v", seq, err)
	}
	e.Next = nil
	equal(t, e, Engagement{k.ID, id(1), mail, true, 2, first, last, &wantVisits, nil})
	e = decode[Engagement](t, f.ok("engagement", EngagementArgs{Piece: mail, Page: &nextPage, Limit: &limit}))
	wantVisits = []Visit{{last, "ua-one", "ref-one", "address-one"}}
	equal(t, e, Engagement{k.ID, id(1), mail, true, 2, first, last, &wantVisits, nil})
	me := decode[Engagement](t, f.ok("engagement", EngagementArgs{Campaign: k.ID, Candidate: id(1)}))
	equal(t, me, Engagement{Campaign: k.ID, Candidate: id(1), Opened: true, Opens: 3, First: first, Last: timeText(fixtureTime().Add(12 * time.Second))})
	untouched := memberRow(t, f, k.ID, id(2)).Pieces[0].Piece.ID
	empty := []Visit{}
	equal(t, decode[Engagement](t, f.ok("engagement", EngagementArgs{Piece: untouched})), Engagement{Campaign: k.ID, Candidate: id(2), Piece: untouched, Visits: &empty})
	r1, r2 := memberRow(t, f, k.ID, id(1)), memberRow(t, f, k.ID, id(2))
	read := decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID, Limit: &limit}))
	wantNext := (paging.Token{Tool: "campaign"}).Encode(timeText(r1.Member.Added), id(1))
	equal(t, read, CampaignRead{k, Counts{2, 1, 1}, []Material{}, []Member{expectedMember(r1)}, &wantNext})
	equal(t, decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID, Limit: &limit, Page: read.Next})), CampaignRead{k, Counts{2, 1, 1}, []Material{}, []Member{expectedMember(r2)}, nil})
	before := len(s.calls())
	rows := decode[MergeList](t, f.ok("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", All: true, Limit: &limit}))
	p := r1.Pieces[0].Piece
	wantNext = (paging.Token{Tool: "merge_list"}).Encode(timeText(p.Created), id(1))
	equal(t, rows, MergeList{[]MergeRow{{p.ID, id(1), "mail", "delivered", true, f.cfg.LinkPrefix + p.Token, expectBusiness(1)}}, &wantNext})
	calls := s.calls()[before:]
	equal(t, len(calls), 1)
	equal(t, calls[0].Args, map[string]string{"candidate": id(1)})
	before = len(s.calls())
	rows = decode[MergeList](t, f.ok("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", All: true, Limit: &limit, Page: rows.Next}))
	p = r2.Pieces[0].Piece
	equal(t, rows, MergeList{Rows: []MergeRow{{p.ID, id(2), "mail", "planned", false, f.cfg.LinkPrefix + p.Token, expectBusiness(2)}}})
	calls = s.calls()[before:]
	equal(t, len(calls), 1)
	equal(t, calls[0].Args, map[string]string{"candidate": id(2)})
	s.mu.Lock()
	delete(s.businesses, id(2))
	s.mu.Unlock()
	missingRows := decode[MergeList](t, f.ok("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", All: true, Limit: &limit, Page: &wantNext}))
	equal(t, missingRows, MergeList{Rows: []MergeRow{{p.ID, id(2), "mail", "planned", false, f.cfg.LinkPrefix + p.Token, Business{Names: []string{}, People: []Person{}}}}})

	f.flush()
	expected := setup(t)
	ctx := identity.NewContext(context.Background(), testCaller)
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 1})
	trail.MemberAdded(ctx, expected.cfg.Telemetry, expected.cfg.Events, k.ID, id(1), "")
	trail.PieceDelivered(ctx, expected.cfg.Telemetry, expected.cfg.Events, k.ID, id(1), mail, "mail", "hand")
	trail.MemberRejected(ctx, expected.cfg.Telemetry, expected.cfg.Events, k.ID, id(1), "fixture why")
	trail.MemberAdded(ctx, expected.cfg.Telemetry, expected.cfg.Events, k.ID, id(2), "")
	expected.flush()
	equal(t, f.bus.Events(), expected.bus.Events())
	equal(t, changeTrail(f.log.Events()), changeTrail(expected.log.Events()))
}
func changeTrail(in []telemetry.Event) []telemetry.Event {
	out := []telemetry.Event{}
	for _, e := range in {
		if e.Name == trail.EventMemberAdded || e.Name == trail.EventMemberRejected || e.Name == trail.EventPieceDelivered {
			out = append(out, e)
		}
	}
	return out
}

// R-T9PQ-W6VW R-TAXN-9YML
func TestAllPagingForms(t *testing.T) {
	f := setup(t)
	f.start()
	k := f.create("mail")
	f.d.SetFailing(true)
	for _, name := range []string{"campaigns", "campaign", "merge_list", "engagement"} {
		args := map[string]any{"limit": paging.MaxLimit + 1, "page": "bad"}
		if name == "campaign" || name == "merge_list" {
			args["campaign"] = k.ID
		}
		if name == "merge_list" {
			args["channel"] = "mail"
		}
		_, e := paging.Window(new(paging.MaxLimit + 1))
		f.refuse(name, args, "", e.Error())
		args["limit"] = 1
		f.refuse(name, args, "", paging.BadPage)
	}
	other := (paging.Token{Tool: "campaign"}).Encode("time", "id")
	f.refuse("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", Page: &other}, "", paging.BadPage)
	wrong := (paging.Token{Tool: "engagement"}).Encode("time", "id")
	f.refuse("engagement", EngagementArgs{Page: &wrong}, "", paging.BadPage)
	f.d.SetFailing(false)
	for _, args := range []map[string]any{{"page": nil, "limit": nil}, {"page": ""}, {}} {
		equal(t, len(decode[CampaignList](t, f.ok("campaigns", args)).Campaigns), 1)
	}
	f.ok("campaign", map[string]any{"campaign": k.ID, "page": nil, "limit": nil})
	for _, args := range []map[string]any{{"campaign": k.ID, "candidate": id(1), "page": nil, "limit": nil}, {"campaign": k.ID, "candidate": id(1), "page": ""}} {
		f.refuse("engagement", args, RefusalNotFound, RefuseNotFound)
	}
}

// R-TC5J-NQDA
func TestThirtyCampaignsOrderedWithCounts(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{id(1): business(1)}}
	f.sibling(s, "candidates")
	f.start()
	campaigns := []Campaign{}
	for n := range 30 {
		*f.at = fixtureTime().Add(time.Duration(n%3) * time.Second)
		campaigns = append(campaigns, f.create("mail"))
	}
	a := decode[AddedMember](t, f.ok("add_member", MemberArgs{campaigns[0].ID, id(1)}))
	f.ok("deliver", PieceArgs{a.Member.Pieces[0].ID})
	p := pieceRow(t, f, a.Member.Pieces[0].ID).Piece
	if _, e := f.cfg.Store.Visit(context.Background(), p.Token, store.VisitInput{}); e != nil {
		t.Fatal(e)
	}
	dead := f.create("mail")
	f.ok("delete_campaign", DeleteArgs{dead.ID})
	sort.Slice(campaigns, func(i, j int) bool {
		a, b := campaigns[i], campaigns[j]
		if a.Created != b.Created {
			return a.Created > b.Created
		}
		return a.ID > b.ID
	})
	want := []CampaignSummary{}
	for _, c := range campaigns {
		n := Counts{}
		if c.ID == a.Member.Campaign {
			n = Counts{1, 1, 1}
		}
		want = append(want, CampaignSummary{c.ID, c.Name, c.Channels, c.Created, n.Members, n.Delivered, n.Opened})
	}
	page := decode[CampaignList](t, f.ok("campaigns", CampaignsArgs{}))
	equal(t, page.Campaigns, want[:paging.DefaultLimit])
	if page.Next == nil {
		t.Fatal("missing next")
	}
	var created, id string
	if e := (paging.Token{Tool: "campaigns"}).Decode(*page.Next, &created, &id); e != nil {
		t.Fatal(e)
	}
	equal(t, []string{created, id}, []string{want[paging.DefaultLimit-1].Created, want[paging.DefaultLimit-1].ID})
	equal(t, decode[CampaignList](t, f.ok("campaigns", CampaignsArgs{Page: page.Next})), CampaignList{Campaigns: want[paging.DefaultLimit:]})
}

// R-T8HU-IF57 R-TKOU-C4K5 R-TN4N-3O1J
func TestBusinessEligibilityMergedAndCohortOrder(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{}}
	for n, key := range []string{"street", "city", "state", "zip"} {
		b := business(n + 1)
		b[key] = ""
		s.businesses[id(n+1)] = b
	}
	white := business(5)
	for _, key := range []string{"street", "city", "state", "zip", "email"} {
		white[key] = " "
	}
	s.businesses[id(5)] = white
	merged := business(6)
	merged["status"] = "merged"
	merged["merged_into"] = id(9)
	merged["id"] = id(9)
	merged["email"] = ""
	s.businesses[id(6)] = merged
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail", "email")
	for n := 1; n <= 4; n++ {
		a := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(n)}))
		equal(t, a.Skipped, []Skip{{"mail", SkipNoAddress}})
		equal(t, len(a.Member.Pieces), 1)
		equal(t, a.Member.Pieces[0].Channel, "email")
	}
	a := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(5)}))
	equal(t, len(a.Member.Pieces), 2)
	equal(t, len(a.Skipped), 0)
	a = decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(6)}))
	equal(t, a.Member.Candidate, id(6))
	equal(t, a.Skipped, []Skip{{"email", SkipNoEmail}})
	equal(t, a.Member.Pieces[0].Channel, "mail")
	cohort := "coh_1111111111111111"
	second := "coh_2222222222222222"
	k2 := f.create("mail", "email")
	b1, b2, b3 := business(1), business(2), business(3)
	b2["email"] = ""
	s.mu.Lock()
	s.members = []map[string]any{{"candidate": b3, "excluded": false}, {"candidate": b2, "excluded": false}, {"candidate": b1, "excluded": false}}
	s.mu.Unlock()
	out := decode[CohortAdd](t, f.ok("add_cohort", CohortArgs{k2.ID, cohort}))
	equal(t, len(out.Added), 3)
	for i, a := range out.Added {
		equal(t, a.Member.Candidate, id(3-i))
		equal(t, a.Member.Cohort, cohort)
	}
	equal(t, out.Added[1].Skipped, []Skip{{"email", SkipNoEmail}})
	original := out.Added[1].Member
	s.mu.Lock()
	s.members = []map[string]any{{"candidate": b3, "excluded": true}, {"candidate": b2, "excluded": false}, {"candidate": b1, "excluded": false}}
	s.mu.Unlock()
	out = decode[CohortAdd](t, f.ok("add_cohort", CohortArgs{k2.ID, second}))
	equal(t, out, CohortAdd{Added: []AddedMember{}, Present: 2, Excluded: 1})
	equal(t, decode[AddedMember](t, f.ok("add_member", MemberArgs{k2.ID, id(2)})), AddedMember{Member: original, Skipped: []Skip{}})
}

// R-TFT8-T1LD R-TI91-KL2R
func TestEditValuesAndDeletedDependents(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{id(1): business(1)}}
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail")
	name, destination := "new name", "https://other.example.test/changed"
	edited := decode[CampaignChange](t, f.ok("edit_campaign", EditArgs{Campaign: k.ID, Name: &name, Destination: &destination}))
	k.Name = name
	k.Destination = destination
	equal(t, edited, CampaignChange{k, true})
	equal(t, decode[CampaignChange](t, f.ok("edit_campaign", EditArgs{Campaign: k.ID, Name: &name, Destination: &destination})), CampaignChange{k, false})
	equal(t, decode[CampaignChange](t, f.ok("edit_campaign", map[string]any{"campaign": k.ID, "name": nil, "description": nil, "destination": nil})), CampaignChange{k, false})
	if _, err := f.call("edit_campaign", map[string]any{"campaign": k.ID, "channels": []string{"email"}}); err == "" {
		t.Fatal("channels edit accepted")
	}
	a := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(1)}))
	f.ok("delete_campaign", DeleteArgs{k.ID})
	for _, name := range []string{"drop", "deliver"} {
		f.refuse(name, PieceArgs{a.Member.Pieces[0].ID}, RefusalNotFound, RefuseNotFound)
	}
	f.refuse("engagement", EngagementArgs{Piece: a.Member.Pieces[0].ID}, RefusalNotFound, RefuseNotFound)
	f.refuse("engagement", EngagementArgs{Campaign: k.ID, Candidate: id(1)}, RefusalNotFound, RefuseNotFound)
	f.refuse("add_member", MemberArgs{k.ID, id(1)}, RefusalNotFound, RefuseNotFound)
	f.refuse("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail"}, RefusalNotFound, RefuseNotFound)
}

// R-GVO6-9Q13
func TestSiblingIsolationAndArtifactOrder(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{id(1): business(1), id(2): business(2)}}
	art := &sibling{artifact: "file-id"}
	f.sibling(s, "candidates")
	f.sibling(art, "artifacts")
	f.start()
	k := f.create("mail", "email")
	a := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(1)}))
	b := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(2)}))
	empty := f.create("mail")
	cn, an := len(s.calls()), len(art.calls())
	cases := []struct {
		name string
		args any
	}{{"campaigns", CampaignsArgs{}}, {"campaign", CampaignArgs{Campaign: k.ID}}, {"create_campaign", CreateArgs{Name: "another", Channels: []string{"mail"}, Destination: "https://example.test/"}}, {"edit_campaign", EditArgs{Campaign: k.ID}}, {"delete_campaign", DeleteArgs{empty.ID}}, {"reject", RejectArgs{k.ID, id(1), "why"}}, {"drop", PieceArgs{a.Member.Pieces[0].ID}}, {"deliver", PieceArgs{b.Member.Pieces[0].ID}}, {"deliver_channel", ChannelArgs{k.ID, "email"}}, {"remove_material", RemoveArgs{k.ID, "path"}}, {"engagement", EngagementArgs{Campaign: k.ID, Candidate: id(1)}}}
	for _, c := range cases {
		f.ok(c.name, c.args)
		equal(t, len(s.calls()), cn)
		equal(t, len(art.calls()), an)
	}
	path := "folder/item.pdf"
	equal(t, decode[MaterialChange](t, f.ok("add_material", MaterialArgs{k.ID, path, "label"})), MaterialChange{Material{path, "label", "file-id", timeText(fixtureTime()), testCaller.UserID, testCaller.Email}, true})
	equal(t, len(s.calls()), cn)
	got := art.calls()[an:]
	want := []siblingCall{{Method: "HEAD", Path: "/files/" + path, User: testCaller.UserID, Email: testCaller.Email}, {Method: "POST", Path: "/mcp", Tool: "show", Args: map[string]string{"path": path}, User: testCaller.UserID, Email: testCaller.Email}}
	equal(t, got, want)
	for _, c := range s.calls() {
		equal(t, c.User, testCaller.UserID)
		equal(t, c.Email, testCaller.Email)
	}
}

// R-GQSK-QN2B R-GT8D-I6JP
func TestCohortAndBatchMatchTrailHelpers(t *testing.T) {
	f := setup(t)
	s := &sibling{}
	for _, n := range []int{3, 1, 2} {
		s.members = append(s.members, map[string]any{"candidate": business(n), "excluded": false})
	}
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail")
	cohort := "coh_1111111111111111"
	added := decode[CohortAdd](t, f.ok("add_cohort", CohortArgs{k.ID, cohort}))
	batch := decode[Delivered](t, f.ok("deliver_channel", ChannelArgs{k.ID, "mail"}))
	equal(t, len(batch.Pieces), 3)
	expected := setup(t)
	ctx := identity.NewContext(context.Background(), testCaller)
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 1})
	for _, a := range added.Added {
		trail.MemberAdded(ctx, expected.cfg.Telemetry, expected.cfg.Events, k.ID, a.Member.Candidate, cohort)
	}
	for _, p := range batch.Pieces {
		trail.PieceDelivered(ctx, expected.cfg.Telemetry, expected.cfg.Events, p.Campaign, p.Candidate, p.ID, p.Channel, p.How)
	}
	f.flush()
	expected.flush()
	equal(t, f.bus.Events(), expected.bus.Events())
	equal(t, changeTrail(f.log.Events()), changeTrail(expected.log.Events()))
	equal(t, len(s.calls()), 1)
	equal(t, s.calls()[0].Args, map[string]string{"cohort": cohort})
}
