package artifacts

import (
	"artifacts/internal/db"
	"context"
	"database/sql"
	"encoding/json"
	"eventplane/outbox"
	"fmt"
	"registry"
)

const (
	kindCreated = "created"
	kindUpdated = "updated"
	kindDeleted = "deleted"
)

// EventSink is the transactional event seam used by artifact mutations.
type EventSink interface {
	AppendCreated(context.Context, *sql.Tx, db.Artifact, Reference) error
	AppendUpdated(context.Context, *sql.Tx, db.Artifact) error
	AppendDeleted(context.Context, *sql.Tx, db.Artifact) error
	Ring()
}

type outboxProducer struct{ outbox *outbox.Outbox }

// NewOutboxProducer adapts the event-plane outbox to the artifact domain.
func NewOutboxProducer(ob *outbox.Outbox) EventSink { return &outboxProducer{outbox: ob} }

type createdPayload struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
	OwnerID     string `json:"owner_id"`
	OwnerEmail  string `json:"owner_email"`
	ContentURL  string `json:"content_url"`
}

type updatedPayload struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	OwnerID     string `json:"owner_id"`
	OwnerEmail  string `json:"owner_email"`
}

type deletedPayload struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	Visibility string `json:"visibility"`
	OwnerID    string `json:"owner_id"`
	OwnerEmail string `json:"owner_email"`
}

var Events = outbox.Registry{
	{Kind: kindCreated, Subject: "/<artifact id>", Description: "An artifact was stored by upload or import.", Sample: createdPayload{ID: "01JARTIFACT", Filename: "report.pdf", Description: "Quarterly report", Visibility: "private", Size: 1234, ContentHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", OwnerID: "owner-123", OwnerEmail: "owner@example.com", ContentURL: registry.BaseURL("artifacts") + "/content?id=01JARTIFACT"}},
	{Kind: kindUpdated, Subject: "/<artifact id>", Description: "An artifact's metadata or visibility changed.", Sample: updatedPayload{ID: "01JARTIFACT", Filename: "report.pdf", Description: "Final report", Visibility: "public", OwnerID: "owner-123", OwnerEmail: "owner@example.com"}},
	{Kind: kindDeleted, Subject: "/<artifact id>", Description: "An artifact was deleted.", Sample: deletedPayload{ID: "01JARTIFACT", Filename: "report.pdf", Visibility: "public", OwnerID: "owner-123", OwnerEmail: "owner@example.com"}},
}

func (o *outboxProducer) AppendCreated(ctx context.Context, tx *sql.Tx, a db.Artifact, ref Reference) error {
	return o.append(ctx, tx, kindCreated, a.ID, createdPayload{a.ID, a.Filename, a.Description, a.Visibility, a.Size, a.ContentHash, a.OwnerID, a.OwnerEmail, ref.ContentURL})
}

func (o *outboxProducer) AppendUpdated(ctx context.Context, tx *sql.Tx, a db.Artifact) error {
	return o.append(ctx, tx, kindUpdated, a.ID, updatedPayload{a.ID, a.Filename, a.Description, a.Visibility, a.OwnerID, a.OwnerEmail})
}

func (o *outboxProducer) AppendDeleted(ctx context.Context, tx *sql.Tx, a db.Artifact) error {
	return o.append(ctx, tx, kindDeleted, a.ID, deletedPayload{a.ID, a.Filename, a.Visibility, a.OwnerID, a.OwnerEmail})
}

func (o *outboxProducer) append(ctx context.Context, tx *sql.Tx, kind, id string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", kind, err)
	}
	return o.outbox.Append(ctx, tx, outbox.Event{Kind: kind, Subject: "/" + id, Payload: raw})
}

func (o *outboxProducer) Ring() { o.outbox.Ring() }

func (s *Service) createdEvent(ctx context.Context, tx *sql.Tx, a db.Artifact) error {
	if s.Outbox == nil {
		return nil
	}
	return s.Outbox.AppendCreated(ctx, tx, a, s.Reference(a))
}

// UpdateArtifact atomically updates an artifact and emits its post-change fact.
func (s *Service) UpdateArtifact(ctx context.Context, id string, p db.UpdateArtifactParams) (db.Artifact, bool, error) {
	var event db.ArtifactEvent
	if s.Outbox != nil {
		event = s.Outbox.AppendUpdated
	}
	a, changed, err := s.Store.UpdateArtifactWithEvent(ctx, id, p, event)
	if err == nil && changed && s.Outbox != nil {
		s.Outbox.Ring()
	}
	return a, changed, err
}

// DeleteArtifact atomically deletes an artifact and emits its last-state fact.
func (s *Service) DeleteArtifact(ctx context.Context, id string) (db.Artifact, bool, error) {
	var event db.ArtifactEvent
	if s.Outbox != nil {
		event = s.Outbox.AppendDeleted
	}
	a, deleted, err := s.Store.DeleteArtifactWithEvent(ctx, id, event)
	if err == nil && deleted && s.Outbox != nil {
		s.Outbox.Ring()
	}
	return a, deleted, err
}
