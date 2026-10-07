package cli

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/scripts/internal/settings"
)

func prepareCgroup(p Process, cfg settings.Settings) (string, string) {
	if p.Cgroup == "" {
		return "", "no control group was found"
	}
	fail := func(err error) (string, string) { return "", strings.ReplaceAll(err.Error(), "\n", " ") }
	b, err := os.ReadFile(filepath.Join(p.Cgroup, "cgroup.procs"))
	if err != nil {
		return fail(err)
	}
	fields := strings.FieldsFunc(string(b), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
	})
	pid := strconv.Itoa(p.Pid)
	if len(fields) != 1 || fields[0] != pid {
		return fail(fmt.Errorf("control group does not contain only process %s", pid))
	}
	main := filepath.Join(p.Cgroup, "main")
	if err = os.MkdirAll(main, 0700); err != nil {
		return fail(err)
	}
	if err = os.WriteFile(filepath.Join(main, "cgroup.procs"), []byte(pid), 0600); err != nil {
		return fail(err)
	}
	if err = os.WriteFile(filepath.Join(p.Cgroup, "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0600); err != nil {
		return fail(err)
	}
	runs := filepath.Join(p.Cgroup, "runs")
	if err = os.MkdirAll(runs, 0700); err != nil {
		return fail(err)
	}
	cpu := "max"
	if cfg.RunsCPUPercent <= math.MaxInt64/1000 {
		cpu = strconv.FormatInt(cfg.RunsCPUPercent*1000, 10)
	}
	for _, file := range []struct{ name, value string }{
		{"memory.max", strconv.FormatInt(cfg.RunsMemoryMaxBytes, 10)},
		{"cpu.max", cpu + " 100000"},
		{"cgroup.subtree_control", "+memory +pids"},
	} {
		if err = os.WriteFile(filepath.Join(runs, file.name), []byte(file.value), 0600); err != nil {
			return fail(err)
		}
	}
	return runs, ""
}
