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

func TestDirectImportAllowLists(t *testing.T) {
	// R-DMCE-FVZF R-DNKA-TNQ4 R-DOS7-7FGT R-DQ03-L77I R-DR7Z-YYY7 R-DSFW-CQOW
	harness := "bytes cmp encoding/json errors io/fs path slices sort strconv strings time internal/session internal/proc"
	allowed := map[string]string{
		"cmd/agent-monitor":       "os internal/cli",
		"internal/cli":            "errors io io/fs strings internal/quote internal/session internal/harness/claude internal/harness/codex internal/harness/grok",
		"internal/quote":          "strconv strings unicode unicode/utf8",
		"internal/session":        "bytes cmp errors slices sort strconv strings time unicode/utf8 internal/quote",
		"internal/proc":           "bufio bytes errors io/fs path strconv strings syscall time",
		"internal/harness/claude": harness,
		"internal/harness/codex":  harness,
		"internal/harness/grok":   harness,
	}
	for path, permitted := range allowed {
		t.Run(path, func(t *testing.T) {
			set := make(map[string]bool)
			for _, name := range strings.Fields(permitted) {
				if strings.HasPrefix(name, "internal/") {
					name = modulePath + "/" + name
				}
				set[name] = true
			}
			for _, name := range strings.Fields(goList(t, "{{join .Imports \" \"}}", "./"+path)) {
				if !set[name] {
					t.Errorf("unexpected direct import %q", name)
				}
			}
		})
	}
}
