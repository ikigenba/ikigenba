package lint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentkit/provider"

	"wiki/internal/config"
	"wiki/internal/integrate"
	"wiki/internal/page"
)

// mockCaller returns a canned raw response per call-site name, so the unit gate
// runs offline with no LLM (the standing rule: mock every LLM from P6a on).
type mockCaller struct {
	bysite map[string]string
	calls  []string
}

func (m *mockCaller) Structured(_ context.Context, site config.CallSite, _ json.RawMessage, _ []provider.Message) (string, error) {
	m.calls = append(m.calls, site.Name)
	return m.bysite[site.Name], nil
}

// fakeStore records the terminal writes lint-dups makes, so a test asserts which
// arm fired without a real DB.
type fakeStore struct {
	pairs     []page.DupPair
	subjects  map[string]page.DupSubject
	stamped   []string
	dismissed []string
	merged    []page.MergePlan
	mergeErr  error
}

func (f *fakeStore) OpenDupPairs(context.Context) ([]page.DupPair, error) { return f.pairs, nil }
func (f *fakeStore) ReadDupSubject(_ context.Context, id string) (page.DupSubject, error) {
	return f.subjects[id], nil
}
func (f *fakeStore) StampJudged(_ context.Context, a, b string, va, vb int) error {
	f.stamped = append(f.stamped, a+","+b)
	return nil
}
func (f *fakeStore) DismissDup(_ context.Context, a, b, run string) error {
	f.dismissed = append(f.dismissed, a+","+b)
	return nil
}
func (f *fakeStore) MergeSubjects(_ context.Context, m page.MergePlan) error {
	if f.mergeErr != nil {
		return f.mergeErr
	}
	f.merged = append(f.merged, m)
	return nil
}

func sites() (config.CallSite, config.CallSite) {
	return config.CallSite{Name: "lint_dup_judge"}, config.CallSite{Name: "lint_fold"}
}

func twoSubjects() map[string]page.DupSubject {
	return map[string]page.DupSubject{
		"01A": {SubjectID: "01A", Type: "entity", CanonicalName: "Acme",
			Body: "Acme is a maker. [01HX0000000000000000000001]", Version: 1},
		"01B": {SubjectID: "01B", Type: "entity", CanonicalName: "ACME Corp",
			Body: "ACME Corp makes things. [01HX0000000000000000000002]", Version: 1},
	}
}

func TestLintDupsMergeArm(t *testing.T) {
	judge, fold := sites()
	mc := &mockCaller{bysite: map[string]string{
		"lint_dup_judge": `{"verdict":"merge","canonical_name":"Acme Corporation"}`,
		"lint_fold":      `{"title":"Acme Corporation","body":"Acme Corporation makes things. [01HX0000000000000000000001] [01HX0000000000000000000002]","superseded":[]}`,
	}}
	fs := &fakeStore{
		pairs:    []page.DupPair{{SubjectA: "01A", SubjectB: "01B"}},
		subjects: twoSubjects(),
	}
	j := NewDupsJob(mc, fs, judge, fold)
	if _, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"}); err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if len(fs.merged) != 1 {
		t.Fatalf("want 1 merge, got %d", len(fs.merged))
	}
	m := fs.merged[0]
	// Older ULID wins mechanically (01A < 01B).
	if m.Winner != "01A" || m.Loser != "01B" {
		t.Fatalf("older ULID must win: winner=%s loser=%s", m.Winner, m.Loser)
	}
	if m.CanonicalName != "Acme Corporation" {
		t.Fatalf("canonical name from judge: %q", m.CanonicalName)
	}
	if m.Run != "run-1" {
		t.Fatalf("run id stamped: %q", m.Run)
	}
	// Both calls fired (judge then fold).
	if len(mc.calls) != 2 || mc.calls[0] != "lint_dup_judge" || mc.calls[1] != "lint_fold" {
		t.Fatalf("expected judge then fold, got %v", mc.calls)
	}
}

func TestLintDupsDismissArm(t *testing.T) {
	judge, fold := sites()
	mc := &mockCaller{bysite: map[string]string{
		"lint_dup_judge": `{"verdict":"dismiss"}`,
	}}
	fs := &fakeStore{pairs: []page.DupPair{{SubjectA: "01A", SubjectB: "01B"}}, subjects: twoSubjects()}
	j := NewDupsJob(mc, fs, judge, fold)
	if _, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"}); err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if len(fs.dismissed) != 1 || len(fs.merged) != 0 || len(fs.stamped) != 0 {
		t.Fatalf("dismiss arm: dismissed=%v merged=%v stamped=%v", fs.dismissed, fs.merged, fs.stamped)
	}
	// No fold call on a dismiss.
	for _, c := range mc.calls {
		if c == "lint_fold" {
			t.Fatal("dismiss must not call fold")
		}
	}
}

func TestLintDupsCantTellArm(t *testing.T) {
	judge, fold := sites()
	mc := &mockCaller{bysite: map[string]string{
		"lint_dup_judge": `{"verdict":"cant_tell"}`,
	}}
	fs := &fakeStore{pairs: []page.DupPair{{SubjectA: "01A", SubjectB: "01B"}}, subjects: twoSubjects()}
	j := NewDupsJob(mc, fs, judge, fold)
	if _, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"}); err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if len(fs.stamped) != 1 || len(fs.merged) != 0 || len(fs.dismissed) != 0 {
		t.Fatalf("cant-tell arm: stamped=%v", fs.stamped)
	}
}

