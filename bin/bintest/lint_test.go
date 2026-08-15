package bintest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cleanSource = `package sample

func Value() int {
	return 1
}
`

const unformattedSource = `package sample
func Add(a,b int)int{return a+b}
`

func strictOnlySource() string {
	var source strings.Builder
	source.WriteString("package sample\n\nfunc Classify(value int) int {\n\tswitch value {\n")
	for value := 0; value < 16; value++ {
		fmt.Fprintf(&source, "\tcase %d:\n\t\treturn %d\n", value, value)
	}
	source.WriteString("\tdefault:\n\t\treturn -1\n\t}\n}\n")
	return source.String()
}

type lintFixture struct {
	root    string
	modules map[string]string
}

func newLintFixture(t *testing.T, names ...string) *lintFixture {
	t.Helper()
	root := t.TempDir()
	uses := make([]string, 0, len(names))
	modules := make(map[string]string, len(names))
	for _, name := range names {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("make module %s: %v", name, err)
		}
		goMod := fmt.Sprintf("module example.com/%s\n\ngo 1.23.0\n", name)
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
			t.Fatalf("write go.mod for %s: %v", name, err)
		}
		uses = append(uses, "\t./"+name)
		modules[name] = dir
	}
	work := "go 1.23.0\n\nuse (\n" + strings.Join(uses, "\n") + "\n)\n"
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte(work), 0o644); err != nil {
		t.Fatalf("write go.work: %v", err)
	}
	return &lintFixture{root: root, modules: modules}
}

func (fixture *lintFixture) writeSource(t *testing.T, module, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(fixture.modules[module], name), []byte(source), 0o644); err != nil {
		t.Fatalf("write %s/%s: %v", module, name, err)
	}
}

func (fixture *lintFixture) setTier(t *testing.T, module, tier string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(fixture.modules[module], ".lint-tier"), []byte(tier+"\n"), 0o644); err != nil {
		t.Fatalf("write tier for %s: %v", module, err)
	}
}

func runLint(t *testing.T, fixture *lintFixture, args ...string) (string, error) {
	t.Helper()
	lint := filepath.Join(repoRoot(t), "bin", "lint")
	return runScript(t, []string{"LINT_REPO_ROOT=" + fixture.root}, lint, args...)
}

func requireContains(t *testing.T, output string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not contain %q:\n%s", want, output)
		}
	}
}

// R-WW5B-L155
func TestLintCheapTierEnforcesFormattingAndAcceptsCleanModule(t *testing.T) {
	fixture := newLintFixture(t, "cheap")
	fixture.setTier(t, "cheap", "cheap")
	fixture.writeSource(t, "cheap", "sample.go", unformattedSource)
	if output, err := runLint(t, fixture, "cheap"); err == nil {
		t.Fatalf("cheap lint accepted unformatted source:\n%s", output)
	}
	fixture.writeSource(t, "cheap", "sample.go", cleanSource)
	if output, err := runLint(t, fixture, "cheap"); err != nil {
		t.Fatalf("cheap lint rejected clean source: %v\n%s", err, output)
	}
}

// R-WXD7-YSVU
func TestLintOffTierReportsFindingsWithoutFailing(t *testing.T) {
	fixture := newLintFixture(t, "off")
	fixture.writeSource(t, "off", "sample.go", unformattedSource)
	output, err := runLint(t, fixture, "off")
	if err != nil {
		t.Fatalf("off lint failed: %v\n%s", err, output)
	}
	requireContains(t, output, "off: tier=off findings=1")
}

// R-WYL4-CKMJ
func TestLintMarkerSelectsStrictOnlyComplexityRule(t *testing.T) {
	fixture := newLintFixture(t, "tiered")
	fixture.writeSource(t, "tiered", "sample.go", strictOnlySource())
	fixture.setTier(t, "tiered", "cheap")
	if output, err := runLint(t, fixture, "tiered"); err != nil {
		t.Fatalf("cheap tier rejected strict-only finding: %v\n%s", err, output)
	}
	fixture.setTier(t, "tiered", "strict")
	if output, err := runLint(t, fixture, "tiered"); err == nil {
		t.Fatalf("strict tier accepted complexity finding:\n%s", output)
	}
}

