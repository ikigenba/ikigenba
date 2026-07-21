package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/agentkit"

	"wiki/internal/eval"
)

type scriptedChat struct {
	responses []string
	err       error
	requests  []agentkit.Request
}

func (p *scriptedChat) Name() string { return "fake-chat" }

func (p *scriptedChat) RoundTrip(_ context.Context, req *agentkit.Request) *agentkit.RoundTrip {
	p.requests = append(p.requests, *req)
	if p.err != nil {
		return agentkit.NewRoundTrip(agentkit.Message{}, agentkit.FinishOther, agentkit.Usage{}, nil, p.err, 0, false)
	}
	response := p.responses[0]
	if len(p.responses) > 1 {
		p.responses = p.responses[1:]
	}
	message := agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: response}}}
	return agentkit.NewRoundTrip(message, agentkit.FinishStop, agentkit.Usage{}, nil, nil, 0, false)
}

type fakeEmbeddingProvider struct{ calls int }

func (p *fakeEmbeddingProvider) Name() string { return "fake-embed" }

func (p *fakeEmbeddingProvider) Embed(_ context.Context, req *agentkit.EmbedRequest) *agentkit.EmbedRoundTrip {
	p.calls++
	vectors := make([][]float32, len(req.Inputs))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}
	return agentkit.NewEmbedRoundTrip(vectors, agentkit.EmbeddingUsage{}, nil, nil)
}

func TestRunScoresCorpusRetriesAndEchoesOverrides(t *testing.T) {
	// R-KZFK-QZA0
	// R-L0NH-4R0P
	root := seedWorkbench(t)
	chat := &scriptedChat{responses: []string{
		`{"subjects":[{"type":"place","kind":"company","name":"Acme","occurred_at":"","claims":["Acme opened a lab."]}]}`,
		validResponse(),
	}}
	embed := &fakeEmbeddingProvider{}
	out := filepath.Join(root, "scorecard.json")
	args := runArgs(root, out, "-model", "override-model", "-temperature", "1")
	var stderr bytes.Buffer
	code := execute(context.Background(), args, ioDiscard{}, &stderr, fakeDependencies(chat, embed))
	if code != 0 {
		t.Fatalf("execute code = %d, stderr = %q", code, stderr.String())
	}
	if len(chat.requests) != 2 {
		t.Fatalf("chat calls = %d, want invalid response plus retry", len(chat.requests))
	}
	if chat.requests[0].Model != "override-model" || chat.requests[0].Gen.Temperature == nil || *chat.requests[0].Gen.Temperature != 1 {
		t.Fatalf("effective request config = %+v", chat.requests[0])
	}
	var card eval.Scorecard
	readScorecard(t, out, &card)
	if len(card.Cases) != 1 || card.Cases[0].Composite != 1 || card.MeanComposite != 1 {
		t.Fatalf("scorecard = %+v, want D65 perfect-case scores", card)
	}
	if card.Config.Eval.Model != "override-model" || card.Config.Eval.Temperature == nil || *card.Config.Eval.Temperature != 1 {
		t.Fatalf("echoed config = %+v", card.Config.Eval)
	}
	if embed.calls == 0 {
		t.Fatal("fake EmbedFunc provider was not used by scorer")
	}
}

