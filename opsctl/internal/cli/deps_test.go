package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestDepsFallbacksAndInjections(t *testing.T) {
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

func TestDepsFields(t *testing.T) {
	// R-8KGR-8VBW
	deps := Deps{
		Root:       "/root",
		EUID:       7,
		Getenv:     func(key string) string { return key },
		DNS:        dns.Env{},
		LookPath:   func(file string) (string, error) { return file, nil },
		LookupHost: func(_ context.Context, host string) ([]string, error) { return []string{host}, nil },
		Execute:    func(context.Context, host.Command) (host.Result, error) { return host.Result{ExitCode: 3}, nil },
		Now:        func() time.Time { return time.Unix(9, 0) },
		Cloud:      cloud.Env{},
		Executable: func() (string, error) { return "injected-path", nil },
		Exec:       func(string, []string, []string) error { return errors.New("exec fixture") },
	}
	executable := typed[func() (string, error)](deps.Executable)
	replacement := typed[func(string, []string, []string) error](deps.Exec)
	if path, err := executable(); path != "injected-path" || err != nil {
		t.Fatal(path, err)
	}
	if replacement("program", nil, nil) == nil {
		t.Fatal("Exec dependency not used")
	}
	root := typed[string](deps.Root)
	euid := typed[int](deps.EUID)
	getenv := typed[func(key string) string](deps.Getenv)
	dnsEnv := typed[dns.Env](deps.DNS)
	lookPath := typed[func(file string) (string, error)](deps.LookPath)
	lookupHost := typed[func(ctx context.Context, host string) ([]string, error)](deps.LookupHost)
	execute := typed[func(context.Context, host.Command) (host.Result, error)](deps.Execute)
	now := typed[func() time.Time](deps.Now)
	cloudEnv := typed[cloud.Env](deps.Cloud)
	_, _ = dnsEnv, cloudEnv
	if root != "/root" || euid != 7 || getenv("K") != "K" {
		t.Fatalf("Deps Root/EUID/Getenv = %q/%d/%q", root, euid, getenv("K"))
	}
	if got, err := lookPath("tool"); got != "tool" || err != nil {
		t.Fatalf("Deps.LookPath = (%q, %v)", got, err)
	}
	if got, err := lookupHost(context.Background(), "h"); err != nil || len(got) != 1 || got[0] != "h" {
		t.Fatalf("Deps.LookupHost = (%v, %v)", got, err)
	}
	if got, err := execute(context.Background(), host.Command{}); err != nil || got.ExitCode != 3 {
		t.Fatalf("Deps.Execute = (%v, %v)", got, err)
	}
	if !now().Equal(time.Unix(9, 0)) {
		t.Fatalf("Deps.Now = %v", now())
	}
}

func TestNormalizeDeps(t *testing.T) {
	// R-8LON-MN2L
	t.Setenv("OPSCTL_EMPTY_ENV", "live value")
	got := normalizeDeps(Deps{})
	if got.Getenv("OPSCTL_EMPTY_ENV") != "" {
		t.Fatal("nil Getenv read live environment")
	}
	for _, pair := range [][2]any{{got.LookPath, exec.LookPath}, {got.LookupHost, net.DefaultResolver.LookupHost}, {got.Now, time.Now}, {got.Executable, os.Executable}, {got.Exec, syscall.Exec}} {
		if reflect.ValueOf(pair[0]).Pointer() != reflect.ValueOf(pair[1]).Pointer() {
			t.Fatal("default dependency was not installed")
		}
	}
	before := time.Now()
	now := got.Now()
	after := time.Now()
	if now.Before(before) || now.After(after) {
		t.Fatalf("default Now = %v outside %v .. %v", now, before, after)
	}
	supplied := Deps{Root: t.TempDir(), EUID: 123, Getenv: func(string) string { return "injected" }, LookPath: func(string) (string, error) { return "injected", nil }, LookupHost: func(context.Context, string) ([]string, error) { return []string{"injected"}, nil }, Now: func() time.Time { return time.Unix(123, 0) }, Execute: func(context.Context, host.Command) (host.Result, error) { return host.Result{}, nil }, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return nil, nil }}, DNS: dns.Env{}, Executable: func() (string, error) { return "injected-path", nil }, Exec: func(string, []string, []string) error { return errors.New("injected") }}
	normalized := normalizeDeps(supplied)
	for _, name := range []string{"Getenv", "LookPath", "LookupHost", "Now", "Execute", "Executable", "Exec"} {
		if reflect.ValueOf(normalized).FieldByName(name).Pointer() != reflect.ValueOf(supplied).FieldByName(name).Pointer() {
			t.Errorf("supplied %s replaced", name)
		}
	}
	if normalized.Root != supplied.Root || normalized.EUID != 123 || normalized.Now() != time.Unix(123, 0) {
		t.Fatal("supplied values replaced")
	}
}

// typed returns v as a T; the call compiles only when v is assignable to T.
func typed[T any](v T) T { return v }
