package judging_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func ptr(n int64) *int64 { return &n }

// R-4FUZ-II7O
func TestCombinedShape(t *testing.T) {
	type shape struct {
		Low        int64
		High       *int64
		Confidence float64
	}
	if shape(judging.Combined{}) != (shape{}) {
		t.Fatal("zero combination")
	}
}

// R-H0XA-BQJO R-4FUZ-II7O
func TestCombine(t *testing.T) {
	cases := []struct {
		name string
		in   []sources.Estimate
		want judging.Combined
	}{
		{"nil", nil, judging.Combined{}},
		{"empty", []sources.Estimate{}, judging.Combined{}},
		{"single", []sources.Estimate{{Low: 2100000, High: ptr(3500000), Confidence: .7}}, judging.Combined{Low: 2100000, High: ptr(3500000), Confidence: .7}},
		{"exclude low only", []sources.Estimate{{Low: 100, Confidence: .25}, {Low: 400, High: ptr(800), Confidence: .75}}, judging.Combined{Low: 400, High: ptr(800), Confidence: .75}},
		{"exclude highest confidence", []sources.Estimate{{Low: 1000, Confidence: .9}, {Low: 400, High: ptr(800), Confidence: .75}}, judging.Combined{Low: 400, High: ptr(800), Confidence: .75}},
		{"bounded weighted means", []sources.Estimate{{Low: 100, High: ptr(200), Confidence: .75}, {Low: 400, High: ptr(800), Confidence: .25}, {Low: 1000, Confidence: .9}}, judging.Combined{Low: 175, High: ptr(350), Confidence: .75}},
		{"unbounded weighted mean", []sources.Estimate{{Low: 100, Confidence: .25}, {Low: 400, Confidence: .75}}, judging.Combined{Low: 325, Confidence: .75}},
		{"bounded zero weights", []sources.Estimate{{Low: 1, High: ptr(-1)}, {Low: 2, High: ptr(-2)}}, judging.Combined{Low: 2, High: ptr(-2)}},
		{"exclude positive weight from zero sum", []sources.Estimate{{Low: 100, High: ptr(200)}, {Low: 400, Confidence: 1}}, judging.Combined{Low: 100, High: ptr(200)}},
		{"bounded cancelling weights", []sources.Estimate{{Low: 1, High: ptr(4), Confidence: -1}, {Low: 2, High: ptr(5), Confidence: 1}, {Low: 1000, Confidence: .9}}, judging.Combined{Low: 2, High: ptr(5), Confidence: 1}},
		{"unbounded zero weights", []sources.Estimate{{Low: 1}, {Low: 2}}, judging.Combined{Low: 2}},
		{"unbounded negative weights", []sources.Estimate{{Low: -1, Confidence: -2}, {Low: -2, Confidence: -1}}, judging.Combined{Low: -2, Confidence: -1}},
		{"bounded weighted rounding", []sources.Estimate{{Low: 1, High: ptr(-1), Confidence: .5}, {Low: 2, High: ptr(-2), Confidence: .5}}, judging.Combined{Low: 2, High: ptr(-2), Confidence: .5}},
		{"unbounded weighted rounding", []sources.Estimate{{Low: -1, Confidence: .5}, {Low: -2, Confidence: .5}}, judging.Combined{Low: -2, Confidence: .5}},
		{"positive saturation", []sources.Estimate{{Low: math.MaxInt64, High: ptr(math.MaxInt64), Confidence: 1}, {Low: math.MaxInt64, High: ptr(math.MaxInt64), Confidence: 1}}, judging.Combined{Low: math.MaxInt64, High: ptr(math.MaxInt64), Confidence: 1}},
		{"negative boundary", []sources.Estimate{{Low: math.MinInt64, High: ptr(math.MinInt64), Confidence: 1}}, judging.Combined{Low: math.MinInt64, High: ptr(math.MinInt64), Confidence: 1}},
		{"negative saturation", []sources.Estimate{{Low: math.MinInt64, High: ptr(math.MinInt64), Confidence: 2}, {Low: math.MaxInt64, High: ptr(math.MaxInt64), Confidence: -1}}, judging.Combined{Low: math.MinInt64, High: ptr(math.MinInt64), Confidence: 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var original []sources.Estimate
			if c.in != nil {
				original = append([]sources.Estimate{}, c.in...)
				for i, e := range original {
					if e.High != nil {
						original[i].High = ptr(*e.High)
					}
				}
			}
			got := judging.Combine(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
			if got.High != nil {
				*got.High = 12345
			}
			if !reflect.DeepEqual(c.in, original) {
				t.Fatal("changed or shared estimates")
			}
		})
	}
}

// R-2X0P-HT05
func TestFreshness(t *testing.T) {
	filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2, t5 := filed.AddDate(2, 0, 0), filed.AddDate(5, 0, 0)
	for _, c := range []struct {
		filed, now time.Time
		want       float64
	}{{time.Time{}, filed, 0}, {filed, filed.AddDate(-1, 0, 0), 1}, {filed, t2, 1}, {filed, t2.Add(t5.Sub(t2) / 2), .5}, {filed, t5, 0}, {filed, t5.AddDate(1, 0, 0), 0}, {filed, t2.Add(time.Hour), float64(t5.Sub(t2.Add(time.Hour))) / float64(t5.Sub(t2))}} {
		if got := judging.Freshness(c.filed, c.now); got != c.want {
			t.Fatal(got, c.want)
		}
	}

}

// R-PUBH-UCS3
func TestWeight(t *testing.T) {
	filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		filed, now time.Time
		want       float64
	}{{filed, filed.AddDate(1, 0, 0), .6}, {filed, time.Date(2023, 7, 3, 0, 0, 0, 0, time.UTC), .3}, {filed, filed.AddDate(5, 0, 0), 0}, {time.Time{}, filed, 0}} {
		if got := judging.Weight(sources.Evidence{Name: sources.EvidenceFMCSAActive, Weight: .6}, tc.filed, tc.now); got != tc.want {
			t.Fatal(got)
		}
		for _, name := range []string{sources.EvidenceOwnDomain, sources.EvidenceFMCSAActivity, sources.EvidencePPPLoan, "future"} {
			if got := judging.Weight(sources.Evidence{Name: name, Weight: .2}, tc.filed, tc.now); got != .2 {
				t.Fatal(got)
			}
		}
	}
}

