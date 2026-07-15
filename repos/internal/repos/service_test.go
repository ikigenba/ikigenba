package repos

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

type staticTokenSource struct {
	token string
	calls int
}

func (s *staticTokenSource) Token(context.Context) (string, error) {
	s.calls++
	return s.token, nil
}

func TestEnsureRepoCreatesCanonicalCloneAndIsIdempotent(t *testing.T) {
	// R-EXFQ-NUVB
	remote := newBareRemote(t)
	store, _ := migratedStore(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "repos")
	clock := fixedClock{time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	service := NewService(store, NewGit(stateRoot, &staticTokenSource{token: "fixture"}), clock, "ikigenba")
	facts := RepoFacts{Name: "fixture", Owner: "owner@example.com", CloneURL: fileURL(remote), DefaultBranch: "main"}
	if err := service.EnsureRepo(context.Background(), facts); err != nil {
		t.Fatalf("first EnsureRepo: %v", err)
	}
	if err := service.EnsureRepo(context.Background(), facts); err != nil {
		t.Fatalf("second EnsureRepo: %v", err)
	}
	got, err := store.GetRepo(context.Background(), facts.Name)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	want := Repo{Name: facts.Name, OwnerEmail: facts.Owner, CloneURL: facts.CloneURL, DefaultBranch: "main", CreatedAt: clock.value}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repo = %#v, want %#v", got, want)
	}
	origin := gitOutput(t, stateRoot+"/fixture", "remote", "get-url", "origin")
	if origin != facts.CloneURL {
		t.Fatalf("origin = %q, want %q", origin, facts.CloneURL)
	}
}

func TestCloneRepoDerivesOrganizationURLAndConflictsWhenTracked(t *testing.T) {
	// R-EYNN-1MM0
	t.Setenv("REPOS_GITHUB_ORG", "ikigenba")
	remote := newBareRemote(t)
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	writeGitConfig(t, globalConfig, "https://github.com/ikigenba/", fileURL(filepath.Dir(remote))+"/")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	store, _ := migratedStore(t)
	service := NewService(store, NewGit(filepath.Join(t.TempDir(), "repos"), &staticTokenSource{token: "fixture"}),
		fixedClock{time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}, "ikigenba")
	// The insteadOf target needs a repository named acme.git.
	parent := filepath.Dir(remote)
	if err := exec.Command("git", "clone", "--bare", remote, filepath.Join(parent, "acme.git")).Run(); err != nil {
		t.Fatalf("make named fixture remote: %v", err)
	}
	if err := service.CloneRepo(context.Background(), "owner@example.com", "acme"); err != nil {
		t.Fatalf("CloneRepo: %v", err)
	}
	got, err := store.GetRepo(context.Background(), "acme")
	if err != nil {
		t.Fatalf("get cloned repo: %v", err)
	}
	if got.CloneURL != "https://github.com/ikigenba/acme.git" {
		t.Fatalf("clone URL = %q", got.CloneURL)
	}
	if err := service.CloneRepo(context.Background(), "owner@example.com", "acme"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second CloneRepo error = %v, want ErrConflict", err)
	}
}
