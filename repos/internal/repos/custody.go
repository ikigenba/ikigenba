package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrValidation = errors.New("repos: validation failed")
	ErrConflict   = errors.New("repos: conflict")
	ErrNotFound   = errors.New("repos: not found")
	ErrForcePush  = errors.New("repos: force push rejected")
	ErrTooLarge   = errors.New("repos: too_large")
)

var Kinds = []string{"sites", "scripts", "prompts", "code"}

var validNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func ValidName(name string) bool {
	return name != "." && name != ".." && validNamePattern.MatchString(name)
}

type Clock interface {
	Now() time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

type Custody struct {
	root  string
	git   Git
	clock Clock
}

func NewCustody(root string, git Git, clock Clock) (*Custody, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: state root must be absolute", ErrValidation)
	}
	if git == nil {
		return nil, fmt.Errorf("%w: git is required", ErrValidation)
	}
	if clock == nil {
		clock = wallClock{}
	}
	return &Custody{root: filepath.Clean(root), git: git, clock: clock}, nil
}

func (c *Custody) Path(kind, name string) (string, error) {
	if !validKind(kind) || !ValidName(name) {
		return "", fmt.Errorf("%w: invalid repository key %q/%q", ErrValidation, kind, name)
	}
	return filepath.Join(c.root, "git", kind, name+".git"), nil
}

func validKind(kind string) bool {
	for _, candidate := range Kinds {
		if kind == candidate {
			return true
		}
	}
	return false
}

func (c *Custody) Exists(kind, name string) bool {
	path, err := c.Path(kind, name)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Now returns the current time from the custody clock shared by repository
// operations and credential validation.
func (c *Custody) Now() time.Time { return c.clock.Now() }

func (c *Custody) Init(ctx context.Context, kind, name string) error {
	path, err := c.Path(kind, name)
	if err != nil {
		return err
	}
	if c.Exists(kind, name) {
		return ErrConflict
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create repository parent: %w", err)
	}
	if _, err := c.git.Run(ctx, "", "init", "--bare", "--initial-branch=main", path); err != nil {
		return err
	}
	return c.EnsureHooks(ctx, kind, name)
}

func (c *Custody) Rename(_ context.Context, kind, from, to string) error {
	fromPath, err := c.Path(kind, from)
	if err != nil {
		return err
	}
	toPath, err := c.Path(kind, to)
	if err != nil {
		return err
	}
	if _, err := os.Stat(fromPath); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(toPath); err == nil {
		return ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(fromPath, toPath); err != nil {
		return fmt.Errorf("rename repository: %w", err)
	}
	return nil
}

func (c *Custody) Archive(_ context.Context, kind, name string) (string, error) {
	live, err := c.Path(kind, name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(live); errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	rel := filepath.Join("archive", kind, fmt.Sprintf("%s.%d.git", name, c.clock.Now().UnixNano()))
	archived := filepath.Join(c.root, rel)
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(archived); err == nil {
		return "", ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(live, archived); err != nil {
		return "", fmt.Errorf("archive repository: %w", err)
	}
	return filepath.ToSlash(rel), nil
}

func (c *Custody) EnsureHooks(_ context.Context, kind, name string) error {
	path, err := c.Path(kind, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	hooks := filepath.Join(path, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-receive"), []byte(preReceiveHook), 0o755); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(hooks, "pre-receive"), 0o755)
}

func (c *Custody) Refs(ctx context.Context, kind, name string) (map[string]string, error) {
	path, err := c.Path(kind, name)
	if err != nil {
		return nil, err
	}
	output, err := c.git.Run(ctx, path, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return nil, fmt.Errorf("unexpected git ref output %q", line)
		}
		refs[parts[0]] = parts[1]
	}
	return refs, nil
}

// SweepHooks repairs the hook in every live repository under the custody root.
func (c *Custody) SweepHooks(ctx context.Context) error {
	for _, kind := range Kinds {
		dir := filepath.Join(c.root, "git", kind)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".git") {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".git")
			if err := c.EnsureHooks(ctx, kind, name); err != nil {
				return err
			}
		}
	}
	return nil
}
