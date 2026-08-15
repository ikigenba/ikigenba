package repos

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

type Change struct {
	Op      string `json:"op"`
	Path    string `json:"path"`
	Content []byte `json:"-"`
}

type CommitBatchRequest struct {
	Kind      string        `json:"kind"`
	Name      string        `json:"name"`
	Message   string        `json:"message"`
	Actor     string        `json:"actor"`
	ParentRev string        `json:"parent_rev"`
	Changes   []BatchChange `json:"changes"`
}

type BatchChange struct {
	Op         string `json:"op"`
	Path       string `json:"path"`
	ContentB64 string `json:"content_b64,omitempty"`
}

type CommitResult struct {
	Rev string `json:"rev"`
}

func (s *Service) PutContent(ctx context.Context, kind, name, filePath, message, actor, pinnedRev string, content []byte) (CommitResult, error) {
	return s.commitChanges(ctx, kind, name, message, actor, pinnedRev, []Change{{Op: "put", Path: filePath, Content: content}})
}

func (s *Service) DeleteContent(ctx context.Context, kind, name, filePath, message, actor string) (CommitResult, error) {
	return s.commitChanges(ctx, kind, name, message, actor, "", []Change{{Op: "delete", Path: filePath}})
}

func (s *Service) CommitBatch(ctx context.Context, kind, name, message, actor, parentRev string, changes []Change) (CommitResult, error) {
	return s.commitChanges(ctx, kind, name, message, actor, parentRev, changes)
}

func (s *Service) commitChanges(ctx context.Context, kind, name, message, actor, pinnedRev string, changes []Change) (CommitResult, error) {
	if err := s.validateCommitRequest(message, actor); err != nil {
		return CommitResult{}, err
	}
	repository, head, err := s.commitRepository(ctx, kind, name, pinnedRev)
	if err != nil {
		return CommitResult{}, err
	}
	indexDir, env, err := s.createCommitIndex(ctx, repository, head)
	if err != nil {
		return CommitResult{}, err
	}
	defer os.RemoveAll(indexDir)
	if err := s.applyChanges(ctx, repository, head, env, changes); err != nil {
		return CommitResult{}, err
	}
	treeSHA, err := s.writeTree(ctx, repository, env)
	if err != nil {
		return CommitResult{}, err
	}
	if result, done, err := s.unchangedCommit(ctx, repository, head, treeSHA, changes); done || err != nil {
		return result, err
	}
	newHead, err := s.createCommit(ctx, repository, head, treeSHA, message, actor, env)
	if err != nil {
		return CommitResult{}, err
	}
	return s.publishCommit(ctx, kind, name, actor, head, newHead)
}

func (s *Service) validateCommitRequest(message, actor string) error {
	if s == nil || s.store == nil || s.custody == nil || s.producer == nil {
		return fmt.Errorf("repos: write dependencies are not configured")
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("%w: message is required", ErrValidation)
	}
	if actor == "" || strings.ContainsAny(actor, "\x00\r\n") {
		return fmt.Errorf("%w: actor is required and must be one line", ErrValidation)
	}
	return nil
}

func (s *Service) commitRepository(ctx context.Context, kind, name, pinnedRev string) (repository, head string, err error) {
	repository, err = s.repositoryPath(kind, name)
	if err != nil {
		return "", "", err
	}
	head, err = s.currentMain(ctx, repository)
	if err != nil {
		return "", "", err
	}
	if pinnedRev != "" && pinnedRev != head {
		return "", "", fmt.Errorf("%w: main is at %s, not pinned rev %s", ErrConflict, head, pinnedRev)
	}
	return repository, head, nil
}

func (s *Service) createCommitIndex(ctx context.Context, repository, head string) (indexDir string, env []string, err error) {
	indexDir, err = os.MkdirTemp("", "repos-index-")
	if err != nil {
		return "", nil, err
	}
	env = []string{"GIT_INDEX_FILE=" + indexDir + "/index", "GIT_WORK_TREE=" + indexDir + "/worktree"}
	if err := os.Mkdir(indexDir+"/worktree", 0o700); err != nil {
		_ = os.RemoveAll(indexDir)
		return "", nil, err
	}
	if head != "" {
		if err := s.custody.git.RunIn(ctx, repository, env, nil, nil, "read-tree", head); err != nil {
			_ = os.RemoveAll(indexDir)
			return "", nil, err
		}
	}
	return indexDir, env, nil
}

