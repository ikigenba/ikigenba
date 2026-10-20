package enrichers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// WebsiteSearchInput supplies identity and source evidence to a website search.
type WebsiteSearchInput struct {
	Candidate   string        `json:"candidate"`
	Name        string        `json:"name"`
	City        string        `json:"city"`
	State       string        `json:"state"`
	Street      string        `json:"street"`
	Zip         string        `json:"zip"`
	Phone       string        `json:"phone"`
	Domain      string        `json:"domain"`
	DomainCheck string        `json:"domain_check"`
	DomainError string        `json:"domain_error"`
	People      []BriefPerson `json:"people"`
	Categories  []Category    `json:"categories"`
}

// MarshalJSON encodes absent collections as empty arrays.
func (v WebsiteSearchInput) MarshalJSON() ([]byte, error) {
	type plain WebsiteSearchInput
	if v.People == nil {
		v.People = []BriefPerson{}
	}
	if v.Categories == nil {
		v.Categories = []Category{}
	}
	return json.Marshal(plain(v))
}

// WebsiteSearchRun records the prompt run and its cost.
type WebsiteSearchRun struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	CostNanos int64  `json:"cost_nanos"`
}

// WebsiteSearchSite records a site set aside by a search.
type WebsiteSearchSite struct {
	URL string `json:"url"`
	Why string `json:"why"`
}

// WebsiteSearchAnswer records the proposed website and alternatives.
type WebsiteSearchAnswer struct {
	URL      string              `json:"url"`
	Why      string              `json:"why"`
	Rejected []WebsiteSearchSite `json:"rejected"`
}

// MarshalJSON encodes an absent alternatives list as an empty array.
func (v WebsiteSearchAnswer) MarshalJSON() ([]byte, error) {
	type plain WebsiteSearchAnswer
	if v.Rejected == nil {
		v.Rejected = []WebsiteSearchSite{}
	}
	return json.Marshal(plain(v))
}

