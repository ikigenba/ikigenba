package repos

import (
	"context"
	"database/sql"
	"encoding/json"
	"eventplane/outbox"
	"fmt"
	"strings"
)

const zeroSHA = "0000000000000000000000000000000000000000"

type RefUpdate struct {
	Kind, Name string
	Ref        string
	OldSHA     string
	NewSHA     string
	Actor      string
}

func (s *Service) ApplyRefUpdate(ctx context.Context, tx *sql.Tx, update RefUpdate) error {
	if s.custody == nil || s.producer == nil {
		return fmt.Errorf("repos: ref update dependencies are not configured")
	}
	path, err := s.custody.Path(update.Kind, update.Name)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(update.Ref, "refs/heads/") || strings.TrimPrefix(update.Ref, "refs/heads/") == "" {
		return fmt.Errorf("%w: invalid branch ref %q", ErrValidation, update.Ref)
	}
	if update.Ref == "refs/heads/main" {
		if update.NewSHA == "" {
			return ErrForcePush
		}
		if update.OldSHA != "" {
			if _, err := s.custody.git.Run(ctx, path, "merge-base", "--is-ancestor", update.OldSHA, update.NewSHA); err != nil {
				return ErrForcePush
			}
		}
	}
	old := update.OldSHA
	if old == "" {
		old = zeroSHA
	}
	if update.NewSHA == "" {
		if _, err := s.custody.git.Run(ctx, path, "update-ref", "-d", update.Ref, old); err != nil {
			return err
		}
		return nil
	}
	if _, err := s.custody.git.Run(ctx, path, "update-ref", update.Ref, update.NewSHA, old); err != nil {
		return err
	}
	payload, err := json.Marshal(PushPayload{Kind: update.Kind, Name: update.Name, Branch: strings.TrimPrefix(update.Ref, "refs/heads/"), SHA: update.NewSHA, OldSHA: update.OldSHA, Actor: update.Actor})
	if err != nil {
		return err
	}
	return s.producer.Append(ctx, tx, outbox.Event{Kind: "push", Subject: "/" + update.Kind + "/" + update.Name, Payload: payload})
}
