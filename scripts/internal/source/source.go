// Package source reads repositories through the host's git.
package source

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// RepoPrefix prefixes repository identifiers.
const RepoPrefix = "rep_"

// ErrRepositoryMissing denotes an invalid id or absent repository directory.
var ErrRepositoryMissing = errors.New("repository missing")

// ErrNoCommit denotes a reference that names no commit.
var ErrNoCommit = errors.New("commit missing")

// Config supplies the repository root and bounded git executor.
type Config struct {
	Repos  string
	Git    *git.Git
	Limits *limits.Limits
}

// Source reads the current state of repositories without retaining it.
type Source struct{ cfg Config }

// New builds a source without touching its repository root.
func New(cfg Config) *Source { return &Source{cfg: cfg} }

// RepoDir computes a repository's directory.
func (s *Source) RepoDir(repo string) string { return filepath.Join(s.cfg.Repos, repo+".git") }

func hexadecimal(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := range len(s) {
		if !strings.ContainsRune("0123456789abcdef", rune(s[i])) {
			return false
		}
	}
	return true
}

// ValidRepo checks a repository identifier.
func ValidRepo(s string) bool {
	return strings.HasPrefix(s, RepoPrefix) && hexadecimal(strings.TrimPrefix(s, RepoPrefix), 16)
}

// ValidRef implements git's ref-name rules with one-level names allowed.
func ValidRef(s string) bool {
	if s == "" || s == "@" || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.Contains(s, "//") || strings.Contains(s, "..") || strings.Contains(s, "@{") {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] == 0x7f || strings.ContainsRune(" ~^:?*[\\", rune(s[i])) {
			return false
		}
	}
	for _, c := range strings.Split(s, "/") {
		if strings.HasPrefix(c, ".") || strings.HasSuffix(c, ".lock") {
			return false
		}
	}
	return true
}
func (s *Source) repository(repo string) error {
	if !ValidRepo(repo) {
		return ErrRepositoryMissing
	}
	st, err := os.Stat(s.RepoDir(repo))
	if err != nil || !st.IsDir() {
		return ErrRepositoryMissing
	}
	return nil
}
func (s *Source) output(ctx context.Context, args ...string) ([]byte, error) {
	op, cancel := s.cfg.Limits.Operation(ctx)
	defer cancel()
	return s.cfg.Git.Output(op, "", args...)
}
func (s *Source) value(ctx context.Context, repo, key string) (string, error) {
	if err := s.repository(repo); err != nil {
		return "", err
	}
	b, err := s.output(ctx, "config", "--file", filepath.Join(s.RepoDir(repo), "config"), "--get", key)
	var ge *git.Error
	if errors.As(err, &ge) && ge.Status == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}

// Owner reads only the repository's owner config key.
func (s *Source) Owner(ctx context.Context, repo string) (string, error) {
	return s.value(ctx, repo, "ikigenba.owner")
}

// Name answers the current repository name, or no name on any failure.
func (s *Source) Name(ctx context.Context, repo string) (string, bool) {
	n, err := s.value(ctx, repo, "ikigenba.name")
	return n, err == nil && n != ""
}