// WebsiteSearchConfirmation records the single confirmation fetch.
type WebsiteSearchConfirmation struct {
	State      string `json:"state"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	StatusCode int    `json:"status_code"`
	Size       int64  `json:"size"`
	Error      string `json:"error"`
}

// WebsiteSearchFound keeps the run, decoded answer and optional confirmation.
type WebsiteSearchFound struct {
	Run          WebsiteSearchRun           `json:"run"`
	Answer       *WebsiteSearchAnswer       `json:"answer,omitempty"`
	Confirmation *WebsiteSearchConfirmation `json:"confirmation,omitempty"`
}

// Failed reports an unsuccessful run or a missing answer.
func (f WebsiteSearchFound) Failed() bool {
	return f.Run.Status != RunExited || f.Run.ExitCode == nil || *f.Run.ExitCode != 0 || f.Answer == nil
}

// WebsiteSearch builds prompt inputs and confirms successful search answers.
type WebsiteSearch struct {
	Prompts *prompts.Client
	Checker *website.Checker
}

// Kind returns the website search kind word.
func (WebsiteSearch) Kind() string { return KindWebsiteSearch }

// Fetched reports that a search reaches outside the service.
func (WebsiteSearch) Fetched() bool { return true }

// Ready reports whether both required clients are present.
func (w WebsiteSearch) Ready() bool { return w.Prompts != nil && w.Checker != nil }

// Prompt returns the catalog name for website search.
func (WebsiteSearch) Prompt() string { return PromptName(KindWebsiteSearch) }

// Inputs fingerprints identity unless another source already found a website.
func (WebsiteSearch) Inputs(s Subject) string {
	if s.Website != "" && s.WebsiteSource != "search" {
		return ""
	}
	sum := sha256.Sum256([]byte(s.Name + "\t" + s.City + "\t" + s.State))
	return hex.EncodeToString(sum[:])
}

// Input supplies identity, own-domain check and source evidence.
func (WebsiteSearch) Input(s Subject) json.RawMessage {
	v := WebsiteSearchInput{Candidate: s.ID, Name: s.Name, City: s.City, State: s.State, Street: s.Street, Zip: s.Zip, Phone: s.Phone, Domain: website.Domain(s.Email), People: []BriefPerson{}, Categories: []Category{}}
	if v.Domain != "" {
		v.DomainCheck = sources.WebsiteUnknown
		if s.Check != nil {
			v.DomainCheck = s.Check.State
			v.DomainError = s.Check.Error
		}
	}
	var brief BriefInput
	_ = json.Unmarshal((Brief{}).Input(s), &brief)
	v.People = append(v.People, brief.Facts.People...)
	if s.Facts != nil {
		for _, c := range s.Facts.Categories {
			if c.Source != KindBrief {
				v.Categories = append(v.Categories, c)
			}
		}
	}
	b, _ := json.Marshal(v)
	return b
}

func object(b json.RawMessage) (map[string]json.RawMessage, bool) {
	b = bytes.TrimSpace(b)
	var m map[string]json.RawMessage
	if len(b) == 0 || b[0] != '{' || json.Unmarshal(b, &m) != nil {
		return nil, false
	}
	return m, true
}
func array(b json.RawMessage) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && b[0] == '['
}
func searchSite(b json.RawMessage) (WebsiteSearchSite, bool) {
	m, ok := object(b)
	var v WebsiteSearchSite
	if !ok {
		return v, false
	}
	for key, dst := range map[string]*string{"url": &v.URL, "why": &v.Why} {
		raw := bytes.TrimSpace(m[key])
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, dst) != nil {
			return v, false
		}
	}
	return v, true
}
func searchAnswer(b json.RawMessage) *WebsiteSearchAnswer {
	site, ok := searchSite(b)
	if !ok {
		return nil
	}
	m, _ := object(b)
	if !array(m["rejected"]) {
		return nil
	}
	var rejected []json.RawMessage
	if json.Unmarshal(m["rejected"], &rejected) != nil {
		return nil
	}
	v := &WebsiteSearchAnswer{URL: site.URL, Why: site.Why, Rejected: []WebsiteSearchSite{}}
	for _, raw := range rejected {
		x, valid := searchSite(raw)
		if !valid {
			return nil
		}
		v.Rejected = append(v.Rejected, x)
	}
	return v
}

// Found retains the run and answer and confirms a successful proposed URL.
func (w WebsiteSearch) Found(ctx context.Context, out prompts.Outcome, _ int64) Result {
	run := WebsiteSearchRun{ID: out.ID, Status: out.Status, CostNanos: out.CostNanos}
	if out.ExitCode != nil {
		code := *out.ExitCode
		run.ExitCode = &code
	}
	f := WebsiteSearchFound{Run: run, Answer: searchAnswer(json.RawMessage(out.Stdout))}
	if !f.Failed() && f.Answer.URL != "" && w.Checker != nil {
		pg := w.Checker.Get(ctx, f.Answer.URL)
		state := sources.WebsiteNone
		if u, err := url.Parse(pg.URL); pg.Error == "" && err == nil {
			switch u.Scheme {
			case "https":
				state = sources.WebsiteHTTPS
			case "http":
				state = sources.WebsiteHTTP
			}
		}
		f.Confirmation = &WebsiteSearchConfirmation{state, pg.URL, pg.Title, pg.StatusCode, pg.Size, pg.Error}
	}
	r := result(SourcePrompts, f)
	r.Cost = float64(out.CostNanos) / 1e9
	return r
}

// Valid checks the strict nested record shape and confirmation invariants.
func (WebsiteSearch) Valid(b json.RawMessage) bool {
	m, ok := object(b)
	if !ok {
		return false
	}
	if _, ok = object(m["run"]); !ok {
		return false
	}
	for key, raw := range m {
		if key != "run" && key != "answer" && key != "confirmation" {
			return false
		}
		nested, valid := object(raw)
		if !valid {
			return false
		}
		if key == "answer" && !array(nested["rejected"]) {
			return false
		}
	}
	var f WebsiteSearchFound
	if !strictObject(b, &f) || f.Run.ID == "" || f.Run.Status == "" || f.Run.CostNanos < 0 {
		return false
	}
	if c := f.Confirmation; c != nil {
		if f.Answer == nil || f.Answer.URL == "" || c.StatusCode < 0 || c.Size < 0 || (c.State != sources.WebsiteHTTPS && c.State != sources.WebsiteHTTP && c.State != sources.WebsiteNone) {
			return false
		}
	}
	return true
}

// Failed applies the decoded record's failure rule.
func (WebsiteSearch) Failed(b json.RawMessage) bool {
	var f WebsiteSearchFound
	return json.Unmarshal(b, &f) == nil && f.Failed()
}
