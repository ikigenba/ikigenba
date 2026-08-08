package repos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeFastForwardsStrictlyAheadBranchWithoutCreatingCommit(t *testing.T) {
	// R-KIY5-5UKT
	service, repository, db := newMergeTestService(t, "fast-forward")
	oldMain := gitText(t, repository, "rev-parse", "main")
	branchHead := commitBranchFile(t, repository, "feature", "feature.txt", "feature\n")
	branchCount := gitText(t, repository, "rev-list", "--count", "feature")

	result, err := service.Merge(context.Background(), "code", "fast-forward", "feature", "client:merge")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Merged || result.Strategy != "fast-forward" || result.Rev != branchHead {
		t.Fatalf("merge result = %#v", result)
	}
	if head := gitText(t, repository, "rev-parse", "main"); head != branchHead {
		t.Fatalf("main = %s, want branch head %s", head, branchHead)
	}
	if count := gitText(t, repository, "rev-list", "--count", "main"); count != branchCount {
		t.Fatalf("main count = %s, branch count = %s; merge created a commit", count, branchCount)
	}
	assertOnlyPush(t, db, branchHead, oldMain, "client:merge", "fast-forward")
}

func TestMergeDivergedBranchCreatesTwoParentCommitWithBothTrees(t *testing.T) {
	// R-KK61-JMBI
	service, repository, db := newMergeTestService(t, "diverged")
	branchHead, previousMain := makeDivergedGraph(t, repository, false)
	result, err := service.Merge(context.Background(), "code", "diverged", "feature", "workflow")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Merged || result.Strategy != "merge-commit" || result.Rev != gitText(t, repository, "rev-parse", "main") {
		t.Fatalf("merge result = %#v main=%s", result, gitText(t, repository, "rev-parse", "main"))
	}
	if parents := gitText(t, repository, "show", "-s", "--format=%P", result.Rev); parents != previousMain+" "+branchHead {
		t.Fatalf("merge parents = %q, want %s %s", parents, previousMain, branchHead)
	}
	if got := gitText(t, repository, "show", "main:feature.txt"); got != "feature" {
		t.Fatalf("feature side content = %q", got)
	}
	if got := gitText(t, repository, "show", "main:main.txt"); got != "main" {
		t.Fatalf("main side content = %q", got)
	}
	assertOnlyPush(t, db, result.Rev, previousMain, "workflow", "diverged")
}

func TestMergeConflictingBranchLeavesMainAndEventsUntouched(t *testing.T) {
	// R-KLDX-XE27
	service, repository, db := newMergeTestService(t, "conflict")
	_, previousMain := makeDivergedGraph(t, repository, true)
	beforeCommits := gitText(t, repository, "rev-list", "--all", "--count")
	result, err := service.Merge(context.Background(), "code", "conflict", "feature", "workflow")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("merge result=%#v error=%v, want ErrConflict", result, err)
	}
	assertRejectedMerge(t, repository, db, previousMain, beforeCommits)
}

func TestMergeFailureStatusBlocksByNameUntilReplacedWithSuccess(t *testing.T) {
	// R-KMLU-B5SW
	service, repository, db := newMergeTestService(t, "failure-gate")
	oldMain := gitText(t, repository, "rev-parse", "main")
	branchHead := commitBranchFile(t, repository, "feature", "feature.txt", "feature\n")
	setMergeStatus(t, service, "failure-gate", branchHead, "unit-tests", "failure")

	_, err := service.Merge(context.Background(), "code", "failure-gate", "feature", "workflow")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "unit-tests") {
		t.Fatalf("blocked error = %v, want named ErrConflict", err)
	}
	if gitText(t, repository, "rev-parse", "main") != oldMain || outboxCount(t, db) != 0 {
		t.Fatalf("blocked merge moved main or published: main=%s events=%d", gitText(t, repository, "rev-parse", "main"), outboxCount(t, db))
	}
	setMergeStatus(t, service, "failure-gate", branchHead, "unit-tests", "success")
	result, err := service.Merge(context.Background(), "code", "failure-gate", "feature", "workflow")
	if err != nil || !result.Merged || result.Rev != branchHead || gitText(t, repository, "rev-parse", "main") != branchHead {
		t.Fatalf("successful retry result=%#v err=%v main=%s", result, err, gitText(t, repository, "rev-parse", "main"))
	}
}

func TestMergePendingStatusBlocksByName(t *testing.T) {
	// R-KNTQ-OXJL
	service, repository, db := newMergeTestService(t, "pending-gate")
	oldMain := gitText(t, repository, "rev-parse", "main")
	branchHead := commitBranchFile(t, repository, "feature", "feature.txt", "feature\n")
	setMergeStatus(t, service, "pending-gate", branchHead, "integration", "pending")
	_, err := service.Merge(context.Background(), "code", "pending-gate", "feature", "workflow")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "integration") || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("blocked error = %v, want pending integration ErrConflict", err)
	}
	if gitText(t, repository, "rev-parse", "main") != oldMain || outboxCount(t, db) != 0 {
		t.Fatalf("pending merge moved main or published: main=%s events=%d", gitText(t, repository, "rev-parse", "main"), outboxCount(t, db))
	}
}

