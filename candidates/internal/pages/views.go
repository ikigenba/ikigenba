package pages

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
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
func money(v float64) string { return dollars(int64(math.Round(v))) }
func two(v float64) string   { return strconv.FormatFloat(v, 'f', 2, 64) }
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
	v := PassRow{ID: p.ID, Trigger: p.Trigger, Status: p.Status, Reason: p.Reason, Rows: p.Rows, Created: p.Created, Rescored: p.Rescored}
	v.Started, v.StartedText = stamp(p.Started, MinuteLayout)
	v.Finished, v.FinishedText = stamp(p.Finished, MinuteLayout)
	return v
}
func passView(p store.Pass, sp string) PassView {
	v := PassView{ID: p.ID, Trigger: p.Trigger, Status: p.Status, Reason: p.Reason, Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Rescored: p.Rescored, Unranged: p.Unranged}
	if p.Trigger == store.TriggerRebuild {
		v.StartedBy = p.UserEmail
		if v.StartedBy == "" {
			v.StartedBy = p.UserID
		}
	} else {
		v.RunPage = urls.RunPage(urls.Scrapes(sp), p.Trigger)
	}
	v.Started, v.StartedText = stamp(p.Started, SecondLayout)
	v.Finished, v.FinishedText = stamp(p.Finished, SecondLayout)
	return v
}
func shortlistRow(e store.Ranked, i int) ShortlistRow {
	v := ShortlistRow{Rank: int64(i) + 1, ID: e.Candidate.ID, Name: e.Candidate.Name, City: e.Candidate.City, State: e.Candidate.State, Class: e.Candidate.Class, Low: dollars(e.Assessment.Low), Confidence: two(e.Assessment.Effective), Score: money(e.Assessment.Score)}
	if e.Assessment.Minutes != nil {
		v.Minutes = *e.Assessment.Minutes
	}
	if e.Assessment.High != nil {
		v.High = dollars(*e.Assessment.High)
	}
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
	freshness := 0.0
	if a.Confidence > 0 {
		freshness = max(0, min(1, a.Effective/a.Confidence))
	}
	point := scoring.Point(a.Low, a.High)
	v := AssessmentView{Fleet: a.Fleet, Low: dollars(a.Low), Confidence: two(a.Confidence), Filed: date(a.Filed), FiledText: date(a.Filed), Freshness: two(freshness), Effective: two(a.Effective), Stale: a.Effective <= 0, Point: dollars(point), Base: money(a.Base), Score: money(a.Score), Excluded: a.Excluded, Exclusions: []ExclusionRow{}, Rules: a.Rules}
	if a.Minutes != nil {
		v.Minutes = strconv.FormatInt(*a.Minutes, 10)
	}
	if a.High != nil {
		v.High = dollars(*a.High)
	}
	for _, e := range a.Exclusions {
		v.Exclusions = append(v.Exclusions, ExclusionRow{e.Reason, e.Detail})
	}
	v.Scored, v.ScoredText = stamp(a.Scored, SecondLayout)
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
