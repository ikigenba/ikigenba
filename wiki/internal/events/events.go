// Package events declares wiki's published event-plane facts — exactly the two
// events of design §8, declared once and referenced at both the (future) emit
// sites and the reflection Registry so the two cannot drift.
//
// The wiki is an event-plane PRODUCER via the standard appkit outbox / /feed.
// Per design §8 it publishes EXACTLY two events, nothing else:
//
//   - wiki.row_dead_lettered — emitted by the failure-path code that sets
//     dead_at, in that transaction.
//   - wiki.ingest_refused    — a plain outbox write at the door, pre-accept.
//
// Neither is emitted yet (P2 only declares the outbox + the two types; the emit
// sites land in P3's ingest path and P4/P5's failure policy). Both are consumed
// by notify; a hosted script can bind later for free.
package events

import "eventplane/outbox"

// The two event type strings, declared once and referenced at both the (future)
// emit sites and the reflection Registry so the two cannot drift (design §8).
const (
	TypeRowDeadLettered = "wiki.row_dead_lettered"
	TypeIngestRefused   = "wiki.ingest_refused"
)

// RowDeadLettered is the wiki.row_dead_lettered payload (design §8 / §12.3):
// an inbox row exhausted its retries and was parked. Emitted in the same
// transaction that sets dead_at.
type RowDeadLettered struct {
	InboxID   string `json:"inbox_id"`
	Source    string `json:"source"`
	Title     string `json:"title"`
	LastError string `json:"last_error"`
}

// IngestRefused is the wiki.ingest_refused payload (design §8 / §12.3): a door
// refused an oversized (or otherwise unacceptable) ingest before accepting it.
// A plain outbox write at the door, pre-accept.
type IngestRefused struct {
	Door   string `json:"door"`
	Source string `json:"source"`
	Size   int64  `json:"size"`
	Cap    int64  `json:"cap"`
}

// Registry is the published-event Registry for the reflection tool and
// Append-time validation (wired via Spec.Events). Each entry carries a
// filled-in Sample instance of its real payload struct — the single source for
// both the reflected JSON Schema and the worked example, so schema/example/wire
// shape cannot diverge.
var Registry = outbox.Registry{
	{
		Type:        TypeRowDeadLettered,
		Description: "An inbox row exhausted its integration retries and was dead-lettered (parked at dead_at). Carries the row's identity and the last error so an operator/consumer can triage.",
		Sample: RowDeadLettered{
			InboxID:   "01J9Z2K7P3QC8M4R6T0V2X5YA",
			Source:    "url:https://example.com/post",
			Title:     "An example document",
			LastError: "extract: model returned unparseable JSON",
		},
	},
	{
		Type:        TypeIngestRefused,
		Description: "A door refused an ingest before accepting it (e.g. the payload exceeded WIKI_INGEST_MAX_BYTES). Carries the door, source, the offending size, and the cap.",
		Sample: IngestRefused{
			Door:   "ingest_text",
			Source: "mcp:ingest_text",
			Size:   262144,
			Cap:    131072,
		},
	},
}
