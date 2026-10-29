package enrichers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// BriefPerson is a named person in live source evidence.
type BriefPerson struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// BriefPage is the bounded page text sent to a prompt.
type BriefPage struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// BriefFacts is the stored evidence supplied without earlier brief categories.
type BriefFacts struct {
	Categories      []Category    `json:"categories,omitempty"`
	GovernmentMatch string        `json:"government_match,omitempty"`
	FleetVerdict    string        `json:"fleet_verdict,omitempty"`
	Class           string        `json:"class,omitempty"`
	PowerUnits      *int64        `json:"power_units,omitempty"`
	Drivers         *int64        `json:"drivers,omitempty"`
	KnownSince      string        `json:"known_since,omitempty"`
	NewestFiling    string        `json:"newest_filing,omitempty"`
	People          []BriefPerson `json:"people,omitempty"`
}

// BriefInput is one candidate's prompt input.
type BriefInput struct {
	Candidate string      `json:"candidate"`
	Name      string      `json:"name"`
	City      string      `json:"city"`
	State     string      `json:"state"`
	Website   string      `json:"website"`
	Facts     BriefFacts  `json:"facts"`
	Pages     []BriefPage `json:"pages"`
}

// BriefCategory is a recognized category and the model's reason.
type BriefCategory struct {
	Name string `json:"name"`
	Why  string `json:"why"`
}

// BriefFound retains the prompt outcome and decoded answer.
type BriefFound struct {
	Run        string          `json:"run"`
	Status     string          `json:"status"`
	ExitCode   *int            `json:"exit_code,omitempty"`
	CostNanos  int64           `json:"cost_nanos"`
	Summary    string          `json:"summary"`
	Categories []BriefCategory `json:"categories"`
	Labels     []string        `json:"labels"`
}

// Failed reports a run without a successful exit and nonempty summary.
func (f BriefFound) Failed() bool {
	return f.Status != RunExited || f.ExitCode == nil || *f.ExitCode != 0 || f.Summary == ""
}

// Brief constructs inputs and records outcomes of the brief prompt.
type Brief struct{ Prompts *prompts.Client }

// Kind returns the brief kind word.
func (Brief) Kind() string { return KindBrief }

// Fetched reports that this kind runs outside the service.
func (Brief) Fetched() bool { return true }

// Ready reports whether a prompts client is available.
func (b Brief) Ready() bool { return b.Prompts != nil }

// Prompt returns the brief catalog name.
func (Brief) Prompt() string { return PromptName(KindBrief) }

// Inputs fingerprints page URLs and copy numbers without changing page order.
func (Brief) Inputs(s Subject) string {
	if len(s.Pages) == 0 {
		return ""
	}
	lines := make([]string, 0, len(s.Pages))
	for _, p := range s.Pages {
		lines = append(lines, p.URL+"\t"+strconv.FormatInt(p.Number, 10)+"\n")
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	return hex.EncodeToString(sum[:])
}

// Input builds bounded page text and evidence without feedback from earlier briefs.
func (Brief) Input(s Subject) json.RawMessage {
	f := BriefFacts{}
	if s.Facts != nil {
		x := s.Facts
		f = BriefFacts{GovernmentMatch: x.GovernmentMatch, FleetVerdict: x.FleetVerdict, PowerUnits: x.PowerUnits, Drivers: x.Drivers, KnownSince: x.KnownSince, NewestFiling: x.NewestFiling}
		for _, c := range x.Categories {
			if c.Source != KindBrief {
				f.Categories = append(f.Categories, c)
			}
		}
	}
	for _, l := range s.Links {
		if !l.Gone && l.Scraper == fmcsa.Name {
			f.Class = l.Reading.Class
			break
		}
	}
	seen := map[BriefPerson]bool{}
	for _, p := range s.People {
		v := BriefPerson{p.Value.Name, p.Value.Role}
		if v.Name != "" && !seen[v] {
			seen[v] = true
			f.People = append(f.People, v)
		}
	}

	pages := []BriefPage{}
	used := 0
	for _, p := range s.Pages {
		text := p.Text
		if len(text) > BriefPageBytes {
			n := BriefPageBytes
			for n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
			text = text[:n]
		}
		if used+len(text) > BriefInputBytes {
			break
		}
		used += len(text)
		pages = append(pages, BriefPage{p.URL, p.Title, text})
	}
	b, _ := json.Marshal(BriefInput{s.ID, s.Name, s.City, s.State, s.Website, f, pages})
	return b
}

// Found retains decoded output even when the run itself failed.
func (Brief) Found(_ context.Context, _ Subject, out prompts.Outcome, _ int64) Result {
	f := BriefFound{Run: out.ID, Status: out.Status, ExitCode: out.ExitCode, CostNanos: out.CostNanos, Categories: []BriefCategory{}, Labels: []string{}}
	var answer struct {
		Summary    string          `json:"summary"`
		Categories []BriefCategory `json:"categories"`
		Labels     []string        `json:"labels"`
	}
	if json.Unmarshal([]byte(out.Stdout), &answer) == nil {
		f.Summary = answer.Summary
		f.Labels = append(f.Labels, answer.Labels...)
		for _, c := range answer.Categories {
			if slices.Contains(sources.Categories(), c.Name) {
				f.Categories = append(f.Categories, c)
			} else {
				f.Labels = append(f.Labels, c.Name)
			}
		}
	}
	r := result(SourcePrompts, f)
	r.Cost = float64(out.CostNanos) / 1e9
	return r
}

// Valid checks strict recorded results and recognized category words.
func (Brief) Valid(b json.RawMessage) bool {
	var f BriefFound
	if !strictObject(b, &f) || f.Run == "" || f.Status == "" || f.CostNanos < 0 {
		return false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return false
	}
	for _, key := range []string{"categories", "labels"} {
		v := bytes.TrimSpace(obj[key])
		if len(v) == 0 || v[0] != '[' {
			return false
		}
	}
	for _, c := range f.Categories {
		if !slices.Contains(sources.Categories(), c.Name) {
			return false
		}
	}
	return true
}

// Failed reports whether a decoded brief result records a failed run.
func (Brief) Failed(b json.RawMessage) bool {
	var f BriefFound
	return json.Unmarshal(b, &f) == nil && f.Failed()
}
