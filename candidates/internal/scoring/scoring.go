// Package scoring computes revenue combinations and their age-adjusted scores.
package scoring

import (
	"math"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// DefaultMaxMinutes is the default drive-time cutoff.
const DefaultMaxMinutes = 120

// DefaultMinRevenue is the default upper revenue cutoff.
const DefaultMinRevenue = 10000000

// Combined is a confidence-weighted revenue estimate.
type Combined struct {
	Low        int64
	High       *int64
	Confidence float64
}

func rounded(m float64) int64 {
	m = math.Round(m)
	if m >= 0x1p63 {
		return math.MaxInt64
	}
	if m < -0x1p63 {
		return math.MinInt64
	}
	return int64(m)
}

// Combine weights bounds independently and retains the highest confidence.
func Combine(estimates []sources.Estimate) Combined {
	if len(estimates) == 0 {
		return Combined{}
	}
	out := Combined{Confidence: estimates[0].Confidence}
	var low, lowPlain, weight, high, highPlain, highWeight float64
	var count int
	for _, e := range estimates {
		low += e.Confidence * float64(e.Low)
		lowPlain += float64(e.Low)
		weight += e.Confidence
		if e.Confidence > out.Confidence {
			out.Confidence = e.Confidence
		}
		if e.High != nil {
			high += e.Confidence * float64(*e.High)
			highPlain += float64(*e.High)
			highWeight += e.Confidence
			count++
		}
	}
	if weight > 0 {
		out.Low = rounded(low / weight)
	} else {
		out.Low = rounded(lowPlain / float64(len(estimates)))
	}
	if count > 0 {
		var h int64
		if highWeight > 0 {
			h = rounded(high / highWeight)
		} else {
			h = rounded(highPlain / float64(count))
		}
		out.High = &h
	}
	return out
}

// Freshness weights a filing between its second and fifth anniversaries.
func Freshness(filed, now time.Time) float64 {
	if filed.IsZero() {
		return 0
	}
	t2, t5 := filed.AddDate(2, 0, 0), filed.AddDate(5, 0, 0)
	if !now.After(t2) {
		return 1
	}
	if !now.Before(t5) {
		return 0
	}
	return float64(t5.Sub(now)) / float64(t5.Sub(t2))
}

// Effective applies filing freshness to confidence.
func Effective(confidence float64, filed, now time.Time) float64 {
	return confidence * Freshness(filed, now)
}

// Point returns a bound's midpoint truncated toward zero without overflow.
func Point(low int64, high *int64) int64 {
	if high == nil {
		return low
	}
	h := *high
	q := low/2 + h/2
	r := low%2 + h%2
	if r == 2 {
		return q + 1
	}
	if r == -2 {
		return q - 1
	}
	if q > 0 && r == -1 {
		return q - 1
	}
	if q < 0 && r == 1 {
		return q + 1
	}
	return q
}

// Score scales the revenue point by its confidence.
func Score(point int64, confidence float64) float64 { return float64(point) * confidence }
