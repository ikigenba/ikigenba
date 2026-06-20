package extract

import (
	"context"
	"strings"
	"testing"
	"time"

	agentkit "github.com/ikigenba/agentkit"

	"wiki/internal/llm"
)

func TestExtractRendersDocumentHeaderAndReturnsSubjects(t *testing.T) {
	// R-VYU0-BPAX
	prov := &scriptedProvider{responses: []string{`{
		"subjects": [
			{
				"type": "entity",
				"kind": "company",
				"name": "Acme Robotics",
				"occurred_at": "",
				"claims": ["Acme Robotics opened a research lab in Tulsa."]
			}
		]
	}`}}
	extractor := New(llm.New(prov, nil), llm.CallSite{Model: "extract-model", System: "extract system"})
	header := DocumentHeader{
		Source:     "mcp:ingest_text",
		Title:      "Tulsa robotics notes",
		Tags:       []string{"robotics", "tulsa"},
		ReceivedAt: time.Date(2026, 6, 20, 19, 45, 0, 0, time.FixedZone("CDT", -5*60*60)),
	}

	got, err := extractor.Extract(context.Background(), header, "Acme Robotics opened a research lab in Tulsa.")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("subjects len = %d, want 1", len(got))
	}
	if got[0].Type != "entity" || got[0].Kind != "company" || got[0].Name != "Acme Robotics" {
		t.Fatalf("subject = %#v, want decoded extracted entity", got[0])
	}
	if len(got[0].Claims) != 1 || got[0].Claims[0] != "Acme Robotics opened a research lab in Tulsa." {
		t.Fatalf("claims = %#v, want decoded claims", got[0].Claims)
	}

	prompt := onlyPrompt(t, prov)
	for _, want := range []string{
		"source: mcp:ingest_text",
		"title: Tulsa robotics notes",
		"tags: robotics, tulsa",
		"received on: 2026-06-20",
		"Acme Robotics opened a research lab in Tulsa.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt %q does not contain %q", prompt, want)
		}
	}
	if strings.Contains(strings.ToLower(prompt), "today is") {
		t.Fatalf("prompt = %q, want received date rendered without relative today wording", prompt)
	}
}

