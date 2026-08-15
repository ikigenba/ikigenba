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
	if s == nil || s.store == nil || s.custody == nil || s.producer == nil {
		return CommitResult{}, fmt.Errorf("repos: write dependencies are not configured")
	}
	if strings.TrimSpace(message) == "" {
		return CommitResult{}, fmt.Errorf("%w: message is required", ErrValidation)
	}
	if actor == "" || strings.ContainsAny(actor, "\x00\r\n") {
		return CommitResult{}, fmt.Errorf("%w: actor is required and must be one line", ErrValidation)
	}
	repository, err := s.repositoryPath(kind, name)
	if err != nil {
		return CommitResult{}, err
	}
	head, err := s.currentMain(ctx, repository)
	if err != nil {
		return CommitResult{}, err
	}
	if pinnedRev != "" && pinnedRev != head {
		return CommitResult{}, fmt.Errorf("%w: main is at %s, not pinned rev %s", ErrConflict, head, pinnedRev)
	}

	indexDir, err := os.MkdirTemp("", "repos-index-")
	if err != nil {
		return CommitResult{}, err
	}
	defer os.RemoveAll(indexDir)
	workTree := indexDir + "/worktree"
	if err := os.Mkdir(workTree, 0o700); err != nil {
		return CommitResult{}, err
	}
	env := []string{"GIT_INDEX_FILE=" + indexDir + "/index", "GIT_WORK_TREE=" + workTree}
	if head != "" {
		if err := s.custody.git.RunIn(ctx, repository, env, nil, nil, "read-tree", head); err != nil {
			return CommitResult{}, err
		}
	}
	for _, change := range changes {
		filePath, validationErr := validateTreePath(change.Path, false)
		if validationErr != nil {
			return CommitResult{}, validationErr
		}
		switch change.Op {
		case "put":
			var blob bytes.Buffer
			if err := s.custody.git.RunIn(ctx, repository, nil, bytes.NewReader(change.Content), &blob, "hash-object", "-w", "--stdin"); err != nil {
				return CommitResult{}, err
			}
			mode := "100644"
			if head != "" {
				_, entryErr := s.treeEntry(ctx, repository, head, "main", filePath)
				if entryErr == nil {
					modeOutput, modeErr := s.custody.git.Run(ctx, repository, "ls-tree", head, "--", filePath)
					if modeErr == nil && strings.HasPrefix(string(modeOutput), "100755 ") {
						mode = "100755"
					}
				}
			}
			if err := s.custody.git.RunIn(ctx, repository, env, nil, nil, "update-index", "--add", "--cacheinfo", mode, strings.TrimSpace(blob.String()), filePath); err != nil {
				return CommitResult{}, err
			}
		case "delete":
			if err := s.custody.git.RunIn(ctx, repository, env, nil, nil, "update-index", "--force-remove", filePath); err != nil {
				return CommitResult{}, err
			}
		default:
			return CommitResult{}, fmt.Errorf("%w: invalid change operation %q", ErrValidation, change.Op)
		}
	}
	var tree bytes.Buffer
	if err := s.custody.git.RunIn(ctx, repository, env, nil, &tree, "write-tree"); err != nil {
		return CommitResult{}, err
	}
	treeSHA := strings.TrimSpace(tree.String())
	if head != "" {
		headTree, treeErr := s.custody.git.Run(ctx, repository, "rev-parse", head+"^{tree}")
		if treeErr != nil {
			return CommitResult{}, treeErr
		}
		if treeSHA == strings.TrimSpace(string(headTree)) {
			return CommitResult{Rev: head}, nil
		}
	} else if len(changes) == 0 || allDeletes(changes) {
		return CommitResult{}, nil
	}

	commitEnv := make([]string, 0, len(env)+6)
	commitEnv = append(commitEnv, env...)
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
		return CommitResult{}, err
	}
	newHead := strings.TrimSpace(commit.String())
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
