package enrichers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-6NS5-A4OX R-I3S3-D7R8 R-7SGL-IM1E R-IH6Z-KOWV R-IKUO-Q04Y
func TestWebsiteSearchClient(t *testing.T) {
	type word string
	const kind word = enrichers.KindWebsiteSearch
	if kind != "website_search" {
		t.Fatal(kind)
	}
	for _, p := range []*prompts.Client{nil, {}} {
		for _, k := range []*website.Checker{nil, {}} {
			var e enrichers.Prompter = enrichers.WebsiteSearch{p, k}
			if e.Kind() != enrichers.KindWebsiteSearch || !e.Fetched() || e.Ready() != (p != nil && k != nil) || e.Prompt() != enrichers.PromptName(enrichers.KindWebsiteSearch) {
				t.Fatal(e)
			}
			if _, ok := e.(enrichers.Fetcher); ok {
				t.Fatal("fetcher")
			}
			if _, ok := e.(enrichers.Deriver); ok {
				t.Fatal("deriver")
			}
			reg := enrichers.New(enrichers.Config{Website: k, Prompts: p})
			if reg.All()[3] != (enrichers.WebsiteSearch{p, k}) {
				t.Fatal(reg.All())
			}
		}
	}
}

// R-WGBU-VP1U R-I7FS-IIZB R-SDQ1-5H81 R-SEXX-J8YQ R-SG5T-X0PF R-AV13-OU59
func TestWebsiteSearchShapes(t *testing.T) {
	zero := 0
	input := enrichers.WebsiteSearchInput{"c", "n", "ci", "st", "street", "zip", "phone", "domain", "check", "error", "website", "check", &enrichers.WebsiteSearchSiteText{"failed", 0}, []enrichers.BriefPerson{{"person", "role"}}, []enrichers.Category{{"cat", "origin"}}}
	run := enrichers.WebsiteSearchRun{"r", "status", &zero, 3}
	answer := enrichers.WebsiteSearchAnswer{"u", "w", []enrichers.WebsiteSearchSite{{"r", "why"}}}
	confirm := enrichers.WebsiteSearchConfirmation{"s", "u", "t", 200, 10, "e"}
	found := enrichers.WebsiteSearchFound{run, &answer, &confirm, "again", "website", "check", &enrichers.WebsiteSearchSiteText{"failed", 0}, "different", nil}
	cases := []struct {
		v    any
		want string
		dst  any
	}{
		{input, `{"candidate":"c","name":"n","city":"ci","state":"st","street":"street","zip":"zip","phone":"phone","domain":"domain","domain_check":"check","domain_error":"error","website":"website","website_from":"check","site_text":{"status":"failed","pages":0},"people":[{"name":"person","role":"role"}],"categories":[{"name":"cat","source":"origin"}]}`, &enrichers.WebsiteSearchInput{}},
		{enrichers.WebsiteSearchInput{}, `{"candidate":"","name":"","city":"","state":"","street":"","zip":"","phone":"","domain":"","domain_check":"","domain_error":"","website":"","website_from":"","site_text":null,"people":[],"categories":[]}`, &enrichers.WebsiteSearchInput{}},
		{run, `{"id":"r","status":"status","exit_code":0,"cost_nanos":3}`, &enrichers.WebsiteSearchRun{}},
		{enrichers.WebsiteSearchRun{}, `{"id":"","status":"","cost_nanos":0}`, &enrichers.WebsiteSearchRun{}},
		{answer.Rejected[0], `{"url":"r","why":"why"}`, &enrichers.WebsiteSearchSite{}},
		{enrichers.WebsiteSearchSite{}, `{"url":"","why":""}`, &enrichers.WebsiteSearchSite{}},
		{answer, `{"url":"u","why":"w","rejected":[{"url":"r","why":"why"}]}`, &enrichers.WebsiteSearchAnswer{}},
		{enrichers.WebsiteSearchAnswer{}, `{"url":"","why":"","rejected":[]}`, &enrichers.WebsiteSearchAnswer{}},
		{confirm, `{"state":"s","url":"u","title":"t","status_code":200,"size":10,"error":"e"}`, &enrichers.WebsiteSearchConfirmation{}},
		{enrichers.WebsiteSearchConfirmation{}, `{"state":"","url":"","title":"","status_code":0,"size":0,"error":""}`, &enrichers.WebsiteSearchConfirmation{}},
		{found, `{"run":{"id":"r","status":"status","exit_code":0,"cost_nanos":3},"answer":{"url":"u","why":"w","rejected":[{"url":"r","why":"why"}]},"confirmation":{"state":"s","url":"u","title":"t","status_code":200,"size":10,"error":"e"},"reason":"again","website":"website","website_from":"check","site_text":{"status":"failed","pages":0},"case":"different"}`, &enrichers.WebsiteSearchFound{}},
		{enrichers.WebsiteSearchFound{}, `{"run":{"id":"","status":"","cost_nanos":0}}`, &enrichers.WebsiteSearchFound{}},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%T %s %v", tc.v, b, err)
		}
		if err = json.Unmarshal(b, tc.dst); err != nil {
			t.Fatal(err)
		}
		again, err := json.Marshal(tc.dst)
		if err != nil || !bytes.Equal(b, again) {
			t.Fatal(string(again), err)
		}
	}
}

