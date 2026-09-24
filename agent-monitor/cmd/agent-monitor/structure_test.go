package main_test

import (
	"os"
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

func TestModuleStructure(t *testing.T) {
	// R-2I01-C8YY R-2J7X-Q0PN
	moduleFile, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	moduleDeclared := false
	for _, line := range strings.Split(string(moduleFile), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" && fields[1] == modulePath {
			moduleDeclared = true
			break
		}
	}
	if !moduleDeclared {
		t.Fatalf("go.mod does not declare module %q", modulePath)
	}

	want := map[string]string{
		"cmd/agent-monitor": "main", "internal/cli": "cli", "internal/quote": "quote",
		"internal/session": "session", "internal/tree": "tree", "internal/proc": "proc",
		"internal/harness/claude": "claude", "internal/harness/codex": "codex", "internal/harness/grok": "grok",
	}
	allowed := map[string]map[string]bool{
		"cmd/agent-monitor":       {"internal/cli": true},
		"internal/cli":            {"internal/quote": true, "internal/session": true, "internal/tree": true, "internal/harness/claude": true, "internal/harness/codex": true, "internal/harness/grok": true},
		"internal/harness/claude": {"internal/session": true, "internal/proc": true, "internal/tree": true},
		"internal/harness/codex":  {"internal/session": true, "internal/proc": true, "internal/tree": true},
		"internal/harness/grok":   {"internal/session": true, "internal/proc": true, "internal/tree": true},
		"internal/session":        {"internal/quote": true, "internal/proc": true},
		"internal/tree":           {"internal/quote": true},
		"internal/proc":           {},
		"internal/quote":          {},
	}
	lines := strings.Split(strings.TrimSpace(goList(t, "{{.ImportPath}}|{{.Name}}|{{if .Module}}{{.Module.Path}}{{end}}|{{range .Imports}}{{.}},{{end}}", "./...")), "\n")
	if len(lines) != len(want) {
		t.Fatalf("package count = %d, want %d: %q", len(lines), len(want), lines)
	}
	for _, line := range lines {
		fields := strings.SplitN(line, "|", 4)
		if len(fields) != 4 || fields[2] != modulePath || !strings.HasPrefix(fields[0], modulePath+"/") {
			t.Fatalf("unexpected package line %q", line)
		}
		path := strings.TrimPrefix(fields[0], modulePath+"/")
		if got, ok := want[path]; !ok || fields[1] != got {
			t.Errorf("package %q has name %q; expected %q, present=%t", path, fields[1], got, ok)
		}
		delete(want, path)
		for _, imp := range strings.Split(strings.TrimSuffix(fields[3], ","), ",") {
			if imp == "" {
				continue
			}
			if imp == modulePath {
				t.Errorf("package %q imports disallowed module root", path)
			} else if strings.HasPrefix(imp, modulePath+"/") {
				dep := strings.TrimPrefix(imp, modulePath+"/")
				if !allowed[path][dep] {
					t.Errorf("package %q imports disallowed package %q", path, dep)
				}
			}
		}
	}
	if len(want) != 0 {
		t.Errorf("missing packages: %v", want)
	}
}
