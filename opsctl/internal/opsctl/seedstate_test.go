package opsctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-EK9S-F7RY
func TestSeedStateWritesCredentialWithPrivateMode(t *testing.T) {
	root := t.TempDir()
	service := "gmail"
	if err := os.Mkdir(filepath.Join(root, service), 0o755); err != nil {
		t.Fatal(err)
	}
	sys := &stubSystem{}
	o := &Opsctl{Root: root, System: sys}

	if err := o.SeedState(context.Background(), service, "refresh-token", strings.NewReader("alpha\nbeta\n"), false); err != nil {
		t.Fatalf("SeedState: %v", err)
	}
	target := filepath.Join(root, service, "state", "refresh-token")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\nbeta" {
		t.Fatalf("credential bytes = %q, want %q", got, "alpha\\nbeta")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("credential mode = %04o, want 0600", gotMode)
	}
	wantChown := "chown:" + service + ":" + service + ":" + target
	if got := sys.opSeq(); len(got) != 1 || got[0] != wantChown {
		t.Fatalf("system operations = %v, want [%s]", got, wantChown)
	}
}

// R-ELHO-SZIN
func TestSeedStateRefusesExistingCredentialUnlessForced(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "gmail", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(stateDir, "refresh-token")
	if err := os.WriteFile(target, []byte("rotated-current-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := &Opsctl{Root: root, System: &stubSystem{}}

	err := o.SeedState(context.Background(), "gmail", "refresh-token", strings.NewReader("stale-seed\n"), false)
	if err == nil {
		t.Fatal("SeedState replaced an existing credential without --force")
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "rotated-current-token" {
		t.Fatalf("credential changed after refusal: got %q", got)
	}

	if err := o.SeedState(context.Background(), "gmail", "refresh-token", strings.NewReader("replacement\n"), true); err != nil {
		t.Fatalf("SeedState(force): %v", err)
	}
	got, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "replacement" {
		t.Fatalf("forced credential bytes = %q, want replacement", got)
	}
}

func TestSeedStateRefusesUnsetRootUnknownServiceAndEmptyInput(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		o    *Opsctl
		in   string
		want string
	}{
		{name: "unset root", o: &Opsctl{System: &stubSystem{}}, in: "value", want: "IKIGENBA_ROOT is unset"},
		{name: "unknown service", o: &Opsctl{Root: t.TempDir(), System: &stubSystem{}}, in: "value", want: "unknown service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.o.SeedState(ctx, "gmail", "refresh-token", strings.NewReader(tc.in), false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
			if err != nil && (!strings.Contains(err.Error(), "gmail") || !strings.Contains(err.Error(), "refresh-token")) {
				t.Fatalf("error does not name service and credential: %v", err)
			}
		})
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "gmail"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := (&Opsctl{Root: root, System: &stubSystem{}}).SeedState(ctx, "gmail", "refresh-token", strings.NewReader("\n"), false)
	if err == nil || !strings.Contains(err.Error(), "stdin is empty") {
		t.Fatalf("empty input error = %v", err)
	}
}