// R-YOL9-OHBC R-4FUZ-II7O
func TestOperating(t *testing.T) {
	for _, tc := range []struct {
		weights []float64
		want    float64
	}{{[]float64{.6, .3, .2}, .776}, {[]float64{.6, .5}, .8}, {[]float64{.2}, .2}, {[]float64{.6, .3, .2, .2}, .8208}, {[]float64{.0000004}, 0}, {[]float64{.1234564}, .123456}, {[]float64{.1234566}, .123457}, {[]float64{.0078125}, .007813}, {[]float64{-.0078125}, -.007813}, {[]float64{0}, 0}} {
		es := []sources.Evidence{}
		for _, w := range tc.weights {
			es = append(es, sources.Evidence{Name: "chosen-signal", Source: "chosen-source", Weight: w, Description: "chosen-description"})
		}
		before := append([]sources.Evidence{}, es...)
		got := judging.Operating(es)
		if got == nil || *got != tc.want || !reflect.DeepEqual(es, before) {
			t.Fatal(got, es)
		}
	}
	if judging.Operating(nil) != nil || judging.Operating([]sources.Evidence{}) != nil {
		t.Fatal("empty")
	}
}

// R-16TT-ZI6Q
func TestOperatingKey(t *testing.T) {
	if judging.OperatingKey(nil) != -1 {
		t.Fatal("nil")
	}
	for _, tc := range []struct {
		in   float64
		want int64
	}{{0, 0}, {.000001, 1}, {.0000015, 2}, {-.0000015, -2}, {.2, 200000}, {.776, 776000}, {1, 1000000}, {1e13, math.MaxInt64}, {-1e13, math.MinInt64}, {0x1p63 / 1000000, math.MaxInt64}} {
		if got := judging.OperatingKey(&tc.in); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
