package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Tool descriptions and refusal sentences are public copy.
const (
	DescribeCohorts      string = "Read the saved cohorts of candidate businesses."
	DescribeCohort       string = "Read a cohort with its members and standing rejects."
	DescribeExclude      string = "Exclude a candidate from every cohort."
	DescribeInclude      string = "Include a candidate in every matching cohort again."
	DescribeCreateCohort string = "Create a saved search over candidate businesses."
	DescribeEditCohort   string = "Edit a cohort definition."
	DescribeDeleteCohort string = "Delete a cohort definition."
	DescribeReject       string = "Reject a candidate from a cohort."
	DescribeUnreject     string = "Remove a candidate's standing rejection from a cohort."
	DescribeCandidates   string = "Find candidate businesses by name, ZIP or phone."
	DescribeCandidate    string = "Read a candidate with its evidence, assessment and rulings."
	DescribePasses       string = "Read recorded passes."
	DescribePass         string = "Read a pass."
	DescribeEnrich       string = "Enrich candidates whose inputs changed since their last result, for one kind or every kind."
	DescribeRebuild      string = "Start a resolution pass over current scraper runs."
	DescribeDecide       string = "Record an owner's ruling and reassess the affected candidates."
	RefuseNotFound       string = "The requested record was not found."
	RefuseInvalid        string = "The arguments are invalid."
	RefuseBusy           string = "A pass is already running."
	RefuseStopping       string = "The service is stopping."
	RefuseUnavailable    string = "The service is unavailable."
)

// Config supplies the engine and database.
type Config struct {
	Passes *passes.Engine
	Store  *store.Store
}

// Entry describes a tool for the tools page.
type Entry struct{ Name, Description string }

// Catalog returns independent ordered tool metadata.
func Catalog() []Entry {
	return []Entry{{"cohorts", DescribeCohorts}, {"cohort", DescribeCohort}, {"candidates", DescribeCandidates}, {"candidate", DescribeCandidate}, {"passes", DescribePasses}, {"pass", DescribePass}, {"rebuild", DescribeRebuild}, {"enrich", DescribeEnrich}, {"decide", DescribeDecide}, {"exclude", DescribeExclude}, {"include", DescribeInclude}, {"create_cohort", DescribeCreateCohort}, {"edit_cohort", DescribeEditCohort}, {"delete_cohort", DescribeDeleteCohort}, {"reject", DescribeReject}, {"unreject", DescribeUnreject}}
}
func refuse(word string) mcp.Result {
	text := RefuseUnavailable
	switch word {
	case passes.RefusalNotFound:
		text = RefuseNotFound
	case passes.RefusalInvalid:
		text = RefuseInvalid
	case passes.RefusalBusy:
		text = RefuseBusy
	case passes.RefusalStopping:
		text = RefuseStopping
	default:
		word = passes.RefusalUnavailable
	}
	return mcp.ErrorResult(word + ": " + text)
}
func failure(err error) mcp.Result {
	if errors.Is(err, store.ErrNotFound) {
		return refuse(passes.RefusalNotFound)
	}
	if errors.Is(err, store.ErrInvalid) {
		return refuse(passes.RefusalInvalid)
	}
	return refuse(passes.RefusalUnavailable)
}
func answer(v any) mcp.Result {
	b, err := json.Marshal(v)
	if err != nil {
		return refuse(passes.RefusalUnavailable)
	}
	content, _ := json.Marshal([]struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{"text", string(b)}})
	var r mcp.Result
	if err := json.Unmarshal(append(append(append([]byte(`{"content":`), content...), []byte(`,"structuredContent":`)...), append(b, '}')...), &r); err != nil {
		return refuse(passes.RefusalUnavailable)
	}
	return r
}
func add[A any](srv *mcp.Server, name, description string, effect mcp.Effect, handler func(context.Context, identity.Caller, A) mcp.Result) {
	mcp.AddRawTool(srv, mcp.RawTool[A]{Name: name, Description: description, Effect: effect, Handler: func(ctx context.Context, c identity.Caller, a A) (mcp.Result, error) { return handler(ctx, c, a), nil }})
}

