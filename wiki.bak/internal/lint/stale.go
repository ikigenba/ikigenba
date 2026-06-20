package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"wiki/internal/config"
	"wiki/internal/integrate"
	"wiki/internal/page"
)

// StaleJobName is lint-stale's stable job name (runs.job; the cron/lint TryLock
// key — at most one in-flight lint-stale run, design §6).
const StaleJobName = "lint-stale"

// staleStore is the slice of *page.Store lint-stale needs (design §6, P9c): the
// open-notes work list batched per subject (the page + its open notes), and the
// single per-subject repair transaction (rewrite page + record dispositions).
// Narrowed to an interface so the job is unit-testable with a fake (no live DB).
type staleStore interface {
	OpenStaleSubjects(ctx context.Context) ([]page.StaleSubject, error)
	ApplyStaleRepair(ctx context.Context, r page.StaleRepair) error
}

// payloadSource resolves an inbox citation id to its raw payload bytes (design §6:
// the repair reads "page + notes + cited payloads"). Declared as an interface so
// the job is unit-testable without a live inbox store; the composition root wires
// an inbox-backed implementation. A missing/unreadable citation is non-fatal — the
// note still carries the human-readable observation text, so the repair degrades
// to notes-only rather than failing the whole subject on one bad citation.
type payloadSource interface {
	CitedPayload(ctx context.Context, inboxID string) ([]byte, error)
}

// StaleJob is the lint-stale maintenance job (design §6, §6.1, P9c): staleness
// repair backed by the stale_notes side channel the document-pass merge and the
// fold append (the flag-only writers, built in P7a). It works the OPEN-notes
// queue, batching each subject's open notes into ONE tool-less call (page + notes
// + cited payloads in; rewritten page + per-note disposition out), and applies the
// result in ONE TRANSACTION PER SUBJECT (per-subject recovery via the queue
// itself). It inherits the §6.1 citation-preservation gate (the rewritten page may
// not silently drop a citation the old page carried). It satisfies
// integrate.Integrator so the worker spine selects and runs it exactly like any
// other job; it owns its writes per-subject, so its Integrate returns an EMPTY
// manifest and the worker's end-of-run Commit is a harmless no-op stamp.
type StaleJob struct {
	caller caller
	store  staleStore
	src    payloadSource
	site   config.CallSite
}

// NewStaleJob builds the lint-stale job over a structured caller, the page store,
// the cited-payload source, and the config-injected stale-repair call-site triple.
// The triple is injected — the call never reads a constant or env (design §10).
func NewStaleJob(c caller, store staleStore, src payloadSource, site config.CallSite) *StaleJob {
	return &StaleJob{caller: c, store: store, src: src, site: site}
}

// Job is the integrate.Integrator job name (runs.job). lint-stale runs under it.
func (j *StaleJob) Job() string { return StaleJobName }

// Integrate runs one lint-stale sweep (design §6): one run per trigger working the
// open-notes queue SUBJECT BY SUBJECT, ONE TRANSACTION PER SUBJECT (per-subject
// recovery — a failure on subject k leaves subjects <k repaired and subject k's
// notes open for a later run). The returned manifest is empty: lint-stale owns its
// writes per-subject, so the worker's end-of-run Commit is a no-op stamp.
func (j *StaleJob) Integrate(ctx context.Context, _ integrate.Unit) (*integrate.Manifest, error) {
	subjects, err := j.store.OpenStaleSubjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("lint-stale: work list: %w", err)
	}
	for _, s := range subjects {
		if err := j.repairOne(ctx, s); err != nil {
			return nil, fmt.Errorf("lint-stale: subject %q: %w", s.SubjectID, err)
		}
	}
	return &integrate.Manifest{}, nil
}

// repairOne runs the stale-repair call for one subject's batched open notes and
// applies the result in its own transaction (design §6). It enforces the §6.1
// citation-preservation gate against the OLD page body before applying — so a
// repair can never silently drop evidence — exactly as the fold inherits it.
func (j *StaleJob) repairOne(ctx context.Context, s page.StaleSubject) error {
	if len(s.Notes) == 0 {
		return nil // nothing to repair
	}
	res, err := j.Repair(ctx, s)
	if err != nil {
		return err
	}
	return j.store.ApplyStaleRepair(ctx, page.StaleRepair{
		SubjectID:    s.SubjectID,
		Title:        res.Title,
		Body:         res.Body,
		Dispositions: res.Dispositions,
	})
}

// StaleRepairResult is the parsed stale-repair output: the rewritten page (title +
// body), the §6.1 superseded list, and one disposition per open note.
type StaleRepairResult struct {
	Title        string
	Body         string
	Superseded   []string
	Dispositions []page.StaleDisposition
}

