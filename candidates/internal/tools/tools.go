package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Tool descriptions and refusal sentences are public copy.
const (
	DescribeProspectLists      string = "Read the saved prospect lists of candidate businesses."
	DescribeProspectList       string = "Read a prospect list with its members and standing rejects."
	DescribeExclude            string = "Exclude a candidate from every prospect list."
	DescribeInclude            string = "Include a candidate in every matching prospect list again."
	DescribeCreateProspectList string = "Create a saved search over candidate businesses."
	DescribeEditProspectList   string = "Edit a prospect list definition."
	DescribeDeleteProspectList string = "Delete a prospect list definition."
	DescribeReject             string = "Reject a candidate from a prospect list."
	DescribeUnreject           string = "Remove a candidate's standing rejection from a prospect list."
	DescribeCandidates         string = "Find candidate businesses by name, ZIP or phone."
	DescribeCandidate          string = "Read a candidate with its enrichments, judgments, evidence and rulings."
	DescribePasses             string = "Read recorded passes."
	DescribePass               string = "Read a pass."
	DescribeEnrich             string = "Enrich candidates whose inputs changed since their last result, for one kind or every kind."
	DescribeAddPage            string = "Keep a page from a candidate website."
	DescribeJudge              string = "Start a judge pass over all candidate businesses."
	DescribeIngest             string = "Start an ingest over current scraper runs."
	DescribeDecide             string = "Record an owner's ruling and judge the affected candidates."
	RefuseNotFound             string = "The requested record was not found."
	RefuseInvalid              string = "The arguments are invalid."
	RefuseBusy                 string = "A pass is already running."
	RefuseStopping             string = "The service is stopping."
	RefuseUnavailable          string = "The service is unavailable."
	RefuseNoWebsite            string = "The candidate has no website."
	RefuseOffSite              string = "The URL is outside the candidate website."
	RefusePageCap              string = "The candidate already holds 20 distinct URLs."
	RefuseUnreadable           string = "The page yielded no readable text."
	RefuseFetch                string = "The page could not be fetched."
)

// Config supplies the engine and database.
type Config struct {
	Passes  *passes.Engine
	Store   *store.Store
	Website *website.Checker
}

// Entry describes a tool for the tools page.
type Entry struct{ Name, Description string }