func TestExtractCaseSendsOnlyConfiguredGenerationKnobsAndDefaultsRetries(t *testing.T) {
	// R-XHOE-XC0J
	gold := eval.GoldCase{Name: "case", Document: "Acme opened a lab."}
	temperature, thinking, maxTokens := 0.5, false, 321
	configured := &scriptedChat{responses: []string{validResponse()}}
	_, err := extractCase(context.Background(), configured, eval.EvalCall{
		Model: "configured", Temperature: &temperature, Thinking: &thinking, MaxTokens: &maxTokens,
	}, "Extract JSON.", gold)
	if err != nil {
		t.Fatal(err)
	}
	gen := configured.requests[0].Gen
	if gen.Temperature == nil || *gen.Temperature != temperature || gen.MaxTokens != maxTokens || !gen.Reasoning.Disabled() {
		t.Fatalf("configured generation settings = %+v", gen)
	}

	minimal := &scriptedChat{responses: []string{"not-json", validResponse()}}
	_, err = extractCase(context.Background(), minimal, eval.EvalCall{Model: "minimal"}, "Extract JSON.", gold)
	if err != nil {
		t.Fatal(err)
	}
	if len(minimal.requests) != 2 {
		t.Fatalf("minimal config calls = %d, want one invalid response plus retry", len(minimal.requests))
	}
	for _, request := range minimal.requests {
		if request.Gen.Temperature != nil || request.Gen.MaxTokens != 0 || !request.Gen.Reasoning.IsUnset() {
			t.Fatalf("minimal config sent generation knobs: %+v", request.Gen)
		}
	}
}

func TestRunFailuresNameCauseAndLeaveNoScorecard(t *testing.T) {
	// R-L1VD-IIRE
	tests := []struct {
		name    string
		prepare func(*testing.T, string) []string
		chat    *scriptedChat
		want    string
	}{
		{
			name: "unreadable prompt",
			prepare: func(t *testing.T, root string) []string {
				return runArgs(root, filepath.Join(root, "out.json"), "-prompt", filepath.Join(root, "missing-prompt"))
			},
			chat: &scriptedChat{responses: []string{validResponse()}}, want: "prompt",
		},
		{
			name: "malformed gold",
			prepare: func(t *testing.T, root string) []string {
				if err := os.WriteFile(filepath.Join(root, "gold", "dev", "case", "gold.json"), []byte(`{`), 0o600); err != nil {
					t.Fatal(err)
				}
				return runArgs(root, filepath.Join(root, "out.json"))
			},
			chat: &scriptedChat{responses: []string{validResponse()}}, want: "case",
		},
		{
			name:    "chat exhausts retries",
			prepare: func(t *testing.T, root string) []string { return runArgs(root, filepath.Join(root, "out.json")) },
			chat:    &scriptedChat{responses: []string{`not-json`}}, want: "exhausted retries",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := seedWorkbench(t)
			out := filepath.Join(root, "out.json")
			var stderr bytes.Buffer
			code := execute(context.Background(), tt.prepare(t, root), ioDiscard{}, &stderr, fakeDependencies(tt.chat, &fakeEmbeddingProvider{}))
			if code == 0 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("code/stderr = %d/%q, want named %q failure", code, stderr.String(), tt.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("scorecard exists after failure: %v", err)
			}
		})
	}
}

func TestRunRepeatRecordsCompositesAndEpsilon(t *testing.T) {
	// R-L339-WAI3
	root := seedWorkbench(t)
	spurious := `{"subjects":[{"type":"entity","kind":"company","name":"Acme","occurred_at":"","claims":["Acme opened a lab."]},{"type":"entity","kind":"company","name":"Other","occurred_at":"","claims":["Other exists."]}]}`
	chat := &scriptedChat{responses: []string{validResponse(), spurious, validResponse()}}
	out := filepath.Join(root, "repeat.json")
	var stderr bytes.Buffer
	code := execute(context.Background(), runArgs(root, out, "-repeat", "3"), ioDiscard{}, &stderr, fakeDependencies(chat, &fakeEmbeddingProvider{}))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var card eval.Scorecard
	readScorecard(t, out, &card)
	if len(card.RunComposites) != 3 || len(chat.requests) != 3 {
		t.Fatalf("run composites/calls = %v/%d", card.RunComposites, len(chat.requests))
	}
	if want := eval.Epsilon(card.RunComposites); math.Abs(card.Epsilon-want) > 0.000001 || card.Epsilon <= 0 {
		t.Fatalf("epsilon = %v, want max-min %v and non-zero", card.Epsilon, want)
	}
}

