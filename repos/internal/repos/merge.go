package repos

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
)

type MergeResult struct {
	Merged   bool
	Rev      string
	Strategy string
}

func gate(statuses []Status) error {
	blocking := make([]string, 0)
	for _, status := range statuses {
		if status.State == "pending" || status.State == "failure" {
			blocking = append(blocking, fmt.Sprintf("%s (%s)", status.CheckName, status.State))
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	sort.Strings(blocking)
	return fmt.Errorf("%w: checks blocking merge: %s", ErrConflict, strings.Join(blocking, ", "))
}

func (s *Service) Merge(ctx context.Context, kind, name, branch, actor string) (MergeResult, error) {
	if s == nil || s.store == nil || s.custody == nil || s.producer == nil {
		return MergeResult{}, fmt.Errorf("repos: merge dependencies are not configured")
	}
	repository, err := s.repositoryPath(kind, name)
	if err != nil {
		return MergeResult{}, err
	}
	branchHead, err := s.resolveRef(ctx, repository, "refs/heads/"+branch)
	if err != nil {
		return MergeResult{}, err
	}
	mainHead, err := s.resolveRef(ctx, repository, "refs/heads/main")
	if err != nil {
		return MergeResult{}, err
	}
	if branch == "main" {
		return MergeResult{}, fmt.Errorf("%w: main cannot be merged into itself", ErrValidation)
	}

	if _, err := s.custody.git.Run(ctx, repository, "merge-base", "--is-ancestor", branchHead, mainHead); err == nil {
		return MergeResult{Rev: mainHead, Strategy: "up-to-date"}, nil
	}

	statuses, err := s.ListStatuses(ctx, kind, name, branchHead)
	if err != nil {
		return MergeResult{}, err
	}
	if err := gate(statuses); err != nil {
		return MergeResult{}, err
	}

	fastForward := false
	if _, err := s.custody.git.Run(ctx, repository, "merge-base", "--is-ancestor", mainHead, branchHead); err == nil {
		fastForward = true
	}

	newHead := branchHead
	strategy := "fast-forward"
	if !fastForward {
		var tree bytes.Buffer
		if err := s.custody.git.RunIn(ctx, repository, nil, nil, &tree, "merge-tree", "--write-tree", mainHead, branchHead); err != nil {
			return MergeResult{}, fmt.Errorf("%w: branch %q conflicts with main", ErrConflict, branch)
		}
		treeSHA := strings.TrimSpace(tree.String())
		if len(strings.Fields(treeSHA)) != 1 {
			return MergeResult{}, fmt.Errorf("%w: branch %q has unresolved conflicts", ErrConflict, branch)
		}
		if actor == "" || strings.ContainsAny(actor, "\x00\r\n") {
			return MergeResult{}, fmt.Errorf("%w: actor is required and must be one line", ErrValidation)
		}
		commitEnv := []string{
			"GIT_AUTHOR_NAME=" + actor,
			"GIT_AUTHOR_EMAIL=" + actor + "@ikigenba.local",
			"GIT_COMMITTER_NAME=ikigenba",
			"GIT_COMMITTER_EMAIL=repos@ikigenba.local",
			"GIT_AUTHOR_DATE=" + s.custody.Now().UTC().Format("2006-01-02T15:04:05Z"),
			"GIT_COMMITTER_DATE=" + s.custody.Now().UTC().Format("2006-01-02T15:04:05Z"),
		}
		var commit bytes.Buffer
		if err := s.custody.git.RunIn(ctx, repository, commitEnv, nil, &commit,
			"commit-tree", treeSHA, "-p", mainHead, "-p", branchHead, "-m", "merge "+branch+" into main"); err != nil {
			return MergeResult{}, err
		}
		newHead = strings.TrimSpace(commit.String())
		strategy = "merge-commit"
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return MergeResult{}, err
	}
	defer rollback(tx)
	if err := s.ApplyRefUpdate(ctx, tx, RefUpdate{
		Kind: kind, Name: name, Ref: "refs/heads/main", OldSHA: mainHead, NewSHA: newHead, Actor: actor,
	}); err != nil {
		return MergeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeResult{}, err
	}
	s.producer.Ring()
	return MergeResult{Merged: true, Rev: newHead, Strategy: strategy}, nil
}
