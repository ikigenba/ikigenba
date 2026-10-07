// Package settings reads the process's startup limits.
package settings

import (
	"math"
	"strconv"
)

// Settings holds startup values in their declared units.
type Settings struct {
	DrainSeconds, TreeMaxBytes, OutputMaxBytes, OperationSeconds, ScriptSeconds, RunMemoryMaxBytes, RunsMemoryMaxBytes, RunsCPUPercent, RunPidsMax, RunMaxActive, RunMaxQueued, RunKeepDays, RunKeepCount int64
	ReposDir                                                                                                                                                                                              string
}

// Defaults returns the values used for unset or empty variables.
func Defaults() Settings {
	return Settings{5, 268435456, 1048576, 600, 600, 268435456, 536870912, 100, 64, 2, 10, 15, 10, "../repos/state/repos"}
}

// Error identifies the first unacceptable startup value.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	if e.Unit == "percentage" {
		return e.Name + " is '" + e.Value + "', not a positive whole percentage"
	}
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Read reads startup values through the caller's environment lookup.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	vars := []struct {
		name, unit string
		field      *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds},
		{"TREE_MAX_BYTES", "bytes", &s.TreeMaxBytes},
		{"OUTPUT_MAX_BYTES", "bytes", &s.OutputMaxBytes},
		{"OPERATION_SECONDS", "seconds", &s.OperationSeconds},
		{"SCRIPT_SECONDS", "seconds", &s.ScriptSeconds},
		{"RUN_MEMORY_MAX_BYTES", "bytes", &s.RunMemoryMaxBytes},
		{"RUNS_MEMORY_MAX_BYTES", "bytes", &s.RunsMemoryMaxBytes},
		{"RUNS_CPU_PERCENT", "percentage", &s.RunsCPUPercent},
		{"RUN_PIDS_MAX", "processes", &s.RunPidsMax},
		{"RUN_MAX_ACTIVE", "runs", &s.RunMaxActive},
		{"RUN_MAX_QUEUED", "runs", &s.RunMaxQueued},
		{"RUN_KEEP_DAYS", "days", &s.RunKeepDays},
		{"RUN_KEEP_COUNT", "runs", &s.RunKeepCount},
	}
	for _, v := range vars {
		raw, set := lookup(v.name)
		if !set || raw == "" {
			continue
		}
		valid := raw[0] >= '1' && raw[0] <= '9'
		for i := 1; i < len(raw); i++ {
			valid = valid && raw[i] >= '0' && raw[i] <= '9'
		}
		if !valid {
			return Settings{}, &Error{Name: v.name, Value: raw, Unit: v.unit}
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*v.field = n
	}
	if raw, set := lookup("REPOS_DIR"); set && raw != "" {
		s.ReposDir = raw
	}
	return s, nil
}
