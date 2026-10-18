package enrichers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-LQ1L-4WOO R-M9JZ-98JS
func TestPromptWords(t *testing.T) {
	type word string
	words := []word{enrichers.KindSiteText, enrichers.SourcePrompts, enrichers.RunQueued, enrichers.RunRunning, enrichers.RunExited}
	if !reflect.DeepEqual(words, []word{"site_text", "prompts", "queued", "running", "exited"}) {
		t.Fatal(words)
	}
	for in, want := range map[string]string{"site_text": "site-text", "drive_minutes": "drive-minutes", "_a__b_": "-a--b-", "website": "website", "é_x": "é-x", "": ""} {
		if got := enrichers.PromptName(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}

// R-KZDF-EA3M R-JYLA-O6W0 R-LXCZ-FJ4U R-M28K-YM3M R-M746-HP2E R-M8C2-VGT3
func TestSiteTextClientAndRegistry(t *testing.T) {
	writer := telemetry.New(telemetry.Config{Service: "fixture", Sink: &telemetry.Capture{}, Now: func() time.Time { return time.Unix(100, 0) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 256))})
	defer writer.Shutdown(context.Background(), "fixture")
	client := prompts.NewClient(prompts.ClientConfig{Telemetry: writer})
	for _, c := range []*prompts.Client{nil, client} {
		cfg := enrichers.Config{new(website.Checker), c}
		reg := enrichers.New(cfg)
		e := enrichers.SiteText{c}
		var p enrichers.Prompter = e
		if p.Kind() != enrichers.KindSiteText || !p.Fetched() || p.Ready() != (c != nil) || p.Prompt() != enrichers.PromptName(enrichers.KindSiteText) {
			t.Fatal(p)
		}
		if reg.All()[2].(enrichers.WebsiteCheck).Checker != cfg.Website || reg.All()[3].(enrichers.SiteText).Prompts != c {
			t.Fatal("clients lost")
		}
		if _, ok := any(e).(enrichers.Deriver); ok {
			t.Fatal("deriver")
		}
		if _, ok := any(e).(enrichers.Fetcher); ok {
			t.Fatal("fetcher")
		}
	}
}

// R-LSHD-WG62 R-M5WA-3XBP R-MARV-N0AH
func TestSiteTextInput(t *testing.T) {
	for _, s := range []enrichers.Subject{{}, {ID: "cand_fixture", Name: "Owned Name", City: "Owned City", State: "MN", Website: "HTTPS://WWW.Owned.example/home"}} {
		before, _ := json.Marshal(s)
		e := enrichers.SiteText{}
		want, _ := json.Marshal(enrichers.SiteTextInput{s.ID, s.Name, s.City, s.State, s.Website})
		if e.Inputs(s) != s.Website || !bytes.Equal(e.Input(s), want) {
			t.Fatal(string(e.Input(s)))
		}
		var decoded enrichers.SiteTextInput
		if err := json.Unmarshal(want, &decoded); err != nil || decoded != (enrichers.SiteTextInput{s.ID, s.Name, s.City, s.State, s.Website}) {
			t.Fatal(decoded, err)
		}
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated subject")
		}
		changed := s
		changed.Email = "other@host.example"
		changed.Zip = "55072"
		changed.Minutes = new(int64)
		changed.Links = []enrichers.Link{{Scraper: "fixture"}}
		if !bytes.Equal(e.Input(changed), want) || e.Inputs(changed) != s.Website {
			t.Fatal("unrelated input")
		}
	}
	for _, tc := range []struct {
		v    enrichers.SiteTextInput
		want string
	}{{enrichers.SiteTextInput{}, `{"candidate":"","name":"","city":"","state":"","website":""}`}, {enrichers.SiteTextInput{"c", "n", "ci", "st", "w"}, `{"candidate":"c","name":"n","city":"ci","state":"st","website":"w"}`}} {
		got, _ := json.Marshal(tc.v)
		if string(got) != tc.want {
			t.Fatal(string(got))
		}
	}
}

// R-LTPA-A7WR R-MBZS-0S16
func TestSiteTextFound(t *testing.T) {
	e := enrichers.SiteText{}
	for _, code := range []*int{nil, new(int), func() *int { v := 2; return &v }()} {
		for _, status := range []string{enrichers.RunExited, "failed"} {
			out := prompts.Outcome{ID: "run_fixture", Status: status, ExitCode: code, CostNanos: 1500000000}
			r := e.Found(out, 3)
			f := enrichers.SiteTextFound{out.ID, out.Status, code, 3, out.CostNanos}
			want, _ := json.Marshal(f)
			if r.Source != enrichers.SourcePrompts || r.Cost != 1.5 || !bytes.Equal(r.Found, want) {
				t.Fatal(r)
			}
			var got enrichers.SiteTextFound
			if err := json.Unmarshal(r.Found, &got); err != nil || !reflect.DeepEqual(got, f) {
				t.Fatal(got, err)
			}
			out.Prompt = "different"
			out.Model = "model"
			out.Reason = "reason"
			out.Stdout = "output"
			out.InputTokens = 9
			out.OutputTokens = 8
			out.CachedTokens = 7
			out.ReasoningTokens = 6
			out.Started = time.Unix(10, 0)
			out.Finished = time.Unix(20, 0)
			if !reflect.DeepEqual(e.Found(out, 3), r) {
				t.Fatal("retained unrelated output")
			}
		}
	}
	r := e.Found(prompts.Outcome{ID: "run_fixture", Status: "failed"}, 0)
	if string(r.Found) != `{"run":"run_fixture","status":"failed","pages":0,"cost_nanos":0}` || r.Cost != 0 {
		t.Fatal(r)
	}
	b, _ := json.Marshal(enrichers.SiteTextFound{"run_fixture", "exited", new(int), 3, 1500000000})
	if string(b) != `{"run":"run_fixture","status":"exited","exit_code":0,"pages":3,"cost_nanos":1500000000}` {
		t.Fatal(string(b))
	}
}

// R-MD7O-EJRV
func TestSiteTextValidation(t *testing.T) {
	e := enrichers.SiteText{}
	for _, raw := range []string{`{"run":"r","status":"killed"}`, ` {"run":"r","status":"exited","exit_code":null} `, `{"run":"r","status":"exited","exit_code":0,"pages":3,"cost_nanos":7}`} {
		if !e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{}`, `null`, `[]`, ``, `{"status":"failed"}`, `{"run":"r"}`, `{"run":"","status":"x"}`, `{"run":"r","status":""}`, `{"run":"r","status":"x","pages":-1}`, `{"run":"r","status":"x","pages":"3"}`, `{"run":"r","status":"x","pages":1.5}`, `{"run":"r","status":"x","cost_nanos":-1}`, `{"run":"r","status":"x","exit_code":"0"}`, `{"run":"r","status":"x","x":0}`, `{"run":"r","status":"x"} {}`} {
		if e.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
}

// R-MEFK-SBIK
func TestSiteTextFailed(t *testing.T) {
	for _, status := range []string{enrichers.RunExited, "failed", "killed", "timed_out", enrichers.RunQueued, enrichers.RunRunning, "EXITED", ""} {
		for _, code := range []*int{nil, new(int), func() *int { v := 1; return &v }(), func() *int { v := -1; return &v }()} {
			f := enrichers.SiteTextFound{Status: status, ExitCode: code}
			if got, want := f.Failed(), status != enrichers.RunExited || code == nil || *code != 0; got != want {
				t.Fatal(status, code, got)
			}
		}
	}
}
