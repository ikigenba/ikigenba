package pages

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func dollars(v int64) string {
	s := strconv.FormatInt(v, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return "$" + s
}
func two(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(DateLayout)
}
func stamp(t time.Time, layout string) (string, string) {
	if t.IsZero() {
		return "", ""
	}
	return t.UTC().Format(time.RFC3339), t.UTC().Format(layout)
}
func passRow(p store.Pass) PassRow {
	v := PassRow{ID: p.ID, Kind: p.Kind, Trigger: p.Trigger, Status: p.Status, Reason: p.Reason, Rows: p.Rows, Created: p.Created, Judged: p.Judged}
	v.Started, v.StartedText = stamp(p.Started, MinuteLayout)
	v.Finished, v.FinishedText = stamp(p.Finished, MinuteLayout)
	return v
}
func passView(p store.Pass, sp string) PassView {
	v := PassView{ID: p.ID, Kind: p.Kind, Origin: p.Origin, Trigger: p.Trigger, Status: p.Status, Reason: p.Reason, Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Judged: p.Judged, Unranged: p.Unranged, Derived: p.Derived, Due: p.Due, Fetched: p.Fetched, HTTPS: p.HTTPS, HTTP: p.HTTP, None: p.None}
	v.Target, v.EnrichKind, v.Runs, v.RunsFailed, v.Pages, v.Cost = p.Target, p.EnrichKind, p.Runs, p.RunsFailed, p.Pages, costText(p.Cost)
	if store.ValidCohortID(p.Target) {
		v.TargetPage = "/cohorts/" + p.Target
	} else if store.ValidCandidateID(p.Target) {
		v.TargetPage = "/candidates/" + p.Target
	}
	switch p.Origin {
	case store.OriginRebuild, store.OriginEnrich:
		v.StartedBy = p.UserEmail
		if v.StartedBy == "" {
			v.StartedBy = p.UserID
		}
	case store.OriginRun:
		v.RunPage = urls.RunPage(urls.Scrapes(sp), p.Trigger)
	}
	v.Started, v.StartedText = stamp(p.Started, SecondLayout)
	v.Finished, v.FinishedText = stamp(p.Finished, SecondLayout)
	return v
}
func memberRow(e store.Ranked, i int) MemberRow {
	a := assessmentView(e.Assessment)
	f := factsView(store.Facts{KnownSince: e.Assessment.KnownSince, NewestFiling: e.Assessment.NewestFiling, Categories: e.Assessment.Categories, GovernmentMatch: e.Assessment.GovernmentMatch, FleetVerdict: e.Assessment.FleetVerdict, PowerUnits: e.Assessment.PowerUnits, Drivers: e.Assessment.Drivers, Minutes: e.Assessment.Minutes})
	v := MemberRow{Rank: int64(i) + 1, ID: e.Candidate.ID, Name: e.Candidate.Name, City: e.Candidate.City, State: e.Candidate.State, OperatingConfidence: a.OperatingConfidence, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, Categories: []string{}, Class: e.Candidate.Class, FleetVerdict: f.FleetVerdict, Website: e.Assessment.Website, WebsiteURL: e.Assessment.WebsiteURL, Minutes: f.Minutes, GovernmentMatch: f.GovernmentMatch, KnownSince: f.KnownSince, KnownSinceText: f.KnownSinceText, NewestFiling: f.NewestFiling, NewestFilingText: f.NewestFilingText, Excluded: a.Excluded}
	v.SiteText, v.SiteTextText = stamp(e.SiteText, MinuteLayout)
	v.Brief, v.BriefText = stamp(e.Brief, MinuteLayout)
	seen := map[string]bool{}
	for _, c := range f.Categories {
		if !seen[c.Name] {
			v.Categories = append(v.Categories, c.Name)
			seen[c.Name] = true
		}
	}
	sort.Strings(v.Categories)
	return v
}
func candidateView(c store.Candidate) CandidateView {
	v := CandidateView{ID: c.ID, Status: c.Status, MergedInto: c.MergedInto, Names: []string{}, People: []PersonRow{}, Cargo: []string{}}
	v.Created, v.CreatedText = stamp(c.Created, SecondLayout)
	v.Updated, v.UpdatedText = stamp(c.Updated, SecondLayout)
	if c.Status == store.StatusMerged {
		return v
	}
	v.Name = c.Name
	v.Names = append(v.Names, c.Names...)
	v.Street = c.Street
	v.City = c.City
	v.State = c.State
	v.Zip = c.Zip
	v.County = c.County
	v.Phone = c.Phone
	v.Email = c.Email
	v.Class = c.Class
	v.Cargo = append(v.Cargo, c.Cargo...)
	for _, p := range c.People {
		v.People = append(v.People, PersonRow{p.Name, p.Role})
	}
	return v
}
func assessmentView(a store.Assessment) AssessmentView {
	v := AssessmentView{OperatingEvidence: []EvidenceRow{}, Excluded: a.Excluded, Rules: a.Rules}
	if a.Operating != nil {
		v.OperatingConfidence = two(*a.Operating)
	}
	if a.RevenueLow != nil {
		v.RevenueLow = dollars(*a.RevenueLow)
	}
	if a.RevenueHigh != nil {
		v.RevenueHigh = dollars(*a.RevenueHigh)
	}
	if a.RevenueConfidence != nil {
		v.RevenueConfidence = two(*a.RevenueConfidence)
	}
	for _, e := range a.Evidence {
		v.OperatingEvidence = append(v.OperatingEvidence, EvidenceRow{e.Name, e.Source, two(e.Weight), e.Description})
	}
	v.Calculated, v.CalculatedText = stamp(a.Calculated, SecondLayout)
	return v
}
func factsView(f store.Facts) FactsView {
	v := FactsView{KnownSince: date(f.KnownSince), KnownSinceText: date(f.KnownSince), NewestFiling: date(f.NewestFiling), NewestFilingText: date(f.NewestFiling), Categories: []CategoryRow{}, GovernmentMatch: f.GovernmentMatch, FleetVerdict: f.FleetVerdict, Written: []WrittenRow{}}
	if f.PowerUnits != nil {
		v.PowerUnits = strconv.FormatInt(*f.PowerUnits, 10)
	}
	if f.Drivers != nil {
		v.Drivers = strconv.FormatInt(*f.Drivers, 10)
	}
	if f.Minutes != nil {
		v.Minutes = strconv.FormatInt(*f.Minutes, 10)
	}
	for _, c := range f.Categories {
		v.Categories = append(v.Categories, CategoryRow{c.Name, c.Source})
	}
	for _, w := range f.Written {
		at, txt := stamp(w.At, SecondLayout)
		v.Written = append(v.Written, WrittenRow{w.Kind, at, txt})
	}
	return v
}

func estimateView(e sources.Estimate) EstimateView {
	v := EstimateView{Source: e.Source, Low: dollars(e.Low), Confidence: two(e.Confidence), Filed: date(e.Filed), FiledText: date(e.Filed), Formula: e.Formula, Facts: []FactRow{}}
	if e.High != nil {
		v.High = dollars(*e.High)
	}
	keys := make([]string, 0, len(e.Facts))
	for k := range e.Facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var b bytes.Buffer
		if err := json.Compact(&b, e.Facts[k]); err != nil {
			b.Write(e.Facts[k])
		}
		v.Facts = append(v.Facts, FactRow{k, b.String()})
	}
	return v
}
func linkView(l store.Link, sp string) LinkView {
	v := LinkView{Scraper: l.Scraper, SourceID: l.SourceID, Run: l.Run, RowPage: urls.RowPage(urls.Scrapes(sp), l.Run, l.SourceID), Name: l.Reading.Name, City: l.Reading.City, Zip: l.Reading.Zip, Reason: l.Reason, Confidence: two(l.Confidence), Filed: date(l.Reading.Filed), FiledText: date(l.Reading.Filed), Gone: l.Gone}
	v.FirstSeen, v.FirstSeenText = stamp(l.FirstSeen, MinuteLayout)
	v.LastSeen, v.LastSeenText = stamp(l.LastSeen, MinuteLayout)
	return v
}
func rulingView(r store.Ruling, id string) RulingView {
	v := RulingView{Kind: r.Kind, Other: r.Other, Keys: append([]string{}, r.Keys...), By: r.UserEmail, User: r.UserID, Why: r.Why}
	if r.Candidate != id {
		v.Other = r.Candidate
	}
	v.At, v.AtText = stamp(r.At, MinuteLayout)
	return v
}

