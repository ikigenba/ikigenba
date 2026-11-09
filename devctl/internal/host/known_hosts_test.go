package host

import (
	"context"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestKnownHostsForEveryTransport(t *testing.T) {
	// R-C9GW-QKOJ R-CAOT-4CF8
	for _, known := range []string{"", "/tmp/create.Ab12/known_hosts"} {
		options := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new"}
		if known != "" {
			options = append(options, "-o", "UserKnownHostsFile="+known)
		}
		var calls []seam.Cmd
		h := Host{Domain: "staging.ikigenba.dev", KnownHosts: known, Deps: seam.Deps{Dir: "/fixture", Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
			calls = append(calls, c)
			return seam.Result{}, nil
		}}}
		if _, err := h.Run(t.Context(), "probe", "a b", "x'y", "*;$(touch x)"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Sudo(t.Context(), "probe", "a b", "x'y", "*;$(touch x)"); err != nil {
			t.Fatal(err)
		}
		if err := h.Copy(t.Context(), "copy", "/fixture/archive", "/tmp/upload"); err != nil {
			t.Fatal(err)
		}
		for i, remote := range []string{"'a b' 'x'\\''y' '*;$(touch x)'", "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'a b' 'x'\\''y' '*;$(touch x)'"} {
			want := append(append([]string{}, options...), h.Target(), remote)
			if calls[i].Path != "ssh" || calls[i].Dir != "/fixture" || !reflect.DeepEqual(calls[i].Args, want) {
				t.Fatalf("%#v", calls[i])
			}
		}
		want := append(append([]string{}, options...), "/fixture/archive", h.Target()+":/tmp/upload")
		if calls[2].Path != "scp" || calls[2].Dir != "/fixture" || !reflect.DeepEqual(calls[2].Args, want) {
			t.Fatalf("%#v", calls[2])
		}
	}
}
