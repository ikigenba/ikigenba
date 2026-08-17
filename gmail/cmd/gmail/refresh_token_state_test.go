package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRefreshTokenFromState(t *testing.T) {
	// R-ENXH-KJ01
	root := t.TempDir()
	wantPath := filepath.Join(root, "gmail", "state", "GMAIL_REFRESH_TOKEN")
	if err := os.MkdirAll(filepath.Dir(wantPath), 0o700); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	const wantToken = "state-refresh-token"
	if err := os.WriteFile(wantPath, []byte(wantToken), 0o600); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}

	getenv := func(key string) string {
		if key == "IKIGENBA_ROOT" {
			return root
		}
		return "ignored-environment-token"
	}
	gotToken, gotPath, err := readRefreshToken(getenv)
	if err != nil {
		t.Fatalf("readRefreshToken: %v", err)
	}
	if gotToken != wantToken || gotPath != wantPath {
		t.Fatalf("readRefreshToken = (%q, %q), want (%q, %q)", gotToken, gotPath, wantToken, wantPath)
	}

	if err := os.Remove(wantPath); err != nil {
		t.Fatalf("remove refresh token: %v", err)
	}
	_, _, err = readRefreshToken(getenv)
	if err == nil || !strings.Contains(err.Error(), "GMAIL_REFRESH_TOKEN") {
		t.Fatalf("missing credential error = %v, want loud GMAIL_REFRESH_TOKEN error", err)
	}
}