func cohortRow(c store.Cohort) CohortRow {
	v := CohortRow{ID: c.ID, Name: c.Name, Size: c.Size, CreatedBy: c.CreatedUserEmail, CreatedUser: c.CreatedUserID}
	v.Changed, v.ChangedText = stamp(c.Changed, MinuteLayout)
	return v
}

func filtersView(f store.CohortFilters) FiltersView {
	v := FiltersView{RevenueUnknown: f.RevenueUnknown, MinutesUnknown: f.MinutesUnknown, Categories: append([]string{}, f.Categories...), CategoryUnknown: f.CategoryUnknown, Fleet: f.Fleet, FleetUnknown: f.FleetUnknown, Website: append([]string{}, f.Website...), WebsiteUnknown: f.WebsiteUnknown, FilingUnknown: f.FilingUnknown, Class: f.Class, ClassUnknown: f.ClassUnknown, ShowExcluded: f.ShowExcluded}
	if f.RevenueMin != nil {
		v.RevenueMin = dollars(*f.RevenueMin)
	}
	if f.RevenueMax != nil {
		v.RevenueMax = dollars(*f.RevenueMax)
	}
	if f.MaxMinutes != nil {
		v.MaxMinutes = strconv.FormatInt(*f.MaxMinutes, 10)
	}
	if f.MaxFilingAgeDays != nil {
		v.MaxFilingAgeDays = strconv.FormatInt(*f.MaxFilingAgeDays, 10)
	}
	if f.Government != nil {
		v.Government = strconv.FormatBool(*f.Government)
	}
	return v
}

