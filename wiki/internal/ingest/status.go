package ingest

import (
	"context"
	"errors"
	"time"

	"agentkit/job"

	"wiki/internal/jobstore"
	"wiki/internal/store"
)

// ErrJobNotFound is returned by JobStatus when no job with that id exists for the
// owner (a foreign-owned id reads the same way — owner-scoping). The MCP verb
// maps it to a tool-error.
var ErrJobNotFound = errors.New("ingest: job not found")

// Status is the owner-scoped view of one job's lifecycle, returned to the
// wiki_job_status verb. It is a flattened, JSON-friendly projection of
// agentkit/job.Record (the generic run record) — no owner/collection leak, no
// internal types.
type Status struct {
	JobID     string `json:"job_id"`
	Status    string `json:"status"` // running | succeeded | failed | cancelled
	Terminal  bool   `json:"terminal"`
	StartedAt string `json:"started_at"`         // RFC3339Nano, "" if unset
	EndedAt   string `json:"ended_at,omitempty"` // RFC3339Nano, "" until terminal
	Error     string `json:"error,omitempty"`    // terminal error message, "" on success
	UsageJSON string `json:"usage,omitempty"`    // opaque accounting blob, "" if none
}

// JobStatus reads the job record for jobID, owner-scoped: a missing or
// foreign-owned id returns ErrJobNotFound. It reads straight from wiki_jobs (the
// runner's goroutine persists terminal state there), so the status is accurate
// even after the spawning request has returned.
func (c *Core) JobStatus(ctx context.Context, owner, collection, jobID string) (Status, error) {
	if collection == "" {
		collection = store.DefaultCollection
	}
	rec, err := jobstore.New(c.db, owner, collection).Load(ctx, jobID)
	if errors.Is(err, job.ErrNotFound) {
		return Status{}, ErrJobNotFound
	}
	if err != nil {
		return Status{}, err
	}
	out := Status{
		JobID:     rec.ID,
		Status:    string(rec.Status),
		Terminal:  rec.Status.Terminal(),
		Error:     rec.Error,
		UsageJSON: rec.UsageJSON,
	}
	if !rec.StartedAt.IsZero() {
		out.StartedAt = rec.StartedAt.Format(time.RFC3339Nano)
	}
	if !rec.EndedAt.IsZero() {
		out.EndedAt = rec.EndedAt.Format(time.RFC3339Nano)
	}
	return out, nil
}
