package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/agentkit"

	"wiki/internal/eval"
	"wiki/internal/wiki"
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

func TestRunScoresCorpusRetriesAndMatchesAnalysisScorer(t *testing.T) {
	// R-BTBX-RX4F
	root := seedWorkbench(t)
	chat := &scriptedChat{responses: []string{"not-json", validResponse()}}
	embed := &fakeEmbeddingProvider{}
	out := filepath.Join(root, "scorecard.json")
	var stderr bytes.Buffer
	code := execute(context.Background(), runArgs(root, out), ioDiscard{}, &stderr, fakeDependencies(chat, embed))
	if code != 0 {
		t.Fatalf("execute code = %d, stderr = %q", code, stderr.String())
	}
	if len(chat.requests) != 2 || !strings.Contains(chat.requests[1].Messages[0].Blocks[0].(agentkit.TextBlock).Text, "previous response was invalid") {
		t.Fatalf("retry requests = %+v", chat.requests)
	}
	var card eval.AnalysisScorecard
	readScorecard(t, out, &card)
	if len(card.Cases) != 1 || card.MeanComposite != 1 || card.Cases[0].Composite != 1 {
		t.Fatalf("scorecard = %+v", card)
	}
	want, err := eval.ScoreAnalysisCase(context.Background(), eval.AnalysisGoldCase{
		Name: "case", Difficulty: "easy", Gold: analysisValue(),
	}, analysisValue(), func(context.Context, []string) ([][]float32, error) {
		return [][]float32{{1, 0}, {1, 0}}, nil
	}, card.Config)
	if err != nil || card.Cases[0].Composite != want.Composite || embed.calls == 0 {
		t.Fatalf("runner/direct score = %+v/%+v, err=%v embed calls=%d", card.Cases[0], want, err, embed.calls)
	}
}

func TestRunFailuresNameCauseAndLeaveNoScorecard(t *testing.T) {
	// R-BUJU-5OV4
	tests := []struct {
		name string
		edit func(*testing.T, string)
		chat *scriptedChat
		want string
	}{
		{"unreadable prompt", func(t *testing.T, root string) { os.Remove(filepath.Join(root, "prompt.txt")) }, &scriptedChat{responses: []string{validResponse()}}, "prompt"},
		{"malformed gold", func(t *testing.T, root string) {
			mustWrite(t, filepath.Join(root, "gold", "dev", "case", "gold.json"), []byte("{"))
		}, &scriptedChat{responses: []string{validResponse()}}, "case"},
		{"chat exhausts retries", func(*testing.T, string) {}, &scriptedChat{responses: []string{"not-json"}}, "exhausted retries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := seedWorkbench(t)
			tt.edit(t, root)
			out := filepath.Join(root, "out.json")
			var stderr bytes.Buffer
			code := execute(context.Background(), runArgs(root, out), ioDiscard{}, &stderr, fakeDependencies(tt.chat, &fakeEmbeddingProvider{}))
			if code == 0 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("code/stderr = %d/%q, want %q", code, stderr.String(), tt.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("scorecard exists after failure: %v", err)
			}
		})
	}
}

func TestRunReportsEveryAnalysisOnlyToStderr(t *testing.T) {
	// R-BVRQ-JGLT
	root := seedWorkbench(t)
	seedGoldCase(t, root, "second")
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
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || bytes.Contains(data, []byte("case ")) || bytes.Contains(data, []byte("repeat ")) {
		t.Fatalf("progress leaked: stdout=%q scorecard=%q", stdout.String(), data)
	}
}

