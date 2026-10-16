package pages

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
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
	v := PassView{ID: p.ID, Kind: p.Kind, Origin: p.Origin, Trigger: p.Trigger, Status: p.Status, Reason: p.Reason, Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Judged: p.Judged, Unranged: p.Unranged, Due: p.Due, Fetched: p.Fetched, HTTPS: p.HTTPS, HTTP: p.HTTP, None: p.None}
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
	v := MemberRow{Rank: int64(i) + 1, ID: e.Candidate.ID, Name: e.Candidate.Name, City: e.Candidate.City, State: e.Candidate.State, OperatingConfidence: a.OperatingConfidence, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, Categories: []string{}, Class: e.Candidate.Class, FleetVerdict: a.FleetVerdict, Website: e.Assessment.Website, WebsiteURL: e.Assessment.WebsiteURL, Minutes: a.Minutes, GovernmentMatch: a.GovernmentMatch, KnownSince: a.KnownSince, KnownSinceText: a.KnownSinceText, NewestFiling: a.NewestFiling, NewestFilingText: a.NewestFilingText, Excluded: a.Excluded}
	seen := map[string]bool{}
	for _, c := range a.Categories {
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
	v := AssessmentView{OperatingEvidence: []EvidenceRow{}, Categories: []CategoryRow{}, GovernmentMatch: a.GovernmentMatch, FleetVerdict: a.FleetVerdict, Excluded: a.Excluded, Rules: a.Rules, KnownSince: date(a.KnownSince), KnownSinceText: date(a.KnownSince), NewestFiling: date(a.NewestFiling), NewestFilingText: date(a.NewestFiling)}
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
	if a.PowerUnits != nil {
		v.PowerUnits = strconv.FormatInt(*a.PowerUnits, 10)
	}
	if a.Drivers != nil {
		v.Drivers = strconv.FormatInt(*a.Drivers, 10)
	}
	if a.Minutes != nil {
		v.Minutes = strconv.FormatInt(*a.Minutes, 10)
	}
	for _, e := range a.Evidence {
		v.OperatingEvidence = append(v.OperatingEvidence, EvidenceRow{e.Name, e.Source, two(e.Weight), e.Description})
	}
	for _, c := range a.Categories {
		v.Categories = append(v.Categories, CategoryRow{c.Name, c.Source})
	}
	v.Calculated, v.CalculatedText = stamp(a.Calculated, SecondLayout)
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
	v := EnrichmentRow{Kind: x.Kind, State: x.State, URL: x.URL, Title: x.Title, Size: x.Size, Error: x.Error, Domain: x.Source}
	if x.StatusCode != 0 {
		v.StatusCode = strconv.Itoa(x.StatusCode)
	}
	v.At, v.AtText = stamp(x.At, MinuteLayout)
	return v
}

func setWebsite(v *CandidateView, es []store.Enrichment) {
	v.Website = sources.WebsiteUnknown
	for _, r := range es {
		if r.Kind != sources.WebsiteCheck {
			continue
		}
		v.Website = r.State
		if r.State == sources.WebsiteHTTPS || r.State == sources.WebsiteHTTP {
			v.WebsiteURL = r.URL
		}
		v.WebsiteTitle = r.Title
		v.WebsiteChecked, v.WebsiteCheckedText = stamp(r.At, SecondLayout)
		return
	}
}
