package repos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
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

// RepositoryDetail combines SQLite-owned metadata with refs read from git.
type RepositoryDetail struct {
	Repository Repository
	Head       string
	Branches   []string
}

func (s *Service) CreateRepository(ctx context.Context, repository Repository) (Repository, error) {
	if s.store == nil || s.custody == nil {
		return Repository{}, fmt.Errorf("repos: create dependencies are not configured")
	}
	if !validKind(repository.Kind) || !ValidName(repository.Name) || repository.OwnerID == "" {
		return Repository{}, fmt.Errorf("%w: invalid repository", ErrValidation)
	}
	if err := s.custody.Init(ctx, repository.Kind, repository.Name); err != nil {
		return Repository{}, err
	}
	repository.DefaultBranch = "main"
	repository.CreatedAt = s.custody.Now()
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return Repository{}, err
	}
	defer tx.Rollback()
	if err := s.store.InsertRepository(ctx, tx, repository); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Repository{}, fmt.Errorf("%w: repository already exists", ErrConflict)
		}
		return Repository{}, err
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, err
	}
	return repository, nil
}

func (s *Service) ListRepositories(ctx context.Context, ownerID string, kind *string) ([]Repository, error) {
	if s.store == nil {
		return nil, fmt.Errorf("repos: list store is not configured")
	}
	if ownerID == "" || (kind != nil && !validKind(*kind)) {
		return nil, fmt.Errorf("%w: invalid owner or kind", ErrValidation)
	}
	return s.store.ListRepositories(ctx, ownerID, kind)
}

func (s *Service) GetRepository(ctx context.Context, ownerID, kind, name string) (RepositoryDetail, error) {
	if s.store == nil || s.custody == nil {
		return RepositoryDetail{}, fmt.Errorf("repos: get dependencies are not configured")
	}
	if !validKind(kind) || !ValidName(name) {
		return RepositoryDetail{}, fmt.Errorf("%w: invalid repository key", ErrValidation)
	}
	repository, err := s.store.GetLiveRepository(ctx, ownerID, kind, name)
	if errors.Is(err, sql.ErrNoRows) {
		return RepositoryDetail{}, ErrNotFound
	}
	if err != nil {
		return RepositoryDetail{}, err
	}
	refs, err := s.custody.Refs(ctx, kind, name)
	if err != nil {
		return RepositoryDetail{}, err
	}
	branches := make([]string, 0, len(refs))
	for ref := range refs {
		branches = append(branches, strings.TrimPrefix(ref, "refs/heads/"))
	}
	sort.Strings(branches)
	return RepositoryDetail{Repository: repository, Head: refs["refs/heads/"+repository.DefaultBranch], Branches: branches}, nil
}

func (s *Service) RenameRepository(ctx context.Context, ownerID, kind, name, to string) (Repository, error) {
	detail, err := s.GetRepository(ctx, ownerID, kind, name)
	if err != nil {
		return Repository{}, err
	}
	if !ValidName(to) {
		return Repository{}, fmt.Errorf("%w: invalid destination name", ErrValidation)
	}
	if err := s.custody.Rename(ctx, kind, name, to); err != nil {
		return Repository{}, err
	}
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return Repository{}, err
	}
	defer tx.Rollback()
	if err := s.store.RenameRepository(ctx, tx, kind, name, to); err != nil {
		return Repository{}, err
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, err
	}
	detail.Repository.Name = to
	return detail.Repository, nil
}

func (s *Service) DeleteRepository(ctx context.Context, ownerID, kind, name string) (Repository, error) {
	if s.store == nil || s.custody == nil || s.producer == nil {
		return Repository{}, fmt.Errorf("repos: archive dependencies are not configured")
	}
	_, err := s.store.GetLiveRepository(ctx, ownerID, kind, name)
	if errors.Is(err, sql.ErrNoRows) {
		archived, archivedErr := s.store.GetArchivedRepository(ctx, ownerID, kind, name)
		if errors.Is(archivedErr, sql.ErrNoRows) {
			return Repository{}, ErrNotFound
		}
		return archived, archivedErr
	}
	if err != nil {
		return Repository{}, err
	}
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return Repository{}, err
	}
	defer tx.Rollback()
	_, err = s.ArchiveRepository(ctx, tx, kind, name)
	if err != nil {
		return Repository{}, err
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, err
	}
	s.producer.Ring()
	archived, err := s.store.GetArchivedRepository(ctx, ownerID, kind, name)
	if err != nil {
		return Repository{}, err
	}
	return archived, nil
}

// SetStatus records the current verdict from one check for a repository commit.
func (s *Service) SetStatus(ctx context.Context, status Status) (Status, error) {
	if s.store == nil || s.custody == nil {
		return Status{}, fmt.Errorf("repos: status dependencies are not configured")
	}
	switch status.State {
	case "pending", "success", "failure":
	default:
		return Status{}, fmt.Errorf("%w: invalid status state %q", ErrValidation, status.State)
	}
	path, err := s.custody.Path(status.Kind, status.Name)
	if err != nil {
		return Status{}, err
	}
	if _, err := s.custody.git.Run(ctx, path, "cat-file", "-e", status.SHA+"^{commit}"); err != nil {
		return Status{}, fmt.Errorf("%w: commit %q does not resolve in %s/%s", ErrNotFound, status.SHA, status.Kind, status.Name)
	}
	status.UpdatedAt = s.custody.clock.Now()
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return Status{}, err
	}
	defer tx.Rollback()
	if err := s.store.SetStatus(ctx, tx, status); err != nil {
		return Status{}, err
	}
	if err := tx.Commit(); err != nil {
		return Status{}, err
	}
	return status, nil
}

// ListStatuses returns all current check verdicts for one repository commit.
func (s *Service) ListStatuses(ctx context.Context, kind, name, sha string) ([]Status, error) {
	if s.store == nil {
		return nil, fmt.Errorf("repos: status store is not configured")
	}
	return s.store.ListStatuses(ctx, kind, name, sha)
}

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
