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
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
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
	st     *store.Store
	eng    *passes.Engine
	client *mcp.Client
	now    time.Time
}

func newFixture(t *testing.T) *fixture                         { return fixtureWithScrapes(t, nil) }
func fixtureWithScrapes(t *testing.T, h http.Handler) *fixture { return fixtureWithWebsite(t, h, nil) }
func fixtureWithWebsite(t *testing.T, h http.Handler, k *website.Checker) *fixture {
	t.Helper()
	f := &fixture{t: t, now: time.Date(2026, 4, 5, 0, 7, 8, 0, time.FixedZone("fixture", 2*60*60))}
	now := func() time.Time { return f.now }
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "db"), Migrations: candidates.Migrations(), Now: now, Service: "candidates"})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(d, store.Config{Now: now, Rand: &random{}})
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
	f.eng = passes.New(passes.Config{Website: k, Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: sc, Telemetry: w, Events: em})
	t.Cleanup(func() { f.eng.Drain(context.Background()) })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: w})
	tools.Register(srv, tools.Config{f.eng, f.st})
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

// R-5795-SH3G     R-5LVY-DPZS
// R-VI0C-NJWH R-VKG5-F3DV R-VLO1-SV4K R-RDYP-P5JC
func TestCatalogAndSchemas(t *testing.T) {
	f := newFixture(t)
	want := []tools.Entry{{"cohorts", tools.DescribeCohorts}, {"cohort", tools.DescribeCohort}, {"candidates", tools.DescribeCandidates}, {"candidate", tools.DescribeCandidate}, {"passes", tools.DescribePasses}, {"pass", tools.DescribePass}, {"rebuild", tools.DescribeRebuild}, {"enrich", tools.DescribeEnrich}, {"decide", tools.DescribeDecide}, {"exclude", tools.DescribeExclude}, {"include", tools.DescribeInclude}, {"create_cohort", tools.DescribeCreateCohort}, {"edit_cohort", tools.DescribeEditCohort}, {"delete_cohort", tools.DescribeDeleteCohort}, {"reject", tools.DescribeReject}, {"unreject", tools.DescribeUnreject}}
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
	properties := [][]string{{"page", "limit"}, {"cohort"}, {"name", "zip", "phone", "page", "limit"}, {"candidate"}, {"page", "limit"}, {"pass"}, {}, {"kind", "older_than_days"}, {"kind", "why", "candidate", "other", "links"}, {"candidate", "why"}, {"candidate", "why"}, {"name", "filters", "order", "size"}, {"cohort", "name", "filters", "order", "size"}, {"cohort"}, {"cohort", "candidate", "why"}, {"cohort", "candidate", "why"}}
	required := [][]string{nil, {"cohort"}, nil, {"candidate"}, nil, {"pass"}, nil, nil, {"kind", "why", "candidate"}, {"candidate", "why"}, {"candidate", "why"}, {"name"}, {"cohort"}, {"cohort"}, {"cohort", "candidate", "why"}, {"cohort", "candidate", "why"}}
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
		if info.Name == "delete_cohort" {
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
	for _, d := range []string{tools.RefuseNotFound, tools.RefuseInvalid, tools.RefuseBusy, tools.RefuseStopping, tools.RefuseUnavailable} {
		if d == "" || strings.Contains(d, "\n") || seen[d] {
			t.Fatal(d)
		}
		seen[d] = true
	}
	_ = tools.Cohort{"id", "name", 1, store.CohortFilters{}, []store.CohortOrder{}, "creator", "email", "created", "changer", "email", "changed"}
	_ = tools.Member{tools.Candidate{}, nil, nil, nil, nil, nil, "", "", "", nil, "", "", "", false}
	_ = tools.Reject{"kind", "cohort", "name", "candidate", "why", "user", "email", "at"}
	_ = tools.CohortRead{tools.Cohort{}, 0, nil, nil}
	_ = tools.Assessment{nil, nil, nil, nil, nil, "", "", nil, "", "", nil, nil, "", "", nil, false, 0, ""}
	_ = tools.Estimate{"source", 1, nil, .5, nil, "", 1}
	_ = tools.Ruling{"kind", "candidate", "other", nil, "why", "user", "email", "at"}
	_ = tools.Detail{tools.Candidate{}, nil, nil, nil, nil, nil, nil}
	_ = tools.Decision{tools.Ruling{}, nil}
	_ = tools.EnrichArgs{"kind", nil}
	_ = tools.Enrichment{"kind", "source", "inputs", nil, "state", "url", "title", 200, nil, "error", "domain", .2, "at"}
	_ = tools.NoArgs{}
	_ = tools.CandidateArgs{"id"}
	_ = tools.PassArgs{"id"}
	_ = tools.DecideArgs{"kind", "why", "candidate", "other", []string{}}
	_ = tools.CandidatesArgs{"name", "zip", "phone", nil, nil}
	_ = tools.PassesArgs{nil, nil}
	_ = tools.CohortsArgs{nil, nil}
	_ = tools.CohortArgs{"id"}
	_ = tools.FilterArgs{nil, nil, false, nil, false, nil, false, "", false, nil, false, nil, nil, false, "", false, false}
	_ = tools.OrderArgs{"field", "direction"}
	_ = tools.CreateCohortArgs{"name", nil, nil, nil}
	_ = tools.EditCohortArgs{"id", nil, nil, nil, nil}
	_ = tools.RejectArgs{"cohort", "candidate", "why"}
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

// R-AEA2-LWFY R-AFHY-ZO6N R-VD4R-4GXP R-AHXR-R7O1 R-AJ5O-4ZEQ R-AKDK-IR5F R-VECN-I8OE R-VGSG-9S5S
// R-RIUB-88I4
// R-6131-Z710 R-AEA2-LWFY R-AFHY-ZO6N R-VD4R-4GXP R-AHXR-R7O1 R-AJ5O-4ZEQ R-AKDK-IR5F R-VECN-I8OE R-VGSG-9S5S
func TestAnswerJSONShapes(t *testing.T) {
	tests := []struct {
		v      any
		keys   string
		arrays string
		object string
	}{
		{tools.Enrichment{}, "kind source inputs cost at", "", ""},
		{tools.Person{}, "name role", "", ""}, {tools.Evidence{}, "name source weight description", "", ""}, {tools.Category{}, "name source", "", ""}, {tools.Candidate{}, "id status name names street city state zip county phone email people class cargo created updated", "names people cargo", ""},
		{tools.Member{}, "candidate categories fleet_verdict website excluded", "categories", ""}, {tools.CohortList{}, "cohorts", "cohorts", ""}, {tools.Cohort{}, "id name size filters order created_by created_email created changed_by changed_email changed", "order", "filters"}, {tools.Reject{}, "kind cohort candidate", "", ""}, {tools.CohortRead{}, "cohort matching members rejects", "members rejects", ""}, {tools.CandidateList{}, "candidates", "candidates", ""}, {tools.PassList{}, "passes", "passes", ""},
		{tools.Link{}, "scraper source_id run digest name names street city state zip county phone email people class cargo fleet_verdict name_key phone_key domain_key reason confidence first_seen last_seen gone", "names people cargo", ""},
		{tools.Estimate{}, "source low confidence facts formula", "", "facts"}, {tools.Assessment{}, "operating_evidence categories fleet_verdict website excluded rules calculated", "operating_evidence categories", ""},
		{tools.Ruling{}, "kind candidate keys why user email at", "keys", ""}, {tools.Detail{}, "candidate estimates links rulings enrichments rejects", "estimates links rulings enrichments rejects", ""}, {tools.Decision{}, "ruling candidates", "candidates", ""},
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
		{tools.Enrichment{URL: "chosen-url", Title: "chosen-title", StatusCode: 200, Error: "chosen-error"}, "kind source inputs url title status_code error cost at"},
		{tools.Candidate{MergedInto: "survivor"}, "id status merged_into name names street city state zip county phone email people class cargo created updated"},
		{tools.Member{OperatingConfidence: &confidence, RevenueLow: &one, RevenueHigh: &one, RevenueConfidence: &confidence, Website: sources.WebsiteHTTPS, WebsiteURL: "https://chosen.test", Minutes: &one, GovernmentMatch: "chosen", KnownSince: "known", NewestFiling: "newest"}, "candidate operating_confidence revenue_low revenue_high revenue_confidence categories fleet_verdict website website_url minutes government_match known_since newest_filing excluded"},
		{tools.CohortList{Next: &continuation}, "cohorts next"}, {tools.CandidateList{Next: &continuation}, "candidates next"}, {tools.PassList{Next: &continuation}, "passes next"},
		{tools.Link{Filed: "2026-01-01"}, "scraper source_id run digest name names street city state zip county phone email people class cargo filed fleet_verdict name_key phone_key domain_key reason confidence first_seen last_seen gone"},
		{tools.Estimate{High: &one, Filed: "2026-01-01"}, "source low high confidence facts filed formula"},
		{tools.Assessment{OperatingConfidence: &confidence, RevenueLow: &one, RevenueHigh: &one, RevenueConfidence: &confidence, KnownSince: "chosen-known", NewestFiling: "chosen-newest", GovernmentMatch: "chosen-government", PowerUnits: &one, Drivers: &one, Website: sources.WebsiteHTTPS, WebsiteURL: "https://chosen.test", Minutes: &one}, "operating_confidence operating_evidence revenue_low revenue_high revenue_confidence known_since newest_filing categories government_match fleet_verdict power_units drivers website website_url minutes excluded rules calculated"},
		{tools.Reject{Kind: "reject", Cohort: "cohort", CohortName: "name", Candidate: "candidate", Why: "why", User: "user", Email: "email", At: "at"}, "kind cohort cohort_name candidate why user email at"},
		{tools.Ruling{Other: "other"}, "kind candidate other keys why user email at"}, {tools.Detail{Assessment: &tools.Assessment{}}, "candidate assessment estimates links rulings enrichments rejects"},
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

// R-6C25-F4P9 R-B3VY-N30J R-B1G5-VJJ5
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

// R-AWKK-CGKD R-VJ89-1BN6 R-VRRJ-PPU1 R-RLA3-ZRZI R-BDN5-P8Y3
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
	a, err := f.st.GetAssessment(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	expected := tools.Detail{Candidate: candidateOf(c)}
	for _, l := range ls {
		r := l.Reading
		expected.Links = append(expected.Links, tools.Link{Scraper: l.Scraper, SourceID: l.SourceID, Run: l.Run, Digest: l.Digest, Name: r.Name, Names: r.Names, Street: r.Street, City: r.City, State: r.State, Zip: r.Zip, County: r.County, Phone: r.Phone, Email: r.Email, People: []tools.Person{{Name: r.People[0].Name, Role: r.People[0].Role}}, Class: r.Class, Cargo: r.Cargo, Filed: r.Filed.UTC().Format("2006-01-02"), FleetVerdict: r.FleetVerdict, NameKey: r.NameKey, PhoneKey: r.PhoneKey, DomainKey: r.DomainKey, Reason: l.Reason, Confidence: l.Confidence, FirstSeen: stamp(l.FirstSeen), LastSeen: stamp(l.LastSeen), Gone: l.Gone})
	}
	for _, e := range es {
		expected.Estimates = append(expected.Estimates, tools.Estimate{Source: e.Source, Low: e.Low, High: e.High, Confidence: e.Confidence, Facts: e.Facts, Filed: e.Filed.UTC().Format("2006-01-02"), Formula: e.Formula})
	}
	expected.Assessment = assessmentExpected(a)
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
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: caller.UserID})
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
	}{{"cohorts", tools.CohortsArgs{}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: ids[0]}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}}, {"include", tools.RulingArgs{Candidate: ids[0], Why: "reason"}}, {"rebuild", tools.NoArgs{}}} {
		refusal(t, f.call(call.name, call.args), passes.RefusalUnavailable, tools.RefuseUnavailable)
	}
	refusal(t, f.call("candidate", tools.CandidateArgs{Candidate: "private-invalid-id"}), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("pass", tools.PassArgs{Pass: "private-invalid-id"}), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("decide", tools.DecideArgs{Kind: "private-kind", Candidate: ids[0], Why: "private-reason"}), passes.RefusalInvalid, tools.RefuseInvalid)
	refusal(t, f.call("include", tools.RulingArgs{Candidate: "bad", Why: "reason"}), passes.RefusalNotFound, tools.RefuseNotFound)
}

