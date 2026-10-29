package pages

import (
	"bytes"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-9JS3-70N0
func TestHeadcountJudgmentViewShape(_ *testing.T) {
	_ = struct {
		OperatingConfidence                                                                     string
		OperatingEvidence                                                                       []EvidenceRow
		RevenueLow, RevenueHigh, RevenueConfidence, HeadcountLow, HeadcountHigh, HeadcountBasis string
		HeadcountYears                                                                          []string
		HeadcountSeries                                                                         []SeriesRow
		Trend, TrendKind, TrendFromYear, TrendToYear, TrendFrom, TrendTo, TrendChange           string
		Excluded                                                                                bool
		Rules                                                                                   int64
		Calculated, CalculatedText                                                              string
	}(JudgmentsView{})
	_ = struct{ Name, Role, Source string }(PersonRow{})
	_ = struct{ Value, Source string }(OtherRow{})
	_ = struct{ Name, Source, Weight, Description string }(EvidenceRow{})
	_ = struct{ Name, Source string }(CategoryRow{})
}

// R-F24D-B6LT R-F5S2-GHTW R-FKEV-1QQ8
func TestHeadcountViewProjections(t *testing.T) {
	f := store.Facts{Headcounts: []sources.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}, {Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 1200}}, BriefLabels: []string{"chosen-label-one", "chosen-label-two"}}
	v := factsView(f, "")
	if !reflect.DeepEqual(v.Headcounts, []HeadcountRow{{ppp.Name, sources.HeadcountJobs, "26", "", "2020-04-28", "2020-04-28"}, {fmcsa.Name, sources.HeadcountDrivers, "1200", "", "", ""}}) || !reflect.DeepEqual(v.BriefLabels, f.BriefLabels) {
		t.Fatal(v)
	}
	empty := factsView(store.Facts{}, "")
	if empty.Headcounts == nil || len(empty.Headcounts) != 0 || empty.BriefLabels == nil || len(empty.BriefLabels) != 0 {
		t.Fatal(empty)
	}
	for _, counts := range [][2]*int64{{nil, nil}, {nil, new(int64(1200))}, {new(int64(40)), nil}, {new(int64(1200)), new(int64(1200))}, {new(int64(40)), new(int64(1200))}, {new(int64(26)), new(int64(26))}} {
		j := store.Judgments{HeadcountLow: counts[0], HeadcountHigh: counts[1]}
		if counts[0] != nil {
			j.HeadcountBasis = "chosen-basis"
		}
		got := judgmentsView(j)
		low, high := "", ""
		if counts[0] != nil {
			low = strconv.FormatInt(*counts[0], 10)
		}
		if counts[1] != nil {
			high = strconv.FormatInt(*counts[1], 10)
		}
		if got.HeadcountLow != low || got.HeadcountHigh != high || got.HeadcountBasis != j.HeadcountBasis {
			t.Fatal(got)
		}
		if member := memberRow(store.Ranked{Judgments: j}, 0); member.HeadcountLow != low {
			t.Fatal(member)
		}
	}
}

