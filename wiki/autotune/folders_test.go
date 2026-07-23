package autotune

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

var pinnedConfig = map[string]any{
	"runner": map[string]any{
		"provider": "openai", "model": "gpt-5.6-luna", "auth": "sub", "effort": "low",
	},
	"improver": map[string]any{
		"provider": "openai", "model": "gpt-5.6-sol", "auth": "sub", "effort": "high",
	},
}

func TestExtractAndAnalysisFoldersSatisfyTuneContract(t *testing.T) {
	for _, folder := range []string{"extract", "analysis"} {
		t.Run(folder, func(t *testing.T) {
			for _, name := range []string{"prompt.txt", "improve.md", "score", "config.json", "README.md", ".gitignore"} {
				assertNonEmptyFile(t, filepath.Join(folder, name))
			}

			info, err := os.Stat(filepath.Join(folder, "score"))
			if err != nil {
				t.Fatalf("stat score: %v", err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("%s/score is not executable: mode %v", folder, info.Mode())
			}

			var config map[string]any
			readJSON(t, filepath.Join(folder, "config.json"), &config)
			if !reflect.DeepEqual(config, pinnedConfig) {
				t.Fatalf("%s/config.json = %#v, want exact pins %#v", folder, config, pinnedConfig)
			}

			dev := validateCases(t, filepath.Join(folder, "cases", "dev"))
			holdout := validateCases(t, filepath.Join(folder, "cases", "holdout"))
			for _, name := range dev {
				if contains(holdout, name) {
					t.Fatalf("case %q appears in both dev and holdout", name)
				}
			}
		})
	}
}

// R-AAOL-YG21
func TestExtractScoreMatchesHandComputedFixtureAndFloorsMalformedOutput(t *testing.T) {
	fixture := filepath.Join("extract", "fixtures", "perfect")
	var expected struct {
		Score          float64 `json:"score"`
		MalformedScore float64 `json:"malformed_score"`
	}
	readJSON(t, filepath.Join(fixture, "expected.json"), &expected)

	valid := runScorer(t, "extract", fixture, "output.json", "")
	if valid.Score != expected.Score {
		t.Fatalf("valid fixture score = %v, want hand-computed %v", valid.Score, expected.Score)
	}
	malformed := runScorer(t, "extract", fixture, "malformed.txt", "")
	if malformed.Score != expected.MalformedScore {
		t.Fatalf("malformed fixture score = %v, want floor %v", malformed.Score, expected.MalformedScore)
	}
}

// R-ABWI-C7SQ
func TestAnalysisScoreReproducesThreeListMetricsAndComposite(t *testing.T) {
	fixture := filepath.Join("analysis", "fixtures", "partial")
	var expected struct {
		Score      float64         `json:"score"`
		SubQueries expectedMetrics `json:"sub_queries"`
		Keywords   expectedMetrics `json:"keywords"`
		Aliases    expectedMetrics `json:"aliases"`
	}
	readJSON(t, filepath.Join(fixture, "expected.json"), &expected)

	embedPath, err := filepath.Abs(filepath.Join(fixture, "embed"))
	if err != nil {
		t.Fatalf("absolute fixture embed path: %v", err)
	}
	got := runScorer(t, "analysis", fixture, "output.json", embedPath)
	if !closeEnough(got.Score, expected.Score) {
		t.Fatalf("composite = %v, want %v", got.Score, expected.Score)
	}
	for name, want := range map[string]expectedMetrics{
		"sub_queries": expected.SubQueries,
		"keywords":    expected.Keywords,
		"aliases":     expected.Aliases,
	} {
		actual, ok := got.Details[name]
		if !ok {
			t.Fatalf("result has no %s metrics: %#v", name, got.Details)
		}
		if actual.Matched != want.Matched || actual.Missed != want.Missed || actual.Spurious != want.Spurious || !closeEnough(actual.F1, want.F1) {
			t.Fatalf("%s metrics = %+v, want %+v", name, actual, want)
		}
	}
}

type expectedMetrics struct {
	Matched  int     `json:"matched"`
	Missed   int     `json:"missed"`
	Spurious int     `json:"spurious"`
	F1       float64 `json:"f1"`
}

type scoreResult struct {
	Score    float64                    `json:"score"`
	Feedback string                     `json:"feedback"`
	Details  map[string]expectedMetrics `json:"details"`
}

func runScorer(t *testing.T, folder, fixture, candidateName, embedPath string) scoreResult {
	t.Helper()
	casePath, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatalf("absolute fixture path: %v", err)
	}
	candidate, err := os.ReadFile(filepath.Join(fixture, candidateName))
	if err != nil {
		t.Fatalf("read candidate %s: %v", candidateName, err)
	}
	command := exec.Command("./score", casePath)
	command.Dir = folder
	command.Stdin = bytes.NewReader(candidate)
	command.Env = os.Environ()
	if embedPath != "" {
		command.Env = append(command.Env, "EMBED_BIN="+embedPath)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s scorer: %v\n%s", folder, err, output)
	}
	var result scoreResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("parse %s scorer output %q: %v", folder, output, err)
	}
	if result.Feedback == "" {
		t.Fatalf("%s scorer returned empty feedback: %s", folder, output)
	}
	return result
}

func validateCases(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read cases %s: %v", root, err)
	}
	if len(entries) == 0 {
		t.Fatalf("case split %s is empty", root)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("unexpected non-directory in %s: %s", root, entry.Name())
		}
		names = append(names, entry.Name())
		assertNonEmptyFile(t, filepath.Join(root, entry.Name(), "input.txt"))
		var gold any
		readJSON(t, filepath.Join(root, entry.Name(), "gold.json"), &gold)
	}
	sort.Strings(names)
	return names
}

func assertNonEmptyFile(t *testing.T, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		t.Fatalf("%s is empty", path)
	}
}

func readJSON(t *testing.T, path string, destination any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(contents, destination); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}
