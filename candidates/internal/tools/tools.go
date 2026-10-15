package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Tool descriptions and refusal sentences are public copy.
const (
	DescribeShortlist  string = "Read the ranked shortlist of candidate businesses."
	DescribeCandidates string = "Find candidate businesses by name, ZIP or phone."
	DescribeCandidate  string = "Read a candidate with its evidence, assessment and rulings."
	DescribePasses     string = "Read recorded passes."
	DescribePass       string = "Read a pass."
	DescribeRebuild    string = "Start a resolution pass over current scraper runs."
	DescribeDecide     string = "Record an owner's ruling and reassess the affected candidates."
	RefuseNotFound     string = "The requested record was not found."
	RefuseInvalid      string = "The ruling is invalid."
	RefuseBusy         string = "A pass is already running."
	RefuseStopping     string = "The service is stopping."
	RefuseUnavailable  string = "The service is unavailable."
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
	return []Entry{{"shortlist", DescribeShortlist}, {"candidates", DescribeCandidates}, {"candidate", DescribeCandidate}, {"passes", DescribePasses}, {"pass", DescribePass}, {"rebuild", DescribeRebuild}, {"decide", DescribeDecide}}
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

// Register installs the seven candidate tools on srv.
func Register(srv *mcp.Server, cfg Config) {
	add(srv, "shortlist", DescribeShortlist, mcp.Read, func(ctx context.Context, _ identity.Caller, a ShortlistArgs) mcp.Result {
		z, key, id, err := scorePosition(a.Page, a.Limit)
		if err != nil {
			return mcp.ErrorResult(err.Error())
		}
		f := store.ShortlistFilter{Class: a.Class, Cargo: a.Cargo}
		if a.MaxMinutes != nil {
			v := int64(*a.MaxMinutes)
			f.MaxMinutes = &v
		}
		if a.MinRevenue != nil {
			v := int64(*a.MinRevenue)
			f.MinRevenue = &v
		}
		rows, err := cfg.Store.Shortlist(ctx, f, key, id, z+1)
		if err != nil {
			return failure(err)
		}
		out := ShortlistPage{Shortlist: []ShortlistEntry{}}
		for i, e := range rows {
			if i == z {
				break
			}
			out.Shortlist = append(out.Shortlist, shortlistOf(e))
		}
		if len(rows) > z {
			last := rows[z-1]
			token := paging.Token{Tool: "shortlist"}.Encode(last.Assessment.OperatingKey, last.Candidate.ID)
			out.Next = &token
		}
		return answer(out)
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
	add(srv, "decide", DescribeDecide, mcp.Additive, func(ctx context.Context, c identity.Caller, a DecideArgs) mcp.Result {
		pair := a.Kind == store.KindMerge || a.Kind == store.KindNotSame
		if a.Kind != store.KindMerge && a.Kind != store.KindNotSame && a.Kind != store.KindSplit && a.Kind != store.KindExclude && a.Kind != store.KindInclude || strings.TrimSpace(a.Why) == "" || pair && a.Other == "" || a.Kind == store.KindSplit && len(a.Links) == 0 {
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
}
func scorePosition(page *string, limit *int) (int, int64, string, error) {
	z, err := paging.Window(limit)
	if err != nil {
		return 0, 0, "", err
	}
	var key int64
	var id string
	if page != nil && *page != "" {
		err = (paging.Token{Tool: "shortlist"}).Decode(*page, &key, &id)
	}
	return z, key, id, err
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
	return Link{l.Scraper, l.SourceID, l.Run, l.Digest, r.Name, r.Names, r.Street, r.City, r.State, r.Zip, r.County, r.Phone, r.Email, peopleOf(r.People), r.Class, r.Cargo, date(r.Filed), r.Fleet, r.NameKey, r.PhoneKey, r.DomainKey, l.Reason, l.Confidence, stamp(l.FirstSeen), stamp(l.LastSeen), l.Gone}
}
func estimateOf(e sources.Estimate) Estimate {
	return Estimate{e.Source, e.Low, e.High, e.Confidence, e.Facts, date(e.Filed), e.Formula}
}
func assessmentOf(a store.Assessment) Assessment {
	v := Assessment{OperatingConfidence: a.Operating, OperatingEvidence: []Evidence{}, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, KnownSince: date(a.KnownSince), NewestFiling: date(a.NewestFiling), Categories: []Category{}, GovernmentMatch: a.GovernmentMatch, Fleet: a.Fleet, Minutes: a.Minutes, Excluded: a.Excluded, Rules: a.Rules, Calculated: stamp(a.Calculated)}
	for _, e := range a.Evidence {
		v.OperatingEvidence = append(v.OperatingEvidence, Evidence{e.Name, e.Source, e.Weight, e.Description})
	}
	for _, c := range a.Categories {
		v.Categories = append(v.Categories, Category{c.Name, c.Source})
	}
	return v
}
func shortlistOf(e store.Ranked) ShortlistEntry {
	a := e.Assessment
	v := ShortlistEntry{Candidate: candidateOf(e.Candidate), OperatingConfidence: a.Operating, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, Categories: []string{}, Fleet: a.Fleet, Minutes: a.Minutes, GovernmentMatch: a.GovernmentMatch}
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
	out := Detail{Candidate: candidateOf(c), Estimates: []Estimate{}, Links: []Link{}, Rulings: []Ruling{}}
	rs, err := cfg.Store.Rulings(ctx, id)
	if err != nil {
		return failure(err)
	}
	for _, r := range rs {
		out.Rulings = append(out.Rulings, rulingOf(r))
	}
	if c.Status == store.StatusActive {
		a, err := cfg.Store.GetAssessment(ctx, id)
		if err == nil {
			v := assessmentOf(a)
			out.Assessment = &v
		} else if !errors.Is(err, store.ErrNotFound) {
			return failure(err)
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