// R-WZT0-QCD8
func TestLintStrictAnalyzersExcludeTestFiles(t *testing.T) {
	fixture := newLintFixture(t, "strict")
	fixture.setTier(t, "strict", "strict")
	fixture.writeSource(t, "strict", "sample.go", cleanSource)
	fixture.writeSource(t, "strict", "sample_test.go", strictOnlySource())
	if output, err := runLint(t, fixture, "strict"); err != nil {
		t.Fatalf("strict lint analyzed test-file complexity: %v\n%s", err, output)
	}
	fixture.writeSource(t, "strict", "sample.go", strictOnlySource())
	fixture.writeSource(t, "strict", "sample_test.go", cleanSource)
	if output, err := runLint(t, fixture, "strict"); err == nil {
		t.Fatalf("strict lint ignored non-test complexity:\n%s", output)
	}
}

// R-X10X-443X
func TestLintCheapFormatterIncludesTestFiles(t *testing.T) {
	fixture := newLintFixture(t, "cheaptests")
	fixture.setTier(t, "cheaptests", "cheap")
	fixture.writeSource(t, "cheaptests", "sample.go", cleanSource)
	fixture.writeSource(t, "cheaptests", "sample_test.go", unformattedSource)
	if output, err := runLint(t, fixture, "cheaptests"); err == nil {
		t.Fatalf("cheap lint accepted unformatted test file:\n%s", output)
	}
}

// R-X28T-HVUM
func TestLintRejectsWrongVersionBeforeAnalysis(t *testing.T) {
	fixture := newLintFixture(t, "versioned")
	fixture.setTier(t, "versioned", "cheap")
	fixture.writeSource(t, "versioned", "sample.go", cleanSource)
	stubDir := t.TempDir()
	analysisLog := filepath.Join(stubDir, "analysis-ran")
	stub := `#!/usr/bin/env bash
if [[ "${1:-}" == "version" ]]; then
	echo "golangci-lint has version 9.9.9 built with go"
	exit 0
fi
touch "$LINT_STUB_ANALYSIS_LOG"
exit 99
`
	stubPath := filepath.Join(stubDir, "golangci-lint")
	if err := os.WriteFile(stubPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("write version stub: %v", err)
	}
	lint := filepath.Join(repoRoot(t), "bin", "lint")
	env := []string{
		"LINT_REPO_ROOT=" + fixture.root,
		"LINT_STUB_ANALYSIS_LOG=" + analysisLog,
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	output, err := runScript(t, env, lint, "versioned")
	if err == nil {
		t.Fatalf("lint accepted wrong engine version:\n%s", output)
	}
	requireContains(t, output, "2.12.2", "9.9.9")
	if _, statErr := os.Stat(analysisLog); !os.IsNotExist(statErr) {
		t.Fatalf("lint ran analysis after version mismatch")
	}
}

// R-X3GP-VNLB
func TestLintRejectsInvalidMarkerAndUnknownTreeWithoutGuessing(t *testing.T) {
	fixture := newLintFixture(t, "alpha", "beta")
	fixture.writeSource(t, "alpha", "sample.go", cleanSource)
	fixture.writeSource(t, "beta", "sample.go", cleanSource)
	if err := os.WriteFile(filepath.Join(fixture.modules["alpha"], ".lint-tier"), []byte("experimental\n\n"), 0o644); err != nil {
		t.Fatalf("write invalid tier: %v", err)
	}
	output, err := runLint(t, fixture, "alpha")
	if err == nil {
		t.Fatalf("lint accepted invalid marker:\n%s", output)
	}
	requireContains(t, output, "alpha", "experimental", "cheap", "strict")
	output, err = runLint(t, fixture, "missing")
	if err == nil {
		t.Fatalf("lint accepted unknown tree:\n%s", output)
	}
	requireContains(t, output, "missing", "alpha", "beta")
}

// R-X4OM-9FC0
func TestLintScoreboardAggregatesEnforcedModulesAndIgnoresOffFailures(t *testing.T) {
	fixture := newLintFixture(t, "cheap", "strict", "off")
	fixture.setTier(t, "cheap", "cheap")
	fixture.setTier(t, "strict", "strict")
	fixture.writeSource(t, "cheap", "sample.go", cleanSource)
	fixture.writeSource(t, "strict", "sample.go", cleanSource)
	fixture.writeSource(t, "off", "sample.go", unformattedSource)
	output, err := runLint(t, fixture)
	if err != nil {
		t.Fatalf("scoreboard failed only for off findings: %v\n%s", err, output)
	}
	requireContains(t, output, "cheap: tier=cheap", "strict: tier=strict", "off: tier=off")

	fixture.writeSource(t, "cheap", "sample.go", unformattedSource)
	output, err = runLint(t, fixture)
	if err == nil {
		t.Fatalf("scoreboard accepted enforced finding:\n%s", output)
	}
	requireContains(t, output, "cheap: tier=cheap", "strict: tier=strict", "off: tier=off")
}
