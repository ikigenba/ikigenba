package checkout

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// ResolveCommit resolves a lowercase hexadecimal object prefix or a tag name.
func (checkout *Checkout) ResolveCommit(ctx context.Context, rev string) (string, bool, error) {
	hex := isCommitHex(rev)
	object := rev
	if !hex {
		object = "refs/tags/" + rev
		if !validTagRef(object) {
			return "", false, nil
		}
	}
	result, err := checkout.Deps.Exec(ctx, seam.Cmd{
		Path: "git",
		Args: []string{"rev-parse", "--verify", "--quiet", "--end-of-options", object + "^{commit}"},
		Dir:  checkout.Root,
	})
	if err != nil {
		return "", false, fmt.Errorf("git rev-parse: %w", err)
	}
	sha := trimTrailingNewlines(string(result.Stdout))
	if result.ExitCode != 0 || (hex && !strings.HasPrefix(sha, rev)) {
		return "", false, nil
	}
	return sha, true, nil
}

func isCommitHex(rev string) bool {
	if len(rev) < 4 || len(rev) > 40 {
		return false
	}
	for i := range len(rev) {
		if (rev[i] < '0' || rev[i] > '9') && (rev[i] < 'a' || rev[i] > 'f') {
			return false
		}
	}
	return true
}

func validTagRef(ref string) bool {
	if strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") ||
		strings.Contains(ref, "..") || strings.Contains(ref, "//") || strings.Contains(ref, "@{") {
		return false
	}
	for i := range len(ref) {
		if ref[i] < 0x20 || ref[i] == 0x7f || strings.ContainsRune(" ~^:?*[\\", rune(ref[i])) {
			return false
		}
	}
	for _, component := range strings.Split(ref, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

// AddWorktree creates a detached linked worktree at dir for sha.
func (checkout *Checkout) AddWorktree(ctx context.Context, dir, sha string) error {
	_, err := checkout.git(ctx, "worktree", "add", "--detach", dir, sha)
	return err
}

// RemoveWorktree forcibly removes the linked worktree at dir.
func (checkout *Checkout) RemoveWorktree(ctx context.Context, dir string) error {
	_, err := checkout.git(ctx, "worktree", "remove", "--force", dir)
	return err
}
