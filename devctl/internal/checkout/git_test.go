package checkout

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestHeadUsesCheckoutRootAndTrimsNewlines(t *testing.T) {
	// R-W7FM-TW6E
	checkout, commands := checkoutWithGitResult(t, seam.Result{Stdout: []byte("deadbeef\n\n")}, nil)

	head, err := checkout.Head(context.Background())
	if err != nil {
		t.Fatalf("Head() error = %v", err)
	}
	if head != "deadbeef" {
		t.Fatalf("Head() = %q, want %q", head, "deadbeef")
	}
	assertCommands(t, *commands, seam.Cmd{
		Path: "git",
		Args: []string{"rev-parse", "HEAD"},
		Dir:  "/work/checkout",
	})
}

func TestCleanReportsPorcelainState(t *testing.T) {
	// R-W8NJ-7NX3
	tests := []struct {
		name   string
		stdout string
		want   bool
	}{
		{name: "empty", want: true},
		{name: "untracked", stdout: "?? new.go\n", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkout, commands := checkoutWithGitResult(t, seam.Result{Stdout: []byte(test.stdout)}, nil)

			got, err := checkout.Clean(context.Background())
			if err != nil {
				t.Fatalf("Clean() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Clean() = %t, want %t", got, test.want)
			}
			assertCommands(t, *commands, seam.Cmd{
				Path: "git",
				Args: []string{"status", "--porcelain"},
				Dir:  "/work/checkout",
			})
		})
	}
}

func TestGitQueriesMapProcessFailures(t *testing.T) {
	// R-5BSS-Z7EJ
	tests := gitQueryTests()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkout, _ := checkoutWithGitResult(t, seam.Result{
				ExitCode: 23,
				Stderr:   []byte("fatal: repository unavailable\n"),
			}, nil)

			err := test.call(checkout)
			var gitErr *GitError
			if !errors.As(err, &gitErr) {
				t.Fatalf("error = %T %v, want *GitError", err, err)
			}
			if !reflect.DeepEqual(gitErr.Args, test.args) || gitErr.ExitCode != 23 || gitErr.Stderr != "fatal: repository unavailable\n" {
				t.Fatalf("GitError = %#v, want args %v, exit 23, and captured stderr", gitErr, test.args)
			}
		})
	}
}

func TestGitQueriesWrapRunnerFailures(t *testing.T) {
	// R-5BSS-Z7EJ
	runnerErr := errors.New("runner unavailable")
	for _, test := range gitQueryTests() {
		t.Run(test.name, func(t *testing.T) {
			checkout, _ := checkoutWithGitResult(t, seam.Result{}, runnerErr)

			err := test.call(checkout)
			if !errors.Is(err, runnerErr) {
				t.Fatalf("error = %v, want wrapped runner error", err)
			}
			var gitErr *GitError
			if errors.As(err, &gitErr) {
				t.Fatalf("error = %T %v, must not be *GitError", err, err)
			}
		})
	}
}

type gitQueryTest struct {
	name string
	args []string
	call func(*Checkout) error
}

func gitQueryTests() []gitQueryTest {
	return []gitQueryTest{
		{
			name: "head",
			args: []string{"rev-parse", "HEAD"},
			call: func(checkout *Checkout) error {
				_, err := checkout.Head(context.Background())
				return err
			},
		},
		{
			name: "clean",
			args: []string{"status", "--porcelain"},
			call: func(checkout *Checkout) error {
				_, err := checkout.Clean(context.Background())
				return err
			},
		},
	}
}

func checkoutWithGitResult(t *testing.T, result seam.Result, runnerErr error) (*Checkout, *[]seam.Cmd) {
	t.Helper()
	commands := new([]seam.Cmd)
	checkout := &Checkout{
		Root: "/work/checkout",
		Deps: seam.Deps{
			Dir: "/work/checkout/subdirectory",
			Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				*commands = append(*commands, command)
				return result, runnerErr
			},
		},
	}
	return checkout, commands
}

func assertCommands(t *testing.T, got []seam.Cmd, want seam.Cmd) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("commands = %#v, want exactly one", got)
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("command = %#v, want %#v", got[0], want)
	}
}
