package eval

import (
	"fmt"
	"sort"
	"strings"
)

// Table is the rig's config × metric results table (plan P13), ALWAYS reported
// per generation — the outer sweep dimension is the generation (its prompt +
// data). P13 produces the table skeleton with the columns the rig can fill before
// the scorer library (P14) lands: config (model, effort), coverage (n cases /
// n cached), and cost + latency (from P0c's per-call accounting). P14 adds the
// headline score + the dangerous-direction axis columns; the per-generation
// caption + one-row-per-config shape (eval design q6) is fixed here so the
// renderer never collapses configs into a single lumped rank.
type Table struct {
	Generation int
	Site       string
	Rows       []Row
}

// Row is one config's aggregate over the generation's cases.
type Row struct {
	Model        string
	Effort       string
	Cases        int     // n cases scored for this config
	Cached       int     // n served from cache (zero paid calls)
	TotalCostUSD float64 // sum of per-case cost
	MeanCostUSD  float64 // per-case mean
	MeanLatency  float64 // mean ms per case
	P95Latency   int64   // p95 ms per case
}

// BuildTable aggregates case results into a per-config table for one generation.
// Results must all belong to the named generation/site (the caller groups by
// generation, the eval-design outer sweep dimension).
func BuildTable(generation int, site string, results []CaseResult) Table {
	type agg struct {
		cases     int
		cached    int
		totalCost float64
		latencies []int64
	}
	order := []string{}
	byConfig := map[string]*agg{}
	for _, r := range results {
		k := r.Model + "\x00" + r.Effort
		a := byConfig[k]
		if a == nil {
			a = &agg{}
			byConfig[k] = a
			order = append(order, k)
		}
		a.cases++
		if r.Cached {
			a.cached++
		}
		a.totalCost += r.CostUSD
		a.latencies = append(a.latencies, r.LatencyMS)
	}

	t := Table{Generation: generation, Site: site}
	for _, k := range order {
		a := byConfig[k]
		parts := strings.SplitN(k, "\x00", 2)
		row := Row{
			Model:        parts[0],
			Effort:       parts[1],
			Cases:        a.cases,
			Cached:       a.cached,
			TotalCostUSD: a.totalCost,
		}
		if a.cases > 0 {
			row.MeanCostUSD = a.totalCost / float64(a.cases)
			var sum int64
			for _, l := range a.latencies {
				sum += l
			}
			row.MeanLatency = float64(sum) / float64(a.cases)
			row.P95Latency = p95(a.latencies)
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// p95 returns the 95th-percentile latency (nearest-rank).
func p95(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := (95 * len(s)) / 100
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

// Render returns the table as text, captioned with the generation it scored (eval
// design q6: "always captioned with the generation it scored"), one row per
// config, never a single lumped rank.
func (t Table) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "site=%s  generation=gen-%d\n", t.Site, t.Generation)
	fmt.Fprintf(&b, "%-22s %-8s %6s %7s %12s %12s %10s %8s\n",
		"model", "effort", "cases", "cached", "total_cost", "mean_cost", "mean_ms", "p95_ms")
	for _, r := range t.Rows {
		fmt.Fprintf(&b, "%-22s %-8s %6d %7d %12.6f %12.6f %10.1f %8d\n",
			r.Model, r.Effort, r.Cases, r.Cached, r.TotalCostUSD, r.MeanCostUSD, r.MeanLatency, r.P95Latency)
	}
	return b.String()
}
