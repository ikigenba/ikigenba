package bintest

import (
	"go/build/constraint"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func markdownSection(document, heading string) string {
	start := strings.Index(document, heading)
	if start < 0 {
		return ""
	}
	section := document[start+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section
}

// R-O1AD-MRKW
func TestAgentsDeclaresTestingContract(t *testing.T) {
	path := filepath.Join(repoRoot(t), "bin", "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed bin/AGENTS.md: %v", err)
	}
	tests := markdownSection(string(data), "## Tests")
	if tests == "" {
		t.Fatal("bin/AGENTS.md has no Tests section")
	}

	for fact, want := range map[string]string{
		"default gate":            "go test ./bin/bintest/...",
		"hermetic layer":          "hermetic layer",
		"manual layer":            "manual layer",
		"no composed layer":       "no composed layer",
		"no live layer":           "no live layer",
		"toolchain preconditions": "no environmental precondition beyond the Go toolchain",
		"workspace mode":          "`GOWORK`\nmode is `workspace`",
	} {
		if !strings.Contains(strings.ToLower(tests), strings.ToLower(want)) {
			t.Errorf("Tests section does not declare %s with %q", fact, want)
		}
	}
}

func hasLiveBuildConstraint(source []byte) bool {
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			if !constraint.IsGoBuild(trimmed) && !strings.HasPrefix(trimmed, "// +build ") {
				continue
			}
			expr, err := constraint.Parse(trimmed)
			if err == nil && buildConstraintHasTag(expr, "live") {
				return true
			}
			continue
		}
		break
	}
	return false
}

func buildConstraintHasTag(expr constraint.Expr, tag string) bool {
	switch expr := expr.(type) {
	case *constraint.TagExpr:
		return expr.Tag == tag
	case *constraint.NotExpr:
		return buildConstraintHasTag(expr.X, tag)
	case *constraint.AndExpr:
		return buildConstraintHasTag(expr.X, tag) || buildConstraintHasTag(expr.Y, tag)
	case *constraint.OrExpr:
		return buildConstraintHasTag(expr.X, tag) || buildConstraintHasTag(expr.Y, tag)
	default:
		return false
	}
}

// R-O2IA-0JBL
func TestHermeticTestsDoNotSkip(t *testing.T) {
	liveHeader := "//go:build " + "live" + "\n\npackage fixture\n"
	if !hasLiveBuildConstraint([]byte(liveHeader)) {
		t.Fatal("live build constraint was not excluded")
	}
	ordinaryHeader := "//go:build " + "linux" + "\n\npackage fixture\n"
	if hasLiveBuildConstraint([]byte(ordinaryHeader)) {
		t.Fatal("ordinary build constraint was classified as live")
	}

	paths, err := filepath.Glob(filepath.Join(repoRoot(t), "bin", "bintest", "*_test.go"))
	if err != nil {
		t.Fatalf("find bintest source files: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("found no bintest source files")
	}
	needles := []string{"t." + "Skip", "t." + "Skipf", "t." + "SkipNow"}
	checked := 0
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if hasLiveBuildConstraint(source) {
			continue
		}
		checked++
		for _, needle := range needles {
			if strings.Contains(string(source), needle) {
				t.Errorf("%s contains forbidden skip call %q", filepath.Base(path), needle)
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no non-live test files")
	}
}
