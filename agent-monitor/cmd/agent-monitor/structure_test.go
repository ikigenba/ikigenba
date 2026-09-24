package main_test

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/ikigenba/ikigenba/agent-monitor"

func goList(t *testing.T, format string, paths ...string) string {
	t.Helper()
	args := append([]string{"list", "-f", format}, paths...)
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go: %v", err)
	}
	cmd := &exec.Cmd{Path: goPath, Args: append([]string{"go"}, args...)}
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %v: %v: %s", paths, err, out)
	}
	return string(out)
}

func TestPackageSet(t *testing.T) {
	// R-DCL7-DQ1V: the module contains exactly the declared eight packages and names.
	want := map[string]string{
		"cmd/agent-monitor": "main", "internal/cli": "cli", "internal/quote": "quote",
		"internal/session": "session", "internal/proc": "proc",
		"internal/harness/claude": "claude", "internal/harness/codex": "codex", "internal/harness/grok": "grok",
	}
	lines := strings.Split(strings.TrimSpace(goList(t, "{{.ImportPath}} {{.Name}} {{if .Module}}{{.Module.Path}}{{end}}", "./...")), "\n")
	if len(lines) != len(want) {
		t.Fatalf("package count = %d, want %d: %q", len(lines), len(want), lines)
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != modulePath || !strings.HasPrefix(fields[0], modulePath+"/") {
			t.Fatalf("unexpected package line %q", line)
		}
		path := strings.TrimPrefix(fields[0], modulePath+"/")
		if got, ok := want[path]; !ok || fields[1] != got {
			t.Errorf("package %q has name %q; expected %q, present=%t", path, fields[1], got, ok)
		}
		delete(want, path)
	}
	if len(want) != 0 {
		t.Errorf("missing packages: %v", want)
	}
}
