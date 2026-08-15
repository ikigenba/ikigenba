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
	repository, branchHead, mainHead, err := s.mergeHeads(ctx, kind, name, branch)
	if err != nil {
		return MergeResult{}, err
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

	newHead, strategy, err := s.mergedHead(ctx, repository, branch, actor, mainHead, branchHead)
	if err != nil {
		return MergeResult{}, err
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

func (s *Service) mergeHeads(ctx context.Context, kind, name, branch string) (repository, branchHead, mainHead string, err error) {
	repository, err = s.repositoryPath(kind, name)
	if err != nil {
		return "", "", "", err
	}
	branchHead, err = s.resolveRef(ctx, repository, "refs/heads/"+branch)
	if err != nil {
		return "", "", "", err
	}
	mainHead, err = s.resolveRef(ctx, repository, "refs/heads/main")
	if err != nil {
		return "", "", "", err
	}
	if branch == "main" {
		return "", "", "", fmt.Errorf("%w: main cannot be merged into itself", ErrValidation)
	}
	return repository, branchHead, mainHead, nil
}

func (s *Service) mergedHead(ctx context.Context, repository, branch, actor, mainHead, branchHead string) (newHead, strategy string, err error) {
	if _, err := s.custody.git.Run(ctx, repository, "merge-base", "--is-ancestor", mainHead, branchHead); err == nil {
		return branchHead, "fast-forward", nil
	}
	treeSHA, err := s.mergeTree(ctx, repository, branch, mainHead, branchHead)
	if err != nil {
		return "", "", err
	}
	if actor == "" || strings.ContainsAny(actor, "\x00\r\n") {
		return "", "", fmt.Errorf("%w: actor is required and must be one line", ErrValidation)
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
		return "", "", err
	}
	return strings.TrimSpace(commit.String()), "merge-commit", nil
}

func (s *Service) mergeTree(ctx context.Context, repository, branch, mainHead, branchHead string) (string, error) {
	var tree bytes.Buffer
	if err := s.custody.git.RunIn(ctx, repository, nil, nil, &tree, "merge-tree", "--write-tree", mainHead, branchHead); err != nil {
		return "", fmt.Errorf("%w: branch %q conflicts with main", ErrConflict, branch)
	}
	treeSHA := strings.TrimSpace(tree.String())
	if len(strings.Fields(treeSHA)) != 1 {
		return "", fmt.Errorf("%w: branch %q has unresolved conflicts", ErrConflict, branch)
	}
	return treeSHA, nil
}