// Register installs the sixteen candidate tools on srv.
func Register(srv *mcp.Server, cfg Config) {
	add(srv, "cohorts", DescribeCohorts, mcp.Read, func(ctx context.Context, _ identity.Caller, a CohortsArgs) mcp.Result {
		z, key, id, err := timePosition("cohorts", a.Page, a.Limit)
		if err != nil {
			return mcp.ErrorResult(err.Error())
		}
		rows, err := cfg.Store.ListCohorts(ctx, key, id, z+1)
		if err != nil {
			return failure(err)
		}
		out := CohortList{Cohorts: []Cohort{}}
		for i, c := range rows {
			if i == z {
				break
			}
			out.Cohorts = append(out.Cohorts, cohortOf(c))
		}
		if len(rows) > z {
			last := rows[z-1]
			token := paging.Token{Tool: "cohorts"}.Encode(last.Name, last.ID)
			out.Next = &token
		}
		return answer(out)
	})
	add(srv, "cohort", DescribeCohort, mcp.Read, func(ctx context.Context, _ identity.Caller, a CohortArgs) mcp.Result {
		if !store.ValidCohortID(a.Cohort) {
			return refuse(passes.RefusalNotFound)
		}
		r, err := cfg.Store.ReadCohort(ctx, a.Cohort)
		if err != nil {
			return failure(err)
		}
		return answer(cohortReadOf(r))
	})
	add(srv, "candidates", DescribeCandidates, mcp.Read, func(ctx context.Context, _ identity.Caller, a CandidatesArgs) mcp.Result {
		z, key, id, err := timePosition("candidates", a.Page, a.Limit)
		if err != nil {
			return mcp.ErrorResult(err.Error())
		}
		rows, err := cfg.Store.ListCandidates(ctx, store.CandidateFilter{Name: a.Name, Zip: a.Zip, Phone: a.Phone}, key, id, z+1)
		if err != nil {
			return failure(err)
		}
		out := CandidateList{Candidates: []Candidate{}}
		for i, c := range rows {
			if i == z {
				break
			}
			out.Candidates = append(out.Candidates, candidateOf(c))
		}
		if len(rows) > z {
			last := rows[z-1]
			token := paging.Token{Tool: "candidates"}.Encode(stamp(last.Created), last.ID)
			out.Next = &token
		}
		return answer(out)
	})
	add(srv, "candidate", DescribeCandidate, mcp.Read, func(ctx context.Context, _ identity.Caller, a CandidateArgs) mcp.Result {
		if !store.ValidCandidateID(a.Candidate) {
			return refuse(passes.RefusalNotFound)
		}
		return detail(ctx, cfg, a.Candidate)
	})
	add(srv, "passes", DescribePasses, mcp.Read, func(ctx context.Context, _ identity.Caller, a PassesArgs) mcp.Result {
		z, key, id, err := timePosition("passes", a.Page, a.Limit)
		if err != nil {
			return mcp.ErrorResult(err.Error())
		}
		rows, err := cfg.Store.ListPasses(ctx, key, id, z+1)
		if err != nil {
			return failure(err)
		}
		out := PassList{Passes: []passes.Record{}}
		for i, p := range rows {
			if i == z {
				break
			}
			out.Passes = append(out.Passes, passes.RecordOf(p))
		}
		if len(rows) > z {
			last := rows[z-1]
			token := paging.Token{Tool: "passes"}.Encode(stamp(last.Started), last.ID)
			out.Next = &token
		}
		return answer(out)
	})
	add(srv, "pass", DescribePass, mcp.Read, func(ctx context.Context, _ identity.Caller, a PassArgs) mcp.Result {
		if !store.ValidPassID(a.Pass) {
			return refuse(passes.RefusalNotFound)
		}
		p, err := cfg.Store.GetPass(ctx, a.Pass)
		if err != nil {
			return failure(err)
		}
		return answer(passes.RecordOf(p))
	})
	add(srv, "rebuild", DescribeRebuild, mcp.Additive, func(ctx context.Context, c identity.Caller, _ NoArgs) mcp.Result {
		p, err := cfg.Passes.Rebuild(ctx, c)
		if err != nil {
			var r *passes.Refusal
			if errors.As(err, &r) {
				return refuse(r.Word)
			}
			return refuse(passes.RefusalUnavailable)
		}
		return answer(passes.RecordOf(p))
	})
	add(srv, "enrich", DescribeEnrich, mcp.Additive, func(ctx context.Context, c identity.Caller, a EnrichArgs) mcp.Result {
		if a.OlderThanDays != nil || a.Kind != "" && !slices.Contains(enrichers.Kinds(), a.Kind) {
			return refuse(passes.RefusalInvalid)
		}
		p, err := cfg.Passes.Enrich(ctx, c, a.Kind)
		if err != nil {
			var r *passes.Refusal
			if errors.As(err, &r) {
				return refuse(r.Word)
			}
			return refuse(passes.RefusalUnavailable)
		}
		return answer(passes.RecordOf(p))
	})

	add(srv, "decide", DescribeDecide, mcp.Additive, func(ctx context.Context, c identity.Caller, a DecideArgs) mcp.Result {
		pair := a.Kind == store.KindMerge || a.Kind == store.KindNotSame
		if a.Kind != store.KindMerge && a.Kind != store.KindNotSame && a.Kind != store.KindSplit || strings.TrimSpace(a.Why) == "" || pair && a.Other == "" || a.Kind == store.KindSplit && len(a.Links) == 0 {
			return refuse(passes.RefusalInvalid)
		}
		if !store.ValidCandidateID(a.Candidate) || pair && !store.ValidCandidateID(a.Other) {
			return refuse(passes.RefusalNotFound)
		}
		ruling := store.Ruling{Kind: a.Kind, Candidate: a.Candidate, Why: a.Why, UserID: c.UserID, UserEmail: c.Email}
		if pair {
			ruling.Other = a.Other
		}
		if a.Kind == store.KindSplit {
			ruling.Keys = a.Links
		}
		r, cs, err := cfg.Store.Decide(ctx, ruling)
		if err != nil {
			return failure(err)
		}
		out := Decision{Ruling: rulingOf(r), Candidates: []Candidate{}}
		for _, c := range cs {
			out.Candidates = append(out.Candidates, candidateOf(c))
		}
		return answer(out)
	})
	for _, tool := range []struct{ name, description, kind string }{{"exclude", DescribeExclude, store.KindExclude}, {"include", DescribeInclude, store.KindInclude}} {
		add(srv, tool.name, tool.description, mcp.Additive, func(ctx context.Context, c identity.Caller, a RulingArgs) mcp.Result {
			if strings.TrimSpace(a.Why) == "" {
				return refuse(passes.RefusalInvalid)
			}
			if !store.ValidCandidateID(a.Candidate) {
				return refuse(passes.RefusalNotFound)
			}
			r, cs, err := cfg.Store.Decide(ctx, store.Ruling{Kind: tool.kind, Candidate: a.Candidate, Why: a.Why, UserID: c.UserID, UserEmail: c.Email})
			if err != nil {
				return failure(err)
			}
			return answer(decisionOf(r, cs))
		})
	}
	add(srv, "create_cohort", DescribeCreateCohort, mcp.Additive, func(ctx context.Context, c identity.Caller, a CreateCohortArgs) mcp.Result {
		if !store.ValidCohortName(a.Name) || !validParts(a.Size, a.Filters, a.Order) {
			return refuse(passes.RefusalInvalid)
		}
		v := store.Cohort{Name: a.Name, Size: store.CohortSizeDefault, CreatedUserID: c.UserID, CreatedUserEmail: c.Email}
		if a.Size != nil {
			v.Size = int64(*a.Size)
		}
		if a.Filters != nil {
			v.Filters = filterOf(*a.Filters)
		}
		if a.Order != nil {
			v.Order = orderOf(*a.Order)
		}
		r, err := cfg.Store.CreateCohort(ctx, v)
		if err != nil {
			return failure(err)
		}
		return answer(cohortOf(r))
	})
	add(srv, "edit_cohort", DescribeEditCohort, mcp.Additive, func(ctx context.Context, c identity.Caller, a EditCohortArgs) mcp.Result {
		if a.Name != nil && !store.ValidCohortName(*a.Name) || !validParts(a.Size, a.Filters, a.Order) {
			return refuse(passes.RefusalInvalid)
		}
		if !store.ValidCohortID(a.Cohort) {
			return refuse(passes.RefusalNotFound)
		}
		v := store.CohortEdit{ID: a.Cohort, Name: a.Name, UserID: c.UserID, UserEmail: c.Email}
		if a.Size != nil {
			sz := int64(*a.Size)
			v.Size = &sz
		}
		if a.Filters != nil {
			f := filterOf(*a.Filters)
			v.Filters = &f
		}
		if a.Order != nil {
			o := orderOf(*a.Order)
			v.Order = &o
		}
		r, err := cfg.Store.EditCohort(ctx, v)
		if err != nil {
			return failure(err)
		}
		return answer(cohortOf(r))
	})
	add(srv, "delete_cohort", DescribeDeleteCohort, mcp.Destructive, func(ctx context.Context, _ identity.Caller, a CohortArgs) mcp.Result {
		if !store.ValidCohortID(a.Cohort) {
			return refuse(passes.RefusalNotFound)
		}
		r, err := cfg.Store.DeleteCohort(ctx, a.Cohort)
		if err != nil {
			return failure(err)
		}
		return answer(cohortOf(r))
	})
	for _, tool := range []struct{ name, description, kind string }{{"reject", DescribeReject, store.KindReject}, {"unreject", DescribeUnreject, store.KindUnreject}} {
		add(srv, tool.name, tool.description, mcp.Additive, func(ctx context.Context, c identity.Caller, a RejectArgs) mcp.Result {
			if strings.TrimSpace(a.Why) == "" {
				return refuse(passes.RefusalInvalid)
			}
			if !store.ValidCohortID(a.Cohort) || !store.ValidCandidateID(a.Candidate) {
				return refuse(passes.RefusalNotFound)
			}
			r, err := cfg.Store.Reject(ctx, store.Reject{Kind: tool.kind, Cohort: a.Cohort, Candidate: a.Candidate, Why: a.Why, UserID: c.UserID, UserEmail: c.Email})
			if err != nil {
				return failure(err)
			}
			return answer(rejectOf(r))
		})
	}

}
func timePosition(tool string, page *string, limit *int) (int, string, string, error) {
	z, err := paging.Window(limit)
	if err != nil {
		return 0, "", "", err
	}
	var key, id string
	if page != nil && *page != "" {
		err = (paging.Token{Tool: tool}).Decode(*page, &key, &id)
	}
	return z, key, id, err
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}
func peopleOf(ps []sources.Person) []Person {
	out := []Person{}
	for _, p := range ps {
		out = append(out, Person{p.Name, p.Role})
	}
	return out
}
func candidateOf(c store.Candidate) Candidate {
	return Candidate{c.ID, c.Status, c.MergedInto, c.Name, c.Names, c.Street, c.City, c.State, c.Zip, c.County, c.Phone, c.Email, peopleOf(c.People), c.Class, c.Cargo, stamp(c.Created), stamp(c.Updated)}
}
func rulingOf(r store.Ruling) Ruling {
	return Ruling{r.Kind, r.Candidate, r.Other, r.Keys, r.Why, r.UserID, r.UserEmail, stamp(r.At)}
}
func linkOf(l store.Link) Link {
	r := l.Reading
	return Link{l.Scraper, l.SourceID, l.Run, l.Digest, r.Name, r.Names, r.Street, r.City, r.State, r.Zip, r.County, r.Phone, r.Email, peopleOf(r.People), r.Class, r.Cargo, date(r.Filed), fleetOf(r.FleetVerdict), r.NameKey, r.PhoneKey, r.DomainKey, l.Reason, l.Confidence, stamp(l.FirstSeen), stamp(l.LastSeen), l.Gone}
}
func estimateOf(e sources.Estimate) Estimate {
	return Estimate{e.Source, e.Low, e.High, e.Confidence, e.Facts, date(e.Filed), e.Formula}
}
func assessmentOf(a store.Assessment) Assessment {
	v := Assessment{OperatingConfidence: a.Operating, OperatingEvidence: []Evidence{}, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, KnownSince: date(a.KnownSince), NewestFiling: date(a.NewestFiling), Categories: []Category{}, GovernmentMatch: a.GovernmentMatch, FleetVerdict: a.FleetVerdict, PowerUnits: a.PowerUnits, Drivers: a.Drivers, Website: a.Website, WebsiteURL: a.WebsiteURL, Minutes: a.Minutes, Excluded: a.Excluded, Rules: a.Rules, Calculated: stamp(a.Calculated)}
	for _, e := range a.Evidence {
		v.OperatingEvidence = append(v.OperatingEvidence, Evidence{e.Name, e.Source, e.Weight, e.Description})
	}
	for _, c := range a.Categories {
		v.Categories = append(v.Categories, Category{c.Name, c.Source})
	}
	return v
}
func memberOf(e store.Ranked) Member {
	a := e.Assessment
	v := Member{Candidate: candidateOf(e.Candidate), OperatingConfidence: a.Operating, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, Categories: []string{}, FleetVerdict: a.FleetVerdict, Website: a.Website, WebsiteURL: a.WebsiteURL, KnownSince: date(a.KnownSince), NewestFiling: date(a.NewestFiling), Excluded: a.Excluded, Minutes: a.Minutes, GovernmentMatch: a.GovernmentMatch}
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

func detail(ctx context.Context, cfg Config, id string) mcp.Result {
	c, err := cfg.Store.GetCandidate(ctx, id)
	if err != nil {
		return failure(err)
	}
	out := Detail{Candidate: candidateOf(c), Estimates: []Estimate{}, Links: []Link{}, Rulings: []Ruling{}, Enrichments: []Enrichment{}, Rejects: []Reject{}}
	rs, err := cfg.Store.Rulings(ctx, id)
	if err != nil {
		return failure(err)
	}
	for _, r := range rs {
		out.Rulings = append(out.Rulings, rulingOf(r))
	}
	xs, err := cfg.Store.Rejects(ctx, id)
	if err != nil {
		return failure(err)
	}
	for _, x := range xs {
		out.Rejects = append(out.Rejects, rejectOf(x))
	}
	if c.Status == store.StatusActive {
		a, err := cfg.Store.GetAssessment(ctx, id)
		if err == nil {
			v := assessmentOf(a)
			out.Assessment = &v
		} else if !errors.Is(err, store.ErrNotFound) {
			return failure(err)
		}
		enrichments, err := cfg.Store.Enrichments(ctx, id)
		if err != nil {
			return failure(err)
		}
		for _, e := range enrichments {
			out.Enrichments = append(out.Enrichments, enrichmentOf(e))
		}

		es, err := cfg.Store.Estimates(ctx, id)
		if err != nil {
			return failure(err)
		}
		for _, e := range es {
			out.Estimates = append(out.Estimates, estimateOf(e))
		}
		ls, err := cfg.Store.Links(ctx, id)
		if err != nil {
			return failure(err)
		}
		for _, l := range ls {
			out.Links = append(out.Links, linkOf(l))
		}
	}
	return answer(out)
}

func enrichmentOf(e store.Enrichment) Enrichment {
	v := Enrichment{Kind: e.Kind, Source: e.Source, Inputs: e.Inputs, Cost: e.Cost, At: stamp(e.At)}
	if e.Kind == sources.WebsiteCheck {
		var r website.Result
		_ = json.Unmarshal(e.Found, &r)
		v.State, v.URL, v.Title, v.StatusCode, v.Error, v.Domain = r.State, r.URL, r.Title, r.StatusCode, r.Error, e.Source
		v.Size = &r.Size
	} else {
		var b bytes.Buffer
		if len(e.Found) == 0 {
			v.Found = json.RawMessage(`{}`)
		} else if json.Compact(&b, e.Found) == nil {
			v.Found = append(json.RawMessage{}, b.Bytes()...)
		}
	}
	return v
}

func fleetOf(v string) string {
	if v == "" {
		return sources.FleetUnknown
	}
	return v
}
func cohortOf(c store.Cohort) Cohort {
	return Cohort{c.ID, c.Name, c.Size, c.Filters, c.Order, c.CreatedUserID, c.CreatedUserEmail, stamp(c.Created), c.ChangedUserID, c.ChangedUserEmail, stamp(c.Changed)}
}
func rejectOf(r store.Reject) Reject {
	at := ""
	if !r.At.IsZero() {
		at = stamp(r.At)
	}
	return Reject{r.Kind, r.Cohort, r.CohortName, r.Candidate, r.Why, r.UserID, r.UserEmail, at}
}
func cohortReadOf(r store.CohortRead) CohortRead {
	v := CohortRead{Cohort: cohortOf(r.Cohort), Matching: r.Matching, Members: []Member{}, Rejects: []Reject{}}
	for _, m := range r.Members {
		v.Members = append(v.Members, memberOf(m))
	}
	for _, x := range r.Rejects {
		v.Rejects = append(v.Rejects, rejectOf(x))
	}
	return v
}
func decisionOf(r store.Ruling, cs []store.Candidate) Decision {
	v := Decision{Ruling: rulingOf(r), Candidates: []Candidate{}}
	for _, c := range cs {
		v.Candidates = append(v.Candidates, candidateOf(c))
	}
	return v
}
func int64Of(p *uint32) *int64 {
	if p == nil {
		return nil
	}
	v := int64(*p)
	return &v
}
func filterOf(a FilterArgs) store.CohortFilters {
	var websites []string
	if a.Website != nil {
		websites = append([]string{}, (*a.Website)...)
	}
	return store.CohortFilters{RevenueMin: int64Of(a.RevenueMin), RevenueMax: int64Of(a.RevenueMax), RevenueUnknown: a.RevenueUnknown, MaxMinutes: int64Of(a.MaxMinutes), MinutesUnknown: a.MinutesUnknown, Categories: append([]string(nil), a.Categories...), CategoryUnknown: a.CategoryUnknown, Fleet: a.Fleet, FleetUnknown: a.FleetUnknown, Website: websites, WebsiteUnknown: a.WebsiteUnknown, Government: a.Government, MaxFilingAgeDays: int64Of(a.MaxFilingAgeDays), FilingUnknown: a.FilingUnknown, Class: a.Class, ClassUnknown: a.ClassUnknown, ShowExcluded: a.ShowExcluded}
}
func orderOf(a []OrderArgs) []store.CohortOrder {
	v := []store.CohortOrder{}
	for _, o := range a {
		v = append(v, store.CohortOrder{Field: o.Field, Direction: o.Direction})
	}
	return v
}
func validParts(size *int, filters *FilterArgs, order *[]OrderArgs) bool {
	return (size == nil || store.ValidCohortSize(int64(*size))) && (filters == nil || store.ValidCohortFilters(filterOf(*filters))) && (order == nil || store.ValidCohortOrder(orderOf(*order)))
}
