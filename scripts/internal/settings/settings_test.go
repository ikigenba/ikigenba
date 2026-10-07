package settings_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/scripts/internal/settings"
)

func lookup(m map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { v, ok := m[key]; return v, ok }
}

// R-LUYG-IHWW R-LBVD-XPR0 R-LW6C-W9NL R-LYM5-NT4Z
func TestSettingsDefaultsAndFields(t *testing.T) {
	want := settings.Settings{DrainSeconds: 5, TreeMaxBytes: 268435456, OutputMaxBytes: 1048576, OperationSeconds: 600, ScriptSeconds: 600, RunMemoryMaxBytes: 268435456, RunsMemoryMaxBytes: 536870912, RunsCPUPercent: 100, RunPidsMax: 64, RunMaxActive: 2, RunMaxQueued: 10, RunKeepDays: 15, RunKeepCount: 10, ReposDir: "../repos/state/repos"}
	if settings.Defaults() != want {
		t.Fatalf("defaults = %+v", settings.Defaults())
	}
	for _, value := range []string{"", "unset"} {
		got, err := settings.Read(func(string) (string, bool) { return "", value != "unset" })
		if err != nil || got != want {
			t.Fatalf("default read = %+v, %v", got, err)
		}
	}
	got, err := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": "1", "TREE_MAX_BYTES": "2", "OUTPUT_MAX_BYTES": "3", "OPERATION_SECONDS": "4", "SCRIPT_SECONDS": "5", "RUN_KEEP_DAYS": "6", "RUN_KEEP_COUNT": "7", "REPOS_DIR": "../unresolved"}))
	want = settings.Settings{DrainSeconds: 1, TreeMaxBytes: 2, OutputMaxBytes: 3, OperationSeconds: 4, ScriptSeconds: 5, RunMemoryMaxBytes: 268435456, RunsMemoryMaxBytes: 536870912, RunsCPUPercent: 100, RunPidsMax: 64, RunMaxActive: 2, RunMaxQueued: 10, RunKeepDays: 6, RunKeepCount: 7, ReposDir: "../unresolved"}
	if err != nil || got != want {
		t.Fatalf("fields = %+v, %v", got, err)
	}
}

var numbers = []struct {
	name, unit string
	get        func(settings.Settings) int64
}{
	{"DRAIN_SECONDS", "seconds", func(s settings.Settings) int64 { return s.DrainSeconds }},
	{"TREE_MAX_BYTES", "bytes", func(s settings.Settings) int64 { return s.TreeMaxBytes }},
	{"OUTPUT_MAX_BYTES", "bytes", func(s settings.Settings) int64 { return s.OutputMaxBytes }},
	{"OPERATION_SECONDS", "seconds", func(s settings.Settings) int64 { return s.OperationSeconds }},
	{"SCRIPT_SECONDS", "seconds", func(s settings.Settings) int64 { return s.ScriptSeconds }},
	{"RUN_MEMORY_MAX_BYTES", "bytes", func(s settings.Settings) int64 { return s.RunMemoryMaxBytes }},
	{"RUNS_MEMORY_MAX_BYTES", "bytes", func(s settings.Settings) int64 { return s.RunsMemoryMaxBytes }},
	{"RUNS_CPU_PERCENT", "percentage", func(s settings.Settings) int64 { return s.RunsCPUPercent }},
	{"RUN_PIDS_MAX", "processes", func(s settings.Settings) int64 { return s.RunPidsMax }},
	{"RUN_MAX_ACTIVE", "runs", func(s settings.Settings) int64 { return s.RunMaxActive }},
	{"RUN_MAX_QUEUED", "runs", func(s settings.Settings) int64 { return s.RunMaxQueued }},
	{"RUN_KEEP_DAYS", "days", func(s settings.Settings) int64 { return s.RunKeepDays }},
	{"RUN_KEEP_COUNT", "runs", func(s settings.Settings) int64 { return s.RunKeepCount }},
}

// R-LXE9-A1EA R-LYM5-NT4Z R-LZU2-1KVO R-M11Y-FCMD
func TestNumericGrammarAndSaturation(t *testing.T) {
	bad := []string{"0", "-1", "+5", "2.5", "5s", "256M", "512M", "50%", "1e2", "1M", "10m", "15d", "1e1", "05", " 5", "5 ", "abc", "１２", "١", "1\n", "1\x00", "1'quoted"}
	for _, variable := range numbers {
		for _, raw := range bad {
			t.Run(variable.name+"/"+raw, func(t *testing.T) {
				_, err := settings.Read(lookup(map[string]string{variable.name: raw}))
				var se *settings.Error
				if !errors.As(err, &se) || se.Name != variable.name || se.Value != raw || se.Unit != variable.unit {
					t.Fatalf("error = %#v", err)
				}
				want := variable.name + " is '" + raw + "', not a positive whole number of " + variable.unit
				if variable.unit == "percentage" {
					want = variable.name + " is '" + raw + "', not a positive whole percentage"
				}
				if se.Error() != want {
					t.Fatalf("text = %q; want %q", se.Error(), want)
				}
			})
		}
		for _, tc := range []struct {
			raw  string
			want int64
		}{{"1", 1}, {"1234567890", 1234567890}, {"9223372036854775807", math.MaxInt64}, {"9223372036854775808", math.MaxInt64}, {strings.Repeat("9", 500), math.MaxInt64}} {
			got, err := settings.Read(lookup(map[string]string{variable.name: tc.raw}))
			if err != nil || variable.get(got) != tc.want {
				t.Fatalf("%s %q = %+v, %v", variable.name, tc.raw, got, err)
			}
		}
	}
}

// R-LZU2-1KVO
func TestFirstInvalidValue(t *testing.T) {
	for start, variable := range numbers {
		m := make(map[string]string)
		for _, later := range numbers[start:] {
			m[later.name] = "invalid\n'"
		}
		_, err := settings.Read(lookup(m))
		var se *settings.Error
		if !errors.As(err, &se) || se.Name != variable.name || se.Value != "invalid\n'" || se.Unit != variable.unit {
			t.Fatalf("first error = %#v", err)
		}
	}
}

// R-LFJ3-30Z3 R-LYM5-NT4Z
func TestReposDirVerbatim(t *testing.T) {
	for _, raw := range []string{"/abs/path", "missing/path", "  relative\n", "\x00", "-1", "../repos", ""} {
		got, err := settings.Read(lookup(map[string]string{"REPOS_DIR": raw}))
		want := settings.Defaults()
		if raw != "" {
			want.ReposDir = raw
		}
		if err != nil || got != want {
			t.Fatalf("repos %q = %+v, %v", raw, got, err)
		}
	}
}
