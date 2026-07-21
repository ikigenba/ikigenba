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
	if len(dev) != 1 || dev[0].Name != "meridian-freshcrate-acquisition" || len(dev[0].Gold) != 3 {
		t.Fatalf("unexpected dev cases: %+v", dev)
	}
	if len(holdout) != 1 || holdout[0].Name != "tulsa-lab-opening" || holdout[0].Document == "" {
		t.Fatalf("unexpected holdout cases: %+v", holdout)
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