// R-WF3Y-HXB5 R-M9NX-E3ED
func TestWebsiteSearchInputs(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	s := enrichers.Subject{People: []sources.Sourced[sources.Person]{{Value: sources.Person{Name: "Ann", Role: "owner"}}, {Value: sources.Person{Name: "Bob"}}, {Value: sources.Person{Name: "Ann", Role: "owner"}}}, ID: "candidate", Name: "Acme", City: "Sandstone", State: "MN", Street: "Main", Zip: "zip", Phone: "phone", Email: "Ops@Acme.test", Links: []enrichers.Link{{Reading: sources.Reading{People: []sources.Person{{Name: "Ann", Role: "owner"}, {Name: "Bob"}, {Name: "Ann", Role: "owner"}}}}, {Gone: true, Reading: sources.Reading{People: []sources.Person{{Name: "Gone"}}}}}, Facts: &enrichers.LinkFound{Categories: []enrichers.Category{{Name: "omit", Source: enrichers.KindBrief}, {Name: "keep", Source: "source"}}}}
	before, _ := json.Marshal(s)
	sum := sha256.Sum256([]byte(s.Name + "\t" + s.City + "\t" + s.State))
	want := hex.EncodeToString(sum[:])
	for _, source := range []string{"", "check", "Search", "search"} {
		changed := s
		changed.Website = "https://site.test/"
		changed.WebsiteSource = source
		expected := ""
		if source == "search" {
			expected = want
		}
		if e.Inputs(changed) != expected {
			t.Fatal(source)
		}
	}
	for _, change := range []func(*enrichers.Subject){func(v *enrichers.Subject) { v.Name += "x" }, func(v *enrichers.Subject) { v.City += "x" }, func(v *enrichers.Subject) { v.State += "x" }} {
		v := s
		change(&v)
		if e.Inputs(v) == want {
			t.Fatal("identity not fingerprinted")
		}
	}
	if e.Inputs(s) != want {
		t.Fatal("fingerprint")
	}
	var input enrichers.WebsiteSearchInput
	if err := json.Unmarshal(e.Input(s), &input); err != nil {
		t.Fatal(err)
	}
	expected := enrichers.WebsiteSearchInput{s.ID, s.Name, s.City, s.State, s.Street, s.Zip, s.Phone, "acme.test", sources.WebsiteUnknown, "", "", "", nil, []enrichers.BriefPerson{{"Ann", "owner"}, {"Bob", ""}}, []enrichers.Category{{"keep", "source"}}}
	if !reflect.DeepEqual(input, expected) {
		t.Fatal(input)
	}
	s.EmailCheck = &website.Result{State: sources.WebsiteNone, Error: website.FailStatus}
	expected.DomainCheck = sources.WebsiteNone
	expected.DomainError = website.FailStatus
	b, _ := json.Marshal(expected)
	if !bytes.Equal(e.Input(s), b) {
		t.Fatal(string(e.Input(s)))
	}
	s.Email = "a@gmail.com"
	expected.Domain = ""
	expected.DomainCheck = ""
	expected.DomainError = ""
	b, _ = json.Marshal(expected)
	if !bytes.Equal(e.Input(s), b) {
		t.Fatal(string(e.Input(s)))
	}
	s.Email = "Ops@Acme.test"
	s.EmailCheck = nil
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("mutated subject")
	}
	altered := s
	altered.ID = "other"
	altered.Street = "other"
	altered.Zip = "other"
	altered.Phone = "other"
	altered.Email = "other"
	altered.Check = &website.Result{}
	altered.Minutes = new(int64)
	altered.Links = nil
	altered.Pages = []enrichers.Page{{URL: "other"}}
	altered.Facts = nil
	if e.Inputs(altered) != want {
		t.Fatal("unrelated fingerprint inputs")
	}
	altered = s
	altered.Website = "other"
	altered.WebsiteSource = "other"
	altered.Minutes = new(int64)
	altered.Pages = []enrichers.Page{{URL: "other"}}
	altered.Facts = &enrichers.LinkFound{KnownSince: "other", Categories: s.Facts.Categories}
	if !bytes.Equal(e.Input(s), e.Input(altered)) {
		t.Fatal("unrelated prompt inputs")
	}
}

