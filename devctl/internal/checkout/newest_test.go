package checkout

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestNewestRelease(t *testing.T) {
	// R-ULRX-7KJX R-D7OH-KNPH
	for _, tc := range []struct{ output, want string }{
		{"feature/x\nr1\nr10\nr2\nr3-rc1\nr9x\nR11\nr01\nrr5\nnon-release-tag\n", "r10"},
		{"r9\nr100000000000000000000\n", "r100000000000000000000"}, {"r0\n", "r0"}, {"r1-rc1\nfeature/x\n", ""}, {"", ""},
	} {
		calls := 0
		c := Checkout{Root: "/root", Deps: seam.Deps{Dir: "/root/sub", Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			calls++
			if cmd.Path != "git" || cmd.Dir != "/root" || !reflect.DeepEqual(cmd.Args, []string{"tag", "--list", "--no-column"}) {
				t.Fatalf("command %#v", cmd)
			}
			return seam.Result{Stdout: []byte(tc.output)}, nil
		}}}
		got, ok, err := c.NewestRelease(context.Background())
		if err != nil || got != tc.want || ok != (tc.want != "") || calls != 1 {
			t.Fatalf("NewestRelease %q %v %v", got, ok, err)
		}
	}
	for _, runner := range []bool{false, true} {
		cause := errors.New("runner")
		c := Checkout{Deps: seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			if runner {
				return seam.Result{}, cause
			}
			return seam.Result{ExitCode: 7, Stderr: []byte("exact")}, nil
		}}}
		_, _, err := c.NewestRelease(context.Background())
		var ge *GitError
		if runner {
			if !errors.Is(err, cause) || errors.As(err, &ge) || !strings.Contains(err.Error(), "git tag") {
				t.Fatal(err)
			}
		} else if !errors.As(err, &ge) || ge.ExitCode != 7 || ge.Stderr != "exact" || !reflect.DeepEqual(ge.Args, []string{"tag", "--list", "--no-column"}) {
			t.Fatal(err)
		}
	}
}
