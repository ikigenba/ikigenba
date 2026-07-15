package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const DefaultWorktreeTTL = 14 * 24 * time.Hour

const maximumSweepInterval = time.Hour

// Reaper removes disposable git state while preserving durable session files.
type Reaper struct {
	store     *Store
	git       *Git
	clock     Clock
	stateRoot string
	ttl       time.Duration
}

func NewReaper(store *Store, git *Git, clock Clock, stateRoot string, ttl time.Duration) (*Reaper, error) {
	if store == nil || git == nil || clock == nil || stateRoot == "" {
		return nil, errors.New("reaper: store, git, clock, and state root are required")
	}
	if ttl <= 0 {
		ttl = DefaultWorktreeTTL
	}
	return &Reaper{store: store, git: git, clock: clock, stateRoot: stateRoot, ttl: ttl}, nil
}

// SweepInterval bounds the dispatcher's cleanup cadence while scaling down for
// deliberately short retention windows.
func (r *Reaper) SweepInterval() time.Duration {
	interval := r.ttl / 2
	if interval <= 0 {
		return time.Nanosecond
	}
	if interval > maximumSweepInterval {
		return maximumSweepInterval
	}
	return interval
}

// Success removes the successful worktree and superseded failed worktrees for
// the same repository issue.
func (r *Reaper) Success(ctx context.Context, session Session) error {
	if err := r.removeWorktree(ctx, session); err != nil {
		return err
	}
	if session.IssueNumber == nil {
		return nil
	}
	sessions, err := r.store.ListSessions(ctx, session.RepoName, "")
	if err != nil {
		return fmt.Errorf("reaper: list superseded sessions: %w", err)
	}
	for _, older := range sessions {
		if older.ID == session.ID || older.IssueNumber == nil || *older.IssueNumber != *session.IssueNumber ||
			older.Attempt >= session.Attempt || (older.Status != StatusFailed && older.Status != StatusCancelled) {
			continue
		}
		if err := r.removeWorktree(ctx, older); err != nil {
			return err
		}
	}
	return nil
}

// Sweep removes only worktrees belonging to terminal sessions older than TTL.
func (r *Reaper) Sweep(ctx context.Context) error {
	sessions, err := r.store.ListSessions(ctx, "", "")
	if err != nil {
		return fmt.Errorf("reaper: list sweep sessions: %w", err)
	}
	cutoff := r.clock.Now().Add(-r.ttl)
	for _, session := range sessions {
		if !terminalStatus(session.Status) || session.EndedAt == nil || !session.EndedAt.Before(cutoff) {
			continue
		}
		if err := r.removeWorktree(ctx, session); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRepo removes the recoverable clone and all of its session worktrees,
// retaining session rows and files.
func (r *Reaper) DeleteRepo(ctx context.Context, name string) error {
	sessions, err := r.store.ListSessions(ctx, name, "")
	if err != nil {
		return fmt.Errorf("reaper: list repository sessions: %w", err)
	}
	for _, session := range sessions {
		if err := r.removeWorktree(ctx, session); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(r.stateRoot, "repos", name)); err != nil {
		return fmt.Errorf("reaper: remove repository clone: %w", err)
	}
	if err := r.store.DeleteRepo(ctx, name); err != nil {
		return fmt.Errorf("reaper: delete repository: %w", err)
	}
	return nil
}

func (r *Reaper) removeWorktree(ctx context.Context, session Session) error {
	path := filepath.Join(r.stateRoot, "sessions", session.ID, "worktree")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("reaper: inspect worktree %s: %w", session.ID, err)
	}
	if err := r.git.WorktreeRemove(ctx, session.RepoName, path); err != nil {
		return fmt.Errorf("reaper: remove worktree %s: %w", session.ID, err)
	}
	return nil
}
