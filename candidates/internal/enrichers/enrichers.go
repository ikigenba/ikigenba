// Package enrichers defines the fixed registry of derived and fetched facts.
package enrichers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Kind and source words identify derived enrichment records.
const (
	KindDriveMinutes = "drive_minutes"
	KindLinkFacts    = "link_facts"
	SourceZipDrive   = "zip_drive"
	SourceLinks      = "links"
	KindSiteText     = "site_text"
	SourcePrompts    = "prompts"
	RunQueued        = "queued"
	RunRunning       = "running"
	RunExited        = "exited"
)

// Link is one source reading in the subject's display order.
type Link struct {
	Scraper, SourceID string
	Gone              bool
	Reading           sources.Reading
}

// Subject holds the stored inputs for a candidate.
type Subject struct {
	ID, Name, City, State, Email, Zip, Website string
	Minutes                                    *int64
	Links                                      []Link
}

// Result is a kind's finding and its provenance.
type Result struct {
	Source string
	Found  json.RawMessage
	Cost   float64
}

// Category records a category and the scraper that reported it.
type Category struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// LinkFound is the projection of live source readings.
type LinkFound struct {
	KnownSince      string     `json:"known_since,omitempty"`
	NewestFiling    string     `json:"newest_filing,omitempty"`
	GovernmentMatch string     `json:"government_match,omitempty"`
	Categories      []Category `json:"categories"`
	FleetVerdict    string     `json:"fleet_verdict"`
	PowerUnits      *int64     `json:"power_units,omitempty"`
	Drivers         *int64     `json:"drivers,omitempty"`
}

// MinutesFound records a known drive time or its absence.
type MinutesFound struct {
	Minutes *int64 `json:"minutes,omitempty"`
}

// Enricher identifies and validates one kind of result.
type Enricher interface {
	Kind() string
	Fetched() bool
	Inputs(s Subject) string
	Valid(found json.RawMessage) bool
}

// Deriver computes a result entirely from stored inputs.
type Deriver interface {
	Enricher
	Derive(s Subject) Result
}

// Fetcher computes results with an outside checker.
type Fetcher interface {
	Enricher
	Ready() bool
	Fetch(ctx context.Context, inputs []string, each func(inputs string, r Result))
}

// DriveMinutes derives the ZIP table's minutes.
type DriveMinutes struct{}

// LinkFacts derives facts shared by a candidate's live links.
type LinkFacts struct{}

// WebsiteCheck fetches candidate domains through its checker.
type WebsiteCheck struct{ Checker *website.Checker }

// Prompter records one run of a prompt per subject.
type Prompter interface {
	Enricher
	Ready() bool
	Prompt() string
	Input(s Subject) json.RawMessage
	Found(out prompts.Outcome, pages int64) Result
}

// SiteTextInput is the input object sent to the site text prompt.
type SiteTextInput struct {
	Candidate string `json:"candidate"`
	Name      string `json:"name"`
	City      string `json:"city"`
	State     string `json:"state"`
	Website   string `json:"website"`
}

