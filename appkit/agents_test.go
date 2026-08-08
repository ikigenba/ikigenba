package appkit

import (
	"go/build/constraint"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsDeclaresAppkitTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	contents, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	testsSection, found := strings.CutPrefix(string(contents), "# appkit")
	if !found {
		t.Fatal("AGENTS.md does not begin with the appkit heading")
	}
	_, testsSection, found = strings.Cut(testsSection, "## Tests\n")
	if !found {
		t.Fatal("AGENTS.md has no Tests section")
	}
	if nextHeading := strings.Index(testsSection, "\n## "); nextHeading >= 0 {
		testsSection = testsSection[:nextHeading]
	}

	required := []string{
		"go test ./...",
		"Hermetic",
		"Composed",
		"Manual",
		"no live test layer",
		"`go` binary",
		"PATH",
		"module cache",
		"no network fetch",
		"hard test failure, never a skip",
		"workspace mode",
		"go.work",
		"GOWORK=off go build ./...",
		"replace",
	}
	for _, fact := range required {
		if !strings.Contains(testsSection, fact) {
			t.Errorf("AGENTS.md Tests section does not declare %q", fact)
		}
	}
}

func TestNonLiveTestsNeverSkip(t *testing.T) {
	// R-O2IA-0JBL
	skipCall := "t." + "Skip"
	needles := []string{skipCall + "Now", skipCall + "f", skipCall}

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasBuildTag(contents, "live") {
			return nil
		}
		for _, needle := range needles {
			if strings.Contains(string(contents), needle) {
				t.Errorf("%s contains forbidden non-live test call %q", path, needle)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan appkit test sources: %v", err)
	}
}

func hasBuildTag(contents []byte, wanted string) bool {
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			return false
		}
		if !strings.HasPrefix(trimmed, "//go:build ") && !strings.HasPrefix(trimmed, "// +build ") {
			continue
		}
		expr, err := constraint.Parse(trimmed)
		if err != nil {
			continue
		}
		if buildConstraintContains(expr, wanted) {
			return true
		}
	}
	return false
}

func buildConstraintContains(expr constraint.Expr, wanted string) bool {
	switch expr := expr.(type) {
	case *constraint.TagExpr:
		return expr.Tag == wanted
	case *constraint.NotExpr:
		return buildConstraintContains(expr.X, wanted)
	case *constraint.AndExpr:
		return buildConstraintContains(expr.X, wanted) || buildConstraintContains(expr.Y, wanted)
	case *constraint.OrExpr:
		return buildConstraintContains(expr.X, wanted) || buildConstraintContains(expr.Y, wanted)
	default:
		return false
	}
}
