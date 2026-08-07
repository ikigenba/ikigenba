package bintest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var startServices = []string{
	"dashboard", "crm", "ledger", "notify", "prompts", "wiki", "dropbox", "cron",
	"gmail", "github", "scripts", "sites", "telemetry", "webhooks", "repos",
}

func serviceManifests(t *testing.T, repo string) map[string][]byte {
	t.Helper()
	manifests := make(map[string][]byte)
	for _, svc := range startServices {
		src := filepath.Join(repo, svc, "etc", "manifest.env")
		data, err := os.ReadFile(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s manifest: %v", svc, err)
		}
		manifests[svc] = data
	}
	if len(manifests) == 0 {
		t.Fatal("found no authored service manifests")
	}
	return manifests
}

func serviceVersion(t *testing.T, repo, svc string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, svc, "VERSION"))
	if os.IsNotExist(err) {
		return "dev"
	}
	if err != nil {
		t.Fatalf("read %s VERSION: %v", svc, err)
	}
	if version := strings.TrimSpace(string(data)); version != "" {
		return version
	}
	return "dev"
}

func runStartStageOnly(t *testing.T, repo, runDir string, env ...string) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(repo, "bin", "start"), "--stage-only")
	cmd.Env = append(os.Environ(), append([]string{"START_RUN_DIR=" + runDir}, env...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bin/start --stage-only failed: %v\n%s", err, out)
	}
	return string(out)
}

// R-V6D9-GUQ5
func TestStartStageOnlyStagesVersionedManifestRootWithoutLaunching(t *testing.T) {
	repo := repoRoot(t)
	before := serviceManifests(t, repo)
	runDir := t.TempDir()

	out := runStartStageOnly(t, repo, runDir)
	if !strings.Contains(out, ">> staging runtime manifests") {
		t.Fatalf("stage-only output %q did not report staging", out)
	}

	for svc, srcData := range before {
		version := serviceVersion(t, repo, svc)
		staged := filepath.Join(runDir, "opt", svc, "etc", version, "manifest.env")
		got, err := os.ReadFile(staged)
		if err != nil {
			t.Fatalf("read staged %s manifest: %v", svc, err)
		}
		if !bytes.Equal(got, srcData) {
			t.Fatalf("staged %s manifest differs without port offset", svc)
		}
		current := filepath.Join(runDir, "opt", svc, "etc", "current")
		info, err := os.Lstat(current)
		if err != nil {
			t.Fatalf("lstat %s current symlink: %v", svc, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s current is not a symlink", svc)
		}
		target, err := os.Readlink(current)
		if err != nil {
			t.Fatalf("readlink %s current: %v", svc, err)
		}
		if target != version {
			t.Fatalf("%s current symlink = %q, want %q", svc, target, version)
		}
		after, err := os.ReadFile(filepath.Join(repo, svc, "etc", "manifest.env"))
		if err != nil {
			t.Fatalf("reread %s source manifest: %v", svc, err)
		}
		if !bytes.Equal(after, srcData) {
			t.Fatalf("%s source manifest changed during staging", svc)
		}
		if _, err := os.Stat(filepath.Join(runDir, "bin", svc)); !os.IsNotExist(err) {
			t.Fatalf("stage-only built %s binary under run bin, stat err %v", svc, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(runDir, "*.pid"))
	if err != nil {
		t.Fatalf("glob pid files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("stage-only created pid files: %v", matches)
	}
}

// R-V7L5-UMGU
func TestStartStageOnlyAppliesPortOffsetOnlyToStagedManifests(t *testing.T) {
	repo := repoRoot(t)
	before := serviceManifests(t, repo)
	runDir := t.TempDir()
	const offset = 17

	runStartStageOnly(t, repo, runDir, "APPKIT_PORT_OFFSET="+strconv.Itoa(offset))

	for svc, srcData := range before {
		version := serviceVersion(t, repo, svc)
		staged := filepath.Join(runDir, "opt", svc, "etc", version, "manifest.env")
		got, err := os.ReadFile(staged)
		if err != nil {
			t.Fatalf("read staged %s manifest: %v", svc, err)
		}
		want := offsetManifestPort(t, srcData, offset)
		if !bytes.Equal(got, want) {
			t.Fatalf("staged %s manifest with offset = %q, want %q", svc, got, want)
		}
		after, err := os.ReadFile(filepath.Join(repo, svc, "etc", "manifest.env"))
		if err != nil {
			t.Fatalf("reread %s source manifest: %v", svc, err)
		}
		if !bytes.Equal(after, srcData) {
			t.Fatalf("%s source manifest changed during offset staging", svc)
		}
	}
}

func offsetManifestPort(t *testing.T, data []byte, offset int) []byte {
	t.Helper()
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	for _, line := range lines {
		if line == "" {
			continue
		}
		body := strings.TrimSuffix(line, "\n")
		key, val, ok := strings.Cut(body, "=")
		if ok && strings.TrimSpace(key) == "PORT" {
			port, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				t.Fatalf("PORT value %q is not numeric", val)
			}
			out.WriteString(key)
			out.WriteByte('=')
			out.WriteString(strconv.Itoa(port + offset))
			if strings.HasSuffix(line, "\n") {
				out.WriteByte('\n')
			}
			continue
		}
		out.WriteString(line)
	}
	return []byte(out.String())
}
