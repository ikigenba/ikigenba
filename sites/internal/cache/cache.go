// Package cache reads repositories and atomically keeps published trees.
package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

// Config supplies the cache's paths, git, limits and optional synchronization hooks.
type Config struct {
	Root     string
	Repos    string
	Git      *git.Git
	Limits   *limits.Limits
	Joined   func(site, sha string)
	Unpacked func(site, sha string)
}

// Cache holds the disposable trees and coordinates running rebuilds.
type Cache struct {
	cfg    Config
	mu     sync.Mutex
	builds map[string]*build
	active map[*unpack]struct{}
}
type build struct {
	done      chan struct{}
	err       error
	site, sha string
	discard   bool
}
type unpack struct {
	site, sha, stage string
	discard          bool
}

// RepoPrefix identifies repository ids.
const RepoPrefix = "rep_"

// Errors distinguish absent repositories from absent commits.
var (
	ErrRepositoryMissing = errors.New("repository missing")
	ErrNoCommit          = errors.New("commit missing")
)

// Unavailable reasons are the event vocabulary.
const (
	ReasonRepositoryMissing = "repository_missing"
	ReasonCommitMissing     = "commit_missing"
	ReasonTooLarge          = "too_large"
	ReasonTimedOut          = "timed_out"
	ReasonGitFailed         = "git_failed"
)

// Open creates the cache root without inspecting repositories.
func Open(cfg Config) (*Cache, error) {
	if err := os.MkdirAll(cfg.Root, 0700); err != nil {
		return nil, err
	}
	return &Cache{cfg: cfg, builds: make(map[string]*build), active: make(map[*unpack]struct{})}, nil
}

// RepoDir returns the repository's bare directory without inspecting it.
func (c *Cache) RepoDir(repo string) string { return filepath.Join(c.cfg.Repos, repo+".git") }

// Dir returns the path of a cached tree without inspecting it.
func (c *Cache) Dir(site, sha string) string { return filepath.Join(c.cfg.Root, site, sha) }
func hex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, b := range []byte(s) {
		if !strings.ContainsRune("0123456789abcdef", rune(b)) {
			return false
		}
	}
	return true
}

// ValidRepo reports whether s is a repository id.
func ValidRepo(s string) bool {
	return strings.HasPrefix(s, RepoPrefix) && hex(strings.TrimPrefix(s, RepoPrefix), 16)
}

// ValidRef checks git's ref grammar while permitting one component.
func ValidRef(s string) bool {
	if s == "" || s == "@" || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.Contains(s, "//") || strings.Contains(s, "..") || strings.Contains(s, "@{") {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".lock") {
			return false
		}
	}
	for _, b := range []byte(s) {
		if b < 0x20 || b == 0x7f || strings.ContainsRune(" ~^:?*[\\", rune(b)) {
			return false
		}
	}
	return true
}
func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
func (c *Cache) repository(repo string) error {
	if !ValidRepo(repo) || !isDir(c.RepoDir(repo)) {
		return ErrRepositoryMissing
	}
	return nil
}
func validTree(site, sha string) error {
	if !store.ValidID(site) {
		return errors.New("invalid site id")
	}
	if !hex(sha, 40) {
		return ErrNoCommit
	}
	return nil
}
func (c *Cache) output(ctx context.Context, args ...string) ([]byte, error) {
	op, cancel := c.cfg.Limits.Operation(ctx)
	defer cancel()
	out, err := c.cfg.Git.Output(op, "", args...)
	if cause := context.Cause(op); cause != nil {
		return nil, cause
	}
	return out, err
}

// Owner reads only the owner in the repository's own configuration.
func (c *Cache) Owner(ctx context.Context, repo string) (string, error) {
	if err := c.repository(repo); err != nil {
		return "", err
	}
	out, err := c.output(ctx, "config", "--file", filepath.Join(c.RepoDir(repo), "config"), "--get", "ikigenba.owner")
	var ge *git.Error
	if errors.As(err, &ge) && ge.Status == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Resolve peels a ref to its commit without modifying the repository.
func (c *Cache) Resolve(ctx context.Context, repo, ref string) (string, error) {
	if err := c.repository(repo); err != nil {
		return "", err
	}
	out, err := c.output(ctx, "--git-dir="+c.RepoDir(repo), "rev-parse", "--verify", "-q", "--end-of-options", ref+"^{commit}")
	var ge *git.Error
	if errors.As(err, &ge) && ge.Status == 1 {
		return "", ErrNoCommit
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Reason classifies the error a rebuild returned.
func Reason(err error) string {
	switch {
	case errors.Is(err, ErrRepositoryMissing):
		return ReasonRepositoryMissing
	case errors.Is(err, ErrNoCommit):
		return ReasonCommitMissing
	case errors.Is(err, limits.ErrTooLarge):
		return ReasonTooLarge
	case errors.Is(err, limits.ErrTimedOut):
		return ReasonTimedOut
	default:
		return ReasonGitFailed
	}
}
