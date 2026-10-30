package tools

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	campaigns "github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

var testCaller = identity.Caller{UserID: "u_7f3a9c21", Email: "mgreenly+fake00@gmail.com", RequestID: "req_fixture"}

type fixture struct {
	t      *testing.T
	cfg    Config
	d      *db.DB
	client *mcp.Client
	server *mcp.Server
	bus    *events.Capture
	log    *telemetry.Capture
	at     *time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "test.db"), Migrations: campaigns.Migrations(), Now: fixtureTime})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	log := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "campaigns", Sink: log, Now: fixtureTime, Rand: &fixtureEntropy{counter: 2}})
	bus := &events.Capture{}
	em := events.New(events.Config{Service: "campaigns", Sink: bus, Telemetry: w, Emits: trail.Emits(), Now: fixtureTime, Rand: &fixtureEntropy{counter: 3}})
	t.Cleanup(func() { em.Shutdown(context.Background()); w.Shutdown(context.Background(), "test") })
	at := fixtureTime()
	s := store.New(d, store.Config{Now: func() time.Time { return at }, Rand: &fixtureEntropy{counter: 1}})
	cfg := Config{s, w, em, nil, nil, "https://michaelgreenly.dev/c/?t="}
	f := &fixture{t: t, cfg: cfg, d: d, bus: bus, log: log, at: &at}
	return f
}
func (f *fixture) start() {
	f.server = mcp.NewServer(mcp.ServerConfig{Name: "campaigns", Telemetry: f.cfg.Telemetry})
	Register(f.server, f.cfg)
	srv := httptest.NewServer(events.Middleware(identity.Require(f.server)))
	f.t.Cleanup(srv.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL, HTTPClient: &http.Client{Transport: causeTransport{http.DefaultTransport}}})
}
func (f *fixture) call(name string, args any) (map[string]json.RawMessage, string) {
	f.t.Helper()
	raw, e := json.Marshal(args)
	if e != nil {
		f.t.Fatal(e)
	}
	ctx := events.NewContext(context.Background(), events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 1})
	r, e := f.client.CallTool(ctx, testCaller, name, raw)
	if e != nil {
		f.t.Fatal(e)
	}
	b, e := r.MarshalJSON()
	if e != nil {
		f.t.Fatal(e)
	}
	var wire struct {
		Content           []struct{ Type, Text string }
		StructuredContent map[string]json.RawMessage
	}
	if e = json.Unmarshal(b, &wire); e != nil {
		f.t.Fatal(e)
	}
	if len(wire.Content) != 1 || wire.Content[0].Type != "text" {
		f.t.Fatalf("bad content: %s", b)
	}
	if r.IsError() {
		if wire.StructuredContent != nil {
			f.t.Fatal("refusal has structured output")
		}
		return nil, wire.Content[0].Text
	}
	var text map[string]json.RawMessage
	if e = json.Unmarshal([]byte(wire.Content[0].Text), &text); e != nil || !reflect.DeepEqual(text, wire.StructuredContent) {
		f.t.Fatalf("inconsistent output %s", b)
	}
	return wire.StructuredContent, ""
}
func (f *fixture) ok(name string, args any) map[string]json.RawMessage {
	f.t.Helper()
	out, err := f.call(name, args)
	if err != "" {
		f.t.Fatalf("%s: %s", name, err)
	}
	return out
}
func (f *fixture) refuse(name string, args any, w, text string) {
	f.t.Helper()
	_, got := f.call(name, args)
	want := w + ": " + text
	if w == "" {
		want = text
	}
	if got != want {
		f.t.Fatalf("%s refusal=%q want %q", name, got, want)
	}
}
func decode[T any](t *testing.T, m map[string]json.RawMessage) T {
	t.Helper()
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	var out T
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func (f *fixture) create(ch ...string) Campaign {
	return decode[Campaign](f.t, f.ok("create_campaign", CreateArgs{Name: "fixture", Channels: ch, Destination: "https://example.test/landing"}))
}
func (f *fixture) flush() {
	f.t.Helper()
	if e := f.cfg.Events.Flush(context.Background()); e != nil {
		f.t.Fatal(e)
	}
	if e := f.cfg.Telemetry.Flush(context.Background()); e != nil {
		f.t.Fatal(e)
	}
}
func id(n int) string { return fmt.Sprintf("cand_%016x", n) }

type sibling struct {
	mu                sync.Mutex
	requests          []string
	details           []siblingCall
	users             []string
	businesses        map[string]map[string]any
	members           []map[string]any
	failure           string
	head              int
	artifact          string
	showFailure       string
	candidateFailures map[string]string
	candidateStatuses map[string]int
}

func (s *sibling) records() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.requests...)
}
func (s *sibling) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = append(s.users, r.Header.Get("X-User-ID"))
	if r.Method == http.MethodHead {
		s.requests = append(s.requests, "HEAD "+r.URL.Path)
		s.details = append(s.details, siblingCall{Method: r.Method, Path: r.URL.Path, User: r.Header.Get("X-User-ID"), Email: r.Header.Get("X-User-Email")})
		status := s.head
		if status == 0 {
			status = 200
		}
		w.Header().Set("Content-Length", "12")
		w.Header().Set("ETag", `"sum"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.WriteHeader(status)
		return
	}
	var req struct {
		ID     json.RawMessage
		Params struct {
			Name      string
			Arguments map[string]string
		}
	}
	if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	s.requests = append(s.requests, req.Params.Name)
	s.details = append(s.details, siblingCall{Method: r.Method, Path: r.URL.Path, Tool: req.Params.Name, Args: req.Params.Arguments, User: r.Header.Get("X-User-ID"), Email: r.Header.Get("X-User-Email")})
	var data any
	failure := s.failure
	switch req.Params.Name {
	case "candidate":
		candidate := req.Params.Arguments["candidate"]
		if status := s.candidateStatuses[candidate]; status != 0 {
			w.WriteHeader(status)
			return
		}
		if text := s.candidateFailures[candidate]; text != "" {
			failure = text
		}
		b, ok := s.businesses[candidate]
		if !ok && failure == "" {
			failure = "not_found: absent"
		}
		data = map[string]any{"candidate": b}
	case "prospect_list":
		data = map[string]any{"members": s.members}
	case "show":
		failure = s.showFailure
		data = map[string]any{"id": s.artifact, "path": req.Params.Arguments["path"], "bytes": 12, "content_type": "application/pdf", "sha256": "sum", "owner": "owner", "created": "2026-01-01T00:00:00Z", "modified": "2026-01-01T00:00:00Z", "url": ""}
	}
	result := map[string]any{"structuredContent": data, "content": []any{}}
	if failure != "" {
		result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": failure}}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}
func (f *fixture) sibling(s *sibling, name string) {
	f.t.Helper()
	sock := filepath.Join(f.t.TempDir(), "s")
	l, e := net.Listen("unix", sock)
	if e != nil {
		f.t.Fatal(e)
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	f.t.Cleanup(func() { _ = srv.Close(); <-done })
	entry := services.Entry{Name: name, Socket: sock}
	if name == "candidates" {
		f.cfg.Candidates = candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: f.cfg.Telemetry})
	} else {
		f.cfg.Artifacts = artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: f.cfg.Telemetry})
	}
}
func business(n int) map[string]any {
	return map[string]any{"id": id(n), "status": "active", "name": fmt.Sprintf("business %d", n), "names": []string{"alias"}, "street": "street", "city": "city", "state": "state", "zip": "zip", "county": "county", "phone": "phone", "email": "mgreenly+fake01@gmail.com", "people": []any{map[string]any{"name": "person", "role": "owner"}}}
}

// R-ZCZ3-MKQN R-F0LP-8C8W R-F1TL-M3ZL R-F31H-ZVQA R-F49E-DNGZ
// R-SBKK-6M4G R-SCSG-KDV5 R-3BPU-NKEA R-SF89-BXCJ R-Q4KM-NRC6
// R-Q5SJ-1J2V R-SIVY-H8KM R-Q70F-FATK R-3GLG-6ND2
func TestCatalogAndSchemas(t *testing.T) {
	f := setup(t)
	f.start()
	list, e := f.client.ListTools(context.Background(), testCaller)
	if e != nil {
		t.Fatal(e)
	}
	cat := Catalog()
	copyCat := Catalog()
	cat[0] = Entry{}
	if copyCat[0].Name == "" {
		t.Fatal("catalog shares storage")
	}
	cat = copyCat
	if len(cat) != 16 || len(list) != 16 || len(f.server.Tools()) != 16 {
		t.Fatal("catalog length")
	}
	names := []string{"campaigns", "campaign", "create_campaign", "edit_campaign", "delete_campaign", "add_prospect_list", "add_member", "add_members", "reject", "drop", "deliver", "deliver_channel", "add_material", "remove_material", "merge_list", "engagement"}
	descriptions := []string{DescribeCampaigns, DescribeCampaign, DescribeCreateCampaign, DescribeEditCampaign, DescribeDeleteCampaign, DescribeAddProspectList, DescribeAddMember, DescribeAddMembers, DescribeReject, DescribeDrop, DescribeDeliver, DescribeDeliverChannel, DescribeAddMaterial, DescribeRemoveMaterial, DescribeMergeList, DescribeEngagement}
	seen := map[string]bool{}
	for i, x := range cat {
		line := FirstLine(x.Description)
		if x.Name != names[i] || x.Description != descriptions[i] || !utf8.ValidString(x.Description) || line == "" || len([]rune(line)) > 120 || !strings.HasSuffix(line, ".") || strings.Contains(line, ". ") || strings.TrimSpace(line) != line || seen[line] {
			t.Fatalf("catalog entry %#v", x)
		}
		seen[line] = true
		if list[i].Name != x.Name || list[i].Description != x.Description || f.server.Tools()[i].Name != x.Name || f.server.Tools()[i].Description != x.Description {
			t.Fatal("registered order/copy")
		}
		want := mcp.Additive
		if i < 2 || i > 13 {
			want = mcp.Read
		}
		if i == 4 || i == 13 {
			want = mcp.Destructive
		}
		if list[i].Effect() != want {
			t.Fatal("wrong effect")
		}
	}
	if FirstLine("alpha\n\nbeta") != "alpha" || FirstLine("") != "" || FirstLine("single") != "single" {
		t.Fatal("first line")
	}
	if RefusalNotFound != "not_found" || RefusalInvalid != "invalid" || RefusalUnavailable != "unavailable" || SkipNoAddress != "no_address" || SkipNoEmail != "no_email" {
		t.Fatal("machine constants")
	}
	seen = map[string]bool{store.Unreachable: true}
	for _, x := range []string{RefuseNotFound, RefuseNoCandidate, RefuseNoProspectList, RefuseNoArtifact, RefuseInvalid, RefuseState, RefuseCandidates, RefuseArtifacts} {
		if x == "" || strings.Contains(x, "\n") || seen[x] {
			t.Fatal("refusal copy")
		}
		seen[x] = true
	}
	schemaCases(t, list)
	if _, e := f.call("create_campaign", map[string]any{"name": "x", "channels": []string{"mail"}, "destination": "https://example.test/", "extra": true}); e == "" {
		t.Fatal("unknown argument accepted")
	}
	if _, e := f.call("deliver", map[string]any{"piece": 123}); e == "" {
		t.Fatal("wrong type accepted")
	}
}

// R-3J18-Y6UG R-QGRM-HGR4 R-T4U5-D3X4 R-3LH1-PQBU R-3MOY-3I2J
func TestPagingPrecedenceAndUnavailable(t *testing.T) {
	f := setup(t)
	f.start()
	f.d.SetFailing(true)
	defer f.d.SetFailing(false)
	for _, name := range []string{"campaigns", "campaign", "merge_list", "engagement"} {
		args := map[string]any{"limit": 0, "page": "bad"}
		if name == "campaign" || name == "merge_list" {
			args["campaign"] = "bad"
		}
		if name == "merge_list" {
			args["channel"] = "bad"
		}
		_, e := paging.Window(new(0))
		f.refuse(name, args, "", e.Error())
		args["limit"] = 1
		f.refuse(name, args, "", paging.BadPage)
	}
	f.refuse("campaigns", map[string]any{}, RefusalUnavailable, store.Unreachable)
	f.refuse("create_campaign", CreateArgs{Name: "x", Channels: []string{"mail"}, Destination: "https://example.test/"}, RefusalUnavailable, store.Unreachable)
	f.refuse("create_campaign", CreateArgs{Channels: []string{}}, RefusalInvalid, RefuseInvalid)
	f.refuse("deliver", PieceArgs{Piece: "piece_1111111111111111"}, RefusalUnavailable, store.Unreachable)
	f.refuse("deliver", PieceArgs{Piece: "malformed"}, RefusalNotFound, RefuseNotFound)
	f.refuse("engagement", EngagementArgs{}, RefusalInvalid, RefuseInvalid)
}

// R-TELC-F9UO R-TFT8-T1LD R-TI91-KL2R R-T621-QVNT R-3P4Q-V1JX
func TestCampaignLifecycleAndWindows(t *testing.T) {
	f := setup(t)
	f.start()
	a := f.create("email", "mail")
	b := f.create("mail")
	if a.ID == b.ID || a.Description != "" || !reflect.DeepEqual(a.Channels, []string{"mail", "email"}) || a.Created != "2026-02-03T03:05:06Z" || a.CreatedBy != testCaller.UserID || a.CreatedEmail != testCaller.Email {
		t.Fatalf("campaign %#v", a)
	}
	for _, chs := range [][]string{{}, {"mail", "mail"}, {"sms"}} {
		f.refuse("create_campaign", CreateArgs{Name: "x", Channels: chs, Destination: "https://example.test/"}, RefusalInvalid, RefuseInvalid)
	}
	f.refuse("create_campaign", CreateArgs{Name: " ", Channels: []string{"mail"}, Destination: "https://example.test/"}, RefusalInvalid, RefuseInvalid)
	f.refuse("create_campaign", CreateArgs{Name: "x", Channels: []string{"mail"}, Destination: "/relative"}, RefusalInvalid, RefuseInvalid)
	x := decode[CampaignChange](t, f.ok("edit_campaign", EditArgs{Campaign: a.ID}))
	if x.Changed {
		t.Fatal("no op changed")
	}
	desc := "details"
	x = decode[CampaignChange](t, f.ok("edit_campaign", EditArgs{Campaign: a.ID, Description: &desc}))
	if !x.Changed || x.Campaign.Description != desc {
		t.Fatal(x)
	}
	desc = ""
	x = decode[CampaignChange](t, f.ok("edit_campaign", EditArgs{Campaign: a.ID, Description: &desc}))
	if !x.Changed || x.Campaign.Description != "" {
		t.Fatal(x)
	}
	name := " "
	f.refuse("edit_campaign", EditArgs{Campaign: "bad", Name: &name}, RefusalInvalid, RefuseInvalid)
	z := 1
	var page *string
	seen := map[string]bool{}
	for {
		w := decode[CampaignList](t, f.ok("campaigns", CampaignsArgs{Page: page, Limit: &z}))
		if len(w.Campaigns) != 1 {
			t.Fatal(w)
		}
		if seen[w.Campaigns[0].ID] {
			t.Fatal("duplicate")
		}
		seen[w.Campaigns[0].ID] = true
		page = w.Next
		if page == nil {
			break
		}
	}
	if len(seen) != 2 {
		t.Fatal(seen)
	}
	for j := range 2 {
		x := decode[CampaignChange](t, f.ok("delete_campaign", DeleteArgs{Campaign: a.ID}))
		if !x.Campaign.Deleted || x.Changed != (j == 0) {
			t.Fatal(x)
		}
	}
	for _, n := range []string{"campaign", "edit_campaign"} {
		f.refuse(n, map[string]any{"campaign": a.ID}, RefusalNotFound, RefuseNotFound)
	}
	w := decode[CampaignList](t, f.ok("campaigns", CampaignsArgs{}))
	if len(w.Campaigns) != 1 || w.Campaigns[0].ID != b.ID {
		t.Fatal(w)
	}
	f.refuse("delete_campaign", DeleteArgs{Campaign: "bad"}, RefusalNotFound, RefuseNotFound)
}

// R-3RKJ-ML1B R-3SSG-0CS0 R-T8HU-IF57 R-3K95-BYL5 R-3QCN-8TAM
// R-GN4V-LBU8 R-GKP2-TSCU R-GOCR-Z3KX R-3WG5-5O03 R-3HTC-KF3R
// R-403U-AZ86 R-41BQ-OQYV R-42JN-2IPK R-3XO1-JFQS R-3YVX-X7HH
// R-2VV5-OJR9 R-2X32-2BHY R-2YAY-G38N R-QRQP-XEFD R-QSYM-B662
func TestMembersPiecesEngagementAndEvents(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{id(1): business(1), id(2): business(2), id(3): business(3)}}
	s.businesses[id(2)]["zip"] = ""
	s.businesses[id(2)]["email"] = ""
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail", "email")
	f.refuse("add_member", MemberArgs{Campaign: "bad", Candidate: id(1)}, RefusalNotFound, RefuseNotFound)
	f.refuse("add_member", MemberArgs{Campaign: k.ID, Candidate: "bad"}, RefusalNotFound, RefuseNoCandidate)
	if len(s.records()) != 0 {
		t.Fatal(s.records())
	}
	a := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(1)}))
	if !a.Added || len(a.Member.Pieces) != 2 || len(a.Skipped) != 0 || a.Member.AddedBy != testCaller.UserID || a.Member.AddedEmail != testCaller.Email {
		t.Fatal(a)
	}
	before := len(s.records())
	a2 := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(1)}))
	if a2.Added || !reflect.DeepEqual(a.Member, a2.Member) || len(s.records()) != before {
		t.Fatal(a2)
	}
	b := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(2)}))
	if !b.Added || len(b.Member.Pieces) != 0 || !reflect.DeepEqual(b.Skipped, []Skip{{"mail", SkipNoAddress}, {"email", SkipNoEmail}}) {
		t.Fatal(b)
	}
	c := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(3)}))
	z := 1
	w := decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID, Limit: &z}))
	if len(w.Members) != 1 || w.Next == nil || w.Counts.Members != 3 || w.Campaign.ID != k.ID {
		t.Fatal(w)
	}
	all := []Member{w.Members[0]}
	for w.Next != nil {
		w = decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID, Limit: &z, Page: w.Next}))
		all = append(all, w.Members...)
	}
	if len(all) != 3 {
		t.Fatal(all)
	}
	mail := a.Member.Pieces[0]
	email := a.Member.Pieces[1]
	p := decode[PieceChange](t, f.ok("deliver", PieceArgs{mail.ID}))
	if !p.Changed || p.Piece.State != "delivered" || p.Piece.How != "hand" || p.Piece.DeliveredBy != testCaller.UserID || p.Piece.DeliveredEmail != testCaller.Email {
		t.Fatal(p)
	}
	if repeat := decode[PieceChange](t, f.ok("deliver", PieceArgs{mail.ID})); repeat.Changed || !reflect.DeepEqual(repeat.Piece, p.Piece) {
		t.Fatal("repeated delivery", repeat)
	}
	f.refuse("drop", PieceArgs{mail.ID}, RefusalInvalid, RefuseState)
	f.refuse("delete_campaign", DeleteArgs{k.ID}, RefusalInvalid, RefuseState)
	dropped := decode[PieceChange](t, f.ok("drop", PieceArgs{email.ID}))
	if !dropped.Changed || dropped.Piece.State != "dropped" || dropped.Piece.DroppedBy != testCaller.UserID {
		t.Fatal(dropped)
	}
	if decode[PieceChange](t, f.ok("drop", PieceArgs{email.ID})).Changed {
		t.Fatal("repeat drop")
	}
	f.refuse("deliver", PieceArgs{email.ID}, RefusalInvalid, RefuseState)
	rej := decode[MemberChange](t, f.ok("reject", RejectArgs{k.ID, id(3), "reason"}))
	if !rej.Changed || !rej.Member.Rejected || rej.Member.Why != "reason" || rej.Member.RejectedBy != testCaller.UserID {
		t.Fatal(rej)
	}
	rej = decode[MemberChange](t, f.ok("reject", RejectArgs{k.ID, id(3), "different"}))
	if rej.Changed || rej.Member.Why != "reason" || rej.Member.Pieces[0].State != "planned" {
		t.Fatal(rej)
	}
	f.refuse("deliver", PieceArgs{c.Member.Pieces[0].ID}, RefusalInvalid, RefuseState)
	f.ok("drop", PieceArgs{c.Member.Pieces[0].ID})
	f.refuse("reject", RejectArgs{"bad", "bad", " "}, RefusalInvalid, RefuseInvalid)
	rows := decode[MergeList](t, f.ok("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", All: true, Limit: &z}))
	if len(rows.Rows) != 1 || rows.Next == nil || rows.Rows[0].Business.Name != s.businesses[id(1)]["name"] || !strings.HasPrefix(rows.Rows[0].Link, f.cfg.LinkPrefix) {
		t.Fatal(rows)
	}
	for rows.Next != nil {
		rows = decode[MergeList](t, f.ok("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", All: true, Limit: &z, Page: rows.Next}))
		if len(rows.Rows) != 1 || !rows.Rows[0].Rejected {
			t.Fatal(rows)
		}
	}
	none := decode[MergeList](t, f.ok("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail"}))
	if len(none.Rows) != 0 {
		t.Fatal(none)
	}
	f.refuse("merge_list", MergeArgs{Campaign: k.ID, Channel: "sms"}, RefusalInvalid, RefuseInvalid)
	r, e := f.cfg.Store.GetPiece(context.Background(), mail.ID)
	if e != nil {
		t.Fatal(e)
	}
	for range 3 {
		if _, e := f.cfg.Store.Visit(context.Background(), r.Piece.Token, store.VisitInput{UserAgent: "agent", Referrer: "ref", Address: "addr"}); e != nil {
			t.Fatal(e)
		}
	}
	eng := decode[Engagement](t, f.ok("engagement", EngagementArgs{Piece: mail.ID, Limit: &z}))
	if eng.Opens != 3 || !eng.Opened || eng.Visits == nil || len(*eng.Visits) != 1 || eng.Next == nil || (*eng.Visits)[0].UserAgent != "agent" {
		t.Fatal(eng)
	}
	count := 1
	for eng.Next != nil {
		eng = decode[Engagement](t, f.ok("engagement", EngagementArgs{Piece: mail.ID, Limit: &z, Page: eng.Next}))
		count += len(*eng.Visits)
	}
	if count != 3 {
		t.Fatal(count)
	}
	f.ok("campaign", CampaignArgs{Campaign: k.ID})
	member := f.ok("engagement", EngagementArgs{Campaign: k.ID, Candidate: id(1)})
	me := decode[Engagement](t, member)
	if me.Opens != 3 || me.Visits != nil || me.Next != nil || me.Piece != "" {
		t.Fatal(me)
	}
	if _, ok := member["visits"]; ok {
		t.Fatal("member visits")
	}
	for _, args := range []EngagementArgs{{}, {Campaign: k.ID}, {Piece: mail.ID, Campaign: k.ID}, {Piece: mail.ID, Campaign: k.ID, Candidate: id(1)}} {
		f.refuse("engagement", args, RefusalInvalid, RefuseInvalid)
	}
	batch := decode[Delivered](t, f.ok("deliver_channel", ChannelArgs{k.ID, "email"}))
	if len(batch.Pieces) != 0 {
		t.Fatal(batch)
	}
	f.flush()
	ev := f.bus.Events()
	if len(ev) != 5 {
		t.Fatalf("bus events %d: %#v", len(ev), ev)
	}
	for _, e := range ev {
		if e.User != testCaller.UserID || e.Cause != "evt_8c3f1a6e2d9b4075" || e.Depth != 2 {
			t.Fatalf("context %#v", e)
		}
	}
	names := []string{trail.EventMemberAdded, trail.EventMemberAdded, trail.EventMemberAdded, trail.EventPieceDelivered, trail.EventMemberRejected}
	for i, e := range ev {
		if e.Name != names[i] {
			t.Fatal(e)
		}
	}
	n := 0
	for _, e := range f.log.Events() {
		if e.Name == trail.EventMemberOpened {
			t.Fatal("tool recorded a member open", e)
		}
		if e.Name == trail.EventMemberAdded || e.Name == trail.EventMemberRejected || e.Name == trail.EventPieceDelivered {
			n++
		}
	}
	if n != len(ev) {
		t.Fatalf("trail %d bus %d", n, len(ev))
	}
	for _, u := range s.users {
		if u != testCaller.UserID {
			t.Fatal("identity not forwarded")
		}
	}
}

type causeTransport struct{ base http.RoundTripper }

func (c causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	events.Forward(r.Context(), r)
	return c.base.RoundTrip(r)
}

// R-GPKO-CVBM R-TVNX-S28E R-N23A-5T0F
func TestMaterialLifecycleAndRefusals(t *testing.T) {
	f := setup(t)
	s := &sibling{artifact: "artifact-fixture"}
	f.sibling(s, "artifacts")
	f.start()
	k := f.create("mail")
	args := MaterialArgs{k.ID, "folder/item.pdf", "label"}
	f.refuse("add_material", MaterialArgs{k.ID, "", "label"}, RefusalInvalid, RefuseInvalid)
	f.refuse("add_material", MaterialArgs{"bad", "path", "label"}, RefusalNotFound, RefuseNotFound)
	if len(s.records()) != 0 {
		t.Fatal(s.records())
	}
	f.refuse("add_material", MaterialArgs{k.ID, "bad%path", "label"}, RefusalInvalid, RefuseInvalid)
	if len(s.records()) != 0 {
		t.Fatal("bad path reached socket")
	}
	for _, tc := range []struct {
		code       int
		word, text string
	}{{400, RefusalInvalid, RefuseInvalid}, {404, RefusalNotFound, RefuseNoArtifact}, {503, RefusalUnavailable, RefuseArtifacts}} {
		s.mu.Lock()
		s.head = tc.code
		s.mu.Unlock()
		n := len(s.records())
		f.refuse("add_material", args, tc.word, tc.text)
		if len(s.records()) != n+1 {
			t.Fatal("show reached after failed head")
		}
	}
	s.mu.Lock()
	s.head = 200
	s.showFailure = "not_found: gone"
	s.mu.Unlock()
	f.refuse("add_material", args, RefusalUnavailable, RefuseArtifacts)
	s.mu.Lock()
	s.showFailure = ""
	s.artifact = ""
	s.mu.Unlock()
	f.refuse("add_material", args, RefusalUnavailable, RefuseArtifacts)
	s.mu.Lock()
	s.artifact = "artifact-fixture"
	s.mu.Unlock()
	a := decode[MaterialChange](t, f.ok("add_material", args))
	if !a.Changed || a.Material.Artifact != "artifact-fixture" || a.Material.Path != args.Path || a.Material.AddedBy != testCaller.UserID || a.Material.AddedEmail != testCaller.Email {
		t.Fatal(a)
	}
	if decode[MaterialChange](t, f.ok("add_material", args)).Changed {
		t.Fatal("repeat changed")
	}
	f.ok("add_material", MaterialArgs{k.ID, "second", "second label"})
	args.Label = "new label"
	a = decode[MaterialChange](t, f.ok("add_material", args))
	if !a.Changed || a.Material.Label != args.Label {
		t.Fatal(a)
	}
	w := decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID}))
	if len(w.Material) != 2 || w.Material[0].Label != args.Label {
		t.Fatal(w)
	}
	n := len(s.records())
	for j := range 2 {
		r := decode[Removed](t, f.ok("remove_material", RemoveArgs{k.ID, args.Path}))
		if r.Path != args.Path || r.Removed != (j == 0) {
			t.Fatal(r)
		}
	}
	if len(s.records()) != n {
		t.Fatal("remove contacted artifacts")
	}
	f.refuse("remove_material", RemoveArgs{k.ID, ""}, RefusalInvalid, RefuseInvalid)
	f.refuse("remove_material", RemoveArgs{"bad", "x"}, RefusalNotFound, RefuseNotFound)
	for _, u := range s.users {
		if u != testCaller.UserID {
			t.Fatal("artifact caller")
		}
	}
	f.flush()
	if len(f.bus.Events()) != 0 {
		t.Fatal("material emitted")
	}
}

// R-3U0C-E4IP R-3V88-RW9E R-30QR-7MQ1
func TestProspectListPartialFailureAndRepeat(t *testing.T) {
	f := setup(t)
	limited := &boundedReader{remaining: 56}
	f.cfg.Store = store.New(f.d, store.Config{Now: fixtureTime, Rand: limited})
	s := &sibling{businesses: map[string]map[string]any{}}
	for n := 1; n <= 6; n++ {
		s.members = append(s.members, map[string]any{"candidate": business(n), "excluded": n == 6})
	}
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail")
	f.refuse("add_prospect_list", ProspectListArgs{"bad", "bad"}, RefusalNotFound, RefuseNotFound)
	for _, malformed := range []string{"bad", "coh_1111111111111111", "pl_111111111111111", "pl_111111111111111g"} {
		f.refuse("add_prospect_list", ProspectListArgs{k.ID, malformed}, RefusalNotFound, RefuseNoProspectList)
	}
	f.refuse("add_prospect_list", ProspectListArgs{"camp_ffffffffffffffff", "pl_1111111111111111"}, RefusalNotFound, RefuseNotFound)
	if len(s.records()) != 0 {
		t.Fatal(s.records())
	}
	prospectList := "pl_1111111111111111"
	f.refuse("add_prospect_list", ProspectListArgs{k.ID, prospectList}, RefusalUnavailable, store.Unreachable)
	rows, e := f.cfg.Store.ListMembers(context.Background(), k.ID, "", "", 10)
	if e != nil || len(rows) != 2 {
		t.Fatalf("partial rows %d %v", len(rows), e)
	}
	f.flush()
	if len(f.bus.Events()) != 2 {
		t.Fatal(f.bus.Events())
	}
	for _, e := range f.bus.Events() {
		if e.Attrs["prospect_list"] != prospectList {
			t.Fatal(e)
		}
	}
	limited.remaining = 10000
	out := decode[ProspectListAdd](t, f.ok("add_prospect_list", ProspectListArgs{k.ID, prospectList}))
	if len(out.Added) != 3 || out.Present != 2 || out.Excluded != 1 {
		t.Fatalf("%#v", out)
	}
	for _, a := range out.Added {
		if !a.Added || a.Member.ProspectList != prospectList || len(a.Member.Pieces) != 1 {
			t.Fatal(a)
		}
	}
	out = decode[ProspectListAdd](t, f.ok("add_prospect_list", ProspectListArgs{k.ID, prospectList}))
	if len(out.Added) != 0 || out.Present != 5 || out.Excluded != 1 {
		t.Fatalf("repeat %#v", out)
	}
	f.flush()
	if len(f.bus.Events()) != 5 {
		t.Fatal(f.bus.Events())
	}
	if !reflect.DeepEqual(s.records(), []string{"prospect_list", "prospect_list", "prospect_list"}) {
		t.Fatal(s.records())
	}
}

type boundedReader struct {
	remaining int
	next      byte
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, fmt.Errorf("fixture entropy exhausted")
	}
	n := min(len(p), r.remaining)
	for i := range n {
		r.next++
		p[i] = r.next
	}
	r.remaining -= n
	return n, nil
}

func TestAbsentSiblingsAndValidatedFailures(t *testing.T) {
	f := setup(t)
	f.start()
	k := f.create("mail")
	f.refuse("add_member", MemberArgs{k.ID, id(1)}, RefusalUnavailable, RefuseCandidates)
	f.refuse("add_prospect_list", ProspectListArgs{k.ID, "pl_1111111111111111"}, RefusalUnavailable, RefuseCandidates)
	f.refuse("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail"}, RefusalUnavailable, RefuseCandidates)
	f.refuse("merge_list", MergeArgs{Campaign: k.ID, Channel: "email"}, RefusalInvalid, RefuseInvalid)
	f.refuse("deliver_channel", ChannelArgs{k.ID, "email"}, RefusalInvalid, RefuseInvalid)
	f.refuse("deliver_channel", ChannelArgs{"bad", "sms"}, RefusalInvalid, RefuseInvalid)
	f.refuse("add_material", MaterialArgs{k.ID, "path", "label"}, RefusalUnavailable, RefuseArtifacts)
	f.refuse("engagement", EngagementArgs{Piece: "bad"}, RefusalNotFound, RefuseNotFound)
	f.refuse("engagement", EngagementArgs{Campaign: k.ID, Candidate: id(1)}, RefusalNotFound, RefuseNotFound)
	f.flush()
	if len(f.bus.Events()) != 0 {
		t.Fatal("refusals emitted")
	}
}

func TestSiblingRefusalsAndBatch(t *testing.T) {
	f := setup(t)
	s := &sibling{businesses: map[string]map[string]any{id(1): business(1), id(2): business(2), id(3): business(3)}}
	f.sibling(s, "candidates")
	f.start()
	k := f.create("mail")
	prospectList := "pl_1111111111111111"
	for _, failure := range []string{"not_found: absent", "unavailable: failed", "stopping: stop", "arbitrary: text"} {
		s.mu.Lock()
		s.failure = failure
		s.mu.Unlock()
		w, ct := RefusalUnavailable, RefuseCandidates
		if strings.HasPrefix(failure, "not_found:") {
			w, ct = RefusalNotFound, RefuseNoCandidate
		}
		f.refuse("add_member", MemberArgs{k.ID, id(1)}, w, ct)
		if w == RefusalNotFound {
			ct = RefuseNoProspectList
		}
		f.refuse("add_prospect_list", ProspectListArgs{k.ID, prospectList}, w, ct)
	}
	s.mu.Lock()
	s.failure = ""
	s.members = []map[string]any{{"candidate": business(1), "excluded": false}, {"candidate": map[string]any{"id": "malformed", "status": "active", "name": "bad"}, "excluded": true}}
	s.mu.Unlock()
	f.refuse("add_prospect_list", ProspectListArgs{k.ID, prospectList}, RefusalUnavailable, RefuseCandidates)
	w := decode[CampaignRead](t, f.ok("campaign", CampaignArgs{Campaign: k.ID}))
	if len(w.Members) != 0 {
		t.Fatal("malformed prospect list partially added")
	}
	var pieces []string
	for n := 1; n <= 3; n++ {
		a := decode[AddedMember](t, f.ok("add_member", MemberArgs{k.ID, id(n)}))
		pieces = append(pieces, a.Member.Pieces[0].ID)
	}
	before := len(s.records())
	batch := decode[Delivered](t, f.ok("deliver_channel", ChannelArgs{k.ID, "mail"}))
	if len(batch.Pieces) != 3 {
		t.Fatal(batch)
	}
	for i, p := range batch.Pieces {
		if p.ID != pieces[i] || p.How != "hand" || p.DeliveredBy != testCaller.UserID {
			t.Fatal(p)
		}
	}
	if len(decode[Delivered](t, f.ok("deliver_channel", ChannelArgs{k.ID, "mail"})).Pieces) != 0 || len(s.records()) != before {
		t.Fatal("batch repeat or sibling request")
	}
	s.mu.Lock()
	delete(s.businesses, id(2))
	s.businesses[id(1)]["name"] = "updated"
	s.mu.Unlock()
	rows := decode[MergeList](t, f.ok("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", All: true}))
	if len(rows.Rows) != 3 || rows.Rows[0].Business.Name != "updated" || rows.Rows[1].Business.Name != "" {
		t.Fatal(rows)
	}
	s.mu.Lock()
	s.failure = "unavailable: failed"
	s.mu.Unlock()
	f.refuse("merge_list", MergeArgs{Campaign: k.ID, Channel: "mail", All: true}, RefusalUnavailable, RefuseCandidates)
	f.flush()
	ev := f.bus.Events()
	if len(ev) != 6 {
		t.Fatal(ev)
	}
	for i, e := range ev[3:] {
		if e.Name != trail.EventPieceDelivered || e.Attrs["piece"] != pieces[i] || e.Attrs["how"] != "hand" || e.Attrs["channel"] != "mail" {
			t.Fatal(e)
		}
	}
}

func fixtureTime() time.Time {
	return time.Date(2026, 2, 3, 4, 5, 6, 987, time.FixedZone("offset", 3600))
}

type siblingCall struct {
	Method, Path, Tool, User, Email string
	Args                            map[string]string
}

func (s *sibling) calls() []siblingCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]siblingCall{}, s.details...)
}

type fixtureEntropy struct {
	counter uint64
	buffer  []byte
}

func (r *fixtureEntropy) Read(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if len(r.buffer) == 0 {
			hash := sha256.Sum256(binary.BigEndian.AppendUint64(nil, r.counter))
			r.counter++
			r.buffer = hash[:]
		}
		size := copy(p, r.buffer)
		p = p[size:]
		r.buffer = r.buffer[size:]
	}
	return n, nil
}