// Resolve peels a git reference to its commit.
func (s *Source) Resolve(ctx context.Context, repo, ref string) (string, error) {
	if err := s.repository(repo); err != nil {
		return "", err
	}
	b, err := s.output(ctx, "--git-dir="+s.RepoDir(repo), "rev-parse", "--verify", "-q", "--end-of-options", ref+"^{commit}")
	var ge *git.Error
	if errors.As(err, &ge) && ge.Status == 1 {
		return "", ErrNoCommit
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}

// Archive unpacks one commit into a new directory, retaining partial results on failure.
func (s *Source) Archive(ctx context.Context, repo, sha, dir string) error {
	if err := s.repository(repo); err != nil {
		return err
	}
	if !hexadecimal(sha, 40) {
		return ErrNoCommit
	}
	if err := s.archiveDestination(dir); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	op, release := s.cfg.Limits.Operation(ctx)
	defer release()
	if cause := context.Cause(op); cause != nil {
		return cause
	}
	runctx, stop := context.WithCancel(op)
	defer stop()
	cmd := s.cfg.Git.Command(runctx, "", "--git-dir="+s.RepoDir(repo), "archive", "--format=tar", sha)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stream, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		if cause := context.Cause(op); cause != nil {
			return cause
		}
		return fmt.Errorf("%w: %w", git.ErrNotFound, err)
	}
	unpackErr := unpack(stream, dir, s.cfg.Limits.Settings().TreeMaxBytes)
	if unpackErr != nil {
		stop()
	}
	waitErr := cmd.Wait()
	if cause := context.Cause(op); cause != nil {
		return cause
	}
	if unpackErr != nil && !errors.Is(unpackErr, io.ErrUnexpectedEOF) && !errors.Is(unpackErr, io.EOF) {
		return unpackErr
	}
	if waitErr != nil {
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			return &git.Error{Status: exit.ExitCode(), Stderr: stderr.String()}
		}
		return waitErr
	}
	return unpackErr
}

// archiveDestination keeps extraction entirely apart from the read-only root.
func (s *Source) archiveDestination(dir string) error {
	repos := s.cfg.Repos
	if repos == "" {
		repos = "."
	}
	root, err := filepath.EvalSymlinks(repos)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	// Split without cleaning: a symlink followed by .. has filesystem semantics.
	name := strings.TrimRight(dir, string(filepath.Separator))
	index := strings.LastIndexByte(name, filepath.Separator)
	parent, base := ".", name
	if index >= 0 {
		parent, base = name[:index+1], name[index+1:]
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return err
	}
	destination := filepath.Join(parent, base)
	for _, pair := range [][2]string{{root, destination}, {destination, root}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			return err
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("archive destination overlaps repositories")
		}
	}
	return nil
}

func unpack(r io.Reader, dir string, maximum int64) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	tr := tar.NewReader(r)
	seen := make(map[string]byte)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		clean := filepath.Clean(filepath.FromSlash(name))
		if name == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("unsafe archive path")
		}
		if _, ok := seen[clean]; ok {
			return errors.New("duplicate archive path")
		}
		for p := filepath.Dir(clean); p != "."; p = filepath.Dir(p) {
			if typ, ok := seen[p]; ok && typ != tar.TypeDir {
				return errors.New("archive path beneath non-directory")
			}
		}
		switch h.Typeflag {
		case tar.TypeReg:
			if h.Size < 0 || h.Size > maximum-total {
				return limits.ErrTooLarge
			}
			total += h.Size
			if err = root.MkdirAll(filepath.Dir(clean), 0700); err != nil {
				return err
			}
			var f *os.File
			f, err = root.OpenFile(clean, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(h.Mode&0777))
			if err != nil {
				return err
			}
			_, err = io.CopyN(f, tr, h.Size)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeDir:
			err = root.MkdirAll(clean, os.FileMode(h.Mode&0777))
		case tar.TypeSymlink:
			if err = root.MkdirAll(filepath.Dir(clean), 0700); err == nil {
				err = root.Symlink(h.Linkname, clean)
			}
		default:
			return errors.New("unsupported archive entry")
		}
		if err != nil {
			return err
		}
		seen[clean] = h.Typeflag
	}
}

// Reason returns the catalog reason for a failed source operation.
func Reason(err error) string {
	switch {
	case errors.Is(err, ErrRepositoryMissing):
		return store.ReasonRepositoryMissing
	case errors.Is(err, ErrNoCommit):
		return store.ReasonCommitMissing
	case errors.Is(err, limits.ErrTooLarge):
		return store.ReasonTooLarge
	case errors.Is(err, limits.ErrTimedOut):
		return store.ReasonTimedOut
	default:
		return store.ReasonGitFailed
	}
}