func TestProviderAuthUsesAliasesSubscriptionAndNamesEarlyFailures(t *testing.T) {
	// R-BWZM-X8CI
	values := map[string]string{"OPENAI_API_KEY": "canonical", "EVAL_OPENAI_API_KEY": "alias"}
	getenv := func(name string) string { return values[name] }
	deps := productionDependencies()
	if provider, err := deps.chat(eval.EvalCall{Provider: "openai", Auth: "key"}, getenv); err != nil || provider == nil {
		t.Fatalf("alias chat provider/error = %v/%v", provider, err)
	}
	if provider, err := deps.embed("openai", getenv); err != nil || provider == nil {
		t.Fatalf("alias embedding provider/error = %v/%v", provider, err)
	}
	if got, err := eval.RequiredKey("OPENAI_API_KEY", getenv); err != nil || got != "alias" {
		t.Fatalf("required key = %q, %v", got, err)
	}
	delete(values, "OPENAI_API_KEY")
	delete(values, "EVAL_OPENAI_API_KEY")
	if _, err := deps.chat(eval.EvalCall{Provider: "openai", Auth: "key"}, getenv); err == nil || !strings.Contains(err.Error(), "EVAL_OPENAI_API_KEY") || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("missing-key error = %v", err)
	}

	root := t.TempDir()
	authPath := filepath.Join(root, "auth.json")
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-test"}}`))
	mustWrite(t, authPath, []byte(`{"access_token":"header.`+payload+`.signature","id_token":"header.`+payload+`.signature"}`))
	if provider, err := deps.chat(eval.EvalCall{Provider: "openai", Auth: "sub", AuthFile: authPath}, func(string) string { t.Fatal("subscription read key"); return "" }); err != nil || provider == nil {
		t.Fatalf("subscription provider/error = %v/%v", provider, err)
	}
	if _, err := deps.chat(eval.EvalCall{Provider: "anthropic", Auth: "sub", AuthFile: authPath}, getenv); err == nil || !strings.Contains(err.Error(), "anthropic") {
		t.Fatalf("non-openai subscription error = %v", err)
	}
}

func TestComparePrintsVerdictAndUsesStrictExitCode(t *testing.T) {
	root := t.TempDir()
	baseline := filepath.Join(root, "baseline.json")
	writeJSON(t, baseline, eval.AnalysisScorecard{MeanComposite: 0.8, Epsilon: 0.02})
	for _, tt := range []struct {
		name    string
		score   float64
		code    int
		verdict string
	}{{"above", .821, 0, "accept"}, {"equal", .82, 1, "reject"}} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := filepath.Join(root, tt.name+".json")
			writeJSON(t, candidate, eval.AnalysisScorecard{MeanComposite: tt.score})
			var stdout, stderr bytes.Buffer
			code := execute(context.Background(), []string{"compare", "-candidate", candidate, "-baseline", baseline}, &stdout, &stderr, dependencies{})
			if code != tt.code || strings.TrimSpace(stdout.String()) != tt.verdict || stderr.Len() != 0 {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func fakeDependencies(chat agentkit.Provider, embed agentkit.EmbeddingProvider) dependencies {
	return dependencies{chat: func(eval.EvalCall, func(string) string) (agentkit.Provider, error) { return chat, nil }, embed: func(string, func(string) string) (agentkit.EmbeddingProvider, error) { return embed, nil }, getenv: func(string) string { return "unused" }}
}

func seedWorkbench(t *testing.T) string {
	root := t.TempDir()
	for _, split := range []string{"dev", "holdout"} {
		if err := os.MkdirAll(filepath.Join(root, "gold", split), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seedGoldCase(t, root, "case")
	mustWrite(t, filepath.Join(root, "config.json"), []byte(`{"eval":{"provider":"anthropic","model":"base-model","max_tokens":16384,"max_parse_retries":2},"embedding":{"provider":"openai","model":"embed-model","dimensions":2,"threshold":0.8,"margin":0.03},"weights":{"sub_queries":0.5,"keywords":0.3,"aliases":0.2}}`))
	mustWrite(t, filepath.Join(root, "prompt.txt"), []byte("Analyze JSON."))
	return root
}

func seedGoldCase(t *testing.T, root, name string) {
	dir := filepath.Join(root, "gold", "dev", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "question.txt"), []byte("Who opened the Tulsa lab?"))
	mustWrite(t, filepath.Join(dir, "gold.json"), []byte(`{"difficulty":"easy","gold":{"sub_queries":["Tulsa lab"],"keywords":["Tulsa"],"aliases":["lab"]}}`))
}

func analysisValue() (out wiki.QueryAnalysis) {
	out.SubQueries = []string{"Tulsa lab"}
	out.Keywords = []string{"Tulsa"}
	out.Aliases = []string{"lab"}
	return
}

func validResponse() string {
	return `{"sub_queries":["Tulsa lab"],"keywords":["Tulsa"],"aliases":["lab"]}`
}

func runArgs(root, out string, extra ...string) []string {
	args := []string{"run", "-prompt", filepath.Join(root, "prompt.txt"), "-gold", filepath.Join(root, "gold"), "-config", filepath.Join(root, "config.json"), "-out", out}
	return append(args, extra...)
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
func readScorecard(t *testing.T, path string, card *eval.AnalysisScorecard) {
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
	mustWrite(t, path, data)
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
