package enrichers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-L6OT-OWJS R-IDJA-FDOS R-K5WO-YTC6 R-K8CH-QCTK R-K9KE-44K9
func TestBriefClient(t *testing.T) {
	type word string
	const kind word = enrichers.KindBrief
	const page float64 = enrichers.BriefPageBytes
	const input float64 = enrichers.BriefInputBytes
	const parity = enrichers.BriefPageBytes%2 + enrichers.BriefInputBytes%2
	if kind != "brief" || page != 12288 || input != 98304 || parity != 0 {
		t.Fatal(kind, page, input)
	}
	writer := telemetry.New(telemetry.Config{Service: "fixture", Sink: &telemetry.Capture{}, Now: func() time.Time { return time.Unix(100, 0) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 256))})
	defer writer.Shutdown(context.Background(), "fixture")
	client := prompts.NewClient(prompts.ClientConfig{Telemetry: writer})
	for _, c := range []*prompts.Client{nil, client} {
		b := enrichers.Brief{c}
		var p enrichers.Prompter = b
		if p.Kind() != enrichers.KindBrief || !p.Fetched() || p.Ready() != (c != nil) || p.Prompt() != enrichers.PromptName(enrichers.KindBrief) {
			t.Fatal(p)
		}
		if _, ok := any(b).(enrichers.Fetcher); ok {
			t.Fatal("fetcher")
		}
		if _, ok := any(b).(enrichers.Deriver); ok {
			t.Fatal("deriver")
		}
		reg := enrichers.New(enrichers.Config{Prompts: c})
		want := []enrichers.Enricher{enrichers.DriveMinutes{}, enrichers.LinkFacts{}, enrichers.WebsiteCheck{}, enrichers.WebsiteSearch{Prompts: c}, enrichers.SiteText{Prompts: c}, enrichers.Brief{Prompts: c}, enrichers.DriveRoute{}}
		if !reflect.DeepEqual(reg.All(), want) {
			t.Fatal(reg.All())
		}
		if reg.All()[4].(enrichers.SiteText).Prompts != c || reg.All()[5].(enrichers.Brief).Prompts != c {
			t.Fatal("shared client lost")
		}
	}
}

// R-K74L-CL2V
func TestBriefFingerprint(t *testing.T) {
	b := enrichers.Brief{}
	if b.Inputs(enrichers.Subject{Website: "website"}) != "" {
		t.Fatal("empty pages")
	}
	a := enrichers.Page{Number: 3, URL: "https://a.test/", Title: "title", Text: "text", Size: 9}
	c := enrichers.Page{Number: 2, URL: "https://b.test/"}
	s := enrichers.Subject{Pages: []enrichers.Page{c, a}}
	before, _ := json.Marshal(s)
	sum := sha256.Sum256([]byte("https://a.test/\t3\nhttps://b.test/\t2\n"))
	want := hex.EncodeToString(sum[:])
	if b.Inputs(s) != want {
		t.Fatal(b.Inputs(s))
	}
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("mutated")
	}
	changed := enrichers.Subject{Name: "different", Email: "email", Website: "site", Links: []enrichers.Link{{Scraper: "source"}}, Facts: &enrichers.LinkFound{KnownSince: "known"}, Pages: []enrichers.Page{a, c}}
	changed.Pages[0].Title = "other"
	changed.Pages[0].Text = "other"
	changed.Pages[0].Size = 20
	if b.Inputs(changed) != want {
		t.Fatal("unrelated field")
	}
	for _, change := range []func(*enrichers.Page){func(p *enrichers.Page) { p.Number++ }, func(p *enrichers.Page) { p.URL += "new" }} {
		copyPage := a
		change(&copyPage)
		if b.Inputs(enrichers.Subject{Pages: []enrichers.Page{copyPage, c}}) == want {
			t.Fatal("page change ignored")
		}
	}
}

