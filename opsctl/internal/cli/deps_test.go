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
	// R-5CW6-TG0X
	typ := reflect.TypeFor[Deps]()
	want := map[string]reflect.Type{
		"Root": reflect.TypeFor[string](), "EUID": reflect.TypeFor[int](),
		"Getenv": reflect.TypeFor[func(string) string](), "DNS": reflect.TypeFor[dns.Env](),
		"LookPath":   reflect.TypeFor[func(string) (string, error)](),
		"LookupHost": reflect.TypeFor[func(context.Context, string) ([]string, error)](),
		"Execute":    reflect.TypeFor[func(context.Context, host.Command) (host.Result, error)](),
		"Now":        reflect.TypeFor[func() time.Time](), "Cloud": reflect.TypeFor[cloud.Env](),
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Deps has %d fields, want %d", typ.NumField(), len(want))
	}
	for name, signature := range want {
		field, ok := typ.FieldByName(name)
		if !ok || !field.IsExported() || field.Type != signature {
			t.Errorf("Deps.%s = %v, want exported %v", name, field.Type, signature)
		}
	}
}

func TestNormalizeDeps(t *testing.T) {
	// R-5E43-77RM
	t.Setenv("OPSCTL_EMPTY_ENV", "live value")
	got := normalizeDeps(Deps{})
	if got.Getenv("OPSCTL_EMPTY_ENV") != "" {
		t.Fatal("nil Getenv read live environment")
	}
	for _, pair := range [][2]any{{got.LookPath, exec.LookPath}, {got.LookupHost, net.DefaultResolver.LookupHost}, {got.Now, time.Now}} {
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
	supplied := Deps{Root: t.TempDir(), EUID: 123, Getenv: func(string) string { return "injected" }, LookPath: func(string) (string, error) { return "injected", nil }, LookupHost: func(context.Context, string) ([]string, error) { return []string{"injected"}, nil }, Now: func() time.Time { return time.Unix(123, 0) }, Execute: func(context.Context, host.Command) (host.Result, error) { return host.Result{}, nil }, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return nil, nil }}, DNS: dns.Env{}}
	normalized := normalizeDeps(supplied)
	for _, name := range []string{"Getenv", "LookPath", "LookupHost", "Now", "Execute"} {
		if reflect.ValueOf(normalized).FieldByName(name).Pointer() != reflect.ValueOf(supplied).FieldByName(name).Pointer() {
			t.Errorf("supplied %s replaced", name)
		}
	}
	if normalized.Root != supplied.Root || normalized.EUID != 123 || normalized.Now() != time.Unix(123, 0) {
		t.Fatal("supplied values replaced")
	}
}