func TestFoldCitationGateFailsUndeclaredDrop(t *testing.T) {
	judge, fold := sites()
	// The fold drops citation ...0002 without declaring it superseded → §6.1 fail.
	mc2 := &mockCaller{bysite: map[string]string{
		"lint_dup_judge": `{"verdict":"merge","canonical_name":"Acme Corporation"}`,
		"lint_fold":      `{"title":"Acme","body":"Acme makes things. [01HX0000000000000000000001]","superseded":[]}`,
	}}
	fs := &fakeStore{pairs: []page.DupPair{{SubjectA: "01A", SubjectB: "01B"}}, subjects: twoSubjects()}
	j := NewDupsJob(mc2, fs, judge, fold)
	_, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"})
	if err == nil {
		t.Fatal("undeclared citation drop must fail the fold (§6.1)")
	}
	if !strings.Contains(err.Error(), "citation preservation") {
		t.Fatalf("want §6.1 gate error, got %v", err)
	}
	if len(fs.merged) != 0 {
		t.Fatal("a failed fold must not apply the merge transaction")
	}
}

func TestFoldCitationGateAllowsDeclaredDrop(t *testing.T) {
	judge, fold := sites()
	mc := &mockCaller{bysite: map[string]string{
		"lint_dup_judge": `{"verdict":"merge","canonical_name":"Acme Corporation"}`,
		"lint_fold":      `{"title":"Acme","body":"Acme makes things. [01HX0000000000000000000001]","superseded":["01HX0000000000000000000002"]}`,
	}}
	fs := &fakeStore{pairs: []page.DupPair{{SubjectA: "01A", SubjectB: "01B"}}, subjects: twoSubjects()}
	j := NewDupsJob(mc, fs, judge, fold)
	if _, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"}); err != nil {
		t.Fatalf("a declared superseded drop must pass the gate: %v", err)
	}
	if len(fs.merged) != 1 {
		t.Fatal("declared-drop fold should apply the merge")
	}
}

// --- Standing prompt-default gate (offline, no key — see Prompt-default
// validation). The dup-judge and fold config-default prompts are non-placeholder
// and carry their §6 sections; the parsers schema-validate committed,
// hand-authored fixtures into the ternary verdict and the body+superseded list. ---

func TestLintPromptDefaultGate(t *testing.T) {
	jp := config.DefaultLintDupJudgePrompt
	fp := config.DefaultLintFoldPrompt
	if strings.Contains(jp, "PLACEHOLDER") || strings.Contains(fp, "PLACEHOLDER") {
		t.Fatal("lint prompt is still a placeholder")
	}
	if len(strings.TrimSpace(jp)) < 400 || len(strings.TrimSpace(fp)) < 400 {
		t.Fatal("lint prompt too short to be real")
	}
	for _, s := range []string{"## 1.", "## 2.", "## 3.", "## 4.", "## 5."} {
		if !strings.Contains(jp, s) {
			t.Errorf("dup-judge prompt missing section %q", s)
		}
		if !strings.Contains(fp, s) {
			t.Errorf("fold prompt missing section %q", s)
		}
	}
	// Load-bearing cross-prompt invariants.
	jl := strings.ToLower(jp)
	if !strings.Contains(jl, "merge") || !strings.Contains(jl, "dismiss") || !strings.Contains(jl, "cant_tell") {
		t.Error("dup-judge prompt missing the ternary verdict vocabulary")
	}
	if !strings.Contains(jl, "canonical") {
		t.Error("dup-judge prompt missing the canonical-name pick")
	}
	if !strings.Contains(strings.ToLower(fp), "superseded") {
		t.Error("fold prompt missing the §6.1 superseded obligation")
	}

	// Parsers schema-validate committed fixtures.
	jraw, err := os.ReadFile(filepath.Join("testdata", "judge_response.json"))
	if err != nil {
		t.Fatalf("read judge fixture: %v", err)
	}
	jr, err := ParseJudge(string(jraw))
	if err != nil {
		t.Fatalf("judge fixture must parse: %v", err)
	}
	if jr.Verdict != VerdictMerge || jr.CanonicalName == "" {
		t.Fatalf("judge fixture should yield merge + canonical name, got %+v", jr)
	}
	fraw, err := os.ReadFile(filepath.Join("testdata", "fold_response.json"))
	if err != nil {
		t.Fatalf("read fold fixture: %v", err)
	}
	fr, err := ParseFold(string(fraw))
	if err != nil {
		t.Fatalf("fold fixture must parse: %v", err)
	}
	if strings.TrimSpace(fr.Body) == "" {
		t.Fatal("fold fixture should yield a non-empty body")
	}
}

// --- Eval hook (obligation 3): the dup judge's ternary verdict is preserved
// verbatim (never collapsed to a binary), and the canonical-name pick is a field of
// the judge output (not a separate call). ---

func TestJudgeTernaryPreserved(t *testing.T) {
	for _, want := range []Verdict{VerdictMerge, VerdictDismiss, VerdictCantTell} {
		raw := `{"verdict":"` + string(want) + `","canonical_name":"X"}`
		r, err := ParseJudge(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", want, err)
		}
		if r.Verdict != want {
			t.Fatalf("verdict collapsed: want %s got %s", want, r.Verdict)
		}
	}
	if _, err := ParseJudge(`{"verdict":"maybe"}`); err == nil {
		t.Fatal("an out-of-set verdict must be rejected")
	}
	if _, err := ParseJudge(`{"verdict":"merge"}`); err == nil {
		t.Fatal("a merge verdict with no canonical_name must be rejected")
	}
}
