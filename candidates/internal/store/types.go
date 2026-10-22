// Package store keeps the candidate records in the service database.
package store

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Config supplies the store clock and random reader.
type Config struct {
	Now    func() time.Time
	Rand   io.Reader
	Origin string
}

// Store owns the persisted service records.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New constructs a store over an existing database handle.
func New(d *db.DB, cfg Config) *Store { return &Store{d: d, cfg: cfg} }

// Candidate is the display and identity of a business.
type Candidate struct {
	ID, Status, MergedInto, Name                   string
	Names                                          []string
	Street, City, State, Zip, County, Phone, Email string
	People                                         []sources.Person
	Class                                          string
	Cargo                                          []string
	Created, Updated                               time.Time
}

// Link attaches a source row to a candidate.
type Link struct {
	Candidate, Scraper, SourceID, Run, Digest string
	Reading                                   sources.Reading
	Reason                                    string
	Confidence                                float64
	FirstSeen, LastSeen                       time.Time
	Gone                                      bool
}

// Judgments holds the last operating and revenue judgments and their evidence.
type Judgments struct {
	Candidate               string
	Operating               *float64
	OperatingKey            int64
	Evidence                []sources.Evidence
	RevenueLow, RevenueHigh *int64
	RevenueConfidence       *float64
	Excluded                bool
	Rules                   int64
	Calculated              time.Time
}

// Enrichment is one recorded result.
type Enrichment struct {
	Candidate, Kind, Inputs, Source string
	Found                           json.RawMessage
	Cost                            float64
	At                              time.Time
}

// Due names a candidate and the inputs requiring enrichment.
type Due struct{ Candidate, Inputs string }

// Facts records the latest enrichment facts.
type Facts struct {
	Candidate                          string
	KnownSince                         time.Time
	GovernmentMatch                    string
	NewestFiling                       time.Time
	Categories                         []sources.Category
	Records                            []sources.Category
	FleetVerdict                       string
	PowerUnits, Drivers                *int64
	Website, WebsiteURL, WebsiteSource string
	Minutes                            *int64
	MinutesSource                      string
	Miles                              *float64
	RouteAddress                       string
	Brief                              string
	Written                            []Written
}

// Written records the result time used for one kind.
type Written struct {
	Kind string
	At   time.Time
}

// Ruling records an owner decision.
type Ruling struct {
	Kind, Candidate, Other string
	Keys                   []string
	Why, UserID, UserEmail string
	At                     time.Time
}

// Pass records one pass.
type Pass struct {
	ID, Kind, Origin, Trigger, Target, EnrichKind, Scraper, Status, Reason                                                                          string
	Started, Finished                                                                                                                               time.Time
	Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged, Derived, Due, Fetched, HTTPS, HTTP, None, Runs, RunsFailed, Pages, Cost int64
	UserID, UserEmail, RequestID, Delivery                                                                                                          string
}

// Counts contains the results of pass work.
type Counts struct{ Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged, Derived, Due, Fetched, HTTPS, HTTP, None, Runs, RunsFailed, Pages, Cost int64 }

// Observation is one source reading to attach.
type Observation struct {
	Scraper, SourceID, Digest string
	Reading                   sources.Reading
}

// CandidateFilter selects active candidates.
type CandidateFilter struct{ Name, Zip, Phone string }

// Ranked contains a candidate and its persisted judgments.
type Ranked struct {
	Candidate Candidate
	Judgments Judgments
	Facts     Facts
	SiteText  time.Time
	Brief     time.Time
	Basis     string
}

// Record vocabulary and identifier prefixes.
const (
	WebsiteByCheck    = "check"
	WebsiteBySearch   = "search"
	CandidateIDPrefix = "cand_"
	PassIDPrefix      = "pass_"
	StatusActive      = "active"
	StatusMerged      = "merged"
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusFailed      = "failed"
	ReasonScrapes     = "scrapes"
	ReasonInterrupted = "interrupted"
	ReasonStore       = "store"
	TriggerIngest     = "ingest"
	TriggerJudge      = "judge_candidates"
	KindEnrichTarget  = "enrich_target"
	ReasonPrompts     = "prompts"
	PageCap           = 20
	KindEnrich        = "enrich"
	OriginEnrich      = "enrich"
	OriginPass        = "pass"
	TriggerEnrich     = "enrich"
	KindIngest        = "ingest"
	KindJudge         = "judge"

	OriginRun        = "run"
	OriginCron       = "cron"
	OriginIngest     = "ingest"
	OriginJudge      = "judge"
	TriggerJudgeTool = "judge"
	LinkSourceKey    = "source_key"
	LinkMatchKey     = "match_key"
	LinkDecision     = "decision"
	KindMerge        = "merge"
	KindSplit        = "split"
	KindNotSame      = "not_same"
	KindExclude      = "exclude"
	KindInclude      = "include"
	Unreachable      = "The candidate database is unavailable."
)

// Errors distinguish record refusals from database failures.
var (
	ErrNotFound    = errors.New("record not found")
	ErrBusy        = errors.New("pass running")
	ErrDelivered   = errors.New("already delivered")
	ErrEnded       = errors.New("pass ended")
	ErrInvalid     = errors.New("invalid ruling")
	ErrPageCap     = errors.New("page cap reached")
	ErrUnreachable = errors.New("database unreachable")
)

func validID(s, prefix string) bool {
	return len(s) == len(prefix)+16 && s[:len(prefix)] == prefix && hexDigits(s[len(prefix):])
}
func hexDigits(s string) bool {
	for _, b := range []byte(s) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ValidCandidateID recognizes candidate identifiers.
func ValidCandidateID(s string) bool { return validID(s, CandidateIDPrefix) }

// ValidPassID recognizes pass identifiers.
func ValidPassID(s string) bool { return validID(s, PassIDPrefix) }
func validZip(s string) bool {
	if len(s) != 5 {
		return false
	}
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
func unreachable(err error) error {
	if err == nil {
		return nil
	}
	for _, e := range []error{ErrNotFound, ErrBusy, ErrDelivered, ErrEnded, ErrInvalid, ErrPageCap} {
		if errors.Is(err, e) {
			return err
		}
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) now() time.Time { return s.cfg.Now().UTC().Truncate(time.Second) }

// Page is one kept copy of a candidate's website page.
type Page struct {
	Candidate         string
	Number            int64
	URL, Title, Text  string
	Size              int64
	Fetched           time.Time
	UserID, UserEmail string
}

// PageCount counts distinct URLs and all kept copies.
type PageCount struct{ URLs, Copies int64 }