// R-F9WT-D83J
func TestBriefInputEvidence(t *testing.T) {
	n := int64(3)
	s := enrichers.Subject{ID: "candidate", Name: "name", City: "city", State: "state", Website: "site", Facts: &enrichers.LinkFound{Categories: []enrichers.Category{{Name: "retail", Source: enrichers.KindBrief}, {Name: "trucking and logistics", Source: "fmcsa"}}, GovernmentMatch: "match", FleetVerdict: "pass", PowerUnits: &n, Drivers: &n, KnownSince: "2015-03-01", NewestFiling: "2025-01-01"}, Links: []enrichers.Link{
		{Reading: sources.Reading{People: []sources.Person{{Name: "Ann", Role: "owner"}, {Name: "Bob"}}}},
		{Gone: true, Reading: sources.Reading{People: []sources.Person{{Name: "Cy"}}}},
		{Reading: sources.Reading{People: []sources.Person{{Name: "Ann", Role: "owner"}, {Role: "manager"}, {Name: "Ann", Role: "president"}}}},
	}}
	before, _ := json.Marshal(s)
	want := enrichers.BriefInput{Candidate: s.ID, Name: s.Name, City: s.City, State: s.State, Website: s.Website, Facts: enrichers.BriefFacts{Categories: []enrichers.Category{{Name: "trucking and logistics", Source: "fmcsa"}}, GovernmentMatch: "match", FleetVerdict: "pass", PowerUnits: &n, Drivers: &n, KnownSince: "2015-03-01", NewestFiling: "2025-01-01", People: []enrichers.BriefPerson{{Name: "Ann", Role: "owner"}, {Name: "Bob"}, {Name: "Ann", Role: "president"}}}, Pages: []enrichers.BriefPage{}}
	encoded, _ := json.Marshal(want)
	b := enrichers.Brief{}
	if !bytes.Equal(b.Input(s), encoded) {
		t.Fatal(string(b.Input(s)))
	}
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("mutated")
	}
	s.Email = "email"
	s.Zip = "zip"
	s.Minutes = &n
	if !bytes.Equal(b.Input(s), encoded) {
		t.Fatal("unrelated input")
	}
	s.Facts = nil
	var got enrichers.BriefInput
	if err := json.Unmarshal(b.Input(s), &got); err != nil || !reflect.DeepEqual(got.Facts.People, want.Facts.People) {
		t.Fatal(got, err)
	}
	if string(b.Input(enrichers.Subject{})) != `{"candidate":"","name":"","city":"","state":"","website":"","facts":{},"pages":[]}` {
		t.Fatal(string(b.Input(enrichers.Subject{})))
	}
	s.Facts = &enrichers.LinkFound{Categories: []enrichers.Category{{Name: "retail", Source: enrichers.KindBrief}}}
	if err := json.Unmarshal(b.Input(s), &got); err != nil || len(got.Facts.Categories) != 0 {
		t.Fatal(got, err)
	}
}

// R-FB4P-QZU8
func TestBriefPageBudget(t *testing.T) {
	for _, tc := range []struct {
		texts []string
		want  []string
	}{
		{nil, []string{}},
		{[]string{strings.Repeat("a", 20000)}, []string{strings.Repeat("a", 12288)}},
		{[]string{strings.Repeat("a", 12288)}, []string{strings.Repeat("a", 12288)}},
		{[]string{strings.Repeat("a", 12287) + "é" + strings.Repeat("a", 100)}, []string{strings.Repeat("a", 12287)}},
		{[]string{"one", "two", "three"}, []string{"one", "two", "three"}},
		{repeatedTexts(12, 12000), repeatedTexts(8, 12000)},
		{repeatedTexts(12, 20000), repeatedTexts(8, 12288)},
		{append(repeatedTexts(8, 12000), strings.Repeat("b", 3000), "c"), repeatedTexts(8, 12000)},
	} {
		s := enrichers.Subject{}
		for i, text := range tc.texts {
			s.Pages = append(s.Pages, enrichers.Page{Number: int64(i), URL: string(rune('a' + i)), Title: string(rune('A' + i)), Text: text})
		}
		before, _ := json.Marshal(s)
		var got enrichers.BriefInput
		if err := json.Unmarshal((enrichers.Brief{}).Input(s), &got); err != nil {
			t.Fatal(err)
		}
		if got.Pages == nil || len(got.Pages) != len(tc.want) {
			t.Fatal(len(got.Pages), len(tc.want))
		}
		for i, p := range got.Pages {
			if p.URL != s.Pages[i].URL || p.Title != s.Pages[i].Title || p.Text != tc.want[i] {
				t.Fatal(i)
			}
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated")
		}
	}
}
func repeatedTexts(count, size int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = strings.Repeat("a", size)
	}
	return out
}

