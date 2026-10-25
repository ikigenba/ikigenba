package tools_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type random struct{ n uint64 }

func (r *random) Read(b []byte) (int, error) {
	r.n++
	var v [8]byte
	binary.LittleEndian.PutUint64(v[:], r.n)
	for i := range b {
		b[i] = v[i%8]
	}
	return len(b), nil
}

type fixture struct {
	t      *testing.T
	d      *db.DB
	dbPath string
	st     *store.Store
	eng    *passes.Engine
	client *mcp.Client
	now    time.Time
}

func newFixture(t *testing.T) *fixture                         { return fixtureWithScrapes(t, nil) }
func fixtureWithScrapes(t *testing.T, h http.Handler) *fixture { return fixtureWithWebsite(t, h, nil) }
func fixtureWithWebsite(t *testing.T, h http.Handler, k *website.Checker) *fixture {
	return fixtureWithPrompts(t, h, k, nil)
}
func fixtureWithPrompts(t *testing.T, h http.Handler, k *website.Checker, pc *prompts.Client) *fixture {
	return fixtureWithRoutes(t, h, k, pc, nil, "")
}
func fixtureWithRoutes(t *testing.T, h http.Handler, k *website.Checker, pc *prompts.Client, rc *routes.Client, origin string) *fixture {
	t.Helper()
	f := &fixture{t: t, now: time.Date(2026, 4, 5, 0, 7, 8, 0, time.FixedZone("fixture", 2*60*60))}
	now := func() time.Time { return f.now }
	f.dbPath = filepath.Join(t.TempDir(), "db")
	d, err := db.Open(t.Context(), db.Config{Path: f.dbPath, Migrations: candidates.Migrations(), Now: now, Service: "candidates"})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(d, store.Config{Now: now, Rand: &random{}, Origin: origin})
	if err := f.st.ReplaceZipDrive(t.Context(), candidates.ZipDrive()); err != nil {
		t.Fatal(err)
	}
	w := telemetry.New(telemetry.Config{Service: "candidates", Sink: &telemetry.Capture{}, Now: now, Sleep: func(context.Context, time.Duration) {}, Rand: &random{}})
	em := events.New(events.Config{Service: "candidates", Sink: &events.Capture{}, Now: now, Sleep: func(context.Context, time.Duration) {}, Rand: &random{}, Emits: trail.Emits()})
	var sc *scrapes.Client
	if h != nil {
		dir, err := os.MkdirTemp("", "tools-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Error(err)
			}
		})
		socket := filepath.Join(dir, "s")
		ln, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
		go func() { _ = server.Serve(ln) }()
		t.Cleanup(func() { _ = server.Close() })
		sc = scrapes.NewClient(scrapes.ClientConfig{Service: services.Entry{Name: "scrapes", Socket: socket}, Telemetry: w})
	}
	f.eng = passes.New(passes.Config{Routes: rc, Prompts: pc, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Website: k, Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: sc, Telemetry: w, Events: em})
	t.Cleanup(func() { f.eng.Drain(context.Background()) })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: w})
	tools.Register(srv, tools.Config{f.eng, f.st, k})
	host := httptest.NewServer(identity.Require(srv))
	t.Cleanup(host.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: host.Client()})
	return f
}

var caller = identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "request-one"}
var other = identity.Caller{UserID: "reviewer", Email: "reviewer@example.test", RequestID: "request-two"}

type wire struct {
	Content    []struct{ Type, Text string }
	Structured json.RawMessage `json:"structuredContent"`
}

func (f *fixture) callAs(c identity.Caller, name string, args any) mcp.Result {
	f.t.Helper()
	if a, ok := args.(tools.DecideArgs); ok && a.Links == nil {
		a.Links = []string{}
		args = a
	}
	b, err := json.Marshal(args)
	if err != nil {
		f.t.Fatal(err)
	}
	r, err := f.client.CallTool(f.t.Context(), c, name, b)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}
