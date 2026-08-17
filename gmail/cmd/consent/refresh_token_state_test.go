package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRefreshTokenToState(t *testing.T) {
	// R-S3ZK-NBWM
	root := t.TempDir()
	t.Setenv("IKIGENBA_ROOT", root)
	const want = "consented-refresh-token"

	if err := writeRefreshToken(want); err != nil {
		t.Fatalf("writeRefreshToken: %v", err)
	}
	path := filepath.Join(root, "gmail", "state", "GMAIL_REFRESH_TOKEN")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state refresh token: %v", err)
	}
	if string(got) != want {
		t.Fatalf("refresh token bytes = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state refresh token: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("refresh token mode = %o, want 600", gotMode)
	}
}
