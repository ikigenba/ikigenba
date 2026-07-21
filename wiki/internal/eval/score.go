package eval

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"

	extractprompt "wiki/eval/extract"
	"wiki/internal/extract"
)

type EmbedFunc func(ctx context.Context, texts []string) ([][]float32, error)

type Metrics struct {
	Matched   int     `json:"matched"`
	Missed    int     `json:"missed"`
	Spurious  int     `json:"spurious"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

type CaseScore struct {
	Name          string  `json:"name"`
	Difficulty    string  `json:"difficulty"`
	Subjects      Metrics `json:"subjects"`
	Claims        Metrics `json:"claims"`
	FieldCorrect  int     `json:"field_correct"`
	FieldTotal    int     `json:"field_total"`
	FieldAccuracy float64 `json:"field_accuracy"`
	Composite     float64 `json:"composite"`
	PromptSHA256  string  `json:"prompt_sha256"`
}

type subjectPair struct{ gold, got int }

func ScoreCase(ctx context.Context, gold GoldCase, got []extract.ExtractedSubject, embed EmbedFunc, cfg Config) (CaseScore, error) {
	pairs, unmatchedGold, unmatchedGot := pairSubjects(gold.Gold, got)
	score := CaseScore{
		Name: gold.Name, Difficulty: gold.Difficulty,
		Subjects:     metrics(len(pairs), unmatchedGold, unmatchedGot),
		FieldTotal:   2 * len(pairs),
		PromptSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(extractprompt.Instructions))),
	}
	claimMatched, claimMissed, claimSpurious := 0, 0, 0
	for _, pair := range pairs {
		goldSubject, gotSubject := gold.Gold[pair.gold], got[pair.got]
		if normalize(goldSubject.Kind) == normalize(gotSubject.Kind) {
			score.FieldCorrect++
		}
		if goldSubject.OccurredAt == gotSubject.OccurredAt {
			score.FieldCorrect++
		}
		matched, err := alignClaims(ctx, goldSubject.Claims, gotSubject.Claims, embed, cfg.Embedding)
		if err != nil {
			return CaseScore{}, fmt.Errorf("score case %s subject %q claims: %w", gold.Name, goldSubject.Name, err)
		}
		claimMatched += matched
		claimMissed += len(goldSubject.Claims) - matched
		claimSpurious += len(gotSubject.Claims) - matched
	}
	for i, subject := range gold.Gold {
		if !hasGoldPair(pairs, i) {
			claimMissed += len(subject.Claims)
		}
	}
	for i, subject := range got {
		if !hasGotPair(pairs, i) {
			claimSpurious += len(subject.Claims)
		}
	}
	score.Claims = metrics(claimMatched, claimMissed, claimSpurious)
	if score.FieldTotal != 0 {
		score.FieldAccuracy = float64(score.FieldCorrect) / float64(score.FieldTotal)
	}
	score.Composite = cfg.Weights.Subject*score.Subjects.F1 + cfg.Weights.Claim*score.Claims.F1 + cfg.Weights.Field*score.FieldAccuracy
	return score, nil
}

func pairSubjects(gold []GoldSubject, got []extract.ExtractedSubject) ([]subjectPair, int, int) {
	used := make([]bool, len(got))
	var pairs []subjectPair
	for gi, want := range gold {
		names := append([]string{want.Name}, want.Aliases...)
		for ei, actual := range got {
			if used[ei] || actual.Type != want.Type || !matchesAnyName(actual.Name, names) {
				continue
			}
			pairs = append(pairs, subjectPair{gi, ei})
			used[ei] = true
			break
		}
	}
	return pairs, len(gold) - len(pairs), len(got) - len(pairs)
}

func matchesAnyName(name string, candidates []string) bool {
	name = normalize(name)
	for _, candidate := range candidates {
		if name == normalize(candidate) {
			return true
		}
	}
	return false
}

func normalize(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

type claimCandidate struct {
	gold, got int
	sim       float64
}

func alignClaims(ctx context.Context, gold, got []string, embed EmbedFunc, cfg Embedding) (int, error) {
	if len(gold) == 0 || len(got) == 0 {
		return 0, nil
	}
	texts := append(append([]string{}, gold...), got...)
	vectors, err := embed(ctx, texts)
	if err != nil {
		return 0, err
	}
	if len(vectors) != len(texts) {
		return 0, fmt.Errorf("embed returned %d vectors for %d texts", len(vectors), len(texts))
	}
	sims := make([][]float64, len(gold))
	var candidates []claimCandidate
	for gi := range gold {
		sims[gi] = make([]float64, len(got))
		for ei := range got {
			sim, err := cosine(vectors[gi], vectors[len(gold)+ei])
			if err != nil {
				return 0, err
			}
			sims[gi][ei] = sim
			candidates = append(candidates, claimCandidate{gi, ei, sim})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].sim != candidates[j].sim {
			return candidates[i].sim > candidates[j].sim
		}
		if candidates[i].gold != candidates[j].gold {
			return candidates[i].gold < candidates[j].gold
		}
		return candidates[i].got < candidates[j].got
	})
	goldUsed, gotUsed := make([]bool, len(gold)), make([]bool, len(got))
	matched := 0
	for _, candidate := range candidates {
		if goldUsed[candidate.gold] || gotUsed[candidate.got] || candidate.sim < cfg.Threshold {
			continue
		}
		if !clearsMargin(sims, candidate, cfg.Margin) || !digitsCompatible(gold[candidate.gold], got[candidate.got]) {
			continue
		}
		goldUsed[candidate.gold], gotUsed[candidate.got] = true, true
		matched++
	}
	return matched, nil
}

func clearsMargin(sims [][]float64, candidate claimCandidate, margin float64) bool {
	goldAlternative, gotAlternative := math.Inf(-1), math.Inf(-1)
	for ei, similarity := range sims[candidate.gold] {
		if ei != candidate.got && similarity > goldAlternative {
			goldAlternative = similarity
		}
	}
	for gi := range sims {
		if gi != candidate.gold && sims[gi][candidate.got] > gotAlternative {
			gotAlternative = sims[gi][candidate.got]
		}
	}
	return candidate.sim-goldAlternative >= margin && candidate.sim-gotAlternative >= margin
}

var digitsPattern = regexp.MustCompile(`[0-9]+`)

func digitsCompatible(a, b string) bool {
	aDigits, bDigits := digitsPattern.FindAllString(a, -1), digitsPattern.FindAllString(b, -1)
	if len(aDigits) == 0 || len(bDigits) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(aDigits))
	for _, digit := range aDigits {
		set[digit] = struct{}{}
	}
	for _, digit := range bDigits {
		if _, ok := set[digit]; ok {
			return true
		}
	}
	return false
}

func cosine(a, b []float32) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, fmt.Errorf("embedding vector dimensions differ or are empty")
	}
	var dot, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0, nil
	}
	return dot / math.Sqrt(aa*bb), nil
}

func metrics(matched, missed, spurious int) Metrics {
	m := Metrics{Matched: matched, Missed: missed, Spurious: spurious}
	if matched+spurious > 0 {
		m.Precision = float64(matched) / float64(matched+spurious)
	}
	if matched+missed > 0 {
		m.Recall = float64(matched) / float64(matched+missed)
	}
	if m.Precision+m.Recall > 0 {
		m.F1 = 2 * m.Precision * m.Recall / (m.Precision + m.Recall)
	}
	return m
}

func hasGoldPair(pairs []subjectPair, index int) bool {
	for _, pair := range pairs {
		if pair.gold == index {
			return true
		}
	}
	return false
}

func hasGotPair(pairs []subjectPair, index int) bool {
	for _, pair := range pairs {
		if pair.got == index {
			return true
		}
	}
	return false
}