func (f *fixture) call(name string, args any) mcp.Result { return f.callAs(caller, name, args) }
func decode(t *testing.T, r mcp.Result) wire {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	return w
}
func success(t *testing.T, r mcp.Result, want any) {
	t.Helper()
	if r.IsError() {
		t.Fatalf("unexpected refusal: %s", decode(t, r).Content)
	}
	w := decode(t, r)
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Content) != 1 || w.Content[0].Type != "text" || w.Content[0].Text != string(b) {
		t.Fatalf("text got %v want %s", w.Content, b)
	}
	var got, expected any
	if err := json.Unmarshal(w.Structured, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("structured got %s want %s", w.Structured, b)
	}
}
func refusal(t *testing.T, r mcp.Result, word, text string) {
	t.Helper()
	w := decode(t, r)
	if !r.IsError() || len(w.Content) != 1 || w.Content[0].Type != "text" || w.Content[0].Text != word+": "+text || len(w.Structured) != 0 {
		t.Fatalf("unexpected refusal: %+v", w)
	}
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func candidateOf(c store.Candidate) tools.Candidate {
	ps := []tools.Person{}
	for _, p := range c.People {
		ps = append(ps, tools.Person{Name: p.Name, Role: p.Role})
	}
	return tools.Candidate{ID: c.ID, Status: c.Status, MergedInto: c.MergedInto, Name: c.Name, Names: c.Names, Street: c.Street, City: c.City, State: c.State, Zip: c.Zip, County: c.County, Phone: c.Phone, Email: c.Email, People: ps, Class: c.Class, Cargo: c.Cargo, Created: stamp(c.Created), Updated: stamp(c.Updated)}
}
func (f *fixture) seed(n int) []string { return f.seedWithStep(n, time.Second, false) }
func (f *fixture) seedWithStep(n int, step time.Duration, tie bool) []string {
	f.t.Helper()
	zip := ""
	for z, m := range candidates.ZipDrive() {
		if m <= 30 && (zip == "" || z < zip) {
			zip = z
		}
	}
	if zip == "" {
		f.t.Fatal("embedded table has no nearby ZIP")
	}
	ids := []string{}
	for i := range n {
		f.now = f.now.Add(step)
		high := int64(16000001 + i)
		if tie {
			high = 16000001
		}
		r := sources.Reading{Name: fmt.Sprintf("Business %02d", i), Names: []string{fmt.Sprintf("Business %02d", i), fmt.Sprintf("Trucking Alias %02d", i)}, NameKey: fmt.Sprintf("key%d", i), PhoneKey: "5551234567", DomainKey: "business.test", Street: "12 Main", City: "City", State: "MN", Zip: zip, County: "MN-001", Phone: "(555) 123-4567", Email: "mail@business.test", People: []sources.Person{{Name: "Person", Role: "Owner"}}, Class: sources.ClassForHire, Cargo: []string{"general"}, FleetVerdict: sources.FleetPass, PowerUnits: ptrInt(52), Drivers: ptrInt(37), Filed: f.now, Estimate: &sources.Estimate{Source: "fmcsa", Low: 10000001, High: &high, Confidence: .7, Facts: map[string]json.RawMessage{"units": json.RawMessage(`52`)}, Filed: f.now, Formula: 1}}
		_, newIDs, err := f.st.Attach(f.t.Context(), "run-fixture", true, []store.Observation{{Scraper: "fmcsa", SourceID: fmt.Sprintf("row%d", i), Digest: fmt.Sprintf("digest%d", i), Reading: r}})
		if err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, newIDs...)
	}
	if _, err := f.st.JudgeAll(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	return ids
}

// R-8884-IJV3 R-5LVY-DPZS
// R-0ES5-6S4R R-XMSD-TLBT R-X85L-8CFH R-ZCZ9-BW58
func TestCatalogAndSchemas(t *testing.T) {
	f := newFixture(t)
	want := []tools.Entry{{"prospect_lists", tools.DescribeProspectLists}, {"prospect_list", tools.DescribeProspectList}, {"candidates", tools.DescribeCandidates}, {"candidate", tools.DescribeCandidate}, {"passes", tools.DescribePasses}, {"pass", tools.DescribePass}, {"ingest", tools.DescribeIngest}, {"judge", tools.DescribeJudge}, {"enrich", tools.DescribeEnrich}, {"add_page", tools.DescribeAddPage}, {"decide", tools.DescribeDecide}, {"exclude", tools.DescribeExclude}, {"include", tools.DescribeInclude}, {"create_prospect_list", tools.DescribeCreateProspectList}, {"edit_prospect_list", tools.DescribeEditProspectList}, {"delete_prospect_list", tools.DescribeDeleteProspectList}, {"reject", tools.DescribeReject}, {"unreject", tools.DescribeUnreject}}
	if !reflect.DeepEqual(tools.Catalog(), want) {
		t.Fatal(tools.Catalog())
	}
	changed := tools.Catalog()
	changed[0].Name = "changed"
	if !reflect.DeepEqual(tools.Catalog(), want) {
		t.Fatal("catalog aliases")
	}
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != len(want) {
		t.Fatal(infos)
	}
	seen := map[string]bool{}
	properties := [][]string{{"page", "limit"}, {"prospect_list", "peek"}, {"name", "zip", "phone", "page", "limit"}, {"candidate"}, {"page", "limit"}, {"pass"}, {"scraper"}, {}, {"kind", "prospect_list", "candidate", "force"}, {"candidate", "url"}, {"kind", "why", "candidate", "other", "links"}, {"candidate", "why"}, {"candidate", "why"}, {"name", "filters", "order", "size"}, {"prospect_list", "name", "filters", "order", "size"}, {"prospect_list"}, {"prospect_list", "candidate", "why"}, {"prospect_list", "candidate", "why"}}
	required := [][]string{nil, {"prospect_list"}, nil, {"candidate"}, nil, {"pass"}, nil, nil, nil, {"candidate", "url"}, {"kind", "why", "candidate"}, {"candidate", "why"}, {"candidate", "why"}, {"name"}, {"prospect_list"}, {"prospect_list"}, {"prospect_list", "candidate", "why"}, {"prospect_list", "candidate", "why"}}
	for i, info := range infos {
		if info.Name != want[i].Name || info.Description != want[i].Description || len(info.OutputSchema) != 0 {
			t.Fatal(info)
		}
		d := info.Description
		if len(d) > 120 || !strings.HasSuffix(d, ".") || strings.Contains(d, "\n") || strings.Contains(d, ". ") || strings.TrimSpace(d) != d || seen[d] {
			t.Fatal(d)
		}
		seen[d] = true
		effect := mcp.Additive
		if i < 6 {
			effect = mcp.Read
		}
		if info.Name == "delete_prospect_list" {
			effect = mcp.Destructive
		}
		if info.Effect() != effect {
			t.Fatal(info)
		}
		var schema struct {
			Properties json.RawMessage
			Required   []string
		}
		if err := json.Unmarshal(info.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(objectKeys(t, schema.Properties), properties[i]) || !reflect.DeepEqual(schema.Required, required[i]) {
			t.Fatal(info.Name, string(info.InputSchema))
		}
	}
	seen = map[string]bool{}
	for _, d := range []string{tools.RefuseNotFound, tools.RefuseInvalid, tools.RefuseBusy, tools.RefuseStopping, tools.RefuseUnavailable, tools.RefuseNoWebsite, tools.RefuseOffSite, tools.RefusePageCap, tools.RefuseFetch} {
		if d == "" || strings.Contains(d, "\n") || seen[d] {
			t.Fatal(d)
		}
		seen[d] = true
	}
	_ = tools.ProspectList{"id", "name", 1, store.ProspectListFilters{}, []store.ProspectListOrder{}, "creator", "email", "created", "changer", "email", "changed"}
	_ = tools.BasisCounts{0, 0, 0}
	_ = tools.Changes{nil, nil, nil, nil}
	_ = tools.Joined{"candidate", "name", "basis", "why"}
	_ = tools.Left{"candidate", "name", "then", "now", "why", "filter"}
	_ = tools.Kinds{nil, nil}
	_ = tools.Enrichments{"basis", tools.Kinds{}, nil, nil}
	_ = tools.Member{tools.Candidate{}, "", nil, nil, nil, nil, nil, nil, "", "", "", "", "", nil, nil, nil, "", "", "", "", false}
	_ = tools.Reject{"kind", "prospect_list", "name", "candidate", "why", "user", "email", "at"}
	_ = tools.ProspectListRead{tools.ProspectList{}, 0, 0, tools.BasisCounts{}, tools.Changes{}, nil, nil}
	_ = tools.Judgments{nil, nil, nil, nil, nil, nil, nil, "", false, 0, ""}
	_ = tools.Estimate{"source", 1, nil, .5, nil, "", 1}
	_ = tools.Ruling{"kind", "candidate", "other", nil, "why", "user", "email", "at"}
	_ = tools.Detail{tools.Candidate{}, nil, nil, nil, nil, nil, nil, nil, nil, nil}
	_ = tools.Decision{tools.Ruling{}, nil}
	_ = tools.EnrichArgs{"kind", "prospect_list", "candidate", false}
	_ = tools.AddPageArgs{"candidate", "url"}
	_ = tools.EnrichmentResult{"kind", "source", "inputs", nil, "state", "url", "title", 200, nil, "error", "domain", "place", "directions", .2, "at"}
	_ = tools.NoArgs{}
	_ = tools.CandidateArgs{"id"}
	_ = tools.PassArgs{"id"}
	_ = tools.DecideArgs{"kind", "why", "candidate", "other", []string{}}
	_ = tools.CandidatesArgs{"name", "zip", "phone", nil, nil}
	_ = tools.PassesArgs{nil, nil}
	_ = tools.ProspectListsArgs{nil, nil}
	_ = tools.ProspectListArgs{"id"}
	_ = tools.ReadProspectListArgs{"id", false}
	_ = tools.FilterArgs{nil, nil, false, nil, false, nil, false, "", false, nil, false, nil, nil, false, nil, "", false, nil, false}
	_ = tools.OrderArgs{"field", "direction"}
	_ = tools.CreateProspectListArgs{"name", nil, nil, nil}
	_ = tools.EditProspectListArgs{"id", nil, nil, nil, nil}
	_ = tools.RejectArgs{"prospect_list", "candidate", "why"}
	_ = tools.RulingArgs{"candidate", "why"}
}
func objectKeys(t *testing.T, b []byte) []string {
	t.Helper()
	if len(b) == 0 {
		return []string{}
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	if _, err := d.Token(); err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for d.More() {
		k, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, k.(string))
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// R-XFGZ-IYVN R-VWLI-BAIE R-MGDJ-LAMN R-SX62-ODQC R-AKDK-IR5F R-QEP5-ZOVY
// R-YNDH-GZXV R-SUQ9-WU8Y R-XIDP-GOVR R-SYDZ-25H1
// R-6131-Z710 R-XFGZ-IYVN R-VWLI-BAIE R-MGDJ-LAMN R-SX62-ODQC R-AKDK-IR5F R-QEP5-ZOVY
func TestAnswerJSONShapes(t *testing.T) {
	tests := []struct {
		v      any
		keys   string
		arrays string
		object string
	}{
		{tools.BasisCounts{}, "records partial enriched", "", ""},
		{tools.Changes{}, "since definition_changed joined left", "definition_changed joined left", ""},
		{tools.Joined{}, "candidate name basis why", "", ""},
		{tools.Left{}, "candidate name basis_then why", "", ""},
		{tools.Kinds{}, "present missing", "present missing", ""},
		{tools.EnrichmentResult{}, "kind source inputs cost at", "", ""},
		{tools.Person{}, "name role", "", ""}, {tools.Evidence{}, "name source weight description", "", ""}, {tools.Category{}, "name source", "", ""}, {tools.Candidate{}, "id status name names street city state zip county phone email people class cargo created updated", "names people cargo", ""},
		{tools.Member{}, "candidate basis categories classified_by fleet_verdict website website_source site_text brief minutes_source excluded", "categories", ""}, {tools.ProspectListList{}, "prospect_lists", "prospect_lists", ""}, {tools.ProspectList{}, "id name size filters order created_by created_email created changed_by changed_email changed", "order", "filters"}, {tools.Reject{}, "kind prospect_list candidate", "", ""}, {tools.ProspectListRead{}, "prospect_list matching classified by_basis changes members rejects", "members rejects", ""}, {tools.CandidateList{}, "candidates", "candidates", ""}, {tools.PassList{}, "passes", "passes", ""},
		{tools.Link{}, "scraper source_id run digest name names street city state zip county phone email people class cargo fleet_verdict name_key phone_key domain_key reason confidence first_seen last_seen gone", "names people cargo", ""},
		{tools.Estimate{}, "source low confidence facts formula", "", "facts"}, {tools.Judgments{}, "operating_evidence excluded rules calculated", "operating_evidence", ""},
		{tools.Ruling{}, "kind candidate keys why user email at", "keys", ""}, {tools.Detail{}, "candidate estimates links rulings pages enrichment_results rejects campaigns", "estimates links rulings pages enrichment_results rejects campaigns", ""}, {tools.Decision{}, "ruling candidates", "candidates", ""},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.v)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(objectKeys(t, b), strings.Fields(tt.keys)) {
			t.Fatalf("%T: %s", tt.v, b)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(b, &obj); err != nil {
			t.Fatal(err)
		}
		for _, k := range strings.Fields(tt.arrays) {
			if string(obj[k]) != "[]" {
				t.Fatalf("%T %s: %s", tt.v, k, b)
			}
		}
		if tt.object != "" && string(obj[tt.object]) != "{}" {
			t.Fatal(string(b))
		}
	}
	one := int64(1)
	confidence := .7123456789
	continuation := "opaque-continuation"
	populated := []struct {
		v    any
		keys string
	}{
		{tools.Changes{Since: &continuation}, "since definition_changed joined left"},
		{tools.Left{BasisNow: "now", Filter: "filter"}, "candidate name basis_then basis_now why filter"},
		{tools.EnrichmentResult{URL: "chosen-url", Title: "chosen-title", StatusCode: 200, Error: "chosen-error"}, "kind source inputs url title status_code error cost at"},
		{tools.Candidate{MergedInto: "survivor"}, "id status merged_into name names street city state zip county phone email people class cargo created updated"},
		{tools.Member{OperatingConfidence: &confidence, RevenueLow: &one, RevenueHigh: &one, RevenueConfidence: &confidence, Website: sources.WebsiteHTTPS, WebsiteURL: "https://chosen.test", Minutes: &one, GovernmentMatch: "chosen", KnownSince: "known", NewestFiling: "newest"}, "candidate basis operating_confidence revenue_low revenue_high revenue_confidence categories classified_by fleet_verdict website website_url website_source site_text brief minutes minutes_source government_match known_since newest_filing excluded"},
		{tools.ProspectListList{Next: &continuation}, "prospect_lists next"}, {tools.CandidateList{Next: &continuation}, "candidates next"}, {tools.PassList{Next: &continuation}, "passes next"},
		{tools.Link{Filed: "2026-01-01"}, "scraper source_id run digest name names street city state zip county phone email people class cargo filed fleet_verdict name_key phone_key domain_key reason confidence first_seen last_seen gone"},
		{tools.Estimate{High: &one, Filed: "2026-01-01"}, "source low high confidence facts filed formula"},
		{tools.Judgments{OperatingConfidence: &confidence, RevenueLow: &one, RevenueHigh: &one, RevenueConfidence: &confidence}, "operating_confidence operating_evidence revenue_low revenue_high revenue_confidence excluded rules calculated"},
		{tools.Reject{Kind: "reject", ProspectList: "prospect_list", ProspectListName: "name", Candidate: "candidate", Why: "why", User: "user", Email: "email", At: "at"}, "kind prospect_list prospect_list_name candidate why user email at"},
		{tools.Ruling{Other: "other"}, "kind candidate other keys why user email at"}, {tools.Detail{Judgments: &tools.Judgments{}}, "candidate judgments estimates links rulings pages enrichment_results rejects campaigns"},
	}
	for _, tt := range populated {
		b, err := json.Marshal(tt.v)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(objectKeys(t, b), strings.Fields(tt.keys)) {
			t.Fatalf("%T: %s", tt.v, b)
		}
	}
}

// R-6C25-F4P9 R-0PR8-MPT0 R-N8F8-E0OM
func TestListsPaginationAndFilters(t *testing.T) {
	f := newFixture(t)
	success(t, f.call("candidates", tools.CandidatesArgs{}), tools.CandidateList{})
	f.seed(30)
	checkDefaultPage(t, f, "candidates")
	for _, args := range []tools.CandidatesArgs{{}, {Name: "TRUCKING"}, {Name: "aLiAs 07"}, {Name: "absent-name"}, {Zip: "bogus"}, {Phone: "5551234567"}, {Phone: "bad"}} {
		rows, err := f.st.ListCandidates(t.Context(), store.CandidateFilter{Name: args.Name, Zip: args.Zip, Phone: args.Phone}, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if args.Name == "aLiAs 07" && (len(rows) != 1 || rows[0].Name != "Business 07") {
			t.Fatal("selective alias fixture", rows)
		}
		for _, limit := range []int{1, 7, 25, 30, 100} {
			args.Limit = &limit
			args.Page = nil
			offset := 0
			for {
				end := min(offset+limit, len(rows))
				want := tools.CandidateList{}
				for _, c := range rows[offset:end] {
					want.Candidates = append(want.Candidates, candidateOf(c))
				}
				if end < len(rows) {
					last := rows[end-1]
					token := paging.Token{Tool: "candidates"}.Encode(stamp(last.Created), last.ID)
					want.Next = &token
				}
				success(t, f.call("candidates", args), want)
				if want.Next == nil {
					break
				}
				args.Page = want.Next
				offset = end
			}
		}
	}
}

func decodeList[T any](t *testing.T, r mcp.Result) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(decode(t, r).Structured, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

var _ io.Reader = (*random)(nil)

// R-R96N-JYFB R-QQW5-TEAW R-27ET-GMFK R-TE8O-1642 R-WX6H-SER8
func TestDetailAndRulings(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	id := ids[0]
	c, err := f.st.GetCandidate(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := f.st.Links(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	es, err := f.st.Estimates(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.st.GetJudgments(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	expected := tools.Detail{Candidate: candidateOf(c), Enrichments: &tools.Enrichments{Basis: store.BasisRecords, Kinds: tools.Kinds{Missing: []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindDriveRoute}}}}
	for _, l := range ls {
		r := l.Reading
		expected.Links = append(expected.Links, tools.Link{Scraper: l.Scraper, SourceID: l.SourceID, Run: l.Run, Digest: l.Digest, Name: r.Name, Names: r.Names, Street: r.Street, City: r.City, State: r.State, Zip: r.Zip, County: r.County, Phone: r.Phone, Email: r.Email, People: []tools.Person{{Name: r.People[0].Name, Role: r.People[0].Role}}, Class: r.Class, Cargo: r.Cargo, Filed: r.Filed.UTC().Format("2006-01-02"), FleetVerdict: r.FleetVerdict, NameKey: r.NameKey, PhoneKey: r.PhoneKey, DomainKey: r.DomainKey, Reason: l.Reason, Confidence: l.Confidence, FirstSeen: stamp(l.FirstSeen), LastSeen: stamp(l.LastSeen), Gone: l.Gone})
	}
	for _, e := range es {
		expected.Estimates = append(expected.Estimates, tools.Estimate{Source: e.Source, Low: e.Low, High: e.High, Confidence: e.Confidence, Facts: e.Facts, Filed: e.Filed.UTC().Format("2006-01-02"), Formula: e.Formula})
	}
	expected.Judgments = judgmentsExpected(a)
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: id}), expected)
	f.now = f.now.AddDate(6, 0, 0)
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: id}), expected)
	f.now = f.now.AddDate(-6, 0, 0)
	for _, kind := range []string{store.KindExclude, store.KindInclude} {
		why := "  owner's reason  "
		result := decodeList[tools.Decision](t, f.call(kind, tools.RulingArgs{Candidate: id, Why: why}))
		rs, err := f.st.Rulings(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := f.st.GetCandidate(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		r := rs[0]
		want := tools.Decision{Ruling: tools.Ruling{Kind: kind, Candidate: id, Why: why, User: caller.UserID, Email: caller.Email, At: stamp(r.At)}, Candidates: []tools.Candidate{candidateOf(updated)}}
		rb, _ := json.Marshal(result)
		wb, _ := json.Marshal(want)
		if string(rb) != string(wb) {
			t.Fatalf("ruling got %+v want %+v", result, want)
		}
		success(t, f.call(kind, tools.RulingArgs{Candidate: id, Why: why}), tools.Decision{Ruling: tools.Ruling{Kind: kind, Candidate: id, Why: why, User: caller.UserID, Email: caller.Email, At: stamp(f.now)}, Candidates: []tools.Candidate{candidateOf(updated)}})
	}
	merged := decodeList[tools.Decision](t, f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: id, Other: ids[1], Why: "same owner"}))
	if len(merged.Candidates) != 2 {
		t.Fatal(merged)
	}
	id = ids[1]
	c, err = f.st.GetCandidate(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := f.st.Rulings(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	want := tools.Detail{Candidate: candidateOf(c)}
	for _, r := range rs {
		want.Rulings = append(want.Rulings, tools.Ruling{Kind: r.Kind, Candidate: r.Candidate, Other: r.Other, Keys: r.Keys, Why: r.Why, User: r.UserID, Email: r.UserEmail, At: stamp(r.At)})
	}
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: id}), want)
	if want.Candidate.Name == "" || want.Candidate.MergedInto != ids[0] {
		t.Fatal("merged display record lost")
	}
}

// R-6LTC-HAMT R-6QOY-0DLL
func TestRefusalsAndPrecedence(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	unknown := store.CandidateIDPrefix + strings.Repeat("0", 16)
	for _, args := range []tools.DecideArgs{{Kind: "bad", Candidate: "bad", Why: "reason"}, {Kind: store.KindExclude, Candidate: "bad", Why: " \t\n"}, {Kind: store.KindMerge, Candidate: "bad", Why: "reason"}, {Kind: store.KindSplit, Candidate: "bad", Why: "reason"}} {
		refusal(t, f.call("decide", args), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	for _, args := range []tools.DecideArgs{{Kind: store.KindNotSame, Candidate: "bad", Other: ids[0], Why: "reason"}, {Kind: store.KindMerge, Candidate: ids[0], Other: "bad", Why: "reason"}} {
		refusal(t, f.call("decide", args), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	refusal(t, f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: unknown, Other: unknown, Why: "reason"}), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[0], Other: ids[0], Why: "reason"}), passes.RefusalInvalid, tools.RefuseInvalid)
	refusal(t, f.call("decide", tools.DecideArgs{Kind: store.KindSplit, Candidate: ids[0], Links: []string{"fmcsa:row0"}, Why: "reason"}), passes.RefusalInvalid, tools.RefuseInvalid)
	for _, id := range []string{"bad", unknown} {
		refusal(t, f.call("candidate", tools.CandidateArgs{Candidate: id}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	for _, id := range []string{"bad", store.PassIDPrefix + strings.Repeat("0", 16)} {
		refusal(t, f.call("pass", tools.PassArgs{Pass: id}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	success(t, f.call("pass", tools.PassArgs{Pass: p.ID}), passes.RecordOf(p))
	if _, err := f.st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.PrunePasses(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	f.d.SetFailing(true)
	defer f.d.SetFailing(false)
	for _, call := range []struct {
		name string
		args any
	}{{"prospect_lists", tools.ProspectListsArgs{}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: ids[0]}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}}, {"include", tools.RulingArgs{Candidate: ids[0], Why: "reason"}}, {"ingest", tools.NoArgs{}}} {
		refusal(t, f.call(call.name, call.args), passes.RefusalUnavailable, tools.RefuseUnavailable)
	}
	refusal(t, f.call("candidate", tools.CandidateArgs{Candidate: "private-invalid-id"}), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("pass", tools.PassArgs{Pass: "private-invalid-id"}), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("decide", tools.DecideArgs{Kind: "private-kind", Candidate: ids[0], Why: "private-reason"}), passes.RefusalInvalid, tools.RefuseInvalid)
	refusal(t, f.call("include", tools.RulingArgs{Candidate: "bad", Why: "reason"}), passes.RefusalNotFound, tools.RefuseNotFound)
}

// R-XD16-RFE9
func TestPassPages(t *testing.T) {
	f := newFixture(t)
	success(t, f.call("passes", tools.PassesArgs{}), tools.PassList{})
	for range 26 {
		p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: caller.UserID, UserEmail: caller.Email})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.FinishPass(t.Context(), p.ID, store.Counts{Rows: 2, Created: 1}); err != nil {
			t.Fatal(err)
		}
	}
	checkDefaultPage(t, f, "passes")
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 7, 25, 26, 100} {
		var page *string
		offset := 0
		for {
			end := min(offset+limit, len(ps))
			want := tools.PassList{}
			for _, p := range ps[offset:end] {
				want.Passes = append(want.Passes, passes.RecordOf(p))
			}
			if end < len(ps) {
				last := ps[end-1]
				token := paging.Token{Tool: "passes"}.Encode(stamp(last.Started), last.ID)
				want.Next = &token
			}
			success(t, f.call("passes", tools.PassesArgs{Page: page, Limit: &limit}), want)
			if want.Next == nil {
				break
			}
			page = want.Next
			offset = end
		}
	}
}

// R-FKII-PSLY R-K0EN-M29C
func TestSharedReadAnswersAndNoMutation(t *testing.T) {
	var websiteRequests, siblingRequests, promptRequests atomic.Int64
	kWebsite := pageChecker(&websiteRequests)
	pc := promptClient(t, false, &promptRequests)
	f := fixtureWithPrompts(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		siblingRequests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}), kWebsite, pc)
	ids := f.seed(1)
	id := ids[0]
	k := f.create("shared")
	setSite(t, f, id, "https://acme.test/")
	if _, err := f.st.AddPage(t.Context(), store.Page{Candidate: id, URL: "https://acme.test/kept", Text: "kept text", Size: 9, UserID: caller.UserID}); err != nil {
		t.Fatal(err)
	}
	f.call("reject", tools.RejectArgs{ProspectList: k.ID, Candidate: id, Why: "shared reject"})
	f.call("include", tools.RulingArgs{Candidate: id, Why: "owner ruling"})
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	p, err = f.st.FinishPass(t.Context(), p.ID, store.Counts{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func(ignoreOpen bool) []byte {
		t.Helper()
		c, err := f.st.GetCandidate(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		ls, err := f.st.Links(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		es, err := f.st.Estimates(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		a, err := f.st.GetJudgments(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		rs, err := f.st.Rulings(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		ps, err := f.st.ListPasses(t.Context(), "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		pass, err := f.st.GetPass(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		xs, err := f.st.Rejects(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		ks, err := f.st.ListProspectLists(t.Context(), "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		cr, err := f.st.ReadProspectList(t.Context(), k.ID)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := f.st.Pages(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		count, err := f.st.CountPages(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		opens, err := f.st.Opens(t.Context(), k.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ignoreOpen {
			cr.Changes = store.Changes{}
			opens = nil
		}
		b, err := json.Marshal([]any{c, ls, es, a, rs, xs, pages, count, ps, pass, ks, cr, opens})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	before := string(snapshot(false))
	stable := string(snapshot(true))
	for _, call := range []struct {
		name string
		args any
	}{{"prospect_lists", tools.ProspectListsArgs{}}, {"prospect_list", tools.ReadProspectListArgs{ProspectList: k.ID, Peek: true}}, {"prospect_list", tools.ReadProspectListArgs{ProspectList: "bad", Peek: true}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: id}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}}, {"candidate", tools.CandidateArgs{Candidate: "bad"}}, {"pass", tools.PassArgs{Pass: "bad"}}} {
		a, b := decode(t, f.callAs(caller, call.name, call.args)), decode(t, f.callAs(other, call.name, call.args))
		if !reflect.DeepEqual(a, b) {
			t.Fatal("caller changes product", call.name)
		}
	}
	f.d.SetFailing(true)
	for _, call := range []struct {
		name string
		args any
	}{{"prospect_lists", tools.ProspectListsArgs{}}, {"prospect_list", tools.ReadProspectListArgs{ProspectList: k.ID, Peek: true}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: id}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}}} {
		refusal(t, f.call(call.name, call.args), passes.RefusalUnavailable, tools.RefuseUnavailable)
	}
	f.d.SetFailing(false)
	if string(snapshot(false)) != before {
		t.Fatal("read changed records")
	}
	// Recording an open changes only that list's open and the ensuing changes baseline.
	passive := identity.Caller{UserID: "passive-reader", Email: "passive-reader@fixture.test", RequestID: "passive-request"}
	peek := decode(t, f.callAs(other, "prospect_list", tools.ReadProspectListArgs{k.ID, true}))
	opened := decode(t, f.callAs(passive, "prospect_list", tools.ReadProspectListArgs{k.ID, false}))
	if !reflect.DeepEqual(peek, opened) {
		t.Fatal("open changed its answer", peek, opened)
	}
	if string(snapshot(true)) != stable {
		t.Fatal("open changed non-open records")
	}
	opens, err := f.st.Opens(t.Context(), k.ID)
	if err != nil || len(opens) != 1 || opens[0].UserID != passive.UserID || opens[0].UserEmail != passive.Email || !opens[0].At.Equal(f.now.UTC().Truncate(time.Second)) {
		t.Fatal(opens, err)
	}
	after := decode(t, f.callAs(other, "prospect_list", tools.ReadProspectListArgs{k.ID, true}))
	read := decodeList[tools.ProspectListRead](t, f.callAs(other, "prospect_list", tools.ReadProspectListArgs{k.ID, true}))
	if read.Changes.Since == nil || *read.Changes.Since != stamp(f.now) || len(read.Changes.DefinitionChanged) != 0 || len(read.Changes.Joined) != 0 || len(read.Changes.Left) != 0 || strings.Contains(string(after.Structured), passive.UserID) || strings.Contains(string(after.Structured), passive.Email) {
		t.Fatal(after)
	}
	for _, args := range []tools.ReadProspectListArgs{{"bad", false}, {store.ProspectListIDPrefix + strings.Repeat("0", 16), false}} {
		refusal(t, f.call("prospect_list", args), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	f.d.SetFailing(true)
	refusal(t, f.call("prospect_list", tools.ReadProspectListArgs{k.ID, false}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	f.d.SetFailing(false)
	again, err := f.st.Opens(t.Context(), k.ID)
	if err != nil || !reflect.DeepEqual(again, opens) {
		t.Fatal(again, opens, err)
	}

	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 1 {
		t.Fatal("unexpected pass", ps, err)
	}
	if websiteRequests.Load() != 0 || siblingRequests.Load() != 0 || promptRequests.Load() != 0 {
		t.Fatal("read tool contacted a remote", websiteRequests.Load(), siblingRequests.Load(), promptRequests.Load())
	}

}

// R-SV30-JZ1Y
func TestIngestAndNoSiblingReads(t *testing.T) {
	entered := make(chan identity.Caller, 1)
	release := make(chan struct{})
	var reads atomic.Int64
	f := fixtureWithScrapes(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		entered <- identity.Caller{UserID: r.Header.Get("X-User-Id"), Email: r.Header.Get("X-User-Email"), RequestID: r.Header.Get("X-Request-Id")}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	ids := f.seed(1)
	for _, call := range []struct {
		name string
		args any
	}{{"prospect_lists", tools.ProspectListsArgs{}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: ids[0]}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: "bad"}}, {"include", tools.RulingArgs{Candidate: ids[0], Why: "reason"}}} {
		r := f.call(call.name, call.args)
		if call.name != "pass" && r.IsError() {
			t.Fatal(decode(t, r))
		}
	}
	if reads.Load() != 0 {
		t.Fatal("read or ruling contacted scrapes")
	}
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal("read or ruling started pass")
	}
	f.d.SetFailing(true)
	unavailableResult := f.call("ingest", tools.NoArgs{})
	f.d.SetFailing(false)
	refusal(t, unavailableResult, passes.RefusalUnavailable, tools.RefuseUnavailable)
	if reads.Load() != 0 {
		t.Fatal("failing ingest contacted scrapes")
	}
	result := f.call("ingest", tools.NoArgs{})
	record := decodeList[passes.Record](t, result)
	p, err := f.st.GetPass(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	success(t, result, passes.RecordOf(p))
	if p.Status != store.StatusRunning || p.UserID != caller.UserID || p.UserEmail != caller.Email || p.RequestID != caller.RequestID {
		t.Fatal("ingest caller", p)
	}
	got := <-entered
	if got != caller {
		t.Fatal("sibling caller", got)
	}

	refusal(t, f.call("ingest", tools.NoArgs{}), passes.RefusalBusy, tools.RefuseBusy)
	close(release)
	f.eng.Drain(context.Background())
	refusal(t, f.call("ingest", tools.NoArgs{}), passes.RefusalStopping, tools.RefuseStopping)
	unavailable := newFixture(t)
	refusal(t, unavailable.call("ingest", tools.NoArgs{}), passes.RefusalUnavailable, tools.RefuseUnavailable)
}

// R-PTYV-HLA5
func TestSplitAndNotSame(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	success(t, f.call("decide", tools.DecideArgs{Kind: store.KindNotSame, Candidate: ids[0], Other: ids[1], Why: "distinct"}), tools.Decision{Ruling: tools.Ruling{Kind: store.KindNotSame, Candidate: ids[0], Other: ids[1], Why: "distinct", User: caller.UserID, Email: caller.Email, At: stamp(f.now)}, Candidates: func() []tools.Candidate {
		out := []tools.Candidate{}
		for _, id := range ids {
			c, err := f.st.GetCandidate(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, candidateOf(c))
		}
		return out
	}()})
	// Bring two links together, then move a proper subset to the new candidate.
	result := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same"})
	if result.IsError() {
		t.Fatal(decode(t, result))
	}
	split := f.callAs(other, "decide", tools.DecideArgs{Kind: store.KindSplit, Candidate: ids[0], Links: []string{"fmcsa:row1"}, Why: " separate ", Other: "ignored"})
	got := decodeList[tools.Decision](t, split)
	if len(got.Candidates) != 2 {
		t.Fatal(got)
	}
	newID := got.Candidates[1].ID
	if !store.ValidCandidateID(newID) || newID == ids[0] || newID == ids[1] {
		t.Fatal("missing new candidate")
	}
	want := tools.Decision{Ruling: tools.Ruling{Kind: store.KindSplit, Candidate: ids[0], Other: newID, Keys: []string{"fmcsa:row1"}, Why: " separate ", User: other.UserID, Email: other.Email, At: stamp(f.now)}}
	for _, id := range []string{ids[0], newID} {
		c, err := f.st.GetCandidate(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		want.Candidates = append(want.Candidates, candidateOf(c))
	}
	success(t, split, want)
	detail := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: newID}))
	if len(detail.Links) != 1 || detail.Links[0].SourceID != "row1" || len(detail.Rulings) == 0 || detail.Rulings[0].User != other.UserID {
		t.Fatal(detail)
	}
}

// R-6QOY-0DLL

func judgmentsExpected(a store.Judgments) *tools.Judgments {
	v := &tools.Judgments{OperatingConfidence: a.Operating, OperatingEvidence: []tools.Evidence{}, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, HeadcountLow: a.HeadcountLow, HeadcountHigh: a.HeadcountHigh, HeadcountBasis: a.HeadcountBasis, Excluded: a.Excluded, Rules: a.Rules, Calculated: stamp(a.Calculated)}
	for _, e := range a.Evidence {
		v.OperatingEvidence = append(v.OperatingEvidence, tools.Evidence{Name: e.Name, Source: e.Source, Weight: e.Weight, Description: e.Description})
	}
	return v
}

// R-SUQ9-WU8Y R-XIDP-GOVR R-SYDZ-25H1 R-SZLV-FX7Q
func TestBasisChangeAndKindValueEncoding(t *testing.T) {
	at := "chosen-at"
	for _, tc := range []struct {
		value any
		want  string
	}{
		{tools.BasisCounts{1, 2, 3}, `{"records":1,"partial":2,"enriched":3}`},
		{tools.Changes{}, `{"since":null,"definition_changed":[],"joined":[],"left":[]}`},
		{tools.Changes{&at, []string{"size", "filters"}, []tools.Joined{{"joined-id", "joined-name", "joined-basis", "joined-why"}}, []tools.Left{{"left-id", "left-name", "then", "now", "left-why", "filter"}}}, `{"since":"chosen-at","definition_changed":["size","filters"],"joined":[{"candidate":"joined-id","name":"joined-name","basis":"joined-basis","why":"joined-why"}],"left":[{"candidate":"left-id","name":"left-name","basis_then":"then","basis_now":"now","why":"left-why","filter":"filter"}]}`},
		{tools.Left{"left", "name", "then", "", "why", ""}, `{"candidate":"left","name":"name","basis_then":"then","why":"why"}`},
		{tools.Kinds{}, `{"present":[],"missing":[]}`},
		{tools.Kinds{[]string{"a", "b"}, []string{"c", "d"}}, `{"present":["a","b"],"missing":["c","d"]}`},
		{tools.Enrichments{"chosen-basis", tools.Kinds{[]string{"kind"}, nil}, nil, nil}, `{"basis":"chosen-basis","kinds":{"present":["kind"],"missing":[]}}`},
	} {
		got, err := json.Marshal(tc.value)
		if err != nil || string(got) != tc.want {
			t.Fatalf("%T: %s, %v; want %s", tc.value, got, err, tc.want)
		}
	}
}

// R-FKII-PSLY
func TestIngestProductsAreSharedAcrossCallers(t *testing.T) {
	f := fixtureWithScrapes(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/scrapers/fmcsa/current", "/runs/shared-run/record":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "shared-run", "scraper": "fmcsa", "status": "done", "started": "2000-01-01T00:00:00Z", "rows": 1, "new": 0, "changed": 0, "unchanged": 0, "gone": 0})
		case "/runs/shared-run/rows":
			_ = json.NewEncoder(w).Encode(map[string]any{"scraper": "fmcsa", "run": "shared-run", "source_id": "shared-source", "digest": "shared-digest", "scraped": "2000-01-01T00:00:00Z", "name": "Shared business", "names": []string{}, "people": []any{}, "facts": map[string]any{}, "raw": map[string]any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	result := f.call("ingest", tools.IngestArgs{Scraper: "fmcsa"})
	if result.IsError() {
		t.Fatal(decode(t, result))
	}
	pass := decodeList[passes.Record](t, result)
	if p := f.waitEnrich(pass.ID); p.Status != store.StatusDone {
		t.Fatal(p)
	}
	candidates := decodeList[tools.CandidateList](t, f.call("candidates", tools.CandidatesArgs{}))
	if len(candidates.Candidates) != 1 {
		t.Fatal(candidates)
	}
	id := candidates.Candidates[0].ID
	list := f.create("Shared ingest list")
	if r := f.call("include", tools.RulingArgs{Candidate: id, Why: "Owner ruling"}); r.IsError() {
		t.Fatal(decode(t, r))
	}
	if r := f.call("reject", tools.RejectArgs{ProspectList: list.ID, Candidate: id, Why: "Owner reject"}); r.IsError() {
		t.Fatal(decode(t, r))
	}
	for _, call := range []struct {
		name string
		args any
	}{
		{"prospect_lists", tools.ProspectListsArgs{}}, {"prospect_list", tools.ReadProspectListArgs{list.ID, true}},
		{"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{id}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{pass.ID}},
	} {
		a, b := decode(t, f.callAs(caller, call.name, call.args)), decode(t, f.callAs(other, call.name, call.args))
		if !reflect.DeepEqual(a, b) {
			t.Fatal(call.name, a, b)
		}
	}
}
