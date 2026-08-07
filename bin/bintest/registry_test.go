package bintest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func runScript(t *testing.T, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// R-V3XG-PB8R
func TestRegistryResolvesOnlyCurrentVersionManifests(t *testing.T) {
	root := t.TempDir()
	goodVersion := filepath.Join(root, "good", "etc", "v1")
	if err := os.MkdirAll(goodVersion, 0o755); err != nil {
		t.Fatalf("make good manifest dir: %v", err)
	}
	goodManifest := strings.Join([]string{
		"PORT=4321",
		"MOUNT=/srv/good/",
		"FEED=/feed",
		"MCP=true",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(goodVersion, "manifest.env"), []byte(goodManifest), 0o644); err != nil {
		t.Fatalf("write good manifest: %v", err)
	}
	if err := os.Symlink("v1", filepath.Join(root, "good", "etc", "current")); err != nil {
		t.Fatalf("link good current: %v", err)
	}

	badDir := filepath.Join(root, "bad", "etc")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("make sibling manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "manifest.env"), []byte(goodManifest), 0o644); err != nil {
		t.Fatalf("write sibling manifest: %v", err)
	}

	registry := filepath.Join(repoRoot(t), "bin", "registry")
	env := []string{"REGISTRY_ROOT=" + root}
	assertOutput := func(args []string, want string) {
		t.Helper()
		got, err := runScript(t, env, registry, args...)
		if err != nil {
			t.Fatalf("registry %v failed: %v\n%s", args, err, got)
		}
		if strings.TrimSpace(got) != want {
			t.Fatalf("registry %v = %q, want %q", args, strings.TrimSpace(got), want)
		}
	}
	assertOutput([]string{"port", "good"}, "4321")
	assertOutput([]string{"addr", "good"}, "127.0.0.1:4321")
	assertOutput([]string{"mount", "good"}, "/srv/good/")
	assertOutput([]string{"feed-url", "good"}, "http://127.0.0.1:4321/feed")

	list, err := runScript(t, env, registry, "list-mcp")
	if err != nil {
		t.Fatalf("registry list-mcp failed: %v\n%s", err, list)
	}
	if got := strings.Fields(list); len(got) != 1 || got[0] != "good" {
		t.Fatalf("registry list-mcp = %q, want only good", list)
	}
	for _, args := range [][]string{
		{"port", "bad"},
		{"addr", "bad"},
		{"mount", "bad"},
		{"feed-url", "bad"},
		{"resource-url", "example.test", "bad"},
	} {
		if out, err := runScript(t, env, registry, args...); err == nil {
			t.Fatalf("registry %v resolved sibling-only manifest, output %q", args, out)
		}
	}
}
