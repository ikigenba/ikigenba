package host

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestCopy(t *testing.T) {
	// R-URVF-4F9E R-CAOT-4CF8
	local := "/w/dist/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz"
	for _, known := range []string{"", "/tmp/create.Ab12/known_hosts"} {
		for _, status := range []int{0, 1, -1} {
			calls := 0
			cause := errors.New("runner")
			h := Host{Domain: "sbx1.ikigenba.dev", KnownHosts: known, Deps: seam.Deps{Dir: "/w/sub", Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
				calls++
				want := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new"}
				if known != "" {
					want = append(want, "-o", "UserKnownHostsFile="+known)
				}
				want = append(want, local, "ec2-user@sbx1.ikigenba.dev:/tmp/tmp.Ab12Cd34Ef")
				if c.Path != "scp" || c.Dir != "/w/sub" || !reflect.DeepEqual(c.Args, want) {
					t.Fatalf("command %#v", c)
				}
				if status < 0 {
					return seam.Result{}, cause
				}
				return seam.Result{ExitCode: status, Stdout: []byte("exact"), Stderr: []byte("No space left on device\n")}, nil
			}}}
			err := h.Copy(context.Background(), "copy", local, "/tmp/tmp.Ab12Cd34Ef")
			var ce *CommandError
			if status == 0 && err != nil {
				t.Fatal(err)
			}
			if status < 0 && (!errors.Is(err, cause) || errors.As(err, &ce)) {
				t.Fatal(err)
			}
			if status == 1 && (!errors.As(err, &ce) || ce.Stdout != "exact" || ce.Status != 1 || ce.Error() != "copy: scp "+local+" ec2-user@sbx1.ikigenba.dev:/tmp/tmp.Ab12Cd34Ef: exit status 1" || ce.Detail() != "> exact\n> No space left on device") {
				t.Fatalf("error %#v", err)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
		}
	}
}
