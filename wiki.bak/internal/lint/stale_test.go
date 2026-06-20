package lint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wiki/internal/config"
	"wiki/internal/integrate"
	"wiki/internal/page"
)

// fakeStaleStore records the per-subject repairs lint-stale applies, so a test
// asserts the work without a real DB.
type fakeStaleStore struct {
	subjects []page.StaleSubject
	applied  []page.StaleRepair
	applyErr error
}

func (f *fakeStaleStore) OpenStaleSubjects(context.Context) ([]page.StaleSubject, error) {
	return f.subjects, nil
}
func (f *fakeStaleStore) ApplyStaleRepair(_ context.Context, r page.StaleRepair) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = append(f.applied, r)
	return nil
}

// fakePayloads resolves cited inbox ids to canned bytes (the cited-evidence the
// repair reads). A missing id returns an error so the degrade path is exercised.
type fakePayloads map[string]string

func (f fakePayloads) CitedPayload(_ context.Context, id string) ([]byte, error) {
	v, ok := f[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(v), nil
}

func staleSite() config.CallSite { return config.CallSite{Name: "lint_stale"} }

func oneStaleSubject() []page.StaleSubject {
	return []page.StaleSubject{{
		SubjectID: "01SUBJ",
		Title:     "Initech",
		Body:      "Initech is an independent software company. [01HX0000000000000000000001]",
		Notes: []page.StaleNote{{
			ID:    "01NOTE",
			Note:  "Globex acquired Initech in 2021.",
			Cites: "01HX0000000000000000000002",
		}},
	}}
}

func TestLintStaleRepairsSubject(t *testing.T) {
	mc := &mockCaller{bysite: map[string]string{
		"lint_stale": `{"title":"Initech","body":"Initech is a software company acquired by Globex in 2021. [01HX0000000000000000000001] [01HX0000000000000000000002]","superseded":[],"dispositions":[{"note_id":"01NOTE","status":"repaired"}]}`,
	}}
	fs := &fakeStaleStore{subjects: oneStaleSubject()}
	src := fakePayloads{"01HX0000000000000000000002": "Globex acquired Initech in 2021."}
	j := NewStaleJob(mc, fs, src, staleSite())

	m, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if m == nil || len(m.Subjects) != 0 {
		t.Fatalf("lint-stale returns an empty manifest, got %+v", m)
	}
	if len(fs.applied) != 1 {
		t.Fatalf("want 1 repair, got %d", len(fs.applied))
	}
	r := fs.applied[0]
	if r.SubjectID != "01SUBJ" {
		t.Fatalf("repair subject: %q", r.SubjectID)
	}
	if len(r.Dispositions) != 1 || r.Dispositions[0].NoteID != "01NOTE" || r.Dispositions[0].Status != "repaired" {
		t.Fatalf("per-note disposition recorded: %+v", r.Dispositions)
	}
	if mc.calls[0] != "lint_stale" {
		t.Fatalf("stale call fired: %v", mc.calls)
	}
}

func TestLintStaleCitationGateFailsUndeclaredDrop(t *testing.T) {
	// The repair drops citation ...0001 (on the old body) without declaring it → §6.1.
	mc := &mockCaller{bysite: map[string]string{
		"lint_stale": `{"title":"Initech","body":"Initech was acquired by Globex. [01HX0000000000000000000002]","superseded":[],"dispositions":[{"note_id":"01NOTE","status":"repaired"}]}`,
	}}
	fs := &fakeStaleStore{subjects: oneStaleSubject()}
	j := NewStaleJob(mc, fs, fakePayloads{}, staleSite())
	_, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"})
	if err == nil {
		t.Fatal("an undeclared citation drop must fail the repair (§6.1)")
	}
	if !strings.Contains(err.Error(), "citation preservation") {
		t.Fatalf("want §6.1 gate error, got %v", err)
	}
	if len(fs.applied) != 0 {
		t.Fatal("a failed repair must not apply the transaction")
	}
}

func TestLintStaleCitationGateAllowsDeclaredDrop(t *testing.T) {
	mc := &mockCaller{bysite: map[string]string{
		"lint_stale": `{"title":"Initech","body":"Initech was acquired by Globex. [01HX0000000000000000000002]","superseded":["01HX0000000000000000000001"],"dispositions":[{"note_id":"01NOTE","status":"repaired"}]}`,
	}}
	fs := &fakeStaleStore{subjects: oneStaleSubject()}
	j := NewStaleJob(mc, fs, fakePayloads{}, staleSite())
	if _, err := j.Integrate(context.Background(), integrate.Unit{CausedBy: "run-1"}); err != nil {
		t.Fatalf("a declared superseded drop must pass the gate: %v", err)
	}
	if len(fs.applied) != 1 {
		t.Fatal("declared-drop repair should apply")
	}
}

func TestLintStaleEmptyNotesSkips(t *testing.T) {
	mc := &mockCaller{bysite: map[string]string{}}
	// A subject with no open notes (defensive — the work list normally excludes it).
	fs := &fakeStaleStore{subjects: []page.StaleSubject{{SubjectID: "01SUBJ"}}}
	j := NewStaleJob(mc, fs, fakePayloads{}, staleSite())
	if _, err := j.Integrate(context.Background(), integrate.Unit{}); err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if len(fs.applied) != 0 || len(mc.calls) != 0 {
		t.Fatal("a subject with no open notes makes no call and no write")
	}
}

func TestLintStaleJobName(t *testing.T) {
	j := NewStaleJob(nil, nil, nil, staleSite())
	if j.Job() != StaleJobName || StaleJobName != "lint-stale" {
		t.Fatalf("job name: %q", j.Job())
	}
}

func TestStaleDispositionRejectsBadStatus(t *testing.T) {
	if _, err := ParseStale(`{"title":"x","body":"b","superseded":[],"dispositions":[{"note_id":"n","status":"maybe"}]}`); err == nil {
		t.Fatal("an out-of-set disposition status must be rejected")
	}
}

// --- Standing prompt-default gate (offline, no key — see Prompt-default
// validation). The stale-repair config-default prompt is non-placeholder and
// carries its §6 sections; the parser schema-validates a committed, hand-authored
// fixture into a rewritten page + per-note disposition + superseded list (§6.1). ---

func TestLintStalePromptDefaultGate(t *testing.T) {
	sp := config.DefaultLintStalePrompt
	if strings.Contains(sp, "PLACEHOLDER") {
		t.Fatal("stale-repair prompt is still a placeholder")
	}
	if len(strings.TrimSpace(sp)) < 400 {
		t.Fatal("stale-repair prompt too short to be real")
	}
	for _, s := range []string{"## 1.", "## 2.", "## 3.", "## 4.", "## 5.", "## 6."} {
		if !strings.Contains(sp, s) {
			t.Errorf("stale-repair prompt missing section %q", s)
		}
	}
	sl := strings.ToLower(sp)
	if !strings.Contains(sl, "superseded") {
		t.Error("stale-repair prompt missing the §6.1 superseded obligation")
	}
	if !strings.Contains(sl, "disposition") {
		t.Error("stale-repair prompt missing the per-note disposition obligation")
	}

	raw, err := os.ReadFile(filepath.Join("testdata", "stale_response.json"))
	if err != nil {
		t.Fatalf("read stale fixture: %v", err)
	}
	r, err := ParseStale(string(raw))
	if err != nil {
		t.Fatalf("stale fixture must parse: %v", err)
	}
	if strings.TrimSpace(r.Body) == "" {
		t.Fatal("stale fixture should yield a non-empty body")
	}
	if len(r.Dispositions) != 1 || r.Dispositions[0].Status != "repaired" {
		t.Fatalf("stale fixture should yield one repaired disposition, got %+v", r.Dispositions)
	}
}