// R-B7JN-SE8M
func TestPassPages(t *testing.T) {
	f := newFixture(t)
	success(t, f.call("passes", tools.PassesArgs{}), tools.PassList{})
	for range 26 {
		p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: caller.UserID, UserEmail: caller.Email})
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

// R-1UJY-J0A1 R-1VRU-WS0Q
func TestSharedReadAnswersAndNoMutation(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(1)
	id := ids[0]
	k := f.create("shared")
	f.call("reject", tools.RejectArgs{Cohort: k.ID, Candidate: id, Why: "shared reject"})
	f.call("include", tools.RulingArgs{Candidate: id, Why: "owner ruling"})
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	p, err = f.st.FinishPass(t.Context(), p.ID, store.Counts{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() []byte {
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
		a, err := f.st.GetAssessment(t.Context(), id)
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
		ks, err := f.st.ListCohorts(t.Context(), "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		cr, err := f.st.ReadCohort(t.Context(), k.ID)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal([]any{c, ls, es, a, rs, xs, ps, pass, ks, cr})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	before := string(snapshot())
	for _, call := range []struct {
		name string
		args any
	}{{"cohorts", tools.CohortsArgs{}}, {"cohort", tools.CohortArgs{Cohort: k.ID}}, {"cohort", tools.CohortArgs{Cohort: "bad"}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: id}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}}, {"candidate", tools.CandidateArgs{Candidate: "bad"}}, {"pass", tools.PassArgs{Pass: "bad"}}} {
		a, b := decode(t, f.callAs(caller, call.name, call.args)), decode(t, f.callAs(other, call.name, call.args))
		if !reflect.DeepEqual(a, b) {
			t.Fatal("caller changes product", call.name)
		}
	}
	f.d.SetFailing(true)
	for _, call := range []struct {
		name string
		args any
	}{{"cohorts", tools.CohortsArgs{}}, {"cohort", tools.CohortArgs{Cohort: k.ID}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: id}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}}} {
		refusal(t, f.call(call.name, call.args), passes.RefusalUnavailable, tools.RefuseUnavailable)
	}
	f.d.SetFailing(false)
	if string(snapshot()) != before {
		t.Fatal("read changed records")
	}
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 1 {
		t.Fatal("unexpected pass", ps, err)
	}
}