func TestRunReportsEveryExtractionOnlyToStderr(t *testing.T) {
	// R-ETV6-57CQ
	root := seedWorkbench(t)
	seedGoldCase(t, root, "second", "Beta opened a lab.", "Beta")
	chat := &scriptedChat{responses: []string{validResponse(), validResponse(), validResponse(), validResponse()}}
	out := filepath.Join(root, "progress.json")
	var stdout, stderr bytes.Buffer
	code := execute(context.Background(), runArgs(root, out, "-repeat", "2"), &stdout, &stderr, fakeDependencies(chat, &fakeEmbeddingProvider{}))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("progress lines = %d, want 4: %q", len(lines), stderr.String())
	}
	for _, want := range []string{"case 1/2 repeat 1/2", "case 2/2 repeat 1/2", "case 1/2 repeat 2/2", "case 2/2 repeat 2/2"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %q", want, stderr.String())
		}
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || bytes.Contains(data, []byte("case ")) || bytes.Contains(data, []byte("repeat ")) {
		t.Fatalf("progress leaked to stdout/scorecard: stdout=%q scorecard=%q", stdout.String(), data)
	}
}

func TestBuildChatProviderSupportsCompleteProviderSetAndNamesFailures(t *testing.T) {
	// R-EWAY-WQU4
	providers := map[string]string{
		"anthropic":  "ANTHROPIC_API_KEY",
		"google":     "GEMINI_API_KEY",
		"openai":     "OPENAI_API_KEY",
		"openrouter": "OPENROUTER_API_KEY",
		"zai":        "ZAI_API_KEY",
	}
	for name, env := range providers {
		t.Run(name, func(t *testing.T) {
			provider, err := buildChatProvider(name, func(got string) string {
				if got != env {
					t.Fatalf("environment variable = %q, want %q", got, env)
				}
				return "test-key"
			})
			if err != nil || provider == nil {
				t.Fatalf("provider/error = %v/%v", provider, err)
			}
			_, err = buildChatProvider(name, func(string) string { return "" })
			if err == nil || !strings.Contains(err.Error(), env) {
				t.Fatalf("missing-key error = %v, want %s", err, env)
			}
		})
	}
	if _, err := buildChatProvider("other", func(string) string { return "test-key" }); err == nil || !strings.Contains(err.Error(), "other") {
		t.Fatalf("unsupported-provider error = %v", err)
	}
}

