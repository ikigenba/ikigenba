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

// R-26PV-9SB3
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

// R-WDFE-H5CE R-26PV-9SB3
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
	if scoring.DefaultMaxMinutes != 120 || scoring.DefaultMinRevenue != 10000000 {
		t.Fatal("defaults")
	}
}

// R-GW8R-ELQS R-GXGN-SDHH R-GZWG-JWYV
func TestFreshnessEffectiveScore(t *testing.T) {
	filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2, t5 := filed.AddDate(2, 0, 0), filed.AddDate(5, 0, 0)
	for _, c := range []struct {
		filed, now time.Time
		want       float64
	}{{time.Time{}, filed, 0}, {filed, filed.AddDate(-1, 0, 0), 1}, {filed, t2, 1}, {filed, t2.Add(t5.Sub(t2) / 2), .5}, {filed, t5, 0}, {filed, t5.AddDate(1, 0, 0), 0}, {filed, t2.Add(time.Hour), float64(t5.Sub(t2.Add(time.Hour))) / float64(t5.Sub(t2))}} {
		if got := scoring.Freshness(c.filed, c.now); got != c.want {
			t.Fatal(got, c.want)
		}
		if got := scoring.Effective(.7, c.filed, c.now); got != .7*c.want {
			t.Fatal(got)
		}
		if got := scoring.Score(123, scoring.Effective(.7, c.filed, c.now)); got != 123*.7*c.want {
			if math.Abs(got-123*.7*c.want) > 1e-12 {
				t.Fatal(got)
			}
		}
	}
	if scoring.Score(-4, .5) != -2 {
		t.Fatal("negative")
	}
}

// R-GYOK-6586
func TestPoint(t *testing.T) {
	for _, c := range [][3]int64{{2100000, 3500000, 2800000}, {1, 2, 1}, {math.MaxInt64, math.MaxInt64, math.MaxInt64}, {math.MinInt64, math.MinInt64, math.MinInt64}, {math.MaxInt64, math.MinInt64, 0}, {4, -1, 1}, {-4, 1, -1}, {-1, -2, -1}, {0, 1, 0}, {0, -1, 0}} {
		if got := scoring.Point(c[0], &c[1]); got != c[2] {
			t.Error(c, got)
		}
	}
	if scoring.Point(math.MinInt64, nil) != math.MinInt64 {
		t.Fatal("floor")
	}
}
