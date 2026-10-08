package checkout

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestWorktreePublicMethods(t *testing.T) {
	// R-F0RI-I4WR
	resolve := checkoutMethod[func(*Checkout, context.Context, string) (string, bool, error)]((*Checkout).ResolveCommit)
	add := checkoutMethod[func(*Checkout, context.Context, string, string) error]((*Checkout).AddWorktree)
	remove := checkoutMethod[func(*Checkout, context.Context, string) error]((*Checkout).RemoveWorktree)
	opened, commands := checkoutWithGitResult(t, seam.Result{Stdout: []byte("sha\n")}, nil)
	if sha, ok, err := resolve(opened, context.Background(), "release"); sha != "sha" || !ok || err != nil {
		t.Fatalf("ResolveCommit = %q, %v, %v", sha, ok, err)
	}
	if err := add(opened, context.Background(), "/tmp/fake-tree", "sha"); err != nil {
		t.Fatal(err)
	}
	if err := remove(opened, context.Background(), "/tmp/fake-tree"); err != nil {
		t.Fatal(err)
	}
	if len(*commands) != 3 {
		t.Fatalf("commands = %#v", *commands)
	}
}

func checkoutMethod[T any](method T) T { return method }

func TestResolveCommitHexAndTagForms(t *testing.T) {
	// R-FKQD-AA3N
	t.Parallel()
	const sha = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"
	tests := []struct{ rev, object string }{
		{"4b22", "4b22"}, {"4b22285", "4b22285"}, {sha, sha},
		{"r1", "refs/tags/r1"}, {"devctl/v1.2.0", "refs/tags/devctl/v1.2.0"},
		{"main", "refs/tags/main"}, {"HEAD", "refs/tags/HEAD"},
		{"4B22285", "refs/tags/4B22285"}, {"abc", "refs/tags/abc"}, {sha + "0", "refs/tags/" + sha + "0"},
		{"1234", "1234"}, {"a-b/c.d", "refs/tags/a-b/c.d"},
	}
	for _, test := range tests {
		t.Run(test.rev, func(t *testing.T) {
			output := sha
			if test.rev == "1234" {
				output = "1234" + strings.Repeat("0", 36)
			}
			opened, commands := checkoutWithGitResult(t, seam.Result{Stdout: []byte(output + "\r\n\n")}, nil)
			ctx := context.WithValue(context.Background(), testContextKey{}, "resolve")
			originalExec := opened.Deps.Exec
			opened.Deps.Exec = func(got context.Context, cmd seam.Cmd) (seam.Result, error) {
				if got != ctx {
					t.Fatal("context not forwarded")
				}
				return originalExec(got, cmd)
			}
			got, ok, err := opened.ResolveCommit(ctx, test.rev)
			if got != output || !ok || err != nil {
				t.Fatalf("ResolveCommit = %q, %v, %v", got, ok, err)
			}
			assertCommands(t, *commands, seam.Cmd{Path: "git", Args: []string{"rev-parse", "--verify", "--quiet", "--end-of-options", test.object + "^{commit}"}, Dir: opened.Root})
		})
	}
}

type testContextKey struct{}

func TestResolveCommitRejectsInvalidRefsWithoutExec(t *testing.T) {
	// R-FKQD-AA3N
	t.Parallel()
	invalid := []string{"", "r1~1", "r1^", "r1^2", "r1@{0}", "HEAD~1", "r1:x", "a..b", "r1.lock", ".r1", "r1.", "r1/", "/r1", "r1//a", "r1/.a", "r1/a.lock", "r 1", "r?1", "r*1", "r[1", "r\\1", "r\x00", "r\x1f", "r\x7f"}
	for _, rev := range invalid {
		t.Run(rev, func(t *testing.T) {
			opened, commands := checkoutWithGitResult(t, seam.Result{Stdout: []byte("must not resolve")}, nil)
			sha, ok, err := opened.ResolveCommit(context.Background(), rev)
			if sha != "" || ok || err != nil || len(*commands) != 0 {
				t.Fatalf("ResolveCommit(%q) = %q, %v, %v; commands %#v", rev, sha, ok, err, *commands)
			}
		})
	}
}

