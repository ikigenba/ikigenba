//go:build live

package autotune

import (
	"os"
	"path/filepath"
	"testing"
)

// R-AFK7-HJ0T
func TestCompileScoreLiveJudgeReturnsComposedRubricScore(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Fatal("OPENAI_API_KEY is required for the live compile judge")
	}
	fixture := filepath.Join("compile", "fixtures", "gates")
	got := runScorer(t, "compile", fixture, "clean.json", "")
	if got.Score < 0 || got.Score > 1 {
		t.Fatalf("composed score = %v, want [0,1]", got.Score)
	}
	if got.GateScore != 1 {
		t.Fatalf("clean fixture gate score = %v, want 1", got.GateScore)
	}
	if len(got.Rubric) != 4 {
		t.Fatalf("judge rubric = %#v, want four subscores", got.Rubric)
	}
	for _, name := range []string{"coverage", "factuality", "lead", "organization"} {
		value, ok := got.Rubric[name]
		if !ok || value < 0 || value > 1 {
			t.Fatalf("rubric %s = %v (present %v), want [0,1]", name, value, ok)
		}
	}
	want := 0.60*got.GateScore + 0.40*got.JudgeScore
	if !closeEnough(got.Score, want) {
		t.Fatalf("score = %v, want composed %v", got.Score, want)
	}
}

// R-AGS3-VARI
func TestSynthesisScoreLiveJudgeReturnsComposedRubricScore(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Fatal("OPENAI_API_KEY is required for the live synthesis judge")
	}
	fixture := filepath.Join("synthesis", "fixtures", "gates")
	got := runScorer(t, "synthesis", fixture, "clean.json", "")
	if got.Score < 0 || got.Score > 1 {
		t.Fatalf("composed score = %v, want [0,1]", got.Score)
	}
	if got.GateScore != 1 {
		t.Fatalf("clean fixture gate score = %v, want 1", got.GateScore)
	}
	if len(got.Rubric) != 2 {
		t.Fatalf("judge rubric = %#v, want two subscores", got.Rubric)
	}
	for _, name := range []string{"groundedness", "completeness"} {
		value, ok := got.Rubric[name]
		if !ok || value < 0 || value > 1 {
			t.Fatalf("rubric %s = %v (present %v), want [0,1]", name, value, ok)
		}
	}
	want := 0.60*got.GateScore + 0.40*got.JudgeScore
	if !closeEnough(got.Score, want) {
		t.Fatalf("score = %v, want composed %v", got.Score, want)
	}
}
