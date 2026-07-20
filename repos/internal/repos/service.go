package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrConflict = errors.New("repos: conflict")

type Clock interface {
	Now() time.Time
}

type RepoFacts struct {
	Name          string
	CloneURL      string
	DefaultBranch string
	OwnerID       string
	OwnerEmail    string
}

type Service struct {
	store     *Store
	git       *Git
	clock     Clock
	githubOrg string
}

func NewService(store *Store, git *Git, clock Clock, githubOrg string) *Service {
	return &Service{store: store, git: git, clock: clock, githubOrg: githubOrg}
}

func (s *Service) EnsureRepo(ctx context.Context, facts RepoFacts) error {
	if _, err := s.store.GetRepo(ctx, facts.Name); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("ensure repo lookup: %w", err)
	}
	repo := Repo{Name: facts.Name, OwnerID: facts.OwnerID, OwnerEmail: facts.OwnerEmail, CloneURL: facts.CloneURL,
		DefaultBranch: facts.DefaultBranch, CreatedAt: s.clock.Now()}
	if err := s.store.InsertRepo(ctx, repo); err != nil {
		return fmt.Errorf("ensure repo insert: %w", err)
	}
	if err := s.git.Clone(ctx, facts.CloneURL, facts.Name); err != nil {
		_ = s.store.DeleteRepo(ctx, facts.Name)
		_ = os.RemoveAll(filepath.Join(s.git.stateRoot, facts.Name))
		return fmt.Errorf("ensure repo clone: %w", err)
	}
	return nil
}

func (s *Service) CloneRepo(ctx context.Context, ownerID, ownerEmail, name string) error {
	if _, err := s.store.GetRepo(ctx, name); err == nil {
		return ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("clone repo lookup: %w", err)
	}
	if s.githubOrg == "" {
		return errors.New("clone repo: github organization is required")
	}
	facts := RepoFacts{Name: name, OwnerID: ownerID, OwnerEmail: ownerEmail, DefaultBranch: "main",
		CloneURL: "https://github.com/" + s.githubOrg + "/" + name + ".git"}
	return s.EnsureRepo(ctx, facts)
}