// R-WL7G-ES0M R-IX1O-JPJW R-IY9K-XHAL R-85VH-Q371
func TestWebsiteSearchFoundAndFailure(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	valid := `{"url":"https://acme.test/","why":"match","rejected":[{"url":"https://other.test/","why":"different","x":1},{"url":"https://third.test/","why":"another"}],"x":2}`
	invalid := []string{"", "bad", "[]", `{"url":"x","why":"y","rejected":[]} {}`, `{"url":"x","why":"y"}`, `{"url":"x","why":"y","rejected":null}`, `{"url":5,"why":"y","rejected":[]}`, `{"URL":"x","why":"y","rejected":[]}`, `{"url":"x","why":"y","rejected":[{"url":"z"}]}`, `{"url":"x","why":"y","rejected":[5]}`, `{"url":null,"why":"y","rejected":[]}`}
	for _, stdout := range append([]string{valid, `{"url":"","why":"none","rejected":[]}`}, invalid...) {
		for _, status := range []string{enrichers.RunExited, "failed", "killed", "timed_out", enrichers.RunRunning, "EXITED", ""} {
			for _, code := range []*int{nil, new(int), func() *int { v := 1; return &v }()} {
				out := prompts.Outcome{ID: "run", Status: status, ExitCode: code, CostNanos: 250000000, Stdout: stdout}
				r := e.Found(context.Background(), enrichers.Subject{}, out, 99)
				var got enrichers.WebsiteSearchFound
				if err := json.Unmarshal(r.Found, &got); err != nil {
					t.Fatal(err)
				}
				good := !slices.Contains(invalid, stdout)
				failed := status != enrichers.RunExited || code == nil || *code != 0 || !good
				if got.Run.ID != out.ID || got.Run.Status != status || !reflect.DeepEqual(got.Run.ExitCode, code) || got.Run.CostNanos != out.CostNanos || (got.Answer != nil) != good || got.Confirmation != nil || got.Failed() != failed || e.Failed(r.Found) != failed || r.Source != enrichers.SourcePrompts || r.Cost != 0.25 {
					t.Fatal(string(r.Found))
				}
				if good {
					expected := &enrichers.WebsiteSearchAnswer{URL: "https://acme.test/", Why: "match", Rejected: []enrichers.WebsiteSearchSite{{URL: "https://other.test/", Why: "different"}, {URL: "https://third.test/", Why: "another"}}}
					if stdout != valid {
						expected = &enrichers.WebsiteSearchAnswer{Why: "none", Rejected: []enrichers.WebsiteSearchSite{}}
					}
					if !reflect.DeepEqual(got.Answer, expected) {
						t.Fatal(got.Answer, expected)
					}
				}
				out.Prompt = "ignored"
				out.Model = "ignored"
				out.Reason = "ignored"
				out.InputTokens = 99
				out.OutputTokens = 98
				out.CachedTokens = 97
				out.ReasoningTokens = 96
				out.Started = time.Unix(1, 0)
				out.Finished = time.Unix(2, 0)
				if !reflect.DeepEqual(r, e.Found(context.Background(), enrichers.Subject{}, out, -1)) {
					t.Fatal("ignored fields")
				}
			}
		}
	}
	for _, raw := range []string{"[]", "{} x", ""} {
		if e.Failed(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	if !e.Failed(json.RawMessage(`{"run":{"status":"failed"},"x":1}`)) || !e.Failed(json.RawMessage(`{}`)) {
		t.Fatal("decoded failure")
	}
	v := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: new(int)}, Answer: &enrichers.WebsiteSearchAnswer{}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone}, Reason: enrichers.SearchReasonAgain, Website: "https://before.test", WebsiteFrom: "check", SiteText: &enrichers.WebsiteSearchSiteText{Status: "failed"}, Case: enrichers.SearchCaseDifferent}
	if v.Failed() {
		t.Fatal(v)
	}
	b, _ := json.Marshal(v)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	for key := range fields {
		for _, old := range []string{"Candidate", "Kind", "State", "URL", "Title", "StatusCode", "Size", "Error", "Source", "Cost", "At"} {
			if strings.EqualFold(key, old) {
				t.Fatal(key)
			}
		}
	}
}