// Catalog returns independent ordered tool metadata.
func Catalog() []Entry {
	return []Entry{{"prospect_lists", DescribeProspectLists}, {"prospect_list", DescribeProspectList}, {"candidates", DescribeCandidates}, {"candidate", DescribeCandidate}, {"passes", DescribePasses}, {"pass", DescribePass}, {"ingest", DescribeIngest}, {"judge", DescribeJudge}, {"enrich", DescribeEnrich}, {"add_page", DescribeAddPage}, {"decide", DescribeDecide}, {"exclude", DescribeExclude}, {"include", DescribeInclude}, {"create_prospect_list", DescribeCreateProspectList}, {"edit_prospect_list", DescribeEditProspectList}, {"delete_prospect_list", DescribeDeleteProspectList}, {"reject", DescribeReject}, {"unreject", DescribeUnreject}, {"parks", DescribeParks}, {"park", DescribePark}, {"park_rules", DescribeParkRules}, {"edit_park_rules", DescribeEditParkRules}, {"walk", DescribeWalk}}
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

// Register installs the twenty-three candidate tools on srv.
func Register(srv *mcp.Server, cfg Config) {
	add(srv, "prospect_lists", DescribeProspectLists, mcp.Read, func(ctx context.Context, _ identity.Caller, a ProspectListsArgs) mcp.Result {
		z, key, id, err := timePosition("prospect_lists", a.Page, a.Limit)
		if err != nil {
			return mcp.ErrorResult(err.Error())
		}
		rows, err := cfg.Store.ListProspectLists(ctx, key, id, z+1)
		if err != nil {
			return failure(err)
		}
		out := ProspectListList{ProspectLists: []ProspectList{}}
		for i, c := range rows {
			if i == z {
				break
			}
			out.ProspectLists = append(out.ProspectLists, prospectListOf(c))
		}
		if len(rows) > z {
			last := rows[z-1]
			token := paging.Token{Tool: "prospect_lists"}.Encode(last.Name, last.ID)
			out.Next = &token
		}
		return answer(out)
	})
	add(srv, "prospect_list", DescribeProspectList, mcp.Read, func(ctx context.Context, c identity.Caller, a ReadProspectListArgs) mcp.Result {
		a.ProspectList = store.MapListID(a.ProspectList)
		if !store.ValidProspectListID(a.ProspectList) {
			return refuse(passes.RefusalNotFound)
		}
		var r store.ProspectListRead
		var err error
		if a.Peek {
			r, err = cfg.Store.ReadProspectList(ctx, a.ProspectList)
		} else {
			r, err = cfg.Store.OpenProspectList(ctx, a.ProspectList, c.UserID, c.Email)
		}
		if err != nil {
			return failure(err)
		}
		return answer(prospectListReadOf(r))
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
		return passAnswer(ctx, cfg.Store, p)
	})
	add(srv, "ingest", DescribeIngest, mcp.Additive, func(ctx context.Context, c identity.Caller, a IngestArgs) mcp.Result {
		p, err := cfg.Passes.Ingest(ctx, c, a.Scraper)
		if err != nil {
			var r *passes.Refusal
			if errors.As(err, &r) {
				return engineRefusal(r)
			}
			return refuse(passes.RefusalUnavailable)
		}
		return answer(passes.RecordOf(p))
	})
	add(srv, "judge", DescribeJudge, mcp.Additive, func(ctx context.Context, c identity.Caller, _ NoArgs) mcp.Result {
		p, err := cfg.Passes.Judge(ctx, c)
		if err != nil {
			var r *passes.Refusal
			if errors.As(err, &r) {
				return engineRefusal(r)
			}
			return refuse(passes.RefusalUnavailable)
		}
		return answer(passes.RecordOf(p))
	})
	add(srv, "enrich", DescribeEnrich, mcp.Additive, func(ctx context.Context, c identity.Caller, a EnrichArgs) mcp.Result {
		a.ProspectList = store.MapListID(a.ProspectList)
		targeted := a.ProspectList != "" || a.Candidate != ""
		if a.Kind != "" && !slices.Contains(enrichers.Kinds(), a.Kind) || a.ProspectList != "" && a.Candidate != "" || a.Force && !targeted || a.Kind == "" && targeted {
			return refuse(passes.RefusalInvalid)
		}
		if !targeted {
			if e, ok := enrichers.New(enrichers.Config{}).Get(a.Kind); ok {
				if _, prompt := e.(enrichers.Prompter); prompt {
					return refuse(passes.RefusalInvalid)
				}
			}
		}
		if a.ProspectList != "" && !store.ValidProspectListID(a.ProspectList) || a.Candidate != "" && !store.ValidCandidateID(a.Candidate) {
			return refuse(passes.RefusalNotFound)
		}
		var p store.Pass
		var err error
		if targeted {
			target := a.ProspectList
			if target == "" {
				target = a.Candidate
			}
			p, err = cfg.Passes.EnrichTarget(ctx, c, a.Kind, target, a.Force)
		} else {
			p, err = cfg.Passes.Enrich(ctx, c, a.Kind)
		}
		if err != nil {
			var r *passes.Refusal
			if errors.As(err, &r) {
				return engineRefusal(r)
			}
			return refuse(passes.RefusalUnavailable)
		}
		return answer(passes.RecordOf(p))
	})
	add(srv, "add_page", DescribeAddPage, mcp.Additive, func(ctx context.Context, c identity.Caller, a AddPageArgs) mcp.Result { return addPage(ctx, c, cfg, a) })

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
	add(srv, "create_prospect_list", DescribeCreateProspectList, mcp.Additive, func(ctx context.Context, c identity.Caller, a CreateProspectListArgs) mcp.Result {
		if !store.ValidProspectListName(a.Name) || !validParts(a.Size, a.Filters, a.Order) {
			return refuse(passes.RefusalInvalid)
		}
		v := store.ProspectList{Name: a.Name, Size: store.ProspectListSizeDefault, CreatedUserID: c.UserID, CreatedUserEmail: c.Email}
		if a.Size != nil {
			v.Size = int64(*a.Size)
		}
		if a.Filters != nil {
			v.Filters = filterOf(*a.Filters)
		}
		if a.Order != nil {
			v.Order = orderOf(*a.Order)
		}
		r, err := cfg.Store.CreateProspectList(ctx, v)
		if err != nil {
			return failure(err)
		}
		return answer(prospectListOf(r))
	})
	add(srv, "edit_prospect_list", DescribeEditProspectList, mcp.Additive, func(ctx context.Context, c identity.Caller, a EditProspectListArgs) mcp.Result {
		a.ProspectList = store.MapListID(a.ProspectList)
		if a.Name != nil && !store.ValidProspectListName(*a.Name) || !validParts(a.Size, a.Filters, a.Order) {
			return refuse(passes.RefusalInvalid)
		}
		if !store.ValidProspectListID(a.ProspectList) {
			return refuse(passes.RefusalNotFound)
		}
		v := store.ProspectListEdit{ID: a.ProspectList, Name: a.Name, UserID: c.UserID, UserEmail: c.Email}
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
		r, err := cfg.Store.EditProspectList(ctx, v)
		if err != nil {
			return failure(err)
		}
		return answer(prospectListOf(r))
	})
	add(srv, "delete_prospect_list", DescribeDeleteProspectList, mcp.Destructive, func(ctx context.Context, _ identity.Caller, a ProspectListArgs) mcp.Result {
		a.ProspectList = store.MapListID(a.ProspectList)
		if !store.ValidProspectListID(a.ProspectList) {
			return refuse(passes.RefusalNotFound)
		}
		r, err := cfg.Store.DeleteProspectList(ctx, a.ProspectList)
		if err != nil {
			return failure(err)
		}
		return answer(prospectListOf(r))
	})
	for _, tool := range []struct{ name, description, kind string }{{"reject", DescribeReject, store.KindReject}, {"unreject", DescribeUnreject, store.KindUnreject}} {
		add(srv, tool.name, tool.description, mcp.Additive, func(ctx context.Context, c identity.Caller, a RejectArgs) mcp.Result {
			a.ProspectList = store.MapListID(a.ProspectList)
			if strings.TrimSpace(a.Why) == "" {
				return refuse(passes.RefusalInvalid)
			}
			if !store.ValidProspectListID(a.ProspectList) || !store.ValidCandidateID(a.Candidate) {
				return refuse(passes.RefusalNotFound)
			}
			r, err := cfg.Store.Reject(ctx, store.Reject{Kind: tool.kind, ProspectList: a.ProspectList, Candidate: a.Candidate, Why: a.Why, UserID: c.UserID, UserEmail: c.Email})
			if err != nil {
				return failure(err)
			}
			return answer(rejectOf(r))
		})
	}

	registerParks(srv, cfg)
	registerWalk(srv, cfg)
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
		out = append(out, Person{Name: p.Name, Role: p.Role})
	}
	return out
}
func sourcedPeopleOf(ps []sources.Sourced[sources.Person]) []Person {
	out := []Person{}
	for _, p := range ps {
		out = append(out, Person{Name: p.Value.Name, Role: p.Value.Role, Source: p.Source})
	}
	return out
}
func sourcesOf(c store.Candidate) Sources {
	out := Sources{Name: c.NameSource, Address: c.AddressSource, Phone: c.PhoneSource, Email: c.EmailSource, OtherNames: []Sourced{}, OtherAddresses: []SourcedAddress{}, OtherPhones: []Sourced{}, OtherEmails: []Sourced{}}
	for _, x := range c.OtherNames {
		out.OtherNames = append(out.OtherNames, Sourced{Value: x.Value, Source: x.Source})
	}
	for _, x := range c.OtherAddresses {
		a := x.Value
		out.OtherAddresses = append(out.OtherAddresses, SourcedAddress{Street: a.Street, City: a.City, State: a.State, Zip: a.Zip, County: a.County, Source: x.Source})
	}
	for _, x := range c.OtherPhones {
		out.OtherPhones = append(out.OtherPhones, Sourced{Value: x.Value, Source: x.Source})
	}
	for _, x := range c.OtherEmails {
		out.OtherEmails = append(out.OtherEmails, Sourced{Value: x.Value, Source: x.Source})
	}
	return out
}
func checkedURL(checked, landed string) string {
	if checked == "" {
		return ""
	}
	if website.Inputs(checked, "") == "" {
		checked = "https://" + checked
	}
	if checked == landed {
		return ""
	}
	a, ea := url.Parse(checked)
	b, eb := url.Parse(landed)
	if ea == nil && eb == nil {
		if a.Path == "" {
			a.Path = "/"
		}
		if b.Path == "" {
			b.Path = "/"
		}
		if a.String() == b.String() {
			return ""
		}
	}
	return checked
}
func candidateOf(c store.Candidate) Candidate {
	return Candidate{c.ID, c.Status, c.MergedInto, c.Name, c.Names, c.Street, c.City, c.State, c.Zip, c.County, c.Phone, c.Email, sourcedPeopleOf(c.People), c.Class, c.Cargo, stamp(c.Created), stamp(c.Updated)}
}
func rulingOf(r store.Ruling) Ruling {
	return Ruling{r.Kind, r.Candidate, r.Other, r.Keys, r.Why, r.UserID, r.UserEmail, stamp(r.At)}
}
func linkOf(l store.Link) Link {
	r := l.Reading
	return Link{l.Scraper, l.SourceID, l.Run, l.Digest, r.Name, r.Names, r.Street, r.City, r.State, r.Zip, r.County, r.Phone, r.Email, peopleOf(r.People), r.Class, r.Cargo, date(r.Filed), fleetOf(r.FleetVerdict), r.NameKey, r.PhoneKey, r.DomainKey, r.EINKey, r.Establishment, r.Year, r.Employees, r.Hours, l.Reason, l.Confidence, stamp(l.FirstSeen), stamp(l.LastSeen), l.Gone, l.Walk, l.Park}
}
func estimateOf(e sources.Estimate) Estimate {
	return Estimate{e.Source, e.Low, e.High, e.Confidence, e.Facts, date(e.Filed), e.Formula}
}
func judgmentsOf(a store.Judgments) Judgments {
	v := Judgments{OperatingConfidence: a.Operating, OperatingEvidence: []Evidence{}, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, HeadcountLow: a.HeadcountLow, HeadcountHigh: a.HeadcountHigh, HeadcountBasis: a.HeadcountBasis, HeadcountSeries: []Series{}, Trend: a.Trend, TrendKind: a.TrendKind, TrendFromYear: a.TrendFromYear, TrendToYear: a.TrendToYear, TrendFrom: a.TrendFrom, TrendTo: a.TrendTo, TrendChange: a.TrendChange, Excluded: a.Excluded, Rules: a.Rules, Calculated: stamp(a.Calculated)}
	for _, s := range a.HeadcountSeries {
		row := Series{Kind: s.Kind, Scraper: s.Scraper, Figures: []Figure{}}
		for _, f := range s.Figures {
			row.Figures = append(row.Figures, Figure{f.Year, f.Count})
		}
		v.HeadcountSeries = append(v.HeadcountSeries, row)
	}
	for _, e := range a.Evidence {
		v.OperatingEvidence = append(v.OperatingEvidence, Evidence{e.Name, e.Source, e.Weight, e.Description})
	}
	return v
}
func factsOf(f store.Facts, origin string) Facts {
	v := Facts{KnownSince: date(f.KnownSince), NewestFiling: date(f.NewestFiling), Categories: []Category{}, ClassifiedBy: store.ClassifiedBy(f.Categories), RecordsDiffer: store.RecordsDiffer(f.Categories, f.Records), RecordCategories: []Category{}, GovernmentMatch: f.GovernmentMatch, FleetVerdict: f.FleetVerdict, PowerUnits: f.PowerUnits, Drivers: f.Drivers, Website: f.Website, WebsiteURL: f.WebsiteURL, WebsiteSource: f.WebsiteSource, Minutes: f.Minutes, MinutesSource: f.MinutesSource, Miles: f.Miles, RouteAddress: f.RouteAddress, Written: []Written{}, Headcounts: []Headcount{}, BriefLabels: append([]string{}, f.BriefLabels...)}
	if !f.KnownSince.IsZero() {
		v.KnownSinceSource = f.KnownSinceSource
	}
	if !f.NewestFiling.IsZero() {
		v.NewestFilingSource = f.NewestFilingSource
	}
	v.WebsiteCheckedURL = checkedURL(f.WebsiteCheckedURL, f.WebsiteURL)
	if f.MinutesSource == store.MinutesByRoute && f.RouteAddress != "" {
		v.PlaceURL = routes.PlaceURL(f.RouteAddress)
		v.DirectionsURL = routes.DirectionsURL(origin, f.RouteAddress)
	}
	for _, c := range f.Categories {
		v.Categories = append(v.Categories, Category{c.Name, c.Source})
	}
	if v.RecordsDiffer {
		for _, c := range f.Records {
			v.RecordCategories = append(v.RecordCategories, Category{c.Name, c.Source})
		}
	}
	for _, h := range f.Headcounts {
		v.Headcounts = append(v.Headcounts, Headcount{h.Scraper, h.Kind, h.Count, date(h.AsOf), h.Hours})
	}
	for _, w := range f.Written {
		v.Written = append(v.Written, Written{w.Kind, stamp(w.At)})
	}
	return v
}
func briefOf(es []store.Enrichment) *Brief {
	for _, e := range es {
		if e.Kind != enrichers.KindBrief {
			continue
		}
		var f enrichers.BriefFound
		if json.Unmarshal(e.Found, &f) != nil || f.Failed() {
			continue
		}
		v := Brief{Summary: f.Summary, Categories: []BriefCategory{}, Labels: f.Labels, At: stamp(e.At)}
		for _, c := range f.Categories {
			v.Categories = append(v.Categories, BriefCategory{c.Name, c.Why})
		}
		return &v
	}
	return nil
}
func memberOf(e store.Ranked) Member {
	a := e.Judgments
	f := e.Facts
	v := Member{Candidate: candidateOf(e.Candidate), Basis: e.Basis, OperatingConfidence: a.Operating, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, HeadcountLow: a.HeadcountLow, Trend: a.Trend, TrendChange: a.TrendChange, Categories: []string{}, ClassifiedBy: store.ClassifiedBy(f.Categories), FleetVerdict: f.FleetVerdict, Website: f.Website, WebsiteURL: f.WebsiteURL, WebsiteSource: f.WebsiteSource, KnownSince: date(f.KnownSince), NewestFiling: date(f.NewestFiling), Excluded: a.Excluded, Minutes: f.Minutes, MinutesSource: f.MinutesSource, GovernmentMatch: f.GovernmentMatch}
	if !e.SiteText.IsZero() {
		t := stamp(e.SiteText)
		v.SiteText = &t
	}
	if !e.Brief.IsZero() {
		t := stamp(e.Brief)
		v.Brief = &t
	}
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

func detail(ctx context.Context, cfg Config, id string) mcp.Result {
	c, err := cfg.Store.GetCandidate(ctx, id)
	if err != nil {
		return failure(err)
	}
	out := Detail{Pages: []Page{}, Candidate: candidateOf(c), Estimates: []Estimate{}, Links: []Link{}, Rulings: []Ruling{}, EnrichmentResults: []EnrichmentResult{}, Rejects: []Reject{}, Campaigns: []CampaignUse{}}
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
		s := sourcesOf(c)
		out.Sources = &s
		park, err := cfg.Store.CandidatePark(ctx, id)
		if err != nil {
			return failure(err)
		}
		if park != "" {
			p, err := cfg.Store.GetPark(ctx, park)
			if err != nil {
				return failure(err)
			}
			out.Park = &ParkRef{park, p.Name}
		}
		subject, err := cfg.Store.Subject(ctx, id)
		if err != nil {
			return failure(err)
		}
		uses, err := cfg.Store.CampaignUses(ctx, id)
		if err != nil {
			return failure(err)
		}
		for _, use := range uses {
			out.Campaigns = append(out.Campaigns, campaignUseOf(use))
		}
		out.Enrichments = &Enrichments{}
		f, err := cfg.Store.GetFacts(ctx, id)
		if err == nil {
			v := factsOf(f, subject.Origin)
			out.Enrichments.Facts = &v
		} else if !errors.Is(err, store.ErrNotFound) {
			return failure(err)
		}
		a, err := cfg.Store.GetJudgments(ctx, id)
		if err == nil {
			v := judgmentsOf(a)
			out.Judgments = &v
		} else if !errors.Is(err, store.ErrNotFound) {
			return failure(err)
		}
		pages, err := cfg.Store.Pages(ctx, id)
		if err != nil {
			return failure(err)
		}
		for _, p := range pages {
			out.Pages = append(out.Pages, pageOf(p))
		}
		enrichments, err := cfg.Store.Enrichments(ctx, id)
		if err != nil {
			return failure(err)
		}
		if out.Enrichments.Facts != nil {
			p, err := cfg.Store.PlacesURL(ctx, id)
			if err != nil {
				return failure(err)
			}
			source, state, landed, _, _, checked, _ := store.WebsiteDecisionOf(enrichments, p)
			out.Enrichments.Facts.WebsiteSource = source
			out.Enrichments.Facts.Website = state
			out.Enrichments.Facts.WebsiteURL = landed
			out.Enrichments.Facts.WebsiteCheckedURL = checkedURL(checked, landed)
		}
		out.Enrichments.Basis = store.BasisOf(enrichments)
		out.Enrichments.Kinds.Present, out.Enrichments.Kinds.Missing = store.KindsOf(enrichments)
		out.Enrichments.Brief = briefOf(enrichments)
		for _, e := range enrichments {
			out.EnrichmentResults = append(out.EnrichmentResults, enrichmentOf(e, subject))
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

func enrichmentOf(e store.Enrichment, subject enrichers.Subject) EnrichmentResult {
	v := EnrichmentResult{Kind: e.Kind, Source: e.Source, Inputs: e.Inputs, Cost: e.Cost, At: stamp(e.At)}
	switch e.Kind {
	case sources.WebsiteCheck:
		var r website.Result
		_ = json.Unmarshal(e.Found, &r)
		v.State, v.URL, v.Title, v.StatusCode, v.Error, v.Domain = r.State, r.URL, r.Title, r.StatusCode, r.Error, e.Source
		v.Size = &r.Size
	case enrichers.KindBrief:
		var found enrichers.BriefFound
		if json.Unmarshal(e.Found, &found) == nil {
			if found.Categories == nil {
				found.Categories = []enrichers.BriefCategory{}
			}
			if found.Labels == nil {
				found.Labels = []string{}
			}
			v.Found, _ = json.Marshal(found)
		}
	default:
		var b bytes.Buffer
		if len(e.Found) == 0 {
			v.Found = json.RawMessage(`{}`)
		} else if json.Compact(&b, e.Found) == nil {
			v.Found = append(json.RawMessage{}, b.Bytes()...)
		}
	}
	if e.Kind == enrichers.KindDriveRoute {
		var found enrichers.RouteFound
		if json.Unmarshal(e.Found, &found) == nil && !found.Failed() && found.Minutes != nil && found.Address != "" {
			v.PlaceURL = routes.PlaceURL(found.Address)
			if e.Inputs == (enrichers.DriveRoute{}).Inputs(subject) {
				v.DirectionsURL = routes.DirectionsURL(subject.Origin, found.Address)
			}
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
func prospectListOf(c store.ProspectList) ProspectList {
	return ProspectList{c.ID, c.Name, c.Size, c.Filters, c.Order, c.CreatedUserID, c.CreatedUserEmail, stamp(c.Created), c.ChangedUserID, c.ChangedUserEmail, stamp(c.Changed)}
}
func rejectOf(r store.Reject) Reject {
	at := ""
	if !r.At.IsZero() {
		at = stamp(r.At)
	}
	return Reject{r.Kind, r.ProspectList, r.ProspectListName, r.Candidate, r.Why, r.UserID, r.UserEmail, at}
}
func prospectListReadOf(r store.ProspectListRead) ProspectListRead {
	v := ProspectListRead{ProspectList: prospectListOf(r.ProspectList), Matching: r.Matching, Classified: r.Classified, Members: []Member{}, Rejects: []Reject{}}
	v.ByBasis = BasisCounts{Records: r.ByBasis.Records, Partial: r.ByBasis.Partial, Enriched: r.ByBasis.Enriched}
	if !r.Changes.Since.IsZero() {
		t := stamp(r.Changes.Since)
		v.Changes.Since = &t
	}
	v.Changes.DefinitionChanged = append([]string{}, r.Changes.DefinitionChanged...)
	for _, j := range r.Changes.Joined {
		v.Changes.Joined = append(v.Changes.Joined, Joined{j.Candidate, j.Name, j.Basis, j.Why})
	}
	for _, l := range r.Changes.Left {
		v.Changes.Left = append(v.Changes.Left, Left{l.Candidate, l.Name, l.BasisThen, l.BasisNow, l.Why, l.Filter})
	}
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
func filterOf(a FilterArgs) store.ProspectListFilters {
	var websites []string
	if a.Website != nil {
		websites = append([]string{}, (*a.Website)...)
	}
	var trends []string
	if a.Trend != nil {
		trends = append([]string{}, (*a.Trend)...)
	}
	var basis []string
	if a.Basis != nil {
		basis = append([]string{}, (*a.Basis)...)
	}
	return store.ProspectListFilters{Basis: basis, RevenueMin: int64Of(a.RevenueMin), RevenueMax: int64Of(a.RevenueMax), RevenueUnknown: a.RevenueUnknown, MaxMinutes: int64Of(a.MaxMinutes), MinutesUnknown: a.MinutesUnknown, Categories: append([]string(nil), a.Categories...), CategoryUnknown: a.CategoryUnknown, Fleet: a.Fleet, FleetUnknown: a.FleetUnknown, Website: websites, WebsiteUnknown: a.WebsiteUnknown, Trend: trends, TrendUnknown: a.TrendUnknown, Government: a.Government, MaxFilingAgeDays: int64Of(a.MaxFilingAgeDays), FilingUnknown: a.FilingUnknown, MinCampaignAgeDays: int64Of(a.MinCampaignAgeDays), Class: a.Class, ClassUnknown: a.ClassUnknown, ShowExcluded: a.ShowExcluded}
}
func orderOf(a []OrderArgs) []store.ProspectListOrder {
	v := []store.ProspectListOrder{}
	for _, o := range a {
		v = append(v, store.ProspectListOrder{Field: o.Field, Direction: o.Direction})
	}
	return v
}
func validParts(size *int, filters *FilterArgs, order *[]OrderArgs) bool {
	return (size == nil || store.ValidProspectListSize(int64(*size))) && (filters == nil || store.ValidProspectListFilters(filterOf(*filters))) && (order == nil || store.ValidProspectListOrder(orderOf(*order)))
}

func engineRefusal(r *passes.Refusal) mcp.Result {
	if r.Text != "" {
		return mcp.ErrorResult(r.Word + ": " + r.Text)
	}
	return refuse(r.Word)
}
func pageOf(p store.Page) Page {
	return Page{p.Number, p.URL, p.Title, p.Size, p.Reason, stamp(p.Fetched), p.UserID, p.UserEmail, urls.PagePath(p.Candidate, p.Number)}
}
func addPage(ctx context.Context, c identity.Caller, cfg Config, a AddPageArgs) mcp.Result {
	if !store.ValidCandidateID(a.Candidate) {
		return refuse(passes.RefusalNotFound)
	}
	u, err := url.Parse(a.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return refuse(passes.RefusalInvalid)
	}
	if cfg.Website == nil {
		return refuse(passes.RefusalUnavailable)
	}
	if _, err := cfg.Store.GetCandidate(ctx, a.Candidate); err != nil {
		return failure(err)
	}
	f, err := cfg.Store.GetFacts(ctx, a.Candidate)
	if errors.Is(err, store.ErrNotFound) || err == nil && f.WebsiteURL == "" {
		return mcp.ErrorResult(passes.RefusalInvalid + ": " + RefuseNoWebsite)
	}
	if err != nil {
		return failure(err)
	}
	if !website.OnSite(f.WebsiteURL, a.URL) {
		return mcp.ErrorResult(passes.RefusalInvalid + ": " + RefuseOffSite)
	}
	n, err := cfg.Store.CountPages(ctx, a.Candidate)
	if err != nil {
		return failure(err)
	}
	if n.URLs >= store.PageCap {
		pages, err := cfg.Store.Pages(ctx, a.Candidate)
		if err != nil {
			return failure(err)
		}
		held := false
		for _, p := range pages {
			if p.URL == a.URL {
				held = true
			}
		}
		if !held {
			return mcp.ErrorResult(passes.RefusalInvalid + ": " + RefusePageCap)
		}
	}
	pg := cfg.Website.Get(ctx, a.URL)
	if pg.Error != "" {
		return mcp.ErrorResult(passes.RefusalUnavailable + ": " + RefuseFetch + " " + pg.Error)
	}
	if !website.OnSite(f.WebsiteURL, pg.URL) {
		return mcp.ErrorResult(passes.RefusalInvalid + ": " + RefuseOffSite)
	}
	p, err := cfg.Store.AddPage(ctx, store.Page{Candidate: a.Candidate, URL: pg.URL, Title: pg.Title, Text: pg.Text, Reason: pg.Reason, Size: pg.Size, UserID: c.UserID, UserEmail: c.Email})
	if errors.Is(err, store.ErrPageCap) {
		return mcp.ErrorResult(passes.RefusalInvalid + ": " + RefusePageCap)
	}
	if errors.Is(err, store.ErrInvalid) {
		return mcp.ErrorResult(passes.RefusalInvalid + ": " + RefuseNoWebsite)
	}
	if err != nil {
		return failure(err)
	}
	if pg.Reason != "" {
		return mcp.ErrorResult(passes.RefusalUnreadable + ": " + RefuseUnreadable + " " + pg.Reason)
	}
	return answer(pageOf(p))
}

func campaignUseOf(u store.CampaignUse) CampaignUse {
	v := CampaignUse{Campaign: u.Campaign, ProspectList: u.ProspectList, Reason: u.Reason}
	if !u.Added.IsZero() {
		v.Added = stamp(u.Added)
	}
	if !u.Rejected.IsZero() {
		v.Rejected = stamp(u.Rejected)
	}
	if !u.Delivered.IsZero() {
		v.Delivered = stamp(u.Delivered)
	}
	return v
}