func TestMergeGateIsOptInAllGreenAndHeadSpecific(t *testing.T) {
	// R-KP1N-2PAA
	for _, test := range []struct {
		name     string
		statuses func(t *testing.T, service *Service, repository, repoName, head string)
	}{
		{name: "zero-statuses"},
		{name: "all-success", statuses: func(t *testing.T, service *Service, _ string, repoName, head string) {
			setMergeStatus(t, service, repoName, head, "unit", "success")
			setMergeStatus(t, service, repoName, head, "lint", "success")
		}},
		{name: "failure-on-different-sha", statuses: func(t *testing.T, service *Service, repository, repoName, _ string) {
			setMergeStatus(t, service, repoName, gitText(t, repository, "rev-parse", "main"), "stale-check", "failure")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repoName := "gate-" + test.name
			service, repository, _ := newMergeTestService(t, repoName)
			head := commitBranchFile(t, repository, "feature", "feature.txt", "feature\n")
			if test.statuses != nil {
				test.statuses(t, service, repository, repoName, head)
			}
			result, err := service.Merge(context.Background(), "code", repoName, "feature", "workflow")
			if err != nil || !result.Merged || result.Rev != head || gitText(t, repository, "rev-parse", "main") != head {
				t.Fatalf("merge result=%#v err=%v main=%s", result, err, gitText(t, repository, "rev-parse", "main"))
			}
		})
	}
}

func TestMergeAlreadyMergedBranchIsEventlessUpToDate(t *testing.T) {
	// R-KQ9J-GH0Z
	service, repository, db := newMergeTestService(t, "up-to-date")
	commitBranchFile(t, repository, "feature", "feature.txt", "feature\n")
	if _, err := service.Merge(context.Background(), "code", "up-to-date", "feature", "workflow"); err != nil {
		t.Fatal(err)
	}
	clearOutbox(t, db)
	main := gitText(t, repository, "rev-parse", "main")
	count := gitText(t, repository, "rev-list", "--count", "main")
	result, err := service.Merge(context.Background(), "code", "up-to-date", "feature", "workflow")
	if err != nil || result.Merged || result.Strategy != "up-to-date" || result.Rev != main {
		t.Fatalf("up-to-date result=%#v err=%v", result, err)
	}
	if gitText(t, repository, "rev-list", "--count", "main") != count || outboxCount(t, db) != 0 {
		t.Fatalf("up-to-date merge changed count or events: count=%s events=%d", gitText(t, repository, "rev-list", "--count", "main"), outboxCount(t, db))
	}
}

func TestMergeRejectsUnknownTargetsAndMainWithoutMutation(t *testing.T) {
	// R-KRHF-U8RO
	service, repository, db := newMergeTestService(t, "validation")
	main := gitText(t, repository, "rev-parse", "main")
	for _, test := range []struct {
		name, repo, branch string
		want               error
	}{
		{name: "unknown-branch", repo: "validation", branch: "missing", want: ErrNotFound},
		{name: "unknown-repository", repo: "missing", branch: "feature", want: ErrNotFound},
		{name: "main", repo: "validation", branch: "main", want: ErrValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Merge(context.Background(), "code", test.repo, test.branch, "workflow")
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
			if gitText(t, repository, "rev-parse", "main") != main || outboxCount(t, db) != 0 {
				t.Fatalf("rejection changed main or events: main=%s events=%d", gitText(t, repository, "rev-parse", "main"), outboxCount(t, db))
			}
		})
	}
}

func newMergeTestService(t *testing.T, name string) (*Service, string, *sql.DB) {
	t.Helper()
	service, repository, db := newWriteTestService(t, name)
	if _, err := service.PutContent(context.Background(), "code", name, "shared.txt", "seed", "seed", "", []byte("base\n")); err != nil {
		t.Fatal(err)
	}
	clearOutbox(t, db)
	return service, repository, db
}

func commitBranchFile(t *testing.T, repository, branch, name, content string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, "", "clone", "file://"+repository, work)
	runGit(t, work, "checkout", "-b", branch)
	writeMergeFile(t, work, name, content)
	runGit(t, work, "add", name)
	runGit(t, work, "commit", "-m", branch+" change")
	runGit(t, work, "push", "origin", "HEAD:refs/heads/"+branch)
	return gitText(t, repository, "rev-parse", "refs/heads/"+branch)
}

func makeDivergedGraph(t *testing.T, repository string, conflict bool) (branchHead, mainHead string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, "", "clone", "file://"+repository, work)
	runGit(t, work, "checkout", "-b", "feature")
	branchFile := "feature.txt"
	if conflict {
		branchFile = "shared.txt"
	}
	writeMergeFile(t, work, branchFile, "feature\n")
	runGit(t, work, "add", branchFile)
	runGit(t, work, "commit", "-m", "feature change")
	runGit(t, work, "push", "origin", "HEAD:refs/heads/feature")
	branchHead = gitText(t, repository, "rev-parse", "feature")

	runGit(t, work, "checkout", "main")
	mainFile := "main.txt"
	if conflict {
		mainFile = "shared.txt"
	}
	writeMergeFile(t, work, mainFile, "main\n")
	runGit(t, work, "add", mainFile)
	runGit(t, work, "commit", "-m", "main change")
	runGit(t, work, "push", "origin", "main")
	mainHead = gitText(t, repository, "rev-parse", "main")
	return branchHead, mainHead
}

func writeMergeFile(t *testing.T, work, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setMergeStatus(t *testing.T, service *Service, name, sha, check, state string) {
	t.Helper()
	if _, err := service.SetStatus(context.Background(), Status{Kind: "code", Name: name, SHA: sha, CheckName: check, State: state, Actor: "check-runner"}); err != nil {
		t.Fatal(err)
	}
}

func assertRejectedMerge(t *testing.T, repository string, db *sql.DB, main, commitCount string) {
	t.Helper()
	if got := gitText(t, repository, "rev-parse", "main"); got != main {
		t.Fatalf("main = %s, want unchanged %s", got, main)
	}
	if got := gitText(t, repository, "rev-list", "--all", "--count"); got != commitCount {
		t.Fatalf("commit count = %s, want unchanged %s", got, commitCount)
	}
	if count := outboxCount(t, db); count != 0 {
		t.Fatalf("outbox count = %d, want zero", count)
	}
}
