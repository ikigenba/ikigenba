package checkout

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

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

// NewestRelease selects the greatest locally present numbered release tag.
func (checkout *Checkout) NewestRelease(ctx context.Context) (string, bool, error) {
	output, err := checkout.git(ctx, "tag", "--list", "--no-column")
	if err != nil {
		return "", false, err
	}
	best := ""
	for _, tag := range strings.Split(output, "\n") {
		if len(tag) < 2 || tag[0] != 'r' || (len(tag) > 2 && tag[1] == '0') {
			continue
		}
		valid := true
		for i := 1; i < len(tag); i++ {
			if tag[i] < '0' || tag[i] > '9' {
				valid = false
				break
			}
		}
		if valid && (len(tag) > len(best) || len(tag) == len(best) && tag > best) {
			best = tag
		}
	}
	return best, best != "", nil
}
