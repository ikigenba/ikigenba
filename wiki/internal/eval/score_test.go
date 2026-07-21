package eval

import (
	"context"
	"math"
	"testing"

	"wiki/internal/extract"
)

func TestSubjectPairingUsesAliasesTypeAndOneToOneCounts(t *testing.T) {
	// R-KM0O-JI4D
	gold := GoldCase{Gold: []GoldSubject{
		{Type: "entity", Name: "Acme Incorporated", Aliases: []string{" ACME,   Inc. "}, Claims: []string{"a"}},
		{Type: "entity", Name: "Missing", Claims: []string{"missing claim"}},
		{Type: "event", Name: "Same Name", Claims: []string{"event claim"}},
	}}
	got := []extract.ExtractedSubject{
		{Type: "entity", Name: "acme inc", Claims: []string{"a"}},
		{Type: "entity", Name: "Spurious", Claims: []string{"spurious claim"}},
		{Type: "entity", Name: "Same Name", Claims: []string{"wrong type claim"}},
	}
	score, err := ScoreCase(context.Background(), gold, got, vectorMap(map[string][]float32{"a": {1, 0}}), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if score.Subjects.Matched != 1 || score.Subjects.Missed != 2 || score.Subjects.Spurious != 2 {
		t.Fatalf("subject counts = %+v", score.Subjects)
	}
}

func TestFieldAccuracyUsesNormalizedKindAndExactOccurredAt(t *testing.T) {
	// R-KN8K-X9V2
	gold := GoldCase{Gold: []GoldSubject{{Type: "event", Kind: "Product Launch", Name: "Launch", OccurredAt: "2026-01-01", Claims: []string{"claim"}}}}
	got := []extract.ExtractedSubject{{Type: "event", Kind: "product launch", Name: "Launch", OccurredAt: "2026-01-02", Claims: []string{"claim"}}}
	score, err := ScoreCase(context.Background(), gold, got, vectorMap(map[string][]float32{"claim": {1, 0}}), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if score.FieldCorrect != 1 || score.FieldTotal != 2 || score.FieldAccuracy != 0.5 {
		t.Fatalf("field score = %d/%d (%v)", score.FieldCorrect, score.FieldTotal, score.FieldAccuracy)
	}
	noMatch, err := ScoreCase(context.Background(), gold, nil, nil, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if noMatch.FieldAccuracy != 0 {
		t.Fatalf("no-match field accuracy = %v", noMatch.FieldAccuracy)
	}
}

func TestClaimAlignmentAcceptsThresholdMarginAndDigits(t *testing.T) {
	// R-KOGH-B1LR
	gold := []string{"Revenue reached 40 in 2025", "Unrelated statement"}
	got := []string{"In 2025 revenue was 40", "Different idea"}
	vectors := map[string][]float32{
		gold[0]: {1, 0}, got[0]: {0.99, 0.1},
		gold[1]: {0, 1}, got[1]: {-1, 0},
	}
	matched, err := alignClaims(context.Background(), gold, got, vectorMap(vectors), Embedding{Threshold: 0.8, Margin: 0.03})
	if err != nil {
		t.Fatal(err)
	}
	if matched != 1 {
		t.Fatalf("matched = %d", matched)
	}
}

func TestClaimAlignmentRejectsEveryGuardIndependently(t *testing.T) {
	// R-KPOD-OTCG
	tests := []struct {
		name      string
		gold, got []string
		vectors   map[string][]float32
	}{
		{"below threshold", []string{"gold"}, []string{"got"}, map[string][]float32{"gold": {1, 0}, "got": {0, 1}}},
		{"within margin", []string{"gold"}, []string{"got one", "got two"}, map[string][]float32{"gold": {1, 0}, "got one": {1, 0}, "got two": {0.999, 0.02}}},
		{"disjoint digits", []string{"Revenue was 40"}, []string{"Revenue was 1998"}, map[string][]float32{"Revenue was 40": {1, 0}, "Revenue was 1998": {1, 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := alignClaims(context.Background(), tt.gold, tt.got, vectorMap(tt.vectors), Embedding{Threshold: 0.8, Margin: 0.03})
			if err != nil {
				t.Fatal(err)
			}
			if matched != 0 {
				t.Fatalf("matched = %d", matched)
			}
		})
	}
}

func TestClaimAlignmentUsesEachClaimOnce(t *testing.T) {
	// R-KQWA-2L35
	gold := []string{"best", "second"}
	got := []string{"only"}
	vectors := map[string][]float32{"best": {1, 0}, "second": {0.8, 0.6}, "only": {1, 0}}
	matched, err := alignClaims(context.Background(), gold, got, vectorMap(vectors), Embedding{Threshold: 0.7, Margin: 0.03})
	if err != nil {
		t.Fatal(err)
	}
	if matched != 1 {
		t.Fatalf("one extracted claim matched %d times", matched)
	}
}

func TestRollupMatchesHandComputedCountsAndHandlesEmptyExtraction(t *testing.T) {
	// R-KTC2-U4KJ
	cfg := testConfig()
	gold := GoldCase{Gold: []GoldSubject{
		{Type: "entity", Kind: "org", Name: "A", OccurredAt: "then", Claims: []string{"A claim"}},
		{Type: "entity", Kind: "org", Name: "B", Claims: []string{"B claim"}},
	}}
	got := []extract.ExtractedSubject{
		{Type: "entity", Kind: "ORG", Name: "A", OccurredAt: "wrong", Claims: []string{"A claim"}},
		{Type: "entity", Kind: "org", Name: "C", Claims: []string{"C claim"}},
	}
	score, err := ScoreCase(context.Background(), gold, got, vectorMap(map[string][]float32{"A claim": {1, 0}}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if score.Subjects.Precision != 0.5 || score.Subjects.Recall != 0.5 || score.Subjects.F1 != 0.5 || score.Claims.F1 != 0.5 || score.FieldAccuracy != 0.5 || score.Composite != 0.5 {
		t.Fatalf("unexpected hand score: %+v", score)
	}
	empty, err := ScoreCase(context.Background(), gold, nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Subjects.Recall != 0 || empty.Subjects.F1 != 0 || empty.Claims.Recall != 0 || empty.Claims.F1 != 0 || math.IsNaN(empty.Composite) {
		t.Fatalf("invalid empty score: %+v", empty)
	}
}

func TestScoreCaseRewardsOnlyHonestAgreementOnEmptiness(t *testing.T) {
	// R-ESN9-RFM1
	cfg := testConfig()

	honestEmpty, err := ScoreCase(context.Background(), GoldCase{}, nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if honestEmpty.Subjects.F1 != 1 || honestEmpty.Claims.F1 != 1 || honestEmpty.FieldAccuracy != 1 || honestEmpty.Composite != 1 {
		t.Fatalf("honest-empty score = %+v", honestEmpty)
	}

	spurious := []extract.ExtractedSubject{{Type: "entity", Name: "Invented"}}
	emptyGold, err := ScoreCase(context.Background(), GoldCase{}, spurious, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if emptyGold.Subjects.F1 != 0 {
		t.Fatalf("empty-gold subject score with spurious extraction = %+v", emptyGold.Subjects)
	}

	nonEmptyGold := GoldCase{Gold: []GoldSubject{{Type: "entity", Name: "Missing", Claims: []string{"Missing claim"}}}}
	emptyExtraction, err := ScoreCase(context.Background(), nonEmptyGold, nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if emptyExtraction.Subjects.F1 != 0 || emptyExtraction.Composite != 0 {
		t.Fatalf("non-empty-gold score with empty extraction = %+v", emptyExtraction)
	}
}

func testConfig() Config {
	return Config{Embedding: Embedding{Threshold: 0.8, Margin: 0.03}, Weights: Weights{Subject: 0.35, Claim: 0.5, Field: 0.15}}
}

func vectorMap(vectors map[string][]float32) EmbedFunc {
	return func(_ context.Context, texts []string) ([][]float32, error) {
		result := make([][]float32, len(texts))
		for i, text := range texts {
			result[i] = vectors[text]
		}
		return result, nil
	}
}
