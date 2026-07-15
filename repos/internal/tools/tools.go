// Package tools provides the deliberately small tool surface available to a
// repository session.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ikigenba/agentkit"
)

// New returns the complete session toolset. All filesystem tools are confined
// to root, and Bash always starts with root as its working directory.
func New(root string) []agentkit.Tool {
	root, err := filepath.Abs(root)
	if err != nil {
		panic(err)
	}
	return []agentkit.Tool{
		agentkit.NewTool("Bash", "Run a shell command in the repository worktree.", func(ctx context.Context, in struct {
			Command string `json:"command" jsonschema:"required"`
		}) (string, error) {
			cmd := exec.CommandContext(ctx, "bash", "-lc", in.Command)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if err != nil {
				return string(output), fmt.Errorf("bash: %w", err)
			}
			return string(output), nil
		}),
		agentkit.NewTool("Read", "Read a UTF-8 file from the repository.", func(_ context.Context, in pathInput) (string, error) {
			path, err := resolveExisting(root, in.Path)
			if err != nil {
				return "", err
			}
			contents, err := os.ReadFile(path)
			return string(contents), err
		}),
		agentkit.NewTool("Write", "Write a file in the repository.", func(_ context.Context, in writeInput) (string, error) {
			path, err := confinePath(root, in.Path)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
				return "", err
			}
			return "written", nil
		}),
		agentkit.NewTool("Edit", "Replace exact text in a repository file.", func(_ context.Context, in editInput) (string, error) {
			path, err := resolveExisting(root, in.Path)
			if err != nil {
				return "", err
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			if in.Old == "" || !strings.Contains(string(contents), in.Old) {
				return "", errors.New("edit: old text not found")
			}
			updated := strings.Replace(string(contents), in.Old, in.New, 1)
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return "", err
			}
			return "edited", nil
		}),
		agentkit.NewTool("Glob", "List repository paths matching a glob.", func(_ context.Context, in struct {
			Pattern string `json:"pattern" jsonschema:"required"`
		}) (string, error) {
			pattern, err := confinePath(root, in.Pattern)
			if err != nil {
				return "", err
			}
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return "", err
			}
			sort.Strings(matches)
			for i := range matches {
				matches[i], _ = filepath.Rel(root, matches[i])
			}
			return strings.Join(matches, "\n"), nil
		}),
		agentkit.NewTool("Grep", "Search repository files with a regular expression.", func(_ context.Context, in grepInput) (string, error) {
			re, err := regexp.Compile(in.Pattern)
			if err != nil {
				return "", err
			}
			start := root
			if in.Path != "" {
				start, err = resolveExisting(root, in.Path)
				if err != nil {
					return "", err
				}
			}
			var matches []string
			err = filepath.WalkDir(start, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil || entry.IsDir() {
					return walkErr
				}
				contents, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				for number, line := range strings.Split(string(contents), "\n") {
					if re.MatchString(line) {
						rel, _ := filepath.Rel(root, path)
						matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, number+1, line))
					}
				}
				return nil
			})
			return strings.Join(matches, "\n"), err
		}),
	}
}

type pathInput struct {
	Path string `json:"path" jsonschema:"required"`
}

type writeInput struct {
	Path    string `json:"path" jsonschema:"required"`
	Content string `json:"content" jsonschema:"required"`
}

type editInput struct {
	Path string `json:"path" jsonschema:"required"`
	Old  string `json:"old" jsonschema:"required"`
	New  string `json:"new"`
}

type grepInput struct {
	Pattern string `json:"pattern" jsonschema:"required"`
	Path    string `json:"path"`
}

func confinePath(root, requested string) (string, error) {
	if requested == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("path %q is outside worktree", requested)
	}
	clean := filepath.Clean(requested)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes worktree", requested)
	}
	joined := filepath.Join(root, clean)
	if resolved, err := filepath.EvalSymlinks(joined); err == nil {
		if !within(root, resolved) {
			return "", fmt.Errorf("path %q escapes worktree through symlink", requested)
		}
		return resolved, nil
	}
	parent := filepath.Dir(joined)
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			if !within(root, resolved) {
				return "", fmt.Errorf("path %q escapes worktree through symlink", requested)
			}
			break
		}
		if !os.IsNotExist(err) || parent == root {
			break
		}
		parent = filepath.Dir(parent)
	}
	return joined, nil
}

func resolveExisting(root, requested string) (string, error) {
	path, err := confinePath(root, requested)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !within(root, resolved) {
		return "", fmt.Errorf("path %q escapes worktree through symlink", requested)
	}
	return resolved, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
