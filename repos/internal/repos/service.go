package repos

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"eventplane/outbox"
)

// Service is the single domain object assembled by the composition root.
// The v2 behavior is introduced by later phases.
type Service struct {
	store          *Store
	producer       *outbox.Outbox
	custody        *Custody
	maxCommitBytes int64
	runTokenTTL    time.Duration
}

func NewService(store *Store) *Service { return &Service{store: store} }

// SetProducer injects the chassis outbox after handlers have been assembled.
func (s *Service) SetProducer(producer *outbox.Outbox) { s.producer = producer }

func (s *Service) SetCustody(custody *Custody) { s.custody = custody }

// SetMaxCommitBytes configures the maximum raw HTTP request body accepted by
// the commit handlers.
func (s *Service) SetMaxCommitBytes(limit int64) { s.maxCommitBytes = limit }

// SetRunTokenTTL configures the lifetime of newly minted run credentials.
func (s *Service) SetRunTokenTTL(ttl time.Duration) { s.runTokenTTL = ttl }

// ArchiveRepository moves custody, updates metadata, and appends the archived event on tx.
func (s *Service) ArchiveRepository(ctx context.Context, tx *sql.Tx, kind, name string) (string, error) {
	if s.custody == nil || s.store == nil || s.producer == nil {
		return "", fmt.Errorf("repos: archive dependencies are not configured")
	}
	path, err := s.custody.Archive(ctx, kind, name)
	if err != nil {
		return "", err
	}
	if err := s.store.ArchiveRepository(ctx, tx, kind, name, s.custody.clock.Now(), path); err != nil {
		return "", err
	}
	payload, err := json.Marshal(ArchivedPayload{Kind: kind, Name: name, ArchivedPath: path})
	if err != nil {
		return "", err
	}
	if err := s.producer.Append(ctx, tx, outbox.Event{Kind: "archived", Subject: "/" + filepath.ToSlash(filepath.Join(kind, name)), Payload: payload}); err != nil {
		return "", err
	}
	return path, nil
}
