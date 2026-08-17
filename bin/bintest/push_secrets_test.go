package bintest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-EJ1W-1G19
func TestPushSecretsDryRunExcludesRotatingAndConfigKeys(t *testing.T) {
	repo := t.TempDir()
	app := "example"
	etc := filepath.Join(repo, app, "etc")
	bin := filepath.Join(repo, "bin")
	for _, dir := range []string{etc, bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("make fixture directory %s: %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(repo, app, "VERSION"): "v1.0.0\n",
		filepath.Join(etc, "deploy.env"):    "ACCOUNT=fixture\n",
		filepath.Join(etc, "env.list"):      "secret API_TOKEN\nrotating SESSION_KEY\nconfig LOG_LEVEL debug\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture file %s: %v", path, err)
		}
	}

	script := filepath.Join(bin, "push-secrets")
	realScript := filepath.Join(repoRoot(t), "bin", "push-secrets")
	if err := os.Symlink(realScript, script); err != nil {
		t.Fatalf("link real push-secrets script: %v", err)
	}

	out, err := runScript(t, nil, script, "--dry-run", app)
	if err != nil {
		t.Fatalf("push-secrets dry-run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "   keys: API_TOKEN\n") {
		t.Fatalf("dry-run omitted secret key:\n%s", out)
	}
	for _, excluded := range []string{"SESSION_KEY", "LOG_LEVEL"} {
		if strings.Contains(out, excluded) {
			t.Fatalf("dry-run included excluded key %s:\n%s", excluded, out)
		}
	}
}
