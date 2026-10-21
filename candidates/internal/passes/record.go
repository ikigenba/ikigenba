package passes

import (
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// BatchSize is the maximum number of observations held before attachment.
const BatchSize = 500

// Prompt scheduling bounds and injected timer durations.
const (
	RunsInFlight   = 4
	RoutesInFlight = 4
	StartTries     = 20
	PollInterval   = 5 * time.Second
	QueueWait      = 30 * time.Second
)

// NoRouteKey explains a missing routing client.
const NoRouteKey string = "A Maps API key is required to route a drive."

// NoPrompt introduces a missing catalog prompt refusal.
const NoPrompt string = "The required prompt is unavailable:"

// Busy is the delivery refusal when a pass is already running.
const Busy string = "Another pass is running."

// Stopping is the delivery refusal after drain begins.
const Stopping string = "The service is stopping."

// Refusal words distinguish tool refusals.
const (
	RefusalNotFound    = "not_found"
	RefusalInvalid     = "invalid"
	RefusalBusy        = "busy"
	RefusalStopping    = "stopping"
	RefusalUnavailable = "unavailable"
)

// Refusal carries a machine-readable refusal word.
type Refusal struct{ Word, Text string }

func (r *Refusal) Error() string {
	if r.Text != "" {
		return r.Word + ": " + r.Text
	}
	return r.Word
}

// Record is the public JSON representation of a pass.
type Record struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Origin     string `json:"origin"`
	Trigger    string `json:"trigger"`
	Target     string `json:"target,omitempty"`
	EnrichKind string `json:"enrich_kind,omitempty"`
	Scraper    string `json:"scraper,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Started    string `json:"started"`
	Finished   string `json:"finished,omitempty"`
	Rows       int64  `json:"rows"`
	New        int64  `json:"new"`
	Changed    int64  `json:"changed"`
	Unchanged  int64  `json:"unchanged"`
	Gone       int64  `json:"gone"`
	Created    int64  `json:"created"`
	Linked     int64  `json:"linked"`
	Judged     int64  `json:"judged"`
	Unranged   int64  `json:"unranged"`
	Derived    int64  `json:"derived"`
	Due        int64  `json:"due"`
	Fetched    int64  `json:"fetched"`
	HTTPS      int64  `json:"https"`
	HTTP       int64  `json:"http"`
	None       int64  `json:"none"`
	Runs       int64  `json:"runs"`
	RunsFailed int64  `json:"runs_failed"`
	Pages      int64  `json:"pages"`
	Cost       int64  `json:"cost"`
	User       string `json:"user,omitempty"`
	Email      string `json:"email,omitempty"`
}

// RecordOf renders a pass with UTC timestamps and its public starter.
func RecordOf(p store.Pass) Record {
	r := Record{ID: p.ID, Kind: p.Kind, Origin: p.Origin, Trigger: p.Trigger, Target: p.Target, EnrichKind: p.EnrichKind, Scraper: p.Scraper, Status: p.Status, Reason: p.Reason, Started: p.Started.UTC().Format(time.RFC3339), Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Judged: p.Judged, Unranged: p.Unranged, Derived: p.Derived, Due: p.Due, Fetched: p.Fetched, HTTPS: p.HTTPS, HTTP: p.HTTP, None: p.None, Runs: p.Runs, RunsFailed: p.RunsFailed, Pages: p.Pages, Cost: p.Cost}
	if !p.Finished.IsZero() {
		r.Finished = p.Finished.UTC().Format(time.RFC3339)
	}
	if p.Origin == store.OriginIngest || p.Origin == store.OriginJudge || p.Origin == store.OriginEnrich {
		r.User = p.UserID
		r.Email = p.UserEmail
	}
	return r
}