// R-B4R3-JM1P R-L0LB-S1UB
func TestBriefRecordEncoding(t *testing.T) {
	n := int64(7)
	code := 2
	cases := []struct {
		value any
		want  string
	}{
		{enrichers.BriefPerson{"name", "role"}, `{"name":"name","role":"role"}`},
		{enrichers.BriefPerson{}, `{"name":"","role":""}`},
		{enrichers.BriefPage{"url", "title", "text"}, `{"url":"url","title":"title","text":"text"}`},
		{enrichers.BriefPage{}, `{"url":"","title":"","text":""}`},
		{enrichers.BriefFacts{[]enrichers.Category{{"category", "source"}}, "match", "pass", "mixed", &n, &n, "known", "filed", []enrichers.BriefPerson{{"person", "role"}}}, `{"categories":[{"name":"category","source":"source"}],"government_match":"match","fleet_verdict":"pass","class":"mixed","power_units":7,"drivers":7,"known_since":"known","newest_filing":"filed","people":[{"name":"person","role":"role"}]}`},
		{enrichers.BriefFacts{}, `{}`},
		{enrichers.BriefFacts{Categories: []enrichers.Category{}, People: []enrichers.BriefPerson{}}, `{}`},
		{enrichers.BriefInput{"c", "n", "city", "state", "site", enrichers.BriefFacts{}, []enrichers.BriefPage{}}, `{"candidate":"c","name":"n","city":"city","state":"state","website":"site","facts":{},"pages":[]}`},
		{enrichers.BriefCategory{"name", "why"}, `{"name":"name","why":"why"}`},
		{enrichers.BriefCategory{}, `{"name":"","why":""}`},
		{enrichers.BriefFound{"run", "status", &code, 7, "summary", []enrichers.BriefCategory{{"name", "why"}}, []string{"label"}}, `{"run":"run","status":"status","exit_code":2,"cost_nanos":7,"summary":"summary","categories":[{"name":"name","why":"why"}],"labels":["label"]}`},
		{enrichers.BriefFound{}, `{"run":"","status":"","cost_nanos":0,"summary":"","categories":null,"labels":null}`},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.value)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%T: %s %v", tc.value, b, err)
		}
		if _, ok := tc.value.(enrichers.BriefFacts); ok && string(b) == `{}` {
			continue
		}
		dst := reflect.New(reflect.TypeOf(tc.value))
		if err := json.Unmarshal([]byte(tc.want), dst.Interface()); err != nil || !reflect.DeepEqual(dst.Elem().Interface(), tc.value) {
			t.Fatalf("%T decode: %v %v", tc.value, dst.Interface(), err)
		}
	}
}

// R-IPQA-933Q
func TestBriefFoundOutput(t *testing.T) {
	b := enrichers.Brief{}
	for _, tc := range []struct {
		stdout, summary string
		categories      []enrichers.BriefCategory
		labels          []string
	}{
		{`{"summary":"summary","categories":[{"name":"retail","why":"reason"},{"name":"shops","why":"unknown"},{"name":"Retail"},{"name":"retail","why":"again"}],"labels":["label"],"x":1}`, "summary", []enrichers.BriefCategory{{"retail", "reason"}, {"retail", "again"}}, []string{"label", "shops", "Retail"}},
		{`{"summary":"x"}`, "x", []enrichers.BriefCategory{}, []string{}},
		{``, "", []enrichers.BriefCategory{}, []string{}},
		{`not json`, "", []enrichers.BriefCategory{}, []string{}},
		{`[]`, "", []enrichers.BriefCategory{}, []string{}},
		{`{"summary":"x","categories":5}`, "", []enrichers.BriefCategory{}, []string{}},
		{`{"categories":[{"name":"retail","why":"w"}],"labels":["a"],"summary":5}`, "", []enrichers.BriefCategory{}, []string{}},
	} {
		for _, status := range []string{"exited", "failed"} {
			var code *int
			if status == "exited" {
				code = new(int)
			}
			out := prompts.Outcome{ID: "run_fixture", Status: status, ExitCode: code, CostNanos: 1500000000, Stdout: tc.stdout}
			want := enrichers.BriefFound{Run: out.ID, Status: status, ExitCode: code, CostNanos: out.CostNanos, Summary: tc.summary, Categories: tc.categories, Labels: tc.labels}
			encoded, _ := json.Marshal(want)
			r := b.Found(context.Background(), out, 3)
			if r.Source != enrichers.SourcePrompts || r.Cost != 1.5 || !bytes.Equal(r.Found, encoded) || !b.Valid(r.Found) {
				t.Fatal(string(r.Found), string(encoded))
			}
			out.Prompt = "other"
			out.Model = "model"
			out.Reason = "reason"
			out.InputTokens = 9
			out.OutputTokens = 8
			out.CachedTokens = 7
			out.ReasoningTokens = 6
			out.Started = time.Unix(10, 0)
			out.Finished = time.Unix(20, 0)
			if !reflect.DeepEqual(b.Found(context.Background(), out, 0), r) {
				t.Fatal("unrelated outcome retained")
			}
		}
	}
}