func cohortView(c store.Cohort) CohortView {
	v := CohortView{ID: c.ID, Name: c.Name, Size: c.Size, Filters: filtersView(c.Filters), Order: []OrderRow{}, CreatedBy: c.CreatedUserEmail, CreatedUser: c.CreatedUserID, ChangedBy: c.ChangedUserEmail, ChangedUser: c.ChangedUserID}
	for _, o := range c.Order {
		v.Order = append(v.Order, OrderRow{o.Field, o.Direction})
	}
	v.Created, v.CreatedText = stamp(c.Created, SecondLayout)
	v.Changed, v.ChangedText = stamp(c.Changed, SecondLayout)
	return v
}

func rejectRow(r store.Reject) RejectRow {
	v := RejectRow{Candidate: r.Candidate, By: r.UserEmail, User: r.UserID, Why: r.Why}
	v.At, v.AtText = stamp(r.At, MinuteLayout)
	return v
}

func rejectRecordRow(r store.Reject) RejectRecordRow {
	v := RejectRecordRow{Kind: r.Kind, Cohort: r.Cohort, CohortName: r.CohortName, By: r.UserEmail, User: r.UserID, Why: r.Why}
	v.At, v.AtText = stamp(r.At, MinuteLayout)
	return v
}

func enrichmentRow(x store.Enrichment) EnrichmentRow {
	v := EnrichmentRow{Kind: x.Kind, Source: x.Source, Found: []FactRow{}}
	if x.Kind == sources.WebsiteCheck {
		var r website.Result
		_ = json.Unmarshal(x.Found, &r)
		v.State, v.URL, v.Title, v.Size, v.Error = r.State, r.URL, r.Title, r.Size, r.Error
		if r.StatusCode != 0 {
			v.StatusCode = strconv.Itoa(r.StatusCode)
		}
	} else {
		var found map[string]json.RawMessage
		_ = json.Unmarshal(x.Found, &found)
		keys := make([]string, 0, len(found))
		for k := range found {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var b bytes.Buffer
			_ = json.Compact(&b, found[k])
			v.Found = append(v.Found, FactRow{k, b.String()})
		}
	}
	if n := int64(math.Round(x.Cost * 1e9)); n > 0 {
		v.Cost = costText(n)
	}
	e, _ := enrichers.New(enrichers.Config{}).Get(x.Kind)
	if p, ok := e.(enrichers.Prompter); ok {
		v.Failed = p.Failed(x.Found)
	}
	v.At, v.AtText = stamp(x.At, MinuteLayout)
	return v
}
func setWebsite(v *CandidateView, es []store.Enrichment) {
	v.Website = sources.WebsiteUnknown
	for _, e := range es {
		if e.Kind != sources.WebsiteCheck {
			continue
		}
		var r website.Result
		_ = json.Unmarshal(e.Found, &r)
		v.Website = r.State
		if r.State == sources.WebsiteHTTPS || r.State == sources.WebsiteHTTP {
			v.WebsiteURL = r.URL
		}
		v.WebsiteTitle = r.Title
		v.WebsiteChecked, v.WebsiteCheckedText = stamp(e.At, SecondLayout)
		return
	}
}

func costText(n int64) string {
	m := n / 1000
	if n%1000 >= 500 {
		m++
	}
	fraction := strconv.FormatInt(m%1000000, 10)
	return "$" + strconv.FormatInt(m/1000000, 10) + "." + strings.Repeat("0", 6-len(fraction)) + fraction
}
func pageRow(p store.Page) PageRow {
	v := PageRow{Number: strconv.FormatInt(p.Number, 10), Path: urls.PagePath(p.Candidate, p.Number), URL: p.URL, Title: p.Title, Size: p.Size, By: p.UserEmail, User: p.UserID}
	v.Fetched, v.FetchedText = stamp(p.Fetched, MinuteLayout)
	return v
}
func pageView(p store.Page, c store.Candidate) PageView {
	v := PageView{Candidate: p.Candidate, Name: c.Name, Number: strconv.FormatInt(p.Number, 10), URL: p.URL, Title: p.Title, Size: p.Size, By: p.UserEmail, User: p.UserID, Text: p.Text}
	v.Fetched, v.FetchedText = stamp(p.Fetched, SecondLayout)
	return v
}

func briefView(es []store.Enrichment) *BriefView {
	e, _ := enrichers.New(enrichers.Config{}).Get(enrichers.KindBrief)
	p := e.(enrichers.Prompter)
	for _, e := range es {
		if e.Kind != enrichers.KindBrief {
			continue
		}
		var b enrichers.BriefFound
		if json.Unmarshal(e.Found, &b) != nil || p.Failed(e.Found) {
			continue
		}
		v := &BriefView{Summary: b.Summary, Categories: []BriefCategoryRow{}, Labels: append([]string{}, b.Labels...)}
		for _, c := range b.Categories {
			v.Categories = append(v.Categories, BriefCategoryRow{Name: c.Name, Why: c.Why})
		}
		v.At, v.AtText = stamp(e.At, SecondLayout)
		return v
	}
	return nil
}
