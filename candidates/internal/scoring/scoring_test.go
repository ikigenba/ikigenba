package scoring_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func ptr(n int64) *int64 { return &n }

// R-PDA4-YXZ2
func TestCombinedShape(t *testing.T) {
	type shape struct {
		Low        int64
		High       *int64
		Confidence float64
	}
	if shape(scoring.Combined{}) != (shape{}) {
		t.Fatal("zero combination")
	}
}

// R-WDFE-H5CE R-PDA4-YXZ2
func TestCombine(t *testing.T) {
	cases := []struct {
		in   []sources.Estimate
		want scoring.Combined
	}{{nil, scoring.Combined{}}, {[]sources.Estimate{{Low: 2100000, High: ptr(3500000), Confidence: .7}}, scoring.Combined{Low: 2100000, High: ptr(3500000), Confidence: .7}}, {[]sources.Estimate{{Low: 100, Confidence: .25}, {Low: 400, High: ptr(800), Confidence: .75}}, scoring.Combined{Low: 325, High: ptr(800), Confidence: .75}}, {[]sources.Estimate{{Low: 1, High: ptr(-1)}, {Low: 2, High: ptr(-2)}}, scoring.Combined{Low: 2, High: ptr(-2), Confidence: 0}}, {[]sources.Estimate{{Low: math.MaxInt64, High: ptr(math.MaxInt64), Confidence: 1}, {Low: math.MaxInt64, High: ptr(math.MaxInt64), Confidence: 1}}, scoring.Combined{Low: math.MaxInt64, High: ptr(math.MaxInt64), Confidence: 1}}, {[]sources.Estimate{{Low: math.MinInt64, High: ptr(math.MinInt64), Confidence: 1}}, scoring.Combined{Low: math.MinInt64, High: ptr(math.MinInt64), Confidence: 1}}, {[]sources.Estimate{{Low: 100, High: ptr(200), Confidence: 0}, {Low: 400, Confidence: 1}}, scoring.Combined{Low: 400, High: ptr(200), Confidence: 1}}}
	cases = append(cases, struct {
		in   []sources.Estimate
		want scoring.Combined
	}{
		in:   []sources.Estimate{{Low: 100, High: ptr(200), Confidence: .75}, {Low: 400, High: ptr(800), Confidence: .25}},
		want: scoring.Combined{Low: 175, High: ptr(350), Confidence: .75},
	})
	for _, c := range cases {
		var original []sources.Estimate
		if c.in != nil {
			original = append([]sources.Estimate{}, c.in...)
			for i, e := range original {
				if e.High != nil {
					original[i].High = ptr(*e.High)
				}
			}
		}
		got := scoring.Combine(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v != %+v", got, c.want)
		}
		if got.High != nil {
			*got.High = 12345
		}
		if !reflect.DeepEqual(c.in, original) {
			t.Fatal("changed or shared estimates")
		}
	}
}

// R-GW8R-ELQS
func TestFreshness(t *testing.T) {
	filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2, t5 := filed.AddDate(2, 0, 0), filed.AddDate(5, 0, 0)
	for _, c := range []struct {
		filed, now time.Time
		want       float64
	}{{time.Time{}, filed, 0}, {filed, filed.AddDate(-1, 0, 0), 1}, {filed, t2, 1}, {filed, t2.Add(t5.Sub(t2) / 2), .5}, {filed, t5, 0}, {filed, t5.AddDate(1, 0, 0), 0}, {filed, t2.Add(time.Hour), float64(t5.Sub(t2.Add(time.Hour))) / float64(t5.Sub(t2))}} {
		if got := scoring.Freshness(c.filed, c.now); got != c.want {
			t.Fatal(got, c.want)
		}
	}

}

// R-41SV-M95N
func TestWeight(t *testing.T) {
	filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		filed, now time.Time
		want       float64
	}{{filed, filed.AddDate(1, 0, 0), .6}, {filed, time.Date(2023, 7, 3, 0, 0, 0, 0, time.UTC), .3}, {filed, filed.AddDate(5, 0, 0), 0}, {time.Time{}, filed, 0}} {
		if got := scoring.Weight(sources.Evidence{Name: sources.EvidenceFMCSAActive, Weight: .6}, tc.filed, tc.now); got != tc.want {
			t.Fatal(got)
		}
		for _, name := range []string{sources.EvidenceOwnDomain, sources.EvidenceFMCSAActivity, sources.EvidencePPPLoan, "future"} {
			if got := scoring.Weight(sources.Evidence{Name: name, Weight: .2}, tc.filed, tc.now); got != .2 {
				t.Fatal(got)
			}
		}
	}
}

// R-BKMJ-570Z R-PDA4-YXZ2
func TestOperating(t *testing.T) {
	for _, tc := range []struct {
		weights []float64
		want    float64
	}{{[]float64{.6, .3, .2}, .776}, {[]float64{.6, .5}, .8}, {[]float64{.2}, .2}, {[]float64{0}, 0}} {
		es := []sources.Evidence{}
		for _, w := range tc.weights {
			es = append(es, sources.Evidence{Weight: w})
		}
		before := append([]sources.Evidence{}, es...)
		got := scoring.Operating(es)
		if got == nil || math.Abs(*got-tc.want) > 1e-9 || !reflect.DeepEqual(es, before) {
			t.Fatal(got, es)
		}
	}
	if scoring.Operating(nil) != nil || scoring.Operating([]sources.Evidence{}) != nil {
		t.Fatal("empty")
	}
}

// R-BLUF-IYRO
func TestOperatingKey(t *testing.T) {
	if scoring.OperatingKey(nil) != -1 {
		t.Fatal("nil")
	}
	for _, tc := range []struct {
		in   float64
		want int64
	}{{0, 0}, {.0000015, 1}, {-.0000015, -1}, {.776, 776000}, {1, 1000000}, {1e13, math.MaxInt64}, {-1e13, math.MinInt64}, {0x1p63 / 1000000, math.MaxInt64}} {
		if got := scoring.OperatingKey(&tc.in); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
