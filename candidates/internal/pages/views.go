package pages

import (
	"bytes"
	"encoding/json"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
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
	v := PassView{Parks: []WalkParkRow{}, Circles: p.Circles, Requests: p.Requests, Found: p.Found, NewCandidates: p.NewCandidates, CeilingReached: p.CeilingReached, ID: p.ID, Kind: p.Kind, Origin: p.Origin, Trigger: p.Trigger, Status: p.Status, Reason: p.Reason, Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Judged: p.Judged, Unranged: p.Unranged, Derived: p.Derived, Due: p.Due, Fetched: p.Fetched, HTTPS: p.HTTPS, HTTP: p.HTTP, None: p.None}
	v.ParcelsRead, v.ByLabel, v.ByValue, v.ByCandidate = p.ParcelsRead, p.ByLabel, p.ByValue, p.ByCandidate
	v.ParksCreated, v.ParksUpdated, v.ParksGone, v.ParksGrown = p.ParksCreated, p.ParksUpdated, p.ParksGone, p.ParksGrown
	v.Target, v.EnrichKind, v.Runs, v.RunsFailed, v.Pages, v.Cost = p.Target, p.EnrichKind, p.Runs, p.RunsFailed, p.Pages, costText(p.Cost)
	if store.ValidProspectListID(p.Target) {
		v.TargetPage = "/prospect-lists/" + p.Target
	} else if store.ValidCandidateID(p.Target) {
		v.TargetPage = "/candidates/" + p.Target
	}
	switch p.Origin {
	case store.OriginIngest, store.OriginJudge, store.OriginEnrich, store.OriginRules, store.OriginWalk:
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
	a := judgmentsView(e.Judgments)
	f := factsView(e.Facts, "")
	v := MemberRow{Rank: int64(i) + 1, Basis: e.Basis, ID: e.Candidate.ID, Name: e.Candidate.Name, City: e.Candidate.City, State: e.Candidate.State, OperatingConfidence: a.OperatingConfidence, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, HeadcountLow: a.HeadcountLow, Trend: a.Trend, TrendChange: a.TrendChange, Categories: []string{}, ClassifiedBy: store.ClassifiedBy(e.Facts.Categories), Class: e.Candidate.Class, FleetVerdict: f.FleetVerdict, Website: e.Facts.Website, WebsiteURL: e.Facts.WebsiteURL, WebsiteSource: e.Facts.WebsiteSource, Minutes: f.Minutes, MinutesSource: f.MinutesSource, GovernmentMatch: f.GovernmentMatch, KnownSince: f.KnownSince, KnownSinceText: f.KnownSinceText, NewestFiling: f.NewestFiling, NewestFilingText: f.NewestFilingText, Excluded: a.Excluded}
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
	v := CandidateView{ID: c.ID, Status: c.Status, MergedInto: c.MergedInto, OtherNames: []OtherRow{}, OtherAddresses: []OtherRow{}, OtherPhones: []OtherRow{}, People: []PersonRow{}, Cargo: []string{}}
	v.Created, v.CreatedText = stamp(c.Created, SecondLayout)
	v.Updated, v.UpdatedText = stamp(c.Updated, SecondLayout)
	if c.Status == store.StatusMerged {
		return v
	}
	v.Name = c.Name
	v.NameSource, v.AddressSource, v.PhoneSource, v.EmailSource = c.NameSource, c.AddressSource, c.PhoneSource, c.EmailSource
	for _, o := range c.OtherNames {
		v.OtherNames = append(v.OtherNames, OtherRow{Value: o.Value, Source: o.Source})
	}
	for _, o := range c.OtherPhones {
		v.OtherPhones = append(v.OtherPhones, OtherRow{Value: o.Value, Source: o.Source})
	}
	seen := map[string]bool{addressLine(sources.Address{Street: c.Street, City: c.City, State: c.State, Zip: c.Zip, County: c.County}): true}
	for _, o := range c.OtherAddresses {
		line := addressLine(o.Value)
		if !seen[line] {
			v.OtherAddresses = append(v.OtherAddresses, OtherRow{Value: line, Source: o.Source})
			seen[line] = true
		}
	}
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
		v.People = append(v.People, PersonRow{p.Value.Name, p.Value.Role, p.Source})
	}
	return v
}
func judgmentsView(a store.Judgments) JudgmentsView {
	v := JudgmentsView{HeadcountBasis: a.HeadcountBasis, OperatingEvidence: []EvidenceRow{}, Excluded: a.Excluded, Rules: a.Rules}
	if a.HeadcountLow != nil {
		v.HeadcountLow = strconv.FormatInt(*a.HeadcountLow, 10)
	}
	if a.HeadcountHigh != nil {
		v.HeadcountHigh = strconv.FormatInt(*a.HeadcountHigh, 10)
	}
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
	v.HeadcountYears, v.HeadcountSeries = seriesView(a)
	v.Trend, v.TrendKind = a.Trend, a.TrendKind
	v.TrendFromYear, v.TrendToYear = integer(a.TrendFromYear), integer(a.TrendToYear)
	v.TrendFrom, v.TrendTo = integer(a.TrendFrom), integer(a.TrendTo)
	if a.TrendChange != nil {
		v.TrendChange = changeText(*a.TrendChange)
	}
	v.Calculated, v.CalculatedText = stamp(a.Calculated, SecondLayout)
	return v
}
func factsView(f store.Facts, origin string) FactsView {
	v := FactsView{KnownSince: date(f.KnownSince), KnownSinceText: date(f.KnownSince), KnownSinceSource: f.KnownSinceSource, NewestFiling: date(f.NewestFiling), NewestFilingText: date(f.NewestFiling), NewestFilingSource: f.NewestFilingSource, Categories: []CategoryRow{}, ClassifiedBy: store.ClassifiedBy(f.Categories), RecordsDiffer: store.RecordsDiffer(f.Categories, f.Records), RecordCategories: []CategoryRow{}, GovernmentMatch: f.GovernmentMatch, FleetVerdict: f.FleetVerdict, Written: []WrittenRow{}, Headcounts: []HeadcountRow{}, BriefLabels: append([]string{}, f.BriefLabels...)}
	if f.KnownSince.IsZero() {
		v.KnownSinceSource = ""
	}
	if f.NewestFiling.IsZero() {
		v.NewestFilingSource = ""
	}
	if f.PowerUnits != nil {
		v.PowerUnits = strconv.FormatInt(*f.PowerUnits, 10)
	}
	if f.Drivers != nil {
		v.Drivers = strconv.FormatInt(*f.Drivers, 10)
	}
	if f.Minutes != nil {
		v.Minutes = strconv.FormatInt(*f.Minutes, 10)
	}
	v.MinutesSource = f.MinutesSource
	if f.MinutesSource == store.MinutesByRoute {
		v.RouteAddress = f.RouteAddress
		v.RoutePlaceURL = routes.PlaceURL(f.RouteAddress)
		v.RouteDirectionsURL = routes.DirectionsURL(origin, f.RouteAddress)
		if f.Miles != nil {
			v.Miles = strconv.FormatFloat(*f.Miles, 'f', 1, 64)
		}
	}
	for _, c := range f.Categories {
		v.Categories = append(v.Categories, CategoryRow{c.Name, c.Source})
	}
	if v.RecordsDiffer {
		for _, c := range f.Records {
			v.RecordCategories = append(v.RecordCategories, CategoryRow{c.Name, c.Source})
		}
	}
	for _, h := range f.Headcounts {
		v.Headcounts = append(v.Headcounts, HeadcountRow{h.Scraper, h.Kind, strconv.FormatInt(h.Count, 10), integer(h.Hours), date(h.AsOf), date(h.AsOf)})
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
	v := LinkView{Walk: l.Walk, Scraper: l.Scraper, SourceID: l.SourceID, Run: l.Run, RowPage: urls.RowPage(urls.Scrapes(sp), l.Run, l.SourceID), EIN: l.Reading.EINKey, Names: append([]string{}, l.Reading.Names...), Name: l.Reading.Name, Establishment: l.Reading.Establishment, Year: integer(l.Reading.Year), Employees: integer(l.Reading.Employees), Hours: integer(l.Reading.Hours), City: l.Reading.City, Zip: l.Reading.Zip, Reason: l.Reason, Confidence: two(l.Confidence), Filed: date(l.Reading.Filed), FiledText: date(l.Reading.Filed), Gone: l.Gone}
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

func prospectListRow(c store.ProspectList) ProspectListRow {
	v := ProspectListRow{ID: c.ID, Name: c.Name, Size: c.Size, CreatedBy: c.CreatedUserEmail, CreatedUser: c.CreatedUserID}
	v.Changed, v.ChangedText = stamp(c.Changed, MinuteLayout)
	return v
}

func filtersView(f store.ProspectListFilters) FiltersView {
	v := FiltersView{Basis: append([]string{}, f.Basis...), RevenueUnknown: f.RevenueUnknown, MinutesUnknown: f.MinutesUnknown, Categories: append([]string{}, f.Categories...), CategoryUnknown: f.CategoryUnknown, Fleet: f.Fleet, FleetUnknown: f.FleetUnknown, Website: append([]string{}, f.Website...), WebsiteUnknown: f.WebsiteUnknown, Trend: append([]string{}, f.Trend...), TrendUnknown: f.TrendUnknown, FilingUnknown: f.FilingUnknown, Class: f.Class, ClassUnknown: f.ClassUnknown, ShowExcluded: f.ShowExcluded}
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
	if f.MinCampaignAgeDays != nil {
		v.MinCampaignAgeDays = strconv.FormatInt(*f.MinCampaignAgeDays, 10)
	}
	if f.Government != nil {
		v.Government = strconv.FormatBool(*f.Government)
	}
	return v
}

func prospectListView(c store.ProspectList) ProspectListView {
	v := ProspectListView{ID: c.ID, Name: c.Name, Size: c.Size, Filters: filtersView(c.Filters), Order: []OrderRow{}, CreatedBy: c.CreatedUserEmail, CreatedUser: c.CreatedUserID, ChangedBy: c.ChangedUserEmail, ChangedUser: c.ChangedUserID}
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
	v := RejectRecordRow{Kind: r.Kind, ProspectList: r.ProspectList, ProspectListName: r.ProspectListName, By: r.UserEmail, User: r.UserID, Why: r.Why}
	v.At, v.AtText = stamp(r.At, MinuteLayout)
	return v
}

func enrichmentRow(x store.Enrichment, subject enrichers.Subject) EnrichmentRow {
	v := EnrichmentRow{Kind: x.Kind, Source: x.Source, Found: []FactRow{}}
	switch x.Kind {
	case sources.WebsiteCheck:
		var r website.Result
		_ = json.Unmarshal(x.Found, &r)
		v.State, v.URL, v.Title, v.Size, v.Error = r.State, r.URL, r.Title, r.Size, r.Error
		if r.StatusCode != 0 {
			v.StatusCode = strconv.Itoa(r.StatusCode)
		}
	case enrichers.KindWebsiteSearch:
		searchViews(&v, x.Found)
	case enrichers.KindDriveRoute:
		var r enrichers.RouteFound
		_ = json.Unmarshal(x.Found, &r)
		v.Route = &RouteView{Address: r.Address, Precision: r.Precision, Status: r.Status, Why: r.Why, Error: r.Error}
		if !r.Failed() && r.Minutes != nil && r.Address != "" {
			v.Route.PlaceURL = routes.PlaceURL(r.Address)
			if x.Inputs == (enrichers.DriveRoute{}).Inputs(subject) {
				v.Route.DirectionsURL = routes.DirectionsURL(subject.Origin, r.Address)
			}
		}
		if r.Minutes != nil {
			v.Route.Minutes = strconv.FormatInt(*r.Minutes, 10)
		}
		if r.Miles != nil {
			v.Route.Miles = strconv.FormatFloat(*r.Miles, 'f', 1, 64)
		}
	default:
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
	if p, ok := e.(enrichers.Failer); ok {
		v.Failed = p.Failed(x.Found)
	}
	v.At, v.AtText = stamp(x.At, MinuteLayout)
	return v
}
func setWebsite(v *CandidateView, es []store.Enrichment, placesURL string) {
	var at time.Time
	var checked string
	v.WebsiteSource, v.Website, v.WebsiteURL, v.WebsiteTitle, v.WebsiteWhy, checked, at = store.WebsiteDecisionOf(es, placesURL)
	v.WebsiteChecked, v.WebsiteCheckedText = stamp(at, SecondLayout)
	v.WebsiteCheckedURL = ""
	if checked != "" {
		if website.Inputs(checked, "") == "" {
			checked = "https://" + checked
		}
		if !sameURL(checked, v.WebsiteURL) {
			v.WebsiteCheckedURL = checked
		}
	}
}
func sameURL(a, b string) bool {
	if a == b {
		return true
	}
	x, err := url.Parse(a)
	if err != nil {
		return false
	}
	y, err := url.Parse(b)
	if err != nil {
		return false
	}
	if x.Path == "" {
		x.Path = "/"
	}
	if y.Path == "" {
		y.Path = "/"
	}
	return x.String() == y.String()
}
func addressLine(a sources.Address) string {
	nonempty := func(vs ...string) []string {
		out := []string{}
		for _, v := range vs {
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return strings.Join(nonempty(a.Street, a.City, strings.Join(nonempty(a.State, a.Zip), " ")), ", ")
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
	v := PageRow{Number: strconv.FormatInt(p.Number, 10), Path: urls.PagePath(p.Candidate, p.Number), URL: p.URL, Title: p.Title, Size: p.Size, Reason: p.Reason, By: p.UserEmail, User: p.UserID}
	v.Fetched, v.FetchedText = stamp(p.Fetched, MinuteLayout)
	return v
}
func pageView(p store.Page, c store.Candidate) PageView {
	v := PageView{Candidate: p.Candidate, Name: c.Name, Number: strconv.FormatInt(p.Number, 10), URL: p.URL, Title: p.Title, Size: p.Size, Reason: p.Reason, By: p.UserEmail, User: p.UserID, Text: p.Text}
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

func searchViews(v *EnrichmentRow, found json.RawMessage) {
	var r enrichers.WebsiteSearchFound
	_ = json.Unmarshal(found, &r)
	v.Run = &RunView{ID: r.Run.ID, Status: r.Run.Status}
	if r.Run.ExitCode != nil {
		v.Run.ExitCode = strconv.Itoa(*r.Run.ExitCode)
	}
	v.Reason, v.Website, v.WebsiteFrom, v.Case = r.Reason, r.Website, r.WebsiteFrom, r.Case
	if r.SiteText != nil {
		v.SiteText = &SearchSiteTextView{Status: r.SiteText.Status, Pages: r.SiteText.Pages}
	}
	if r.Answer != nil {
		v.Answer = &AnswerView{URL: r.Answer.URL, Why: r.Answer.Why, Rejected: []RejectedRow{}}
		for _, x := range r.Answer.Rejected {
			v.Answer.Rejected = append(v.Answer.Rejected, RejectedRow{URL: x.URL, Why: x.Why})
		}
	}
	if r.Confirmation != nil {
		c := r.Confirmation
		v.Confirmation = &ConfirmationView{State: c.State, URL: c.URL, Title: c.Title, Size: c.Size, Error: c.Error}
		if c.StatusCode != 0 {
			v.Confirmation.StatusCode = strconv.Itoa(c.StatusCode)
		}
	}
}

func basisCountsView(b store.BasisCounts) BasisCountsView {
	return BasisCountsView{b.Records, b.Partial, b.Enriched}
}
func changesView(x store.Changes) ChangesView {
	v := ChangesView{DefinitionChanged: append([]string{}, x.DefinitionChanged...), Joined: []JoinedRow{}, Left: []LeftRow{}}
	v.Since, v.SinceText = stamp(x.Since, MinuteLayout)
	for _, j := range x.Joined {
		v.Joined = append(v.Joined, JoinedRow{j.Candidate, j.Name, j.Basis, j.Why})
	}
	for _, l := range x.Left {
		v.Left = append(v.Left, LeftRow{l.Candidate, l.Name, l.BasisThen, l.BasisNow, l.Why, l.Filter})
	}
	return v
}
func enrichmentKinds(es []store.Enrichment) KindsView {
	present, missing := store.KindsOf(es)
	return KindsView{append([]string{}, present...), append([]string{}, missing...)}
}

func integer(n *int64) string {
	if n == nil {
		return ""
	}
	return strconv.FormatInt(*n, 10)
}

func changeText(v int64) string {
	if v > 0 {
		return "+" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}

func seriesView(a store.Judgments) ([]string, []SeriesRow) {
	years, rows := []string{}, []SeriesRow{}
	var first, last int64
	found := false
	for _, s := range a.HeadcountSeries {
		for _, f := range s.Figures {
			if !found || f.Year < first {
				first = f.Year
			}
			if !found || f.Year > last {
				last = f.Year
			}
			found = true
		}
	}
	if found {
		for y := first; ; y++ {
			years = append(years, strconv.FormatInt(y, 10))
			if y == last {
				break
			}
		}
	}
	for _, s := range a.HeadcountSeries {
		row := SeriesRow{Kind: s.Kind, Source: s.Scraper, Figures: []FigureCell{}}
		for _, year := range years {
			cell := FigureCell{Year: year}
			for _, f := range s.Figures {
				if strconv.FormatInt(f.Year, 10) == year {
					cell.Count = strconv.FormatInt(f.Count, 10)
					break
				}
			}
			row.Figures = append(row.Figures, cell)
		}
		rows = append(rows, row)
	}
	return years, rows
}
