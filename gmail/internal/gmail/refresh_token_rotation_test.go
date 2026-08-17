package gmail

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenRefreshPersistsRotatedRefreshToken(t *testing.T) {
	// R-EP5D-YAQQ
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "GMAIL_REFRESH_TOKEN")
	if err := os.WriteFile(path, []byte("old-refresh-token"), 0o600); err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse request body: %v", err)
		}
		if got := form.Get("refresh_token"); got != "old-refresh-token" {
			t.Errorf("refresh_token = %q, want old token", got)
		}
		return resp(http.StatusOK, `{"access_token":"access","expires_in":3600,"refresh_token":"rotated-refresh-token"}`), nil
	})}
	c := NewClient(Config{
		ClientID:         "client-id",
		ClientSecret:     "client-secret",
		RefreshToken:     "old-refresh-token",
		RefreshTokenPath: path,
	}, httpClient)

	if _, err := c.token.token(context.Background(), false); err != nil {
		t.Fatalf("refresh access token: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read refresh token: %v", err)
	}
	if string(got) != "rotated-refresh-token" {
		t.Fatalf("persisted refresh token = %q, want rotated token", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat refresh token: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("refresh token mode = %o, want 600", gotMode)
	}
	if matches, err := filepath.Glob(filepath.Join(stateDir, ".GMAIL_REFRESH_TOKEN-*")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary files after rotation = %q, err=%v", strings.Join(matches, ","), err)
	}
}
