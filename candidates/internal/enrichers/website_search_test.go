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

// R-6NS5-A4OX R-I3S3-D7R8 R-IER6-T5FH R-IH6Z-KOWV R-IKUO-Q04Y
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

// R-I67W-4R8M R-I7FS-IIZB R-SDQ1-5H81 R-SEXX-J8YQ R-SG5T-X0PF R-I9VL-A2GP
func TestWebsiteSearchShapes(t *testing.T) {
	zero := 0
	input := enrichers.WebsiteSearchInput{"c", "n", "ci", "st", "street", "zip", "phone", "domain", "check", "error", []enrichers.BriefPerson{{"person", "role"}}, []enrichers.Category{{"cat", "origin"}}}
	run := enrichers.WebsiteSearchRun{"r", "status", &zero, 3}
	answer := enrichers.WebsiteSearchAnswer{"u", "w", []enrichers.WebsiteSearchSite{{"r", "why"}}}
	confirm := enrichers.WebsiteSearchConfirmation{"s", "u", "t", 200, 10, "e"}
	found := enrichers.WebsiteSearchFound{run, &answer, &confirm}
	cases := []struct {
		v    any
		want string
		dst  any
	}{
		{input, `{"candidate":"c","name":"n","city":"ci","state":"st","street":"street","zip":"zip","phone":"phone","domain":"domain","domain_check":"check","domain_error":"error","people":[{"name":"person","role":"role"}],"categories":[{"name":"cat","source":"origin"}]}`, &enrichers.WebsiteSearchInput{}},
		{enrichers.WebsiteSearchInput{}, `{"candidate":"","name":"","city":"","state":"","street":"","zip":"","phone":"","domain":"","domain_check":"","domain_error":"","people":[],"categories":[]}`, &enrichers.WebsiteSearchInput{}},
		{run, `{"id":"r","status":"status","exit_code":0,"cost_nanos":3}`, &enrichers.WebsiteSearchRun{}},
		{enrichers.WebsiteSearchRun{}, `{"id":"","status":"","cost_nanos":0}`, &enrichers.WebsiteSearchRun{}},
		{answer.Rejected[0], `{"url":"r","why":"why"}`, &enrichers.WebsiteSearchSite{}},
		{enrichers.WebsiteSearchSite{}, `{"url":"","why":""}`, &enrichers.WebsiteSearchSite{}},
		{answer, `{"url":"u","why":"w","rejected":[{"url":"r","why":"why"}]}`, &enrichers.WebsiteSearchAnswer{}},
		{enrichers.WebsiteSearchAnswer{}, `{"url":"","why":"","rejected":[]}`, &enrichers.WebsiteSearchAnswer{}},
		{confirm, `{"state":"s","url":"u","title":"t","status_code":200,"size":10,"error":"e"}`, &enrichers.WebsiteSearchConfirmation{}},
		{enrichers.WebsiteSearchConfirmation{}, `{"state":"","url":"","title":"","status_code":0,"size":0,"error":""}`, &enrichers.WebsiteSearchConfirmation{}},
		{found, `{"run":{"id":"r","status":"status","exit_code":0,"cost_nanos":3},"answer":{"url":"u","why":"w","rejected":[{"url":"r","why":"why"}]},"confirmation":{"state":"s","url":"u","title":"t","status_code":200,"size":10,"error":"e"}}`, &enrichers.WebsiteSearchFound{}},
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

// R-MXG4-Y30D R-FCCM-4RKX
func TestWebsiteSearchInputs(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	s := enrichers.Subject{ID: "candidate", Name: "Acme", City: "Sandstone", State: "MN", Street: "Main", Zip: "zip", Phone: "phone", Email: "Ops@Acme.test", Links: []enrichers.Link{{Reading: sources.Reading{People: []sources.Person{{Name: "Ann", Role: "owner"}, {Name: "Bob"}, {Name: "Ann", Role: "owner"}}}}, {Gone: true, Reading: sources.Reading{People: []sources.Person{{Name: "Gone"}}}}}, Facts: &enrichers.LinkFound{Categories: []enrichers.Category{{Name: "omit", Source: enrichers.KindBrief}, {Name: "keep", Source: "source"}}}}
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
	expected := enrichers.WebsiteSearchInput{s.ID, s.Name, s.City, s.State, s.Street, s.Zip, s.Phone, "acme.test", sources.WebsiteUnknown, "", []enrichers.BriefPerson{{"Ann", "owner"}, {"Bob", ""}}, []enrichers.Category{{"keep", "source"}}}
	if !reflect.DeepEqual(input, expected) {
		t.Fatal(input)
	}
	s.Check = &website.Result{State: sources.WebsiteNone, Error: website.FailStatus}
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
	s.Check = nil
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

// R-ITDZ-EEBT R-IX1O-JPJW R-IY9K-XHAL R-J0PD-P0RZ
func TestWebsiteSearchFoundAndFailure(t *testing.T) {
	e := enrichers.WebsiteSearch{}
	valid := `{"url":"https://acme.test/","why":"match","rejected":[{"url":"https://other.test/","why":"different","x":1},{"url":"https://third.test/","why":"another"}],"x":2}`
	invalid := []string{"", "bad", "[]", `{"url":"x","why":"y","rejected":[]} {}`, `{"url":"x","why":"y"}`, `{"url":"x","why":"y","rejected":null}`, `{"url":5,"why":"y","rejected":[]}`, `{"URL":"x","why":"y","rejected":[]}`, `{"url":"x","why":"y","rejected":[{"url":"z"}]}`, `{"url":"x","why":"y","rejected":[5]}`, `{"url":null,"why":"y","rejected":[]}`}
	for _, stdout := range append([]string{valid, `{"url":"","why":"none","rejected":[]}`}, invalid...) {
		for _, status := range []string{enrichers.RunExited, "failed", "killed", "timed_out", enrichers.RunRunning, "EXITED", ""} {
			for _, code := range []*int{nil, new(int), func() *int { v := 1; return &v }()} {
				out := prompts.Outcome{ID: "run", Status: status, ExitCode: code, CostNanos: 250000000, Stdout: stdout}
				r := e.Found(context.Background(), out, 99)
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
				if !reflect.DeepEqual(r, e.Found(context.Background(), out, -1)) {
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
	v := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: new(int)}, Answer: &enrichers.WebsiteSearchAnswer{}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone}}
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

// R-IVTS-5XT7
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

// R-IULV-S62I
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
		r := e.Found(context.Background(), out, 0)
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
			r = e.Found(context.Background(), o, 0)
			_ = json.Unmarshal(r.Found, &f)
			var decoded enrichers.WebsiteSearchFound
			_ = json.Unmarshal(r.Found, &decoded)
			if decoded.Confirmation != nil || len(requests) != 0 || len(timers) != 0 {
				t.Fatal(string(r.Found), requests, timers)
			}
		}
	}
}

// R-IULV-S62I
func TestWebsiteSearchConfirmationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ck := &website.Checker{Client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
	r := (enrichers.WebsiteSearch{Checker: ck}).Found(ctx, prompts.Outcome{ID: "run", Status: enrichers.RunExited, ExitCode: new(int), Stdout: `{"url":"https://acme.test/","why":"match","rejected":[]}`}, 0)
	var f enrichers.WebsiteSearchFound
	if err := json.Unmarshal(r.Found, &f); err != nil || f.Confirmation == nil || f.Confirmation.State != sources.WebsiteNone || f.Confirmation.Error == "" {
		t.Fatal(string(r.Found), err)
	}
}

// R-INAH-HJMC R-IPQA-933Q R-IB3H-NU7E
func TestOtherPrompterContextsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := prompts.Outcome{ID: "run", Status: enrichers.RunExited, ExitCode: new(int), CostNanos: 12, Stdout: `{"summary":"owned","categories":[],"labels":[]}`}
	for _, p := range []enrichers.Prompter{enrichers.SiteText{}, enrichers.Brief{}} {
		if !reflect.DeepEqual(p.Found(ctx, out, 3), p.Found(context.Background(), out, 3)) {
			t.Fatal(p.Kind())
		}
	}
}
