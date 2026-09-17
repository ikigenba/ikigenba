package checkout

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// Head returns the object name of the checkout's current HEAD.
func (checkout *Checkout) Head(ctx context.Context) (string, error) {
	stdout, err := checkout.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(stdout, "\r\n"), nil
}

// Clean reports whether the checkout has no tracked or untracked changes.
func (checkout *Checkout) Clean(ctx context.Context) (bool, error) {
	stdout, err := checkout.git(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return stdout == "", nil
}

// TagsAtHead returns the tags that point at the checkout's current HEAD.
func (checkout *Checkout) TagsAtHead(ctx context.Context) ([]string, error) {
	stdout, err := checkout.git(ctx, "tag", "--points-at", "HEAD")
	if err != nil {
		return nil, err
	}

	var tags []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line != "" {
			tags = append(tags, line)
		}
	}
	return tags, nil
}

func (checkout *Checkout) git(ctx context.Context, args ...string) (string, error) {
	command := seam.Cmd{Path: "git", Args: args, Dir: checkout.Root}
	result, err := checkout.Deps.Exec(ctx, command)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	if result.ExitCode != 0 {
		return "", &GitError{
			Args:     append([]string(nil), args...),
			ExitCode: result.ExitCode,
			Stderr:   string(result.Stderr),
		}
	}
	return string(result.Stdout), nil
}