func TestResolveCommitNegativeAnswersAndRunnerErrors(t *testing.T) {
	// R-FKQD-AA3N
	t.Parallel()
	for _, test := range []struct {
		name, rev string
		result    seam.Result
	}{
		{"different prefix", "4b22285", seam.Result{Stdout: []byte("9e1c7a3b5d2f4e6a8c0b1d3f5a7c9e2b4d6f8a0c\n")}},
		{"empty hex output", "4b22", seam.Result{}},
		{"hex exit", "4b22", seam.Result{ExitCode: 1, Stdout: []byte("4b22285\n"), Stderr: []byte("ambiguous")}},
		{"tag exit", "release", seam.Result{ExitCode: 128, Stdout: []byte("sha\n"), Stderr: []byte("not found")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			opened, commands := checkoutWithGitResult(t, test.result, nil)
			sha, ok, err := opened.ResolveCommit(context.Background(), test.rev)
			if sha != "" || ok || err != nil || len(*commands) != 1 {
				t.Fatalf("ResolveCommit = %q, %v, %v; commands %#v", sha, ok, err, *commands)
			}
		})
	}
	for _, rev := range []string{"4b22", "release"} {
		runnerErr := errors.New("cannot execute")
		opened, commands := checkoutWithGitResult(t, seam.Result{}, runnerErr)
		sha, ok, err := opened.ResolveCommit(context.Background(), rev)
		var gitErr *GitError
		if sha != "" || ok || err == nil || !strings.Contains(err.Error(), "git rev-parse") || errors.As(err, &gitErr) || len(*commands) != 1 {
			t.Fatalf("ResolveCommit(%q) = %q, %v, %v; commands %#v", rev, sha, ok, err, *commands)
		}
	}
}

func TestWorktreeCommandsAndFailures(t *testing.T) {
	// R-F4F7-NG4U R-F5N4-17VJ
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		call func(*Checkout, context.Context) error
	}{
		{"add", []string{"worktree", "add", "--detach", "/tmp/fake tree", "sha"}, func(c *Checkout, ctx context.Context) error { return c.AddWorktree(ctx, "/tmp/fake tree", "sha") }},
		{"remove", []string{"worktree", "remove", "--force", "/tmp/fake tree"}, func(c *Checkout, ctx context.Context) error { return c.RemoveWorktree(ctx, "/tmp/fake tree") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), testContextKey{}, "worktree")
			opened, commands := checkoutWithGitResult(t, seam.Result{}, nil)
			originalExec := opened.Deps.Exec
			opened.Deps.Exec = func(got context.Context, cmd seam.Cmd) (seam.Result, error) {
				if got != ctx {
					t.Fatal("context not forwarded")
				}
				return originalExec(got, cmd)
			}
			if err := test.call(opened, ctx); err != nil {
				t.Fatal(err)
			}
			assertCommands(t, *commands, seam.Cmd{Path: "git", Args: test.args, Dir: opened.Root})
			opened, commands = checkoutWithGitResult(t, seam.Result{ExitCode: 23, Stderr: []byte("fatal: worktree failure\n")}, nil)
			err := test.call(opened, ctx)
			var gitErr *GitError
			if !errors.As(err, &gitErr) || !reflect.DeepEqual(gitErr.Args, test.args) || gitErr.ExitCode != 23 || gitErr.Stderr != "fatal: worktree failure\n" {
				t.Fatalf("error = %#v", err)
			}
			assertCommands(t, *commands, seam.Cmd{Path: "git", Args: test.args, Dir: opened.Root})
			runnerErr := errors.New("cannot execute")
			opened, commands = checkoutWithGitResult(t, seam.Result{}, runnerErr)
			err = test.call(opened, ctx)
			if !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "git worktree") || errors.As(err, &gitErr) {
				t.Fatalf("runner error = %v", err)
			}
			assertCommands(t, *commands, seam.Cmd{Path: "git", Args: test.args, Dir: opened.Root})
		})
	}
}