// R-IXH5-BRUF
func TestHeadcountTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any) string {
		t.Helper()
		var b bytes.Buffer
		if err := set.templates.ExecuteTemplate(&b, name, data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	for n := 0; n <= 2; n++ {
		for _, dated := range []bool{false, true} {
			for labels := 0; labels <= 2; labels++ {
				for _, shape := range []string{"empty", "equal", "range"} {
					for _, pair := range []bool{false, true} {
						f := &FactsView{Headcounts: []HeadcountRow{}, BriefLabels: []string{}}
						values := []string{}
						for i := 0; i < n; i++ {
							suffix := strconv.Itoa(i)
							h := HeadcountRow{"chosen-scraper-" + suffix, "chosen-kind-" + suffix, "chosen-count-" + suffix, "", "", ""}
							if dated {
								h.AsOf = "chosen-asof-" + suffix
								h.AsOfText = "chosen-date-" + suffix
							}
							f.Headcounts = append(f.Headcounts, h)
							values = append(values, h.Scraper, h.Kind, h.Count, h.AsOf, h.AsOfText)
						}
						for i := 0; i < labels; i++ {
							label := "chosen-label-" + strconv.Itoa(i)
							f.BriefLabels = append(f.BriefLabels, label)
							values = append(values, label)
						}
						j := &JudgmentsView{}
						if shape != "empty" {
							j.HeadcountLow = "chosen-low"
							j.HeadcountHigh = j.HeadcountLow
							j.HeadcountBasis = "chosen-judge-basis"
							if shape == "range" {
								j.HeadcountHigh = "chosen-high"
							}
							values = append(values, j.HeadcountLow, j.HeadcountHigh, j.HeadcountBasis)
						}
						es := []EstimateView{{Source: judging.HeadcountSource}}
						if pair {
							es = append(es, EstimateView{Source: fmcsa.Name})
						}
						body := render("candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Enrichments: &EnrichmentsView{Facts: f}, Judgments: j, Estimates: es})
						for _, v := range values {
							if v != "" && !strings.Contains(body, v) {
								t.Fatalf("missing %q", v)
							}
						}
					}
				}
			}
		}
	}
	for _, low := range []string{"", "chosen-member-count"} {
		body := render("prospect_list", ProspectListData{Members: []MemberRow{{HeadcountLow: low}}})
		if low != "" && !strings.Contains(body, low) {
			t.Fatal(low)
		}
	}
}

// R-F24D-B6LT R-F5S2-GHTW R-FKEV-1QQ8 R-VJ8X-RPW0
func TestHeadcountCandidateAndProspectListPages(t *testing.T) {
	cfg, _ := pageFixture(t)
	high := int64(2500000)
	_, ids, err := cfg.Store.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: fmcsa.Name, SourceID: "chosen-row", Digest: "chosen-digest", Reading: sources.Reading{Name: "chosen-business", NameKey: "chosen-business", Estimate: &sources.Estimate{Source: fmcsa.Name, Low: 1500000, High: &high, Confidence: .7}}}})
	if err != nil {
		t.Fatal(err)
	}
	found := enrichers.LinkFound{Categories: []enrichers.Category{{Name: sources.CategoryTrucking, Source: fmcsa.Name}}, FleetVerdict: sources.FleetUnknown, Headcounts: []enrichers.Headcount{{Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 12}}}
	brief := enrichers.BriefFound{Run: "chosen-brief", Status: "exited", ExitCode: new(0), Summary: "chosen-summary", Categories: []enrichers.BriefCategory{}, Labels: []string{"chosen-label-one", "chosen-label-two"}}
	_, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "chosen-links", Found: encodedJSON(found)}, {Candidate: ids[0], Kind: enrichers.KindBrief, Source: "chosen-source", Inputs: "chosen-brief-inputs", Found: encodedJSON(brief)}})
	if err != nil {
		t.Fatal(err)
	}
	if err = judgePageFixture(t.Context(), cfg.Store); err != nil {
		t.Fatal(err)
	}
	c := capturedCandidate(t, cfg, ids[0])
	f := c.Enrichments.Facts
	if !reflect.DeepEqual(f.Headcounts, []HeadcountRow{{fmcsa.Name, sources.HeadcountDrivers, "12", "", "", ""}}) || !reflect.DeepEqual(f.BriefLabels, brief.Labels) {
		t.Fatal(f)
	}
	j, err := cfg.Store.GetJudgments(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if c.Judgments.HeadcountLow != "12" || c.Judgments.HeadcountHigh != "12" || c.Judgments.HeadcountBasis == "" || c.Judgments.HeadcountBasis != j.HeadcountBasis {
		t.Fatal(c.Judgments, j)
	}
	es, err := cfg.Store.Estimates(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || len(c.Estimates) != 2 {
		t.Fatal(es, c.Estimates)
	}
	for i, e := range es {
		if !reflect.DeepEqual(c.Estimates[i], estimateView(e)) {
			t.Fatal(c.Estimates[i], e)
		}
	}
	if c.Estimates[0].Source != fmcsa.Name || c.Estimates[0].Low != "$1,500,000" || c.Estimates[0].High != "$2,500,000" || c.Estimates[0].Confidence != "0.70" || c.Estimates[1].Source != judging.HeadcountSource || c.Estimates[1].Low != "$1,440,000" || c.Estimates[1].High != "$2,400,000" || c.Estimates[1].Confidence != "0.30" || c.Estimates[1].Filed != "" || c.Estimates[1].FiledText != "" || c.Estimates[1].Formula != sources.Rules {
		t.Fatal(c.Estimates)
	}
	for i, row := range c.Estimates[1].Facts {
		if i > 0 && c.Estimates[1].Facts[i-1].Key >= row.Key {
			t.Fatal(c.Estimates[1].Facts)
		}
	}
	list, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "chosen-list", Size: 10, CreatedUserID: "chosen-owner"})
	if err != nil {
		t.Fatal(err)
	}
	members := pageProspectList(t, cfg, list.ID).Members
	if len(members) != 1 || members[0].HeadcountLow != c.Judgments.HeadcountLow {
		t.Fatal(members)
	}
	// A later facts result and brief are projected by the candidate route independently of judging.
	found.Headcounts = []enrichers.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}}
	if _, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "changed-links", Found: encodedJSON(found)}}); err != nil {
		t.Fatal(err)
	}
	f = capturedCandidate(t, cfg, ids[0]).Enrichments.Facts
	if !reflect.DeepEqual(f.Headcounts, []HeadcountRow{{ppp.Name, sources.HeadcountJobs, "26", "", "2020-04-28", "2020-04-28"}}) || !reflect.DeepEqual(f.BriefLabels, brief.Labels) {
		t.Fatal(f)
	}
}
