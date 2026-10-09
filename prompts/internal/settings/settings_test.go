package settings_test

import (
	"errors"
	"math"
	"testing"

	"github.com/ikigenba/ikigenba/prompts/internal/settings"
)

var names = []string{"DRAIN_SECONDS", "PROMPT_SECONDS", "OUTPUT_MAX_BYTES", "RUN_MAX_TOOL_CALLS", "RUN_MEMORY_MAX_BYTES", "RUNS_MEMORY_MAX_BYTES", "RUNS_CPU_PERCENT", "RUN_PIDS_MAX", "RUN_MAX_ACTIVE", "RUN_MAX_QUEUED", "RUN_KEEP_DAYS", "RUN_KEEP_COUNT"}
var units = []string{"seconds", "seconds", "bytes", "tool calls", "bytes", "bytes", "percentage", "processes", "runs", "runs", "days", "runs"}

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}
func fields(s settings.Settings) []int64 {
	return []int64{s.DrainSeconds, s.PromptSeconds, s.OutputMaxBytes, s.RunMaxToolCalls, s.RunMemoryMaxBytes, s.RunsMemoryMaxBytes, s.RunsCPUPercent, s.RunPidsMax, s.RunMaxActive, s.RunMaxQueued, s.RunKeepDays, s.RunKeepCount}
}

// R-5P23-ZZN5 R-5QA0-DRDU R-5RHW-RJ4J R-5WDI-AM3B R-5V5L-WUCM
func TestValues(t *testing.T) {
	want := settings.Settings{DrainSeconds: 5, PromptSeconds: 600, OutputMaxBytes: 1048576, RunMaxToolCalls: 50, RunMemoryMaxBytes: 134217728, RunsMemoryMaxBytes: 536870912, RunsCPUPercent: 100, RunPidsMax: 64, RunMaxActive: 4, RunMaxQueued: 10, RunKeepDays: 15, RunKeepCount: 10}
	if settings.Defaults() != want {
		t.Fatal(settings.Defaults())
	}
	for _, m := range []map[string]string{{}, {"TREE_MAX_BYTES": "abc", "OPERATION_SECONDS": "abc", "SCRIPT_SECONDS": "abc", "REPOS_DIR": "abc"}} {
		s, e := settings.Read(lookup(m))
		if e != nil || s != want {
			t.Fatalf("%v %v", s, e)
		}
	}
	for i, name := range names {
		for _, v := range []string{"", "17", "9223372036854775807", "9223372036854775808", "99999999999999999999999"} {
			s, e := settings.Read(lookup(map[string]string{name: v}))
			if e != nil {
				t.Fatal(e)
			}
			expected := fields(want)
			if v == "17" {
				expected[i] = 17
			} else if v != "" {
				expected[i] = math.MaxInt64
			}
			got := fields(s)
			for j := range expected {
				if got[j] != expected[j] {
					t.Fatalf("%s=%s fields %v want %v", name, v, got, expected)
				}
			}
		}
	}
}

// R-5SPT-5AV8 R-5XLE-ODU0 R-5YTB-25KP
func TestRefusalAndOrder(t *testing.T) {
	for i, name := range names {
		for _, v := range []string{"0", "00", "-1", "+5", "2.5", "5s", "128M", "1M", "10m", "15d", "50%", "1e1", "05", " 5", "5 ", "٥", "abc"} {
			_, err := settings.Read(lookup(map[string]string{name: v}))
			var e *settings.Error
			if !errors.As(err, &e) || e.Name != name || e.Value != v || e.Unit != units[i] {
				t.Fatalf("%s %s: %v", name, v, err)
			}
			want := name + " is '" + v + "', not a positive whole number of " + units[i]
			if units[i] == "percentage" {
				want = name + " is '" + v + "', not a positive whole percentage"
			}
			if e.Error() != want {
				t.Fatal(e.Error())
			}
		}
		m := map[string]string{}
		for j := i; j < len(names); j++ {
			m[names[j]] = "abc"
		}
		_, err := settings.Read(lookup(m))
		var e *settings.Error
		if !errors.As(err, &e) || e.Name != name {
			t.Fatal(err)
		}
	}
}