// SiteTextFound records the outcome and retained page count of a prompt run.
type SiteTextFound struct {
	Run       string `json:"run"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Pages     int64  `json:"pages"`
	CostNanos int64  `json:"cost_nanos"`
}

// Failed reports whether the run failed to exit successfully.
func (f SiteTextFound) Failed() bool {
	return f.Status != RunExited || f.ExitCode == nil || *f.ExitCode != 0
}

// SiteText builds inputs and records outcomes of the site text prompt.
type SiteText struct{ Prompts *prompts.Client }

// Config supplies the clients used by fetched kinds.
type Config struct {
	Website *website.Checker
	Prompts *prompts.Client
}

// Registry is the fixed ordered set of enrichment kinds.
type Registry struct{ cfg Config }

// Kinds returns independent storage in processing order.
func Kinds() []string {
	return []string{KindDriveMinutes, KindLinkFacts, sources.WebsiteCheck, KindSiteText}
}

// New constructs the registry, including an unavailable website checker.
func New(cfg Config) *Registry { return &Registry{cfg: cfg} }

// All returns fresh ordered element storage.
func (r *Registry) All() []Enricher {
	return []Enricher{DriveMinutes{}, LinkFacts{}, WebsiteCheck{Checker: r.cfg.Website}, SiteText{Prompts: r.cfg.Prompts}}
}

// Get returns the kind with the exact matching word.
func (r *Registry) Get(kind string) (Enricher, bool) {
	for _, e := range r.All() {
		if e.Kind() == kind {
			return e, true
		}
	}
	return nil, false
}

// Valid dispatches validation to the named kind.
func (r *Registry) Valid(kind string, found json.RawMessage) bool {
	e, ok := r.Get(kind)
	return ok && e.Valid(found)
}

// Kind returns the drive-time kind word.
func (DriveMinutes) Kind() string { return KindDriveMinutes }

// Fetched reports whether this kind reaches outside.
func (DriveMinutes) Fetched() bool { return false }

// Inputs returns the display ZIP.
func (DriveMinutes) Inputs(s Subject) string { return s.Zip }

// Derive records the table value, including an unknown ZIP.
func (DriveMinutes) Derive(s Subject) Result {
	return result(SourceZipDrive, MinutesFound{Minutes: knownCount(s.Minutes)})
}

// Valid checks the strict drive-time JSON shape.
func (DriveMinutes) Valid(f json.RawMessage) bool {
	var m MinutesFound
	return strictObject(f, &m) && nonnegative(m.Minutes)
}

// Kind returns the link-facts kind word.
func (LinkFacts) Kind() string { return KindLinkFacts }

// Fetched reports whether this kind reaches outside.
func (LinkFacts) Fetched() bool { return false }

// Inputs fingerprints all links without changing their display order.
func (LinkFacts) Inputs(s Subject) string {
	if len(s.Links) == 0 {
		return ""
	}
	lines := make([]string, 0, len(s.Links))
	for _, l := range s.Links {
		state := "live"
		if l.Gone {
			state = "gone"
		}
		b, _ := json.Marshal(l.Reading)
		lines = append(lines, sources.SourceKey(l.Scraper, l.SourceID)+"\t"+state+"\t"+string(b)+"\n")
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	return hex.EncodeToString(sum[:])
}

// Derive projects the ordered live readings into one result.
func (LinkFacts) Derive(s Subject) Result {
	f := LinkFound{Categories: []Category{}, FleetVerdict: sources.FleetUnknown}
	seen := map[Category]bool{}
	fleet := false
	var known, filed time.Time
	for _, l := range s.Links {
		if l.Gone {
			continue
		}
		r := l.Reading
		if datable(r.KnownSince) && (known.IsZero() || r.KnownSince.Before(known)) {
			known = r.KnownSince
		}
		if datable(r.Filed) && (filed.IsZero() || r.Filed.After(filed)) {
			filed = r.Filed
		}
		if f.GovernmentMatch == "" {
			f.GovernmentMatch = r.GovernmentMatch
		}
		if r.Category != "" && l.Scraper != "" {
			c := Category{Name: r.Category, Source: l.Scraper}
			if !seen[c] {
				seen[c] = true
				f.Categories = append(f.Categories, c)
			}
		}
		if !fleet && l.Scraper == fmcsa.Name {
			fleet = true
			if r.FleetVerdict == sources.FleetPass || r.FleetVerdict == sources.FleetFail || r.FleetVerdict == sources.FleetUnknown {
				f.FleetVerdict = r.FleetVerdict
			}
			f.PowerUnits = knownCount(r.PowerUnits)
			f.Drivers = knownCount(r.Drivers)
		}
	}
	if !known.IsZero() {
		f.KnownSince = known.UTC().Format(time.DateOnly)
	}
	if !filed.IsZero() {
		f.NewestFiling = filed.UTC().Format(time.DateOnly)
	}
	if f.GovernmentMatch != "" {
		category := sources.CategoryGovernment
		if f.GovernmentMatch == "SCHOOL DISTRICT" || f.GovernmentMatch == "UNIVERSITY" {
			category = sources.CategoryEducation
		}
		c := Category{Name: category, Source: "name"}
		if !seen[c] {
			f.Categories = append(f.Categories, c)
		}
	}
	slices.SortFunc(f.Categories, func(a, b Category) int {
		if n := strings.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return strings.Compare(a.Source, b.Source)
	})
	return result(SourceLinks, f)
}

func datable(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 0 && t.UTC().Year() <= 9999
}

// Valid checks the strict link-facts JSON shape.
func (LinkFacts) Valid(b json.RawMessage) bool {
	var f LinkFound
	if !strictObject(b, &f) {
		return false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return false
	}
	categories := bytes.TrimSpace(obj["categories"])
	if len(categories) == 0 || categories[0] != '[' {
		return false
	}
	if !validDate(f.KnownSince) || !validDate(f.NewestFiling) || !nonnegative(f.PowerUnits) || !nonnegative(f.Drivers) {
		return false
	}
	if f.FleetVerdict != sources.FleetPass && f.FleetVerdict != sources.FleetFail && f.FleetVerdict != sources.FleetUnknown {
		return false
	}
	for _, c := range f.Categories {
		if c.Name == "" || c.Source == "" {
			return false
		}
	}
	return true
}
func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}
func nonnegative(v *int64) bool { return v == nil || *v >= 0 }
func knownCount(v *int64) *int64 {
	if !nonnegative(v) {
		return nil
	}
	return v
}
func strictObject(b json.RawMessage, v any) bool {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return false
	}
	return d.Decode(new(any)) == io.EOF
}
func result(source string, v any) Result {
	b, _ := json.Marshal(v)
	return Result{Source: source, Found: b}
}

// Kind returns the website kind word.
func (WebsiteCheck) Kind() string { return sources.WebsiteCheck }

// Fetched reports whether this kind reaches outside.
func (WebsiteCheck) Fetched() bool { return true }

// Inputs returns the checked own domain.
func (WebsiteCheck) Inputs(s Subject) string { return website.Domain(s.Email) }

// Valid accepts website results, including earlier records' extra members.
func (WebsiteCheck) Valid(b json.RawMessage) bool {
	b = bytes.TrimSpace(b)
	var w website.Result
	return len(b) > 0 && b[0] == '{' && json.Unmarshal(b, &w) == nil && (w.State == sources.WebsiteHTTPS || w.State == sources.WebsiteHTTP || w.State == sources.WebsiteNone) && w.StatusCode >= 0 && w.Size >= 0
}

// Ready reports whether a checker is available.
func (w WebsiteCheck) Ready() bool { return w.Checker != nil }

// Fetch relays checked domains as canonical website results.
func (w WebsiteCheck) Fetch(ctx context.Context, inputs []string, each func(string, Result)) {
	w.Checker.CheckAll(ctx, inputs, func(in string, r website.Result) { each(in, result(in, r)) })
}

// PromptName converts a kind word into its prompt name.
func PromptName(kind string) string { return strings.ReplaceAll(kind, "_", "-") }

// Kind returns the site text kind word.
func (SiteText) Kind() string { return KindSiteText }

// Fetched reports that this kind runs outside the service.
func (SiteText) Fetched() bool { return true }

// Inputs returns the stored website URL without normalization.
func (SiteText) Inputs(s Subject) string { return s.Website }

// Ready reports whether a prompts client is available.
func (s SiteText) Ready() bool { return s.Prompts != nil }

// Prompt returns the catalog name of the prompt to run.
func (SiteText) Prompt() string { return PromptName(KindSiteText) }

// Input constructs the prompt's candidate input object.
func (SiteText) Input(s Subject) json.RawMessage {
	b, _ := json.Marshal(SiteTextInput{s.ID, s.Name, s.City, s.State, s.Website})
	return b
}

// Found records the run's outcome without retaining its output.
func (SiteText) Found(out prompts.Outcome, pages int64) Result {
	r := result(SourcePrompts, SiteTextFound{out.ID, out.Status, out.ExitCode, pages, out.CostNanos})
	r.Cost = float64(out.CostNanos) / 1e9
	return r
}

// Valid checks the strict site text outcome shape.
func (SiteText) Valid(b json.RawMessage) bool {
	var f SiteTextFound
	return strictObject(b, &f) && f.Run != "" && f.Status != "" && f.Pages >= 0 && f.CostNanos >= 0
}
