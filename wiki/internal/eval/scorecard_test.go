package eval

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"wiki/internal/extract"
)

func TestScorecardBytesAreDeterministicWithCachedVectorsAndInputOrder(t *testing.T) {
	// R-KUJZ-7WB8
	calls := 0
	provider := func(_ context.Context, texts []string) ([][]float32, error) {
		calls++
		result := make([][]float32, len(texts))
		for i := range texts {
			result[i] = []float32{1, 0}
		}
		return result, nil
	}
	embed := NewDiskCache(t.TempDir(), "pinned", provider)
	cfg := testConfig()
	gold := GoldCase{Name: "b-case", Gold: []GoldSubject{{Type: "entity", Kind: "org", Name: "A", Claims: []string{"gold"}}}}
	got := []extract.ExtractedSubject{{Type: "entity", Kind: "org", Name: "A", Claims: []string{"got"}}}
	first, err := ScoreCase(context.Background(), gold, got, embed, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ScoreCase(context.Background(), gold, got, embed, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cached repeat provider calls = %d", calls)
	}
	other := first
	other.Name = "a-case"
	a, err := Aggregate([]CaseScore{first, other}, cfg).MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Aggregate([]CaseScore{other, second}, cfg).MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("bytes differ:\n%s\n%s", a, b)
	}
	if !strings.Contains(string(a), `"mean_composite":1.000000`) {
		t.Fatalf("float is not fixed precision: %s", a)
	}
}