// R-IS63-0ML4
func TestBriefValidation(t *testing.T) {
	b := enrichers.Brief{}
	for _, raw := range []string{`{"run":"r","status":"killed","categories":[],"labels":[]}`, ` {"run":"r","status":"killed","categories":[],"labels":[],"exit_code":null,"summary":""} `, `{"run":"r","status":"killed","categories":[{"name":"retail","why":""}],"labels":[]}`} {
		if !b.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{
		`{"run":"r","status":"x","labels":[]}`, `{"run":"r","status":"x","categories":[]}`,
		`{"run":"r","status":"x","categories":null,"labels":[]}`, `{"run":"r","status":"x","categories":[],"labels":null}`,
		`{"run":"r","status":"x","Categories":[],"labels":[]}`, `{"run":"r","status":"x","categories":[],"Labels":[]}`,
		`{"run":"r","status":"x","categories":[{"name":"shops"}],"labels":[]}`, `{"run":"r","status":"x","categories":[{"name":"Retail"}],"labels":[]}`, `{"run":"r","status":"x","categories":[{"name":""}],"labels":[]}`,
		`{"run":"","status":"x","categories":[],"labels":[]}`, `{"status":"x","categories":[],"labels":[]}`, `{"run":"r","status":"","categories":[],"labels":[]}`,
		`{"run":"r","status":"x","categories":[],"labels":[],"cost_nanos":-1}`, `{"run":"r","status":"x","categories":[],"labels":[],"exit_code":"0"}`,
		`{"run":"r","status":"x","categories":[],"labels":[],"pages":0}`, `{"run":"r","status":"x","categories":[],"labels":[],"x":1}`,
		`{}`, `null`, `[]`, ``, `{"run":"r","status":"x","categories":[],"labels":[]} {}`,
	} {
		if b.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
}

// R-L1T8-5TL0
func TestBriefFailed(t *testing.T) {
	for _, status := range []string{"exited", "failed", "killed", "timed_out", "running", "EXITED", ""} {
		for _, code := range []*int{nil, new(int), func() *int { n := 1; return &n }()} {
			for _, summary := range []string{"", "summary", " "} {
				f := enrichers.BriefFound{Status: status, ExitCode: code, Summary: summary}
				want := status != "exited" || code == nil || *code != 0 || summary == ""
				if f.Failed() != want {
					t.Fatal(f)
				}
			}
		}
	}
}

// R-L7WQ-2OAH
func TestPrompterFailedDecode(t *testing.T) {
	for _, tc := range []struct {
		raw         string
		site, brief bool
	}{
		{`{"run":"r","status":"failed","x":1}`, true, true},
		{`{}`, true, true}, {`null`, true, true},
		{`{"status":"exited","exit_code":0}`, false, true},
		{`{"status":"exited","exit_code":0,"summary":"summary"}`, false, false},
		{`[]`, false, false}, {`{} x`, false, false}, {`{} {}`, false, false}, {``, false, false}, {`{"exit_code":"0"}`, false, false},
	} {
		var site enrichers.Prompter = enrichers.SiteText{}
		var brief enrichers.Prompter = enrichers.Brief{}
		if site.Failed(json.RawMessage(tc.raw)) != tc.site || brief.Failed(json.RawMessage(tc.raw)) != tc.brief {
			t.Fatal(tc)
		}
	}
}

// R-F9WT-D83J
func TestBriefInputCarrierClass(t *testing.T) {
	for _, facts := range []*enrichers.LinkFound{nil, {Categories: []enrichers.Category{{Name: sources.CategoryTrucking, Source: "fmcsa"}}, FleetVerdict: sources.FleetPass}} {
		s := enrichers.Subject{Facts: facts, Links: []enrichers.Link{
			{Scraper: "ppp", Reading: sources.Reading{Class: sources.ClassForHire}},
			{Scraper: "fmcsa", Gone: true, Reading: sources.Reading{Class: sources.ClassForHire}},
			{Scraper: "fmcsa", Reading: sources.Reading{Class: sources.ClassMixed}},
			{Scraper: "fmcsa", Reading: sources.Reading{Class: sources.ClassPrivate}},
		}}
		before, _ := json.Marshal(s)
		var got enrichers.BriefInput
		if err := json.Unmarshal((enrichers.Brief{}).Input(s), &got); err != nil || got.Facts.Class != sources.ClassMixed {
			t.Fatal(got, err)
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated subject")
		}
		s.Links[2].Gone = true
		s.Links[3].Gone = true
		// Decode into fresh storage because absent members do not reset an existing value.
		got = enrichers.BriefInput{}
		if err := json.Unmarshal((enrichers.Brief{}).Input(s), &got); err != nil || got.Facts.Class != "" {
			t.Fatal(got, err)
		}
	}
}
