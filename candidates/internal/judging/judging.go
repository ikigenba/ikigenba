// Package judging computes revenue combinations and their age-adjusted scores.
package judging

import (
	"math"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

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

// Combine weights bounds and confidence over estimates with a high, if any.
func Combine(estimates []sources.Estimate) Combined {
	var bounded bool
	for _, e := range estimates {
		if e.High != nil {
			bounded = true
			break
		}
	}
	var out Combined
	var low, lowPlain, weight, high, highPlain float64
	var count int
	for _, e := range estimates {
		if bounded && e.High == nil {
			continue
		}
		low += e.Confidence * float64(e.Low)
		lowPlain += float64(e.Low)
		weight += e.Confidence
		if count == 0 || e.Confidence > out.Confidence {
			out.Confidence = e.Confidence
		}
		if bounded {
			high += e.Confidence * float64(*e.High)
			highPlain += float64(*e.High)
		}
		count++
	}
	if count == 0 {
		return out
	}
	if weight > 0 {
		out.Low = rounded(low / weight)
	} else {
		out.Low = rounded(lowPlain / float64(count))
	}
	if bounded {
		var h int64
		if weight > 0 {
			h = rounded(high / weight)
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

// Weight applies freshness only to active registration evidence.
func Weight(e sources.Evidence, filed, now time.Time) float64 {
	if e.Name == sources.EvidenceFMCSAActive {
		return e.Weight * Freshness(filed, now)
	}
	return e.Weight
}

// Operating combines independent signs of operation.
func Operating(evidence []sources.Evidence) *float64 {
	if len(evidence) == 0 {
		return nil
	}
	p := 1.0
	for _, e := range evidence {
		p *= 1 - e.Weight
	}
	v := math.Round((1-p)*1000000) / 1000000
	return &v
}

// OperatingKey provides the persisted ordering key.
func OperatingKey(operating *float64) int64 {
	if operating == nil {
		return -1
	}
	v := math.Round(*operating * 1000000)
	if v >= 0x1p63 {
		return math.MaxInt64
	}
	if v < -0x1p63 {
		return math.MinInt64
	}
	return int64(v)
}
