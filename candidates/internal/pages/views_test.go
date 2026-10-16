package pages

import (
	"bytes"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-IKZ3-GG4N R-59OY-K0KU R-TLDF-6XH3
func TestFormatting(t *testing.T) {
	const service, description string = ServiceName, Description
	if service == "" || description == "" {
		t.Fatal("page identity constants")
	}
	if MinuteLayout != "2006-01-02 15:04" || SecondLayout != "2006-01-02 15:04:05 UTC" || DateLayout != "2006-01-02" || ToolsLevel.Name != "tools" || ToolsLevel.URL != "/tools" || AboutLevel.Name != "about" || AboutLevel.URL != "/about" {
		t.Fatal("constants")
	}
	for _, tc := range []struct {
		n int64
		s string
	}{{0, "$0"}, {999, "$999"}, {10000000, "$10,000,000"}} {
		if dollars(tc.n) != tc.s {
			t.Fatal(tc)
		}
	}
	if two(.7) != "0.70" {
		t.Fatal("numbers")
	}
	tm := time.Date(2026, 10, 5, 10, 0, 7, 0, time.FixedZone("offset", 3600))
	if date(tm) != "2026-10-05" || date(time.Time{}) != "" {
		t.Fatal("date")
	}
	a, b := stamp(tm, MinuteLayout)
	if a != "2026-10-05T09:00:07Z" || b != "2026-10-05 09:00" {
		t.Fatal(a, b)
	}
	_, b = stamp(tm, SecondLayout)
	if b != "2026-10-05 09:00:07 UTC" {
		t.Fatal(b)
	}
	a, b = stamp(time.Time{}, SecondLayout)
	if a != "" || b != "" {
		t.Fatal("zero")
	}
}

// R-AAIJ-2DB7 R-AECZ-1FC4 R-AE68-7OJA R-4VMG-YZTE R-AI0O-6QK7 R-4ZA6-4B1H
func TestTemplateDataShape(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b := page.Banner{Service: ServiceName}
	cv := CandidateView{"id", store.StatusActive, "", "name", []string{"alias"}, "street", "city", "MN", "55072", "county", "phone", "email", "website", "https://chosen.example.test/", "title", "checked", "checked-text", []PersonRow{{"person", "role"}}, "class", []string{"cargo"}, "", "", "", ""}
	av := AssessmentView{"operating", []EvidenceRow{{"name", "source", "weight", "description"}}, "low", "high", "confidence", "date", "date-text", "newest", "newest-text", []CategoryRow{{"category", "source"}}, "government", "fleet", "power", "drivers", "minutes", false, int64(sources.Rules), "calculated", "calculated-text"}
	ev := EstimateView{"source", "low", "high", "confidence", "", "", 1, []FactRow{{"key", "value"}}}
	lv := LinkView{"scraper", "source", "run", "", "name", "city", "55072", "reason", "confidence", "", "", "", "", "", "", false}
	rv := RulingView{"kind", "other", []string{"key"}, "by", "user", "", "", "why"}
	pv := PassView{"id", store.KindRebuild, store.OriginRebuild, store.TriggerRebuild, "", store.StatusRunning, "", "starter", "", "", "", "", 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	row := CohortRow{"id", "name", 11, "by", "user", "changed", "changed-text"}
	pass := PassRow{"id", store.KindRebuild, "trigger", "status", "reason", "started", "started-text", "finished", "finished-text", 21, 22, 23}
	landing := LandingData{b, []CohortRow{row}, "page", "prev", "next", []PassRow{pass}, "page", "prev", "next"}
	if cv.Website != "website" || cv.WebsiteURL != "https://chosen.example.test/" || cv.WebsiteTitle != "title" || cv.WebsiteChecked != "checked" || cv.WebsiteCheckedText != "checked-text" || pv.Due != 10 || pv.Fetched != 11 || pv.HTTPS != 12 || pv.HTTP != 13 || pv.None != 14 {
		t.Fatal("website positional shape")
	}
	if row.Size != 11 || pass.Kind != store.KindRebuild || pass.Judged != 23 || av.FleetVerdict != "fleet" || av.PowerUnits != "power" || av.Drivers != "drivers" || pv.Judged != 8 {
		t.Fatal("positional shape")
	}
	for _, v := range []struct {
		name string
		data any
	}{{"landing", landing}, {"candidate", CandidateData{b, cv, &av, []EstimateView{ev}, []LinkView{lv}, []RulingView{rv}, []EnrichmentRow{}, []RejectRecordRow{{"kind", "cohort", "cohort-name", "by", "user", "at", "at-text", "why"}}}}, {"pass", PassData{b, pv}}, {"tools", ToolsData{b, []ToolRow{{"name", "description"}}}}, {"about", AboutData{b, Description}}, {"notfound", NotFoundData{b}}} {
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, v.name, v.data); err != nil {
			t.Fatal(v.name, err)
		}
	}
}
