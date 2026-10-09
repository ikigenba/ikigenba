// Package settings reads the process's immutable resource settings.
package settings

import (
	"math"
	"strconv"
)

// Settings holds the resource limits read at process start.
type Settings struct{ DrainSeconds, PromptSeconds, OutputMaxBytes, RunMaxToolCalls, RunMemoryMaxBytes, RunsMemoryMaxBytes, RunsCPUPercent, RunPidsMax, RunMaxActive, RunMaxQueued, RunKeepDays, RunKeepCount int64 }

// Error identifies the first invalid environment setting.
type Error struct{ Name, Value, Unit string }

// Error identifies the first invalid environment setting.
func (e *Error) Error() string {
	if e.Unit == "percentage" {
		return e.Name + " is '" + e.Value + "', not a positive whole percentage"
	}
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Defaults returns the resource defaults.
func Defaults() Settings {
	return Settings{5, 600, 1048576, 50, 134217728, 536870912, 100, 64, 4, 10, 15, 10}
}

// Read reads and validates settings through lookup.
func Read(lookup func(string) (string, bool)) (Settings, error) {
	s := Defaults()
	entries := []struct {
		name, unit string
		dst        *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds}, {"PROMPT_SECONDS", "seconds", &s.PromptSeconds}, {"OUTPUT_MAX_BYTES", "bytes", &s.OutputMaxBytes}, {"RUN_MAX_TOOL_CALLS", "tool calls", &s.RunMaxToolCalls}, {"RUN_MEMORY_MAX_BYTES", "bytes", &s.RunMemoryMaxBytes}, {"RUNS_MEMORY_MAX_BYTES", "bytes", &s.RunsMemoryMaxBytes}, {"RUNS_CPU_PERCENT", "percentage", &s.RunsCPUPercent}, {"RUN_PIDS_MAX", "processes", &s.RunPidsMax}, {"RUN_MAX_ACTIVE", "runs", &s.RunMaxActive}, {"RUN_MAX_QUEUED", "runs", &s.RunMaxQueued}, {"RUN_KEEP_DAYS", "days", &s.RunKeepDays}, {"RUN_KEEP_COUNT", "runs", &s.RunKeepCount},
	}
	for _, e := range entries {
		v, ok := lookup(e.name)
		if !ok || v == "" {
			continue
		}
		valid := v[0] >= '1' && v[0] <= '9'
		for i := 1; i < len(v); i++ {
			valid = valid && v[i] >= '0' && v[i] <= '9'
		}
		if !valid {
			return Settings{}, &Error{e.name, v, e.unit}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*e.dst = n
	}
	return s, nil
}
