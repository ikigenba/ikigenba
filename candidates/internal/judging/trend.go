package judging

import (
	"math/big"
	"slices"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Figure records one calendar year's headcount.
type Figure struct{ Year, Count int64 }

// Series records a kind's figures in ascending year order.
type Series struct {
	Kind, Scraper string
	Figures       []Figure
}

// Trend records the direction and the figures that decided it.
type Trend struct {
	Direction, Kind                    string
	FromYear, ToYear, From, To, Change int64
}

// Trend directions and thresholds are the owner's judging table.
const (
	TrendGrowing     = "growing"
	TrendFlat        = "flat"
	TrendShrinking   = "shrinking"
	TrendUnknown     = "unknown"
	TrendYearsBack   = 3
	TrendWindowYears = 4
	TrendMinYears    = 2
	TrendDivisor     = 5
	TrendFloor       = 5
)

// HeadcountSeries forms yearly figures from the pick's site-summed contenders.
func HeadcountSeries(hs []sources.Headcount) []Series {
	out := []Series{}
	contenders := headcountContenders(hs)
	for _, kind := range []string{sources.HeadcountEmployees, sources.HeadcountParticipants, sources.HeadcountJobs} {
		byYear := map[int64]sources.Headcount{}
		for _, h := range contenders {
			if h.Kind != kind || h.Count <= 0 || h.AsOf.IsZero() {
				continue
			}
			year := int64(h.AsOf.UTC().Year())
			if old, ok := byYear[year]; !ok || h.Count > old.Count {
				byYear[year] = h
			}
		}
		if len(byYear) == 0 {
			continue
		}
		s := Series{Kind: kind, Figures: []Figure{}}
		for year, h := range byYear {
			s.Figures = append(s.Figures, Figure{Year: year, Count: h.Count})
		}
		slices.SortFunc(s.Figures, func(a, b Figure) int {
			if a.Year < b.Year {
				return -1
			}
			if a.Year > b.Year {
				return 1
			}
			return 0
		})
		s.Scraper = byYear[s.Figures[len(s.Figures)-1].Year].Scraper
		out = append(out, s)
	}
	return out
}

// HeadcountTrend uses the first series with an eligible comparison year.
func HeadcountTrend(series []Series) Trend {
	for _, s := range series {
		if len(s.Figures) == 0 {
			continue
		}
		newest := s.Figures[0]
		for _, f := range s.Figures[1:] {
			if f.Year > newest.Year {
				newest = f
			}
		}
		var preferred, fallback *Figure
		for _, f := range s.Figures {
			age := new(big.Int).Sub(big.NewInt(newest.Year), big.NewInt(f.Year))
			if age.Cmp(big.NewInt(TrendWindowYears)) > 0 || age.Cmp(big.NewInt(TrendMinYears)) < 0 {
				continue
			}
			if fallback == nil || f.Year < fallback.Year {
				v := f
				fallback = &v
			}
			if age.Cmp(big.NewInt(TrendYearsBack)) >= 0 && (preferred == nil || f.Year > preferred.Year) {
				v := f
				preferred = &v
			}
		}
		if preferred == nil {
			preferred = fallback
		}
		if preferred == nil {
			continue
		}
		difference := new(big.Int).Sub(big.NewInt(newest.Count), big.NewInt(preferred.Count))
		direction := TrendFlat
		if threshold(difference, preferred.Count) {
			direction = TrendGrowing
		} else if threshold(new(big.Int).Neg(difference), preferred.Count) {
			direction = TrendShrinking
		}
		return Trend{Direction: direction, Kind: s.Kind, FromYear: preferred.Year, ToYear: newest.Year, From: preferred.Count, To: newest.Count, Change: saturatedInt64(difference)}
	}
	return Trend{Direction: TrendUnknown}
}

func threshold(difference *big.Int, from int64) bool {
	return difference.Cmp(big.NewInt(TrendFloor)) >= 0 && new(big.Int).Mul(difference, big.NewInt(TrendDivisor)).Cmp(big.NewInt(from)) >= 0
}
