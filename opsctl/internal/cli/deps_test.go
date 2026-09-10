package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDepsFallbacksAndInjections(t *testing.T) {
	// R-E9DW-L66P
	if got := (Deps{}).getenv("OPSCTL_TEST"); got != "" {
		t.Fatalf("nil Getenv read = %q, want empty", got)
	}

	deps := Deps{Getenv: func(key string) string { return "value for " + key }}
	if got := deps.getenv("OPSCTL_TEST"); got != "value for OPSCTL_TEST" {
		t.Fatalf("injected Getenv read = %q", got)
	}

	binDir := t.TempDir()
	executable := filepath.Join(binDir, "opsctl-deps-test")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(testBinary, executable); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	gotPath, gotErr := (Deps{}).lookPath("opsctl-deps-test")
	if gotPath != executable || gotErr != nil {
		t.Fatalf("nil LookPath read = (%q, %v), want (%q, nil)", gotPath, gotErr, executable)
	}

	deps.LookPath = func(file string) (string, error) { return "/injected/" + file, nil }
	if got, err := deps.lookPath("tool"); err != nil || got != "/injected/tool" {
		t.Fatalf("injected LookPath read = (%q, %v)", got, err)
	}

	defaultResolver := net.DefaultResolver
	t.Cleanup(func() { net.DefaultResolver = defaultResolver })
	var resolverCalled atomic.Bool
	wantErr := errors.New("default resolver used")
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			resolverCalled.Store(true)
			return nil, wantErr
		},
	}
	_, gotErr = (Deps{}).lookupHost(context.Background(), "example.test")
	if !resolverCalled.Load() || gotErr == nil || !strings.Contains(gotErr.Error(), wantErr.Error()) {
		t.Fatalf("nil LookupHost called default resolver = %t, error = %v", resolverCalled.Load(), gotErr)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	wantAddresses := []string{"192.0.2.1", "2001:db8::1"}
	deps.LookupHost = func(ctx context.Context, host string) ([]string, error) {
		if ctx != canceled || host != "example.test" {
			t.Fatalf("LookupHost called with (%v, %q)", ctx, host)
		}
		return wantAddresses, nil
	}
	gotAddresses, err := deps.lookupHost(canceled, "example.test")
	if err != nil || !reflect.DeepEqual(gotAddresses, wantAddresses) {
		t.Fatalf("injected LookupHost read = (%v, %v)", gotAddresses, err)
	}
}