// R-8YDY-8SNX
func TestRebuildAndNoSiblingReads(t *testing.T) {
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
	}{{"cohorts", tools.CohortsArgs{}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: ids[0]}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: "bad"}}, {"include", tools.RulingArgs{Candidate: ids[0], Why: "reason"}}} {
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
	unavailableResult := f.call("rebuild", tools.NoArgs{})
	f.d.SetFailing(false)
	refusal(t, unavailableResult, passes.RefusalUnavailable, tools.RefuseUnavailable)
	if reads.Load() != 0 {
		t.Fatal("failing rebuild contacted scrapes")
	}
	result := f.call("rebuild", tools.NoArgs{})
	record := decodeList[passes.Record](t, result)
	p, err := f.st.GetPass(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	success(t, result, passes.RecordOf(p))
	if p.Status != store.StatusRunning || p.UserID != caller.UserID || p.UserEmail != caller.Email || p.RequestID != caller.RequestID {
		t.Fatal("rebuild caller", p)
	}
	got := <-entered
	if got != caller {
		t.Fatal("sibling caller", got)
	}

	refusal(t, f.call("rebuild", tools.NoArgs{}), passes.RefusalBusy, tools.RefuseBusy)
	close(release)
	f.eng.Drain(context.Background())
	refusal(t, f.call("rebuild", tools.NoArgs{}), passes.RefusalStopping, tools.RefuseStopping)
	unavailable := newFixture(t)
	refusal(t, unavailable.call("rebuild", tools.NoArgs{}), passes.RefusalUnavailable, tools.RefuseUnavailable)
}

// R-BB7C-XPGP
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

//   R-6QOY-0DLL

//

func assessmentExpected(a store.Assessment) *tools.Assessment {
	v := &tools.Assessment{OperatingConfidence: a.Operating, OperatingEvidence: []tools.Evidence{}, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, Categories: []tools.Category{}, GovernmentMatch: a.GovernmentMatch, FleetVerdict: a.FleetVerdict, PowerUnits: a.PowerUnits, Drivers: a.Drivers, Minutes: a.Minutes, Excluded: a.Excluded, Website: a.Website, WebsiteURL: a.WebsiteURL, Rules: a.Rules, Calculated: stamp(a.Calculated)}
	if !a.KnownSince.IsZero() {
		v.KnownSince = a.KnownSince.UTC().Format("2006-01-02")
	}
	if !a.NewestFiling.IsZero() {
		v.NewestFiling = a.NewestFiling.UTC().Format("2006-01-02")
	}
	for _, e := range a.Evidence {
		v.OperatingEvidence = append(v.OperatingEvidence, tools.Evidence{Name: e.Name, Source: e.Source, Weight: e.Weight, Description: e.Description})
	}
	for _, c := range a.Categories {
		v.Categories = append(v.Categories, tools.Category{Name: c.Name, Source: c.Source})
	}
	return v
}