// R-WOV5-K38P
func TestWebsiteSearchValidation(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	run := `"run":{"id":"run","status":"killed","cost_nanos":0}`
	answer := `"answer":{"url":"https://acme.test/","why":"w","rejected":[{"url":"https://b.test/","why":"v"}]}`
	confirmation := `"confirmation":{"state":"none","url":"","title":"","status_code":0,"size":0,"error":"x"}`
	for _, raw := range []string{"{" + run + "}", "{" + run + "," + answer + "}", "{" + run + "," + answer + "," + confirmation + "}", "{" + run + `,"answer":{"url":"","why":"","rejected":[]}}`} {
		if !e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	full := "{" + run + "," + answer + "," + confirmation + "}"
	invalid := []string{"{}", "null", "[]", "", full + " {}", "{" + answer + "}", `{"run":null}`, strings.Replace(full, `"run":`, `"Run":`, 1), strings.Replace(full, `"id":"run"`, `"id":""`, 1), strings.Replace(full, `"status":"killed"`, `"status":""`, 1), strings.Replace(full, `"cost_nanos":0`, `"cost_nanos":-1`, 1), strings.Replace(full, `"cost_nanos":0`, `"cost_nanos":0,"exit_code":"0"`, 1), "{" + run + `,"answer":null}`, "{" + run + `,"answer":{"url":"x"}}`, strings.Replace(full, `"rejected":[{"url":"https://b.test/","why":"v"}]`, `"rejected":null`, 1), strings.Replace(full, `"state":"none"`, `"state":"unknown"`, 1), strings.Replace(full, `"status_code":0`, `"status_code":-1`, 1), strings.Replace(full, `"size":0`, `"size":-1`, 1), "{" + run + "," + confirmation + "}", strings.Replace(full, `"url":"https://acme.test/"`, `"url":""`, 1)}
	for _, location := range []string{`"run":{`, `"answer":{`, `"confirmation":{`, `"rejected":[{`} {
		invalid = append(invalid, strings.Replace(full, location, location+`"x":1,`, 1))
	}
	invalid = append(invalid, strings.Replace(full, "{", `{"x":1,`, 1), strings.Replace(full, "{", `{"pages":1,`, 1))
	for _, raw := range invalid {
		if e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
}

// R-WNN9-6BI0
func TestWebsiteSearchConfirmation(t *testing.T) {
	for _, address := range []string{"https://acme.test/", "https://secure.test/", "http://acme.test/", "https://missing.test/", "acme.test/about"} {
		requests := []string{}
		timers := []time.Duration{}
		ck := &website.Checker{Client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.String())
			if r.URL.String() == "https://acme.test/" {
				return &http.Response{StatusCode: http.StatusMovedPermanently, Header: http.Header{"Location": []string{"http://landed.test/home"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			status := http.StatusOK
			if r.URL.Host == "missing.test" {
				status = http.StatusNotFound
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<title>Owned</title>body"))}, nil
		})}, After: func(d time.Duration) <-chan time.Time { timers = append(timers, d); return make(chan time.Time) }}
		e := enrichers.WebsiteSearch{Checker: ck}
		out := prompts.Outcome{ID: "run", Status: enrichers.RunExited, ExitCode: new(int)}
		a, _ := json.Marshal(enrichers.WebsiteSearchAnswer{URL: address, Why: "match"})
		out.Stdout = string(a)
		r := e.Found(context.Background(), enrichers.Subject{}, out, 0)
		var f enrichers.WebsiteSearchFound
		if err := json.Unmarshal(r.Found, &f); err != nil {
			t.Fatal(err)
		}
		c := f.Confirmation
		if c == nil || f.Failed() || !e.Valid(r.Found) {
			t.Fatal(string(r.Found))
		}
		switch address {
		case "https://acme.test/":
			if c.State != sources.WebsiteHTTP || c.URL != "http://landed.test/home" || c.Title != "Owned" || c.StatusCode != 200 || c.Size != int64(len("<title>Owned</title>body")) || c.Error != "" || !slices.Equal(requests, []string{address, c.URL}) {
				t.Fatal(c, requests)
			}
		case "https://secure.test/":
			if c.State != sources.WebsiteHTTPS || c.URL != address || c.Title != "Owned" || c.StatusCode != 200 || c.Size <= 0 || c.Error != "" || len(requests) != 1 {
				t.Fatal(c, requests)
			}
		case "http://acme.test/":
			if c.State != sources.WebsiteHTTP || c.URL != address || c.Title != "Owned" || len(requests) != 1 {
				t.Fatal(c, requests)
			}
		case "https://missing.test/":
			if c.State != sources.WebsiteNone || c.StatusCode != 404 || c.Error != website.FailStatus || len(requests) != 1 {
				t.Fatal(c, requests)
			}
		default:
			if c.State != sources.WebsiteNone || c.Error != website.FailRefused || len(requests) != 0 {
				t.Fatal(c, requests)
			}
		}
		if address == "acme.test/about" {
			if len(timers) != 0 {
				t.Fatal(timers)
			}
		} else if len(timers) != 1 || timers[0] != website.Timeout {
			t.Fatal(timers)
		}
		for _, change := range []func(*prompts.Outcome){func(o *prompts.Outcome) { o.Status = "failed" }, func(o *prompts.Outcome) { o.Status = "killed" }, func(o *prompts.Outcome) { o.Status = "timed_out" }, func(o *prompts.Outcome) { o.ExitCode = nil }, func(o *prompts.Outcome) { n := 1; o.ExitCode = &n }, func(o *prompts.Outcome) { o.Stdout = `{"url":"","why":"none","rejected":[]}` }, func(o *prompts.Outcome) { o.Stdout = "bad" }} {
			requests = nil
			timers = nil
			o := out
			change(&o)
			r = e.Found(context.Background(), enrichers.Subject{}, o, 0)
			_ = json.Unmarshal(r.Found, &f)
			var decoded enrichers.WebsiteSearchFound
			_ = json.Unmarshal(r.Found, &decoded)
			if decoded.Confirmation != nil || len(requests) != 0 || len(timers) != 0 {
				t.Fatal(string(r.Found), requests, timers)
			}
		}
	}
}

// R-WNN9-6BI0
func TestWebsiteSearchConfirmationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ck := &website.Checker{Client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
	r := (enrichers.WebsiteSearch{Checker: ck}).Found(ctx, enrichers.Subject{}, prompts.Outcome{ID: "run", Status: enrichers.RunExited, ExitCode: new(int), Stdout: `{"url":"https://acme.test/","why":"match","rejected":[]}`}, 0)
	var f enrichers.WebsiteSearchFound
	if err := json.Unmarshal(r.Found, &f); err != nil || f.Confirmation == nil || f.Confirmation.State != sources.WebsiteNone || f.Confirmation.Error == "" {
		t.Fatal(string(r.Found), err)
	}
}

// R-7TOH-WDS3 R-7W4A-NX9H R-7NKZ-ZJ2M
func TestOtherPrompterContextsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := prompts.Outcome{ID: "run", Status: enrichers.RunExited, ExitCode: new(int), CostNanos: 12, Stdout: `{"summary":"owned","categories":[],"labels":[]}`}
	for _, p := range []enrichers.Prompter{enrichers.SiteText{}, enrichers.Brief{}} {
		if !reflect.DeepEqual(p.Found(ctx, enrichers.Subject{ID: "ignored", SiteText: &enrichers.SiteTextFound{Run: "ignored"}, SearchInputs: "ignored"}, out, 3), p.Found(context.Background(), enrichers.Subject{}, out, 3)) {
			t.Fatal(p.Kind())
		}
	}
}

// R-M92P-SAMI R-7L57-7ZL8
func TestWebsiteSearchContextDeclarations(t *testing.T) {
	type word string
	const reasons word = enrichers.SearchReasonNoWebsite + ":" + enrichers.SearchReasonEmptySite + ":" + enrichers.SearchReasonAgain
	const cases word = enrichers.SearchCaseDifferent + ":" + enrichers.SearchCaseStands + ":" + enrichers.SearchCaseNone
	if reasons != "no_website:empty_site:again" || cases != "different:stands:none" {
		t.Fatal(reasons, cases)
	}
	for _, v := range []enrichers.WebsiteSearchSiteText{{}, {"failed", 3}} {
		b, err := json.Marshal(v)
		want := `{"status":"","pages":0}`
		if v.Status != "" {
			want = `{"status":"failed","pages":3}`
		}
		var got enrichers.WebsiteSearchSiteText
		if err != nil || string(b) != want || json.Unmarshal(b, &got) != nil || got != v {
			t.Fatal(string(b), got, err)
		}
	}
}

// R-WF3Y-HXB5
func TestWebsiteSearchEmptySiteFingerprint(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	hash := func(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
	base := enrichers.Subject{Name: "Company", City: "Town", State: "ST"}
	b := hash("Company\tTown\tST")
	for _, source := range []string{"", "check", "search", "Search"} {
		s := base
		s.Website = "https://prior.test/home"
		s.WebsiteSource = source
		s.SiteText = &enrichers.SiteTextFound{Run: "run_old", Status: "failed"}
		want := b + "/" + hash(s.Website+"\t"+s.SiteText.Run)
		before, _ := json.Marshal(s)
		if e.Inputs(s) != want {
			t.Fatal(source, e.Inputs(s), want)
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("subject mutation")
		}
		for _, change := range []func(*enrichers.Subject){func(v *enrichers.Subject) { v.Name += "x" }, func(v *enrichers.Subject) { v.City += "x" }, func(v *enrichers.Subject) { v.State += "x" }} {
			changed := s
			change(&changed)
			if e.Inputs(changed) == want {
				t.Fatal("identity not fingerprinted")
			}
		}
		s.SearchInputs = "ignored"
		s.SiteText.Status = "timed_out"
		s.SiteText.ExitCode = new(int)
		s.SiteText.CostNanos = 99
		s.ID = "other"
		s.Origin = "elsewhere"
		if e.Inputs(s) != want {
			t.Fatal("unrelated inputs")
		}
		s.SiteText.Run = "run_new"
		if e.Inputs(s) == want {
			t.Fatal("new site text not due")
		}
		s.SiteText.Run = "run_old"
		s.Website += "/other"
		if e.Inputs(s) == want {
			t.Fatal("new website not due")
		}
		s.Website = "https://prior.test/home"
		s.Pages = []enrichers.Page{{URL: "kept"}}
		expected := ""
		if source == "search" {
			expected = b
		}
		if e.Inputs(s) != expected {
			t.Fatal("kept page", source, e.Inputs(s))
		}
		s.Pages = nil
		s.SiteText.Pages = 2
		if e.Inputs(s) != expected {
			t.Fatal("nonempty text", source, e.Inputs(s))
		}
	}
	s := base
	s.Website = "https://found.test/"
	s.WebsiteSource = "search"
	for _, prior := range []string{"", b, hash("changed") + "/suffix", b + "/suffix"} {
		s.SearchInputs = prior
		want := b
		if strings.HasPrefix(prior, b+"/") {
			want = prior
		}
		for _, text := range []*enrichers.SiteTextFound{nil, {Run: "r", Pages: 3}} {
			s.SiteText = text
			if e.Inputs(s) != want {
				t.Fatal(prior, e.Inputs(s), want)
			}
		}
	}
	s.Website = ""
	s.SiteText = &enrichers.SiteTextFound{Pages: 0}
	if e.Inputs(s) != b {
		t.Fatal("empty website")
	}
}

// R-M9NX-E3ED R-WGBU-VP1U
func TestWebsiteSearchEmptySiteInput(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	for _, source := range []string{"", "check", "Search", "search"} {
		s := enrichers.Subject{ID: "c", Name: "Company", Website: "https://prior.test/", WebsiteSource: source, SiteText: &enrichers.SiteTextFound{Run: "r", Status: "timed_out"}, SearchInputs: "ignored"}
		want := enrichers.WebsiteSearchInput{Candidate: s.ID, Name: s.Name, Website: s.Website, WebsiteFrom: "check", SiteText: &enrichers.WebsiteSearchSiteText{Status: "timed_out"}}
		if source == "search" {
			want.WebsiteFrom = "search"
		}
		before, _ := json.Marshal(s)
		encoded, _ := json.Marshal(want)
		if !bytes.Equal(e.Input(s), encoded) {
			t.Fatal(string(e.Input(s)), string(encoded))
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("subject mutation")
		}
		for _, change := range []func(*enrichers.Subject){func(s *enrichers.Subject) { s.Website = "" }, func(s *enrichers.Subject) { s.SiteText = nil }, func(s *enrichers.Subject) { s.SiteText = &enrichers.SiteTextFound{Pages: 2} }, func(s *enrichers.Subject) { s.Pages = []enrichers.Page{{URL: "kept"}} }} {
			changed := s
			change(&changed)
			want.Website = ""
			want.WebsiteFrom = ""
			want.SiteText = nil
			encoded, _ = json.Marshal(want)
			if !bytes.Equal(e.Input(changed), encoded) {
				t.Fatal(string(e.Input(changed)))
			}
		}
	}
}

// R-WJZK-109X R-WL7G-ES0M
func TestWebsiteSearchReasonsAndCases(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	for _, s := range []enrichers.Subject{
		{}, {SiteText: &enrichers.SiteTextFound{Status: "failed"}, WebsiteSource: "search"},
		{Website: "https://www.acme.test/home"},
		{Website: "https://www.acme.test/home", WebsiteSource: "Search", SiteText: &enrichers.SiteTextFound{Status: "failed"}},
		{Website: "https://www.acme.test/home", WebsiteSource: "search", SiteText: &enrichers.SiteTextFound{Status: "exited", Pages: 4}},
		{Website: "https://www.acme.test/home", WebsiteSource: "check", SiteText: &enrichers.SiteTextFound{Status: "failed"}, Pages: []enrichers.Page{{URL: "kept"}}},
	} {
		for _, address := range []string{"", "https://other.test/", "http://acme.test", "https://acme.test:8443/about", "HTTPS://WWW.ACME.TEST./contact", "acme.test", "https://shop.acme.test/", "https://acme.test.example/", "://bad"} {
			for _, failed := range []bool{false, true} {
				out := prompts.Outcome{ID: "run", Status: enrichers.RunExited, ExitCode: new(int), CostNanos: 5}
				if failed {
					out.Status = "failed"
				}
				answer := enrichers.WebsiteSearchAnswer{URL: address, Why: "fixture"}
				stdout, _ := json.Marshal(answer)
				out.Stdout = string(stdout)
				before, _ := json.Marshal(s)
				r := e.Found(context.Background(), s, out, 99)
				after, _ := json.Marshal(s)
				if !bytes.Equal(before, after) {
					t.Fatal("subject mutation")
				}
				var f enrichers.WebsiteSearchFound
				if err := json.Unmarshal(r.Found, &f); err != nil {
					t.Fatal(err)
				}
				reason := enrichers.SearchReasonAgain
				from := "check"
				var text *enrichers.WebsiteSearchSiteText
				if s.Website == "" {
					reason = enrichers.SearchReasonNoWebsite
					from = ""
				} else {
					if s.WebsiteSource == "search" {
						from = "search"
					}
					if s.SiteText != nil {
						text = &enrichers.WebsiteSearchSiteText{Status: s.SiteText.Status, Pages: s.SiteText.Pages}
						if s.SiteText.Pages == 0 && len(s.Pages) == 0 {
							reason = enrichers.SearchReasonEmptySite
						}
					}
				}
				wantCase := enrichers.SearchCaseDifferent
				switch {
				case failed:
					wantCase = ""
				case address == "":
					wantCase = enrichers.SearchCaseNone
				case s.Website != "" && slices.Contains([]string{"http://acme.test", "https://acme.test:8443/about", "HTTPS://WWW.ACME.TEST./contact", "acme.test"}, address):
					wantCase = enrichers.SearchCaseStands
				}
				if f.Reason != reason || f.Website != s.Website || f.WebsiteFrom != from || !reflect.DeepEqual(f.SiteText, text) || f.Case != wantCase || !e.Valid(r.Found) {
					t.Fatal(s, address, string(r.Found), wantCase)
				}
				out.Stdout = "not an answer"
				r = e.Found(context.Background(), s, out, 0)
				var missing enrichers.WebsiteSearchFound
				_ = json.Unmarshal(r.Found, &missing)
				if missing.Case != "" || missing.Reason != reason || !missing.Failed() || !e.Valid(r.Found) {
					t.Fatal(string(r.Found))
				}
			}
		}
	}
}

// R-WJZK-109X R-WL7G-ES0M R-WNN9-6BI0 R-WOV5-K38P
func TestWebsiteSearchRedirectCases(t *testing.T) {
	const proposed = "https://acme-trucking.test/"
	const prior = "https://www.social.test/acme"
	const body = "<title>Landing</title>fixture"
	for _, tc := range []struct {
		name, website, answer, landing, wantCase string
		status                                   int
	}{
		{"landing matches", prior, proposed, "https://social.test/acme?ref=fixture", enrichers.SearchCaseStands, http.StatusOK},
		{"landing normalizes", prior, proposed, "https://WWW.Social.test./acme?ref=fixture", enrichers.SearchCaseStands, http.StatusOK},
		{"http landing matches", prior, proposed, "http://social.test:8080/elsewhere", enrichers.SearchCaseStands, http.StatusOK},
		{"failed landing falls back", prior, proposed, "https://social.test/acme", enrichers.SearchCaseDifferent, http.StatusNotFound},
		{"direct answer differs", prior, proposed, "", enrichers.SearchCaseDifferent, http.StatusOK},
		{"matching answer redirects away", prior, "https://social.test/acme", "https://login.portal.test/?next=fixture", enrichers.SearchCaseDifferent, http.StatusOK},
		{"failed redirect keeps matching answer", prior, "https://social.test/acme", "https://login.portal.test/?next=fixture", enrichers.SearchCaseStands, http.StatusNotFound},
		{"no prior website", "", proposed, "https://social.test/acme", enrichers.SearchCaseDifferent, http.StatusOK},
		{"subdomain differs", prior, proposed, "https://shop.social.test/acme", enrichers.SearchCaseDifferent, http.StatusOK},
		{"suffix differs", prior, proposed, "https://social.test.example/acme", enrichers.SearchCaseDifferent, http.StatusOK},
		{"one www removed", prior, proposed, "https://www.www.social.test/acme", enrichers.SearchCaseDifferent, http.StatusOK},
		{"one trailing dot removed", prior, proposed, "https://social.test../acme", enrichers.SearchCaseDifferent, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := []string{}
			timers := []time.Duration{}
			ck := &website.Checker{Client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.String())
				if len(requests) == 1 && tc.landing != "" {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{tc.landing}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, After: func(d time.Duration) <-chan time.Time { timers = append(timers, d); return make(chan time.Time) }}
			e := enrichers.WebsiteSearch{Checker: ck}
			s := enrichers.Subject{Website: tc.website, WebsiteSource: "check", SiteText: &enrichers.SiteTextFound{Run: "old", Status: "failed"}}
			answer := enrichers.WebsiteSearchAnswer{URL: tc.answer, Why: "fixture", Rejected: []enrichers.WebsiteSearchSite{{URL: "https://alternative.test/", Why: "fixture alternative"}}}
			stdout, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			out := prompts.Outcome{ID: "run", Status: enrichers.RunExited, ExitCode: new(int), CostNanos: 250000000, Stdout: string(stdout)}
			r := e.Found(context.Background(), s, out, 99)
			var f enrichers.WebsiteSearchFound
			if err := json.Unmarshal(r.Found, &f); err != nil {
				t.Fatal(err)
			}
			landed := tc.landing
			if landed == "" {
				landed = tc.answer
			}
			wantRequests := []string{tc.answer}
			if tc.landing != "" {
				wantRequests = append(wantRequests, tc.landing)
			}
			state, title, failure, size := sources.WebsiteHTTPS, "Landing", "", int64(len(body))
			if strings.HasPrefix(landed, "http:") {
				state = sources.WebsiteHTTP
			}
			if tc.status != http.StatusOK {
				state, title, failure, size = sources.WebsiteNone, "", website.FailStatus, 0
			}
			want := enrichers.WebsiteSearchFound{
				Run:          enrichers.WebsiteSearchRun{ID: out.ID, Status: out.Status, ExitCode: out.ExitCode, CostNanos: out.CostNanos},
				Answer:       &answer,
				Confirmation: &enrichers.WebsiteSearchConfirmation{State: state, URL: landed, Title: title, StatusCode: tc.status, Size: size, Error: failure},
				Reason:       enrichers.SearchReasonEmptySite,
				Website:      tc.website,
				WebsiteFrom:  "check",
				SiteText:     &enrichers.WebsiteSearchSiteText{Status: s.SiteText.Status, Pages: s.SiteText.Pages},
				Case:         tc.wantCase,
			}
			if tc.website == "" {
				want.Reason, want.WebsiteFrom, want.SiteText = enrichers.SearchReasonNoWebsite, "", nil
			}
			encoded, err := json.Marshal(want)
			if err != nil || !bytes.Equal(r.Found, encoded) || !reflect.DeepEqual(f, want) || r.Source != enrichers.SourcePrompts || r.Cost != 0.25 || !e.Valid(r.Found) || f.Failed() {
				t.Fatalf("found %s, want %s, error %v", r.Found, encoded, err)
			}
			if !slices.Equal(requests, wantRequests) || !slices.Equal(timers, []time.Duration{website.Timeout}) {
				t.Fatal(requests, timers)
			}
		})
	}
}

// R-WOV5-K38P
func TestWebsiteSearchConfirmationCaseValidation(t *testing.T) {
	for _, tc := range []struct {
		name, answer, landed, state, caseWord string
		confirmation, valid                   bool
	}{
		{"https landing stands", "https://other.test/", "https://acme.test/x?ref=fixture", sources.WebsiteHTTPS, enrichers.SearchCaseStands, true, true},
		{"normalized landing stands", "https://other.test/", "https://WWW.Acme.test./x", sources.WebsiteHTTPS, enrichers.SearchCaseStands, true, true},
		{"http landing stands", "https://other.test/", "http://acme.test:8080/x", sources.WebsiteHTTP, enrichers.SearchCaseStands, true, true},
		{"successful landing cannot differ", "https://other.test/", "https://acme.test/", sources.WebsiteHTTPS, enrichers.SearchCaseDifferent, true, false},
		{"failed landing uses answer", "https://other.test/", "https://acme.test/", sources.WebsiteNone, enrichers.SearchCaseDifferent, true, true},
		{"failed landing cannot stand", "https://other.test/", "https://acme.test/", sources.WebsiteNone, enrichers.SearchCaseStands, true, false},
		{"http landing overrides matching answer", "http://acme.test/", "http://other.test/", sources.WebsiteHTTP, enrichers.SearchCaseDifferent, true, true},
		{"http landing cannot keep answer case", "http://acme.test/", "http://other.test/", sources.WebsiteHTTP, enrichers.SearchCaseStands, true, false},
		{"failed landing keeps matching answer", "http://acme.test/", "http://other.test/", sources.WebsiteNone, enrichers.SearchCaseStands, true, true},
		{"no confirmation falls back", "http://acme.test/", "", "", enrichers.SearchCaseStands, false, true},
		{"no confirmation differs", "https://other.test/", "", "", enrichers.SearchCaseDifferent, false, true},
		{"empty successful landing differs", "http://acme.test/", "", sources.WebsiteHTTPS, enrichers.SearchCaseDifferent, true, true},
		{"empty successful landing cannot fall back", "http://acme.test/", "", sources.WebsiteHTTPS, enrichers.SearchCaseStands, true, false},
		{"empty answer has none", "", "", "", enrichers.SearchCaseNone, false, true},
		{"empty answer forbids confirmation", "", "https://acme.test/", sources.WebsiteHTTPS, enrichers.SearchCaseNone, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := enrichers.WebsiteSearchFound{
				Run:         enrichers.WebsiteSearchRun{ID: "run", Status: enrichers.RunExited, ExitCode: new(int)},
				Answer:      &enrichers.WebsiteSearchAnswer{URL: tc.answer, Why: "fixture"},
				Reason:      enrichers.SearchReasonAgain,
				Website:     "https://www.acme.test/home",
				WebsiteFrom: "check",
				Case:        tc.caseWord,
			}
			if tc.confirmation {
				f.Confirmation = &enrichers.WebsiteSearchConfirmation{State: tc.state, URL: tc.landed, StatusCode: 200, Size: 9}
			}
			raw, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []*prompts.Client{nil, {}} {
				for _, c := range []*website.Checker{nil, {}} {
					if got := (enrichers.WebsiteSearch{Prompts: p, Checker: c}).Valid(append(append([]byte(" \n"), raw...), '\t')); got != tc.valid {
						t.Fatalf("valid = %v, want %v for %s", got, tc.valid, raw)
					}
				}
			}
		})
	}
}

// R-WOV5-K38P
func TestWebsiteSearchContextValidation(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	killed := `"run":{"id":"r","status":"killed","cost_nanos":0}`
	exited := `"run":{"id":"r","status":"exited","exit_code":0,"cost_nanos":0},"answer":{"url":"http://acme.test","why":"w","rejected":[]}`
	valid := []string{
		"{" + killed + `,"reason":"no_website"}`,
		"{" + killed + `,"reason":"empty_site","website":"https://acme.test","website_from":"check","site_text":{"status":"failed","pages":0}}`,
		"{" + killed + `,"reason":"again","website":"https://acme.test","website_from":"search","site_text":{"status":"exited","pages":4}}`,
		"{" + killed + `,"reason":"again","website":"https://acme.test","website_from":"check","site_text":{"status":"failed","pages":0}}`,
		"{" + killed + `,"reason":"again","website":"https://acme.test","website_from":"check"}`,
		"{" + exited + `,"reason":"no_website","case":"different"}`,
		"{" + exited + `,"reason":"again","website":"https://www.acme.test/home","website_from":"check","case":"stands"}`,
		strings.Replace("{"+exited+`,"reason":"no_website","case":"none"}`, `"url":"http://acme.test"`, `"url":""`, 1),
	}
	valid = append(valid, "{"+exited+"}")
	for _, raw := range valid {
		if !e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	invalid := []string{
		`"reason":""`, `"reason":"other"`, `"reason":null`, `"case":"none"`, `"website":"https://acme.test"`,
		`"reason":"no_website","case":"different"`,
		`"reason":"no_website","website":"","website_from":"check"`,
		`"reason":"no_website","website":"https://acme.test","website_from":"check"`,
		`"reason":"no_website","website":"https://acme.test"`, `"reason":"no_website","website_from":"check"`,
		`"reason":"no_website","site_text":{"status":"exited","pages":0}`, `"reason":"empty_site"`, `"reason":"again"`,
		`"reason":"again","website":5,"website_from":"check"`, `"reason":"again","website":null,"website_from":"check"`,
		`"reason":"again","website":"u","website_from":"Check"`, `"reason":"again","website":"u","website_from":""`,
		`"reason":"again","website":"u","website_from":"check","site_text":null`,
	}
	for _, extra := range invalid {
		raw := "{" + killed + "," + extra + "}"
		if e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for _, text := range []string{`{"pages":0}`, `{"status":"failed"}`, `{"status":"","pages":0}`, `{"status":"failed","pages":-1}`, `{"status":"failed","pages":"0"}`, `{"status":"failed","pages":0,"x":1}`, `{"status":"failed","pages":0,"Pages":0}`} {
		raw := "{" + killed + `,"reason":"empty_site","website":"u","website_from":"check","site_text":` + text + "}"
		if e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for _, extra := range []string{`"reason":"no_website"`, `"reason":"no_website","case":"stands"`, `"reason":"no_website","case":"none"`, `"case":"different"`, `"reason":"no_website","case":"same"`, `"reason":"no_website","case":""`} {
		raw := "{" + exited + "," + extra + "}"
		if e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for name, raw := range map[string]string{
		"nonempty extraction":         "{" + killed + `,"reason":"empty_site","website":"https://acme.test","website_from":"check","site_text":{"status":"exited","pages":2}}`,
		"different site cannot stand": "{" + exited + `,"reason":"again","website":"https://other.test","website_from":"check","case":"stands"}`,
		"empty answer cannot differ":  strings.Replace("{"+exited+`,"reason":"no_website","case":"different"}`, `"url":"http://acme.test"`, `"url":""`, 1),
		"same site cannot differ":     "{" + exited + `,"reason":"again","website":"https://www.acme.test/home","website_from":"check","case":"different"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if e.Valid(json.RawMessage(raw)) {
				t.Fatal(raw)
			}
		})
	}
	for _, key := range []string{"reason", "website", "website_from", "site_text", "case"} {
		raw := strings.Replace(valid[6], `"`+key+`":`, `"`+strings.ToUpper(key)+`":`, 1)
		if raw != valid[6] && e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
}