func TestExtractRejectsInvalidSubjectTypesAndEmptyClaims(t *testing.T) {
	// R-W01W-PH1M
	tests := []struct {
		name     string
		response string
		wantErr  string
	}{
		{
			name: "invalid type",
			response: `{"subjects":[{
				"type":"place",
				"kind":"city",
				"name":"Tulsa",
				"occurred_at":"",
				"claims":["Tulsa hosted the meeting."]
			}]}`,
			wantErr: "type",
		},
		{
			name: "empty claims",
			response: `{"subjects":[{
				"type":"concept",
				"kind":"method",
				"name":"retrieval augmented generation",
				"occurred_at":"",
				"claims":[]
			}]}`,
			wantErr: "claims required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &scriptedProvider{responses: []string{tt.response}}
			extractor := New(llm.New(prov, nil), llm.CallSite{Model: "extract-model"})

			got, err := extractor.Extract(context.Background(), validHeader(), "source text")
			if err == nil {
				t.Fatal("Extract returned nil error")
			}
			if got != nil {
				t.Fatalf("subjects = %#v, want nil on invalid response", got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestExtractValidatesEventOccurredAtOnly(t *testing.T) {
	// R-W19T-38SB
	tests := []struct {
		name      string
		subject   string
		wantError bool
	}{
		{
			name: "event accepts ISO year month day prefix",
			subject: `{
				"type":"event",
				"kind":"launch",
				"name":"Acme Robotics lab opening",
				"occurred_at":"2026-06-20",
				"claims":["Acme Robotics opened a research lab on June 20, 2026."]
			}`,
		},
		{
			name: "event rejects non ISO prefix",
			subject: `{
				"type":"event",
				"kind":"launch",
				"name":"Acme Robotics lab opening",
				"occurred_at":"June 20, 2026",
				"claims":["Acme Robotics opened a research lab on June 20, 2026."]
			}`,
			wantError: true,
		},
		{
			name: "entity rejects occurred at",
			subject: `{
				"type":"entity",
				"kind":"company",
				"name":"Acme Robotics",
				"occurred_at":"2026",
				"claims":["Acme Robotics opened a research lab."]
			}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &scriptedProvider{responses: []string{`{"subjects":[` + tt.subject + `]}`}}
			extractor := New(llm.New(prov, nil), llm.CallSite{Model: "extract-model"})

			got, err := extractor.Extract(context.Background(), validHeader(), "Acme Robotics opened a research lab.")
			if tt.wantError {
				if err == nil {
					t.Fatal("Extract returned nil error")
				}
				if got != nil {
					t.Fatalf("subjects = %#v, want nil on invalid occurred_at", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			if len(got) != 1 || got[0].OccurredAt != "2026-06-20" {
				t.Fatalf("subjects = %#v, want event with ISO occurred_at", got)
			}
		})
	}
}

func TestExtractUsesInjectedLLMCallSiteWithoutTools(t *testing.T) {
	// R-W2HP-H0J0
	temp := 0.0
	prov := &scriptedProvider{responses: []string{`{"subjects":[{
		"type":"concept",
		"kind":"method",
		"name":"claim extraction",
		"occurred_at":"",
		"claims":["Claim extraction turns source text into self-contained statements."]
	}]}`}}
	site := llm.CallSite{
		Model:       "extract-model",
		Temperature: &temp,
		Reasoning:   agentkit.DisableReasoning(),
		System:      "extract subjects only",
	}
	extractor := New(llm.New(prov, nil), site)

	if _, err := extractor.Extract(context.Background(), validHeader(), "Claim extraction turns source text into statements."); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests len = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.Model != site.Model || req.System != site.System {
		t.Fatalf("request config = %#v, want injected call site model and system", req)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("request tools len = %d, want tool-less extract generation", len(req.Tools))
	}
	if req.Gen.Temperature == nil || *req.Gen.Temperature != temp || !req.Gen.Reasoning.Disabled() {
		t.Fatalf("gen settings = %#v, want injected temperature and disabled reasoning", req.Gen)
	}
}

func validHeader() DocumentHeader {
	return DocumentHeader{
		Source:     "mcp:ingest_text",
		Title:      "Source",
		Tags:       []string{"tag"},
		ReceivedAt: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
	}
}

func onlyPrompt(t *testing.T, prov *scriptedProvider) string {
	t.Helper()
	if len(prov.requests) != 1 {
		t.Fatalf("requests len = %d, want 1", len(prov.requests))
	}
	texts := requestTexts(prov.requests[0])
	if len(texts) != 1 {
		t.Fatalf("request texts = %#v, want one user prompt", texts)
	}
	return texts[0]
}

type scriptedProvider struct {
	responses []string
	requests  []agentkit.Request
}

func (p *scriptedProvider) RoundTrip(ctx context.Context, req *agentkit.Request) *agentkit.RoundTrip {
	p.requests = append(p.requests, cloneRequest(req))
	text := `{"subjects":[]}`
	if len(p.responses) > 0 {
		text = p.responses[0]
		p.responses = p.responses[1:]
	}
	return agentkit.NewRoundTrip(
		agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: text}}},
		agentkit.FinishStop,
		agentkit.Usage{InputUncached: 1, Output: 1, Total: 2},
		nil,
		nil,
	)
}

func (p *scriptedProvider) Name() string {
	return "scripted"
}

func (p *scriptedProvider) Pricing(string) (agentkit.Pricing, bool) {
	return agentkit.Pricing{Tiers: []agentkit.RateTier{{MinInputTokens: 0}}}, true
}

func cloneRequest(req *agentkit.Request) agentkit.Request {
	if req == nil {
		return agentkit.Request{}
	}
	return agentkit.Request{
		Model:    req.Model,
		System:   req.System,
		Messages: append([]agentkit.Message(nil), req.Messages...),
		Tools:    append([]agentkit.Tool(nil), req.Tools...),
		Gen:      req.Gen,
	}
}

func requestTexts(req agentkit.Request) []string {
	var out []string
	for _, msg := range req.Messages {
		var b strings.Builder
		for _, block := range msg.Blocks {
			if text, ok := block.(agentkit.TextBlock); ok {
				b.WriteString(text.Text)
			}
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	return out
}
