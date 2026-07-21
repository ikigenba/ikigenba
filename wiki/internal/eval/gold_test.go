package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadGoldLoadsSplitsAndNamesInvalidCases(t *testing.T) {
	// R-KKSS-5QDO
	dev, holdout, err := LoadGold(filepath.Join("..", "..", "eval", "extract", "gold"))
	if err != nil {
		t.Fatal(err)
	}
	// The corpus grows by operator review outside build phases, so assert the
	// seeded cases are present and every loaded case is well-formed — never a
	// frozen census of the corpus.
	seed := func(cases []GoldCase, name string) *GoldCase {
		for i := range cases {
			if cases[i].Name == name {
				return &cases[i]
			}
		}
		return nil
	}
	if c := seed(dev, "meridian-freshcrate-acquisition"); c == nil || len(c.Gold) != 3 {
		t.Fatalf("seeded dev case missing or malformed: %+v", dev)
	}
	if c := seed(holdout, "tulsa-lab-opening"); c == nil || c.Document == "" {
		t.Fatalf("seeded holdout case missing or malformed: %+v", holdout)
	}
	for _, cases := range [][]GoldCase{dev, holdout} {
		for _, c := range cases {
			if c.Document == "" || len(c.Gold) == 0 {
				t.Fatalf("loaded case %s is empty", c.Name)
			}
		}
	}

	tests := []struct {
		name, document, gold, want string
	}{
		{"missing-document", "", validGoldJSON(), "missing-document"},
		{"bad-json", "text", `{`, "bad-json"},
		{"unknown-type", "text", goldJSON(`{"type":"place","kind":"x","name":"A","claims":["c"]}`), "unknown-type"},
		{"empty-name", "text", goldJSON(`{"type":"entity","kind":"x","name":"","claims":["c"]}`), "empty-name"},
		{"zero-claims", "text", goldJSON(`{"type":"entity","kind":"x","name":"A","claims":[]}`), "zero-claims"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "dev", tt.name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "holdout"), 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.document != "" {
				if err := os.WriteFile(filepath.Join(root, "dev", tt.name, "document.txt"), []byte(tt.document), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "dev", tt.name, "gold.json"), []byte(tt.gold), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := LoadGold(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error naming %q, got %v", tt.want, err)
			}
		})
	}
}

func validGoldJSON() string {
	return goldJSON(`{"type":"entity","kind":"x","name":"A","claims":["c"]}`)
}

func goldJSON(subject string) string {
	return `{"difficulty":"easy","header":{"source":"s","title":"t","tags":[],"received_at":"2026-01-01T00:00:00Z"},"gold":[` + subject + `]}`
}

func TestLoadAnalysisGoldLoadsBothSplitsAndNamesInvalidCases(t *testing.T) {
	// R-BM0J-HAO9
	root := t.TempDir()
	writeAnalysisCase(t, root, "dev", "case-b", "Who bought FreshCrate?", `{"difficulty":"easy","gold":{"sub_queries":["FreshCrate acquisition"],"keywords":["FreshCrate"],"aliases":[]}}`)
	writeAnalysisCase(t, root, "dev", "case-a", "When did it happen?", `{"difficulty":"medium","gold":{"sub_queries":[],"keywords":["date"],"aliases":[]}}`)
	writeAnalysisCase(t, root, "holdout", "case-c", "Where is the lab?", `{"difficulty":"hard","gold":{"sub_queries":["lab location"],"keywords":[],"aliases":["laboratory"]}}`)
	dev, holdout, err := LoadAnalysisGold(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev) != 2 || dev[0].Name != "case-a" || dev[1].Question != "Who bought FreshCrate?" || dev[1].Gold.Keywords[0] != "FreshCrate" {
		t.Fatalf("unexpected dev cases: %+v", dev)
	}
	if len(holdout) != 1 || holdout[0].Name != "case-c" || holdout[0].Difficulty != "hard" || holdout[0].Gold.Aliases[0] != "laboratory" {
		t.Fatalf("unexpected holdout cases: %+v", holdout)
	}

	for name, setup := range map[string]func(string){
		"missing-question": func(root string) {
			dir := filepath.Join(root, "dev", "missing-question")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "gold.json"), []byte(`{"difficulty":"easy","gold":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"bad-json": func(root string) { writeAnalysisCase(t, root, "dev", "bad-json", "question", `{`) },
		"missing-gold": func(root string) {
			writeAnalysisCase(t, root, "dev", "missing-gold", "question", `{"difficulty":"easy"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "holdout"), 0o755); err != nil {
				t.Fatal(err)
			}
			setup(root)
			_, _, err := LoadAnalysisGold(root)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected error naming case %q, got %v", name, err)
			}
		})
	}
}

func writeAnalysisCase(t *testing.T, root, split, name, question, gold string) {
	t.Helper()
	dir := filepath.Join(root, split, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "question.txt"), []byte(question), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gold.json"), []byte(gold), 0o600); err != nil {
		t.Fatal(err)
	}
}
