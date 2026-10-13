package passes

import (
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// BatchSize is the maximum number of observations held before attachment.
const BatchSize = 500

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
type Refusal struct{ Word string }

func (r *Refusal) Error() string { return r.Word }

// Record is the public JSON representation of a pass.
type Record struct {
	ID        string `json:"id"`
	Trigger   string `json:"trigger"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Started   string `json:"started"`
	Finished  string `json:"finished,omitempty"`
	Rows      int64  `json:"rows"`
	New       int64  `json:"new"`
	Changed   int64  `json:"changed"`
	Unchanged int64  `json:"unchanged"`
	Gone      int64  `json:"gone"`
	Created   int64  `json:"created"`
	Linked    int64  `json:"linked"`
	Rescored  int64  `json:"rescored"`
	Unranged  int64  `json:"unranged"`
	User      string `json:"user,omitempty"`
	Email     string `json:"email,omitempty"`
}

// RecordOf renders a pass with UTC timestamps and its public starter.
func RecordOf(p store.Pass) Record {
	r := Record{ID: p.ID, Trigger: p.Trigger, Status: p.Status, Reason: p.Reason, Started: p.Started.UTC().Format(time.RFC3339), Rows: p.Rows, New: p.New, Changed: p.Changed, Unchanged: p.Unchanged, Gone: p.Gone, Created: p.Created, Linked: p.Linked, Rescored: p.Rescored, Unranged: p.Unranged}
	if !p.Finished.IsZero() {
		r.Finished = p.Finished.UTC().Format(time.RFC3339)
	}
	if p.Trigger == store.TriggerRebuild {
		r.User = p.UserID
		r.Email = p.UserEmail
	}
	return r
}