func (s *Service) applyChanges(ctx context.Context, repository, head string, env []string, changes []Change) error {
	for _, change := range changes {
		filePath, err := validateTreePath(change.Path, false)
		if err != nil {
			return err
		}
		switch change.Op {
		case "put":
			if err := s.putChange(ctx, repository, head, filePath, env, change.Content); err != nil {
				return err
			}
		case "delete":
			if err := s.custody.git.RunIn(ctx, repository, env, nil, nil, "update-index", "--force-remove", filePath); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: invalid change operation %q", ErrValidation, change.Op)
		}
	}
	return nil
}

func (s *Service) putChange(ctx context.Context, repository, head, filePath string, env []string, content []byte) error {
	var blob bytes.Buffer
	if err := s.custody.git.RunIn(ctx, repository, nil, bytes.NewReader(content), &blob, "hash-object", "-w", "--stdin"); err != nil {
		return err
	}
	mode := "100644"
	if head != "" {
		if _, err := s.treeEntry(ctx, repository, head, "main", filePath); err == nil {
			modeOutput, modeErr := s.custody.git.Run(ctx, repository, "ls-tree", head, "--", filePath)
			if modeErr == nil && strings.HasPrefix(string(modeOutput), "100755 ") {
				mode = "100755"
			}
		}
	}
	return s.custody.git.RunIn(ctx, repository, env, nil, nil, "update-index", "--add", "--cacheinfo", mode, strings.TrimSpace(blob.String()), filePath)
}

func (s *Service) writeTree(ctx context.Context, repository string, env []string) (string, error) {
	var tree bytes.Buffer
	if err := s.custody.git.RunIn(ctx, repository, env, nil, &tree, "write-tree"); err != nil {
		return "", err
	}
	return strings.TrimSpace(tree.String()), nil
}

func (s *Service) unchangedCommit(ctx context.Context, repository, head, treeSHA string, changes []Change) (CommitResult, bool, error) {
	if head == "" {
		return CommitResult{}, len(changes) == 0 || allDeletes(changes), nil
	}
	headTree, err := s.custody.git.Run(ctx, repository, "rev-parse", head+"^{tree}")
	if err != nil {
		return CommitResult{}, false, err
	}
	if treeSHA == strings.TrimSpace(string(headTree)) {
		return CommitResult{Rev: head}, true, nil
	}
	return CommitResult{}, false, nil
}

func (s *Service) createCommit(ctx context.Context, repository, head, treeSHA, message, actor string, env []string) (string, error) {
	commitEnv := append([]string{}, env...)
	commitEnv = append(commitEnv,
		"GIT_AUTHOR_NAME="+actor,
		"GIT_AUTHOR_EMAIL="+actor+"@ikigenba.local",
		"GIT_COMMITTER_NAME=ikigenba",
		"GIT_COMMITTER_EMAIL=repos@ikigenba.local",
		"GIT_AUTHOR_DATE="+s.custody.clock.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"GIT_COMMITTER_DATE="+s.custody.clock.Now().UTC().Format("2006-01-02T15:04:05Z"),
	)
	args := []string{"commit-tree", treeSHA}
	if head != "" {
		args = append(args, "-p", head)
	}
	args = append(args, "-m", message)
	var commit bytes.Buffer
	if err := s.custody.git.RunIn(ctx, repository, commitEnv, nil, &commit, args...); err != nil {
		return "", err
	}
	return strings.TrimSpace(commit.String()), nil
}

func (s *Service) publishCommit(ctx context.Context, kind, name, actor, head, newHead string) (CommitResult, error) {
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	defer rollback(tx)
	if err := s.ApplyRefUpdate(ctx, tx, RefUpdate{Kind: kind, Name: name, Ref: "refs/heads/main", OldSHA: head, NewSHA: newHead, Actor: actor}); err != nil {
		return CommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommitResult{}, err
	}
	s.producer.Ring()
	return CommitResult{Rev: newHead}, nil
}

func (s *Service) currentMain(ctx context.Context, repository string) (string, error) {
	output, err := s.custody.git.Run(ctx, repository, "rev-parse", "--verify", "refs/heads/main^{commit}")
	if err != nil {
		if s.emptyRepository(ctx, repository) {
			return "", nil
		}
		return "", fmt.Errorf("resolve main: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func allDeletes(changes []Change) bool {
	for _, change := range changes {
		if change.Op != "delete" {
			return false
		}
	}
	return true
}

func rollback(tx *sql.Tx) { _ = tx.Rollback() }
