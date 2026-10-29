package enrichers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Search reasons and cases record why a search ran and how its answer compares.
const (
	SearchReasonNoWebsite = "no_website"
	SearchReasonEmptySite = "empty_site"
	SearchReasonAgain     = "again"
	SearchCaseDifferent   = "different"
	SearchCaseStands      = "stands"
	SearchCaseNone        = "none"
)

// WebsiteSearchSiteText records the latest extraction against the recorded site.
type WebsiteSearchSiteText struct {
	Status string `json:"status"`
	Pages  int64  `json:"pages"`
}

// WebsiteSearchInput supplies identity and source evidence to a website search.
type WebsiteSearchInput struct {
	Candidate   string                 `json:"candidate"`
	Name        string                 `json:"name"`
	City        string                 `json:"city"`
	State       string                 `json:"state"`
	Street      string                 `json:"street"`
	Zip         string                 `json:"zip"`
	Phone       string                 `json:"phone"`
	Domain      string                 `json:"domain"`
	DomainCheck string                 `json:"domain_check"`
	DomainError string                 `json:"domain_error"`
	Website     string                 `json:"website"`
	WebsiteFrom string                 `json:"website_from"`
	SiteText    *WebsiteSearchSiteText `json:"site_text"`
	People      []BriefPerson          `json:"people"`
	Categories  []Category             `json:"categories"`
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
	Reason       string                     `json:"reason,omitempty"`
	Website      string                     `json:"website,omitempty"`
	WebsiteFrom  string                     `json:"website_from,omitempty"`
	SiteText     *WebsiteSearchSiteText     `json:"site_text,omitempty"`
	Case         string                     `json:"case,omitempty"`
	InputSources []InputSource              `json:"input_sources,omitempty"`
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
	if s.WebsiteSource == sources.Owner {
		return ""
	}
	b := searchHash(s.Name + "\t" + s.City + "\t" + s.State)
	if emptySite(s) {
		return b + "/" + searchHash(s.Website+"\t"+s.SiteText.Run)
	}
	if s.Website != "" && s.WebsiteSource != "search" {
		return ""
	}
	if s.Website != "" && strings.HasPrefix(s.SearchInputs, b+"/") {
		return s.SearchInputs
	}
	return b
}

func searchHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
func emptySite(s Subject) bool {
	return s.Website != "" && s.SiteText != nil && s.SiteText.Pages == 0 && len(s.Pages) == 0
}
func websiteFrom(s Subject) string {
	if s.Website == "" {
		return ""
	}
	if s.WebsiteSource == "search" {
		return "search"
	}
	return "check"
}
func searchSiteText(s Subject) *WebsiteSearchSiteText {
	if s.Website == "" || s.SiteText == nil {
		return nil
	}
	return &WebsiteSearchSiteText{Status: s.SiteText.Status, Pages: s.SiteText.Pages}
}
func siteKey(address string) string {
	for _, raw := range []string{address, "http://" + address} {
		u, err := url.Parse(raw)
		if err == nil && u.Hostname() != "" {
			return strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."), "www.")
		}
	}
	return ""
}
func searchCase(f WebsiteSearchFound) string {
	if f.Failed() {
		return ""
	}
	if f.Answer.URL == "" {
		return SearchCaseNone
	}
	address := f.Answer.URL
	if c := f.Confirmation; c != nil && (c.State == sources.WebsiteHTTPS || c.State == sources.WebsiteHTTP) {
		address = c.URL
	}
	if key := siteKey(address); key != "" && key == siteKey(f.Website) {
		return SearchCaseStands
	}
	return SearchCaseDifferent
}

// Input supplies identity, own-domain check and source evidence.
func (WebsiteSearch) Input(s Subject) json.RawMessage {
	v := WebsiteSearchInput{Candidate: s.ID, Name: s.Name, City: s.City, State: s.State, Street: s.Street, Zip: s.Zip, Phone: s.Phone, Domain: website.Domain(s.Email), People: []BriefPerson{}, Categories: []Category{}}
	if v.Domain != "" {
		v.DomainCheck = sources.WebsiteUnknown
		if s.EmailCheck != nil {
			v.DomainCheck = s.EmailCheck.State
			v.DomainError = s.EmailCheck.Error
		}
	}
	if emptySite(s) {
		v.Website = s.Website
		v.WebsiteFrom = websiteFrom(s)
		v.SiteText = searchSiteText(s)
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
func (w WebsiteSearch) Found(ctx context.Context, s Subject, out prompts.Outcome, _ int64) Result {
	run := WebsiteSearchRun{ID: out.ID, Status: out.Status, CostNanos: out.CostNanos}
	if out.ExitCode != nil {
		code := *out.ExitCode
		run.ExitCode = &code
	}
	f := WebsiteSearchFound{Run: run, Answer: searchAnswer(json.RawMessage(out.Stdout)), Website: s.Website, WebsiteFrom: websiteFrom(s), SiteText: searchSiteText(s), Reason: SearchReasonAgain}
	if s.Website == "" {
		f.Reason = SearchReasonNoWebsite
	} else if emptySite(s) {
		f.Reason = SearchReasonEmptySite
	}
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
	f.InputSources = append(f.InputSources, inputSource(sources.FactName, s.NameSource, s.NameRecord)...)
	f.InputSources = append(f.InputSources, inputSource(sources.FactAddress, s.AddressSource, s.AddressRecord)...)
	f.InputSources = append(f.InputSources, inputSource(sources.FactPhone, s.PhoneSource, s.PhoneRecord)...)
	f.InputSources = append(f.InputSources, inputSource(sources.FactEmail, s.EmailSource, s.EmailRecord)...)
	f.Case = searchCase(f)
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
	contextual := false
	for key, raw := range m {
		switch key {
		case "run", "answer", "confirmation", "site_text":
			nested, valid := object(raw)
			if !valid {
				return false
			}
			if key == "answer" && !array(nested["rejected"]) {
				return false
			}
			if key == "site_text" {
				if len(nested) != 2 || nested["status"] == nil || nested["pages"] == nil {
					return false
				}
				contextual = true
			}
		case "input_sources":
			if !validInputSources(b) {
				return false
			}
		case "reason", "website", "website_from", "case":
			raw = bytes.TrimSpace(raw)
			if len(raw) == 0 || raw[0] != '"' {
				return false
			}
			contextual = true
		default:
			return false
		}
	}
	if _, ok = object(m["run"]); !ok {
		return false
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
	if !contextual {
		return true
	}
	if f.Reason != SearchReasonNoWebsite && f.Reason != SearchReasonEmptySite && f.Reason != SearchReasonAgain {
		return false
	}
	hasWebsite, hasFrom := m["website"] != nil, m["website_from"] != nil
	if hasWebsite != hasFrom || hasWebsite == (f.Reason == SearchReasonNoWebsite) {
		return false
	}
	if hasWebsite && (f.Website == "" || (f.WebsiteFrom != "check" && f.WebsiteFrom != "search")) {
		return false
	}
	if f.SiteText != nil && (!hasWebsite || f.SiteText.Status == "" || f.SiteText.Pages < 0) {
		return false
	}
	if f.Reason == SearchReasonEmptySite && (f.SiteText == nil || f.SiteText.Pages != 0) {
		return false
	}
	if (m["case"] != nil) == f.Failed() {
		return false
	}
	return f.Case == searchCase(f)
}

// Failed applies the decoded record's failure rule.
func (WebsiteSearch) Failed(b json.RawMessage) bool {
	var f WebsiteSearchFound
	return json.Unmarshal(b, &f) == nil && f.Failed()
}
