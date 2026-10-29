package pages

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-0B4G-1GWO R-59OY-K0KU R-TLDF-6XH3
func TestFormatting(t *testing.T) {
	const service, description string = ServiceName, Description
	if service != "candidates" || description == "" || strings.ContainsAny(description, "\n\"\\") || strings.TrimSpace(description) != description {
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

// R-5DEB-1HUN R-RU1K-AXX2 R-QNJT-WLSZ R-QCKQ-GO4Q R-4ZA6-4B1H
func TestTemplateDataShape(t *testing.T) {
	_ = struct {
		Source, Low, High, Confidence, Filed, FiledText string
		Formula                                         int64
		Facts                                           []FactRow
	}(EstimateView{})
	_ = struct{ Key, Value string }(FactRow{})
	_ = struct {
		Scraper, SourceID, Run, RowPage, EIN, Name                                                                                               string
		Names                                                                                                                                    []string
		Establishment, City, Zip, Year, Employees, Hours, Reason, Confidence, Filed, FiledText, FirstSeen, FirstSeenText, LastSeen, LastSeenText string
		Gone                                                                                                                                     bool
		Walk                                                                                                                                     string
		Park                                                                                                                                     *ParkLink
	}(LinkView{})
	_ = struct {
		Kind, Other               string
		Keys                      []string
		By, User, At, AtText, Why string
	}(RulingView{})
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b := page.Banner{Service: ServiceName}
	cv := CandidateView{"id", store.StatusActive, "", "name", "street", "city", "MN", "55072", "county", "phone", "email", "name-source", "address-source", "phone-source", "email-source", []OtherRow{{"alias", "source"}}, []OtherRow{}, []OtherRow{}, nil, "source", "website", "https://chosen.example.test/", "title", "why", "checked", "checked-text", "checked-url", []PersonRow{{"person", "role", "source"}}, "", "", []OtherRow{}, nil, map[string]OwnerNoteView{}, "class", []string{"cargo"}, "", "", "", ""}
	av := JudgmentsView{"operating", []EvidenceRow{{"name", "source", "weight", "description"}}, "low", "high", "confidence", "", "", "", []string{}, []SeriesRow{}, "", "", "", "", "", "", "", false, int64(sources.Rules), "calculated", "calculated-text"}
	ev := EstimateView{"source", "low", "high", "confidence", "", "", 1, []FactRow{{"key", "value"}}}
	lv := LinkView{"scraper", "source", "run", "", "ein", "name", []string{"alias"}, "establishment", "city", "55072", "2024", "23", "47840", "reason", "confidence", "", "", "", "", "", "", false, "", nil}
	rv := RulingView{"kind", "other", []string{"key"}, "by", "user", "", "", "why"}
	pv := PassView{"id", store.KindIngest, store.OriginIngest, store.TriggerIngest, "", store.StatusRunning, "", "starter", "", "", "", "", 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 10, 11, 12, 13, 14, "target", "/prospect-lists/target", "kind", 15, 16, 17, "$0.000000", 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, false, []WalkParkRow{}}
	row := ProspectListRow{"id", "name", 11, "by", "user", "changed", "changed-text"}
	pass := PassRow{"id", store.KindIngest, "trigger", "status", "reason", "started", "started-text", "finished", "finished-text", 21, 22, 23}
	landing := LandingData{b, []ProspectListRow{row}, "page", "prev", "next", []PassRow{pass}, "page", "prev", "next", LandingParks{}}
	if cv.Website != "website" || cv.WebsiteURL != "https://chosen.example.test/" || cv.WebsiteTitle != "title" || cv.WebsiteChecked != "checked" || cv.WebsiteCheckedText != "checked-text" || pv.Due != 10 || pv.Fetched != 11 || pv.HTTPS != 12 || pv.HTTP != 13 || pv.None != 14 {
		t.Fatal("website positional shape")
	}
	if lv.EIN != "ein" || lv.Name != "name" || len(lv.Names) != 1 || lv.Names[0] != "alias" {
		t.Fatal(lv)
	}
	if row.Size != 11 || pass.Kind != store.KindIngest || pass.Judged != 23 || pv.Judged != 8 {
		t.Fatal("positional shape")
	}
	for _, v := range []struct {
		name string
		data any
	}{{"landing", landing}, {"candidate", CandidateData{b, cv, nil, &av, []EstimateView{ev}, []LinkView{lv}, []RulingView{rv}, []OwnerRecordRow{}, []PageRow{}, []EnrichmentRow{}, []RejectRecordRow{{"kind", "prospect_list", "prospect_list-name", "by", "user", "at", "at-text", "why"}}, []CampaignRow{}}}, {"pass", PassData{b, pv}}, {"tools", ToolsData{b, []ToolRow{{"name", "description"}}}}, {"about", AboutData{b, Description}}, {"notfound", NotFoundData{b}}} {
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, v.name, v.data); err != nil {
			t.Fatal(v.name, err)
		}
	}
}
