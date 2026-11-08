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
	// R-ULRX-7KJX R-T2RH-10C9
	for _, tc := range []struct{ output, want string }{

		{"feature/x\nr1\nr7\nr100\nrelease-86\nrelease-87\nrelease-88-rc1\nrelease-9x\nRelease-99\nrelease-01\nrelease-0\nrelease-\nreleases-90\nxrelease-95\nv1.0.0\n", "release-87"},
		{"release-99\nrelease-100\n", "release-100"},
		{"release-9\nrelease-100000000000000000000\n", "release-100000000000000000000"},
		{"r1\nr2\nr3\nr4\nr5\nr6\nr7\n", ""},
		{"release-1-rc1\nfeature/x\n", ""}, {"release-0\n", ""}, {"", ""},
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