func TestBuildConfiguredChatProviderUsesSubscriptionAuthAndNamesEarlyFailures(t *testing.T) {
	// R-EXIV-AIKT
	root := t.TempDir()
	authPath := filepath.Join(root, "auth.json")
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-test"}}`))
	auth := `{"access_token":"header.` + payload + `.signature","id_token":"header.` + payload + `.signature"}`
	if err := os.WriteFile(authPath, []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := buildConfiguredChatProvider(eval.EvalCall{Provider: "openai", Auth: "sub", AuthFile: authPath}, func(string) string {
		t.Fatal("subscription chat auth must not read an API key")
		return ""
	})
	if err != nil || provider == nil {
		t.Fatalf("subscription provider/error = %v/%v", provider, err)
	}
	for _, tt := range []struct {
		name string
		cfg  eval.EvalCall
		want string
	}{
		{"non-openai", eval.EvalCall{Provider: "anthropic", Auth: "sub", AuthFile: authPath}, "anthropic"},
		{"missing", eval.EvalCall{Provider: "openai", Auth: "sub", AuthFile: filepath.Join(root, "missing.json")}, "missing.json"},
		{"malformed", eval.EvalCall{Provider: "openai", Auth: "sub", AuthFile: filepath.Join(root, "bad.json")}, "bad.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "malformed" {
				if err := os.WriteFile(tt.cfg.AuthFile, []byte(`{`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := buildConfiguredChatProvider(tt.cfg, func(string) string { t.Fatal("failure must precede any API-key read"); return "" })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want path/provider %q", err, tt.want)
			}
		})
	}
}

func TestComparePrintsVerdictAndUsesStrictExitCode(t *testing.T) {
	// R-L4B6-A28S
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline.json")
	writeJSON(t, baseline, eval.Scorecard{MeanComposite: 0.8, Epsilon: 0.02})
	for _, tt := range []struct {
		name    string
		score   float64
		code    int
		verdict string
	}{
		{"above", 0.821, 0, "accept"},
		{"equal", 0.82, 1, "reject"},
		{"below", 0.81, 1, "reject"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := filepath.Join(root, tt.name+".json")
			writeJSON(t, candidate, eval.Scorecard{MeanComposite: tt.score})
			var stdout, stderr bytes.Buffer
			code := execute(context.Background(), []string{"compare", "-candidate", candidate, "-baseline", baseline}, &stdout, &stderr, dependencies{})
			if code != tt.code || strings.TrimSpace(stdout.String()) != tt.verdict || stderr.Len() != 0 {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func fakeDependencies(chat agentkit.Provider, embed agentkit.EmbeddingProvider) dependencies {
	return dependencies{
		chat:   func(eval.EvalCall, func(string) string) (agentkit.Provider, error) { return chat, nil },
		embed:  func(string, func(string) string) (agentkit.EmbeddingProvider, error) { return embed, nil },
		getenv: func(string) string { return "unused" },
	}
}

func seedWorkbench(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, split := range []string{"dev", "holdout"} {
		if err := os.MkdirAll(filepath.Join(root, "gold", split), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	caseDir := filepath.Join(root, "gold", "dev", "case")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "document.txt"), []byte("Acme opened a lab."), 0o600); err != nil {
		t.Fatal(err)
	}
	gold := `{"difficulty":"easy","header":{"source":"test","title":"Acme","tags":[],"received_at":"2026-01-01T00:00:00Z"},"gold":[{"type":"entity","kind":"company","name":"Acme","aliases":[],"occurred_at":"","claims":["Acme opened a lab."]}]}`
	if err := os.WriteFile(filepath.Join(caseDir, "gold.json"), []byte(gold), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `{"eval":{"provider":"anthropic","model":"base-model","temperature":0,"thinking":false,"max_tokens":16384,"max_parse_retries":2},"embedding":{"provider":"openai","model":"embed-model","dimensions":2,"threshold":0.8,"margin":0.03},"weights":{"subject":0.35,"claim":0.5,"field":0.15}}`
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompt.txt"), []byte("Extract JSON."), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func seedGoldCase(t *testing.T, root, name, document, subject string) {
	t.Helper()
	caseDir := filepath.Join(root, "gold", "dev", name)
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "document.txt"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	gold := fmt.Sprintf(`{"difficulty":"easy","header":{"source":"test","title":%q,"tags":[],"received_at":"2026-01-01T00:00:00Z"},"gold":[{"type":"entity","kind":"company","name":%q,"aliases":[],"occurred_at":"","claims":[%q]}]}`, subject, subject, document)
	if err := os.WriteFile(filepath.Join(caseDir, "gold.json"), []byte(gold), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runArgs(root, out string, extra ...string) []string {
	args := []string{"run", "-prompt", filepath.Join(root, "prompt.txt"), "-gold", filepath.Join(root, "gold"), "-config", filepath.Join(root, "config.json"), "-out", out}
	for i := 0; i < len(extra); i += 2 {
		flag := extra[i]
		for j := 0; j+1 < len(args); j++ {
			if args[j] == flag {
				args[j+1] = extra[i+1]
				flag = ""
				break
			}
		}
		if flag != "" {
			args = append(args, flag, extra[i+1])
		}
	}
	return args
}

func validResponse() string {
	return `{"subjects":[{"type":"entity","kind":"company","name":"Acme","occurred_at":"","claims":["Acme opened a lab."]}]}`
}

func readScorecard(t *testing.T, path string, card *eval.Scorecard) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, card); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