// StaleSchema pins the stale-repair structured output (design §6/§6.1): the
// rewritten title/body, the dropped-citation list, and one disposition per note.
var StaleSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "body", "superseded", "dispositions"],
  "properties": {
    "title": {"type": "string"},
    "body": {"type": "string"},
    "superseded": {"type": "array", "items": {"type": "string"}},
    "dispositions": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["note_id", "status"],
        "properties": {
          "note_id": {"type": "string"},
          "status": {"type": "string", "enum": ["repaired", "dismissed"]}
        }
      }
    }
  }
}`)

// Repair runs the stale-repair call for one subject (design §6). It renders the
// subject's page body, its batched open notes (each with its id + cited payloads),
// into the user message, invokes the injected triple, parses the result, and
// ENFORCES the §6.1 citation gate against the old page body so the rewrite can
// never silently drop evidence.
func (j *StaleJob) Repair(ctx context.Context, s page.StaleSubject) (StaleRepairResult, error) {
	raw, err := j.caller.Structured(ctx, j.site, StaleSchema, userMsg(j.renderStaleInput(ctx, s)))
	if err != nil {
		return StaleRepairResult{}, fmt.Errorf("lint: stale repair call: %w", err)
	}
	res, err := ParseStale(raw)
	if err != nil {
		return StaleRepairResult{}, err
	}
	if err := checkCitationPreservation([]string{s.Body}, res.Body, res.Superseded); err != nil {
		return StaleRepairResult{}, err
	}
	return res, nil
}

// renderStaleInput builds the stale-repair user message: the subject's current
// page body plus each open note (its id, the observation, and the cited evidence
// payloads). Deterministic for a fixed subject (notes already ordered by id). A
// citation that cannot be read is rendered as a marker rather than failing — the
// note text still carries the observation.
func (j *StaleJob) renderStaleInput(ctx context.Context, s page.StaleSubject) string {
	var b strings.Builder
	b.WriteString("Repair this knowledge-base page using the staleness notes below.\n")
	body := s.Body
	if strings.TrimSpace(body) == "" {
		body = "(no page yet)"
	}
	fmt.Fprintf(&b, "\n--- current page ---\ntitle: %s\n%s\n", s.Title, body)
	for _, n := range s.Notes {
		fmt.Fprintf(&b, "\n--- note %s ---\n%s\n", n.ID, n.Note)
		for _, cite := range splitCites(n.Cites) {
			payload := j.citedText(ctx, cite)
			fmt.Fprintf(&b, "[%s]: %s\n", cite, payload)
		}
	}
	return b.String()
}

// citedText reads one cited inbox payload, degrading to a marker on any read error
// (a single bad citation must not fail the whole subject's repair — the note text
// still carries the observation).
func (j *StaleJob) citedText(ctx context.Context, inboxID string) string {
	if j.src == nil {
		return "(citation unavailable)"
	}
	raw, err := j.src.CitedPayload(ctx, inboxID)
	if err != nil {
		return "(citation unavailable)"
	}
	return string(raw)
}

// splitCites splits a stale_notes.cites field into individual inbox ids. The field
// is whitespace/comma separated; empties are dropped.
func splitCites(cites string) []string {
	fields := strings.FieldsFunc(cites, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ParseStale parses+validates a stale-repair response. Separated from the call so
// the prompt-default gate and goldens exercise the parser + schema offline against
// a committed fixture, with no client (obligation 5 / the standing prompt gate). A
// non-empty body is required (the repaired page must exist).
func ParseStale(raw string) (StaleRepairResult, error) {
	var out struct {
		Title        string   `json:"title"`
		Body         string   `json:"body"`
		Superseded   []string `json:"superseded"`
		Dispositions []struct {
			NoteID string `json:"note_id"`
			Status string `json:"status"`
		} `json:"dispositions"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &out); err != nil {
		return StaleRepairResult{}, fmt.Errorf("lint: parse stale response: %w", err)
	}
	if strings.TrimSpace(out.Body) == "" {
		return StaleRepairResult{}, fmt.Errorf("lint: stale repair produced an empty body")
	}
	res := StaleRepairResult{
		Title:      strings.TrimSpace(out.Title),
		Body:       out.Body,
		Superseded: cleanList(out.Superseded),
	}
	for _, d := range out.Dispositions {
		id := strings.TrimSpace(d.NoteID)
		status := strings.TrimSpace(d.Status)
		if id == "" {
			continue
		}
		if status != "repaired" && status != "dismissed" {
			return StaleRepairResult{}, fmt.Errorf("lint: stale disposition for %q has invalid status %q (must be repaired|dismissed)", id, status)
		}
		res.Dispositions = append(res.Dispositions, page.StaleDisposition{NoteID: id, Status: status})
	}
	return res, nil
}
