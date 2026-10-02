package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func teardownUnits(e registryEntry) []string {
	apps := append([]registryApp(nil), e.Apps...)
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	units := []string{nginxUnit(e.Name)}
	for _, app := range apps {
		units = append(units, socketUnit(e.Name, app.Name), serviceUnit(e.Name, app.Name))
	}
	return units
}

func (r *invocation) teardownRun(args []string, action string) int {
	result, err := r.deps.Exec(r.ctx, seam.Cmd{Path: "systemctl", Args: append([]string{"--user"}, args...), Dir: "/"})
	if err != nil {
		return r.runnerError(action, err)
	}
	if result.ExitCode != 0 {
		return r.externalFailure(action, result, "")
	}
	return 0
}

func (r *invocation) teardown(t target) int {
	units := teardownUnits(t.entry)
	states := make([]string, len(units))
	for i, unit := range units {
		state, err := r.unitState(unit, "show "+unit)
		if err != nil {
			return r.report(err)
		}
		states[i] = state
	}
	for i, unit := range units {
		info, err := os.Stat(filepath.Join(t.paths.units, unit))
		regular := err == nil && info.Mode().IsRegular()
		if regular || states[i] != "inactive" {
			if code := r.teardownRun([]string{"stop", unit}, "stop "+unit); code != 0 {
				return code
			}
		}
	}
	for _, unit := range units {
		p := filepath.Join(t.paths.units, unit)
		if err := os.RemoveAll(p); err != nil {
			return r.fileError(p, err)
		}
	}
	if code := r.teardownRun([]string{"daemon-reload"}, "systemctl --user daemon-reload"); code != 0 {
		return code
	}
	for _, unit := range units {
		state, err := r.unitState(unit, "show "+unit)
		if err != nil {
			return r.report(err)
		}
		if state == "failed" {
			if code := r.teardownRun([]string{"reset-failed", unit}, "reset-failed "+unit); code != 0 {
				return code
			}
		}
	}
	for _, entry := range []string{"bin", "stage", "env", "services.json", "nginx"} {
		p := filepath.Join(t.paths.data(t.entry.Name), entry)
		if err := os.RemoveAll(p); err != nil {
			return r.fileError(p, err)
		}
	}
	return 0
}

func (r *invocation) runDown(name string, explicit bool) int {
	if explicit && name == "" {
		return r.report(unknownSandbox(name, true))
	}
	t, err := r.resolve("down", name, false, true)
	if err != nil {
		return r.report(err)
	}
	defer t.unlock()
	return r.teardown(t)
}

func (r *invocation) runWipe(name string, explicit bool) int {
	if explicit && name == "" {
		return r.report(unknownSandbox(name, true))
	}
	t, err := r.resolve("wipe", name, false, true)
	if err != nil {
		return r.report(err)
	}
	defer t.unlock()
	up, err := r.sandboxUp(t.entry)
	if err != nil {
		return r.report(err)
	}
	if up {
		_, _ = fmt.Fprintf(r.stderr, "sandbox: sandbox '%s' is up\n\nrun 'sandbox down %s' first\n", t.entry.Name, t.entry.Name)
		return 2
	}
	if code := r.teardown(t); code != 0 {
		return code
	}
	data := t.paths.data(t.entry.Name)
	if err := os.RemoveAll(data); err != nil {
		return r.fileError(data, err)
	}
	err = withRegistry(t.paths, func(reg *registry) error {
		kept := make([]registryEntry, 0, len(reg.Sandboxes))
		for _, entry := range reg.Sandboxes {
			if entry.Name != t.entry.Name {
				kept = append(kept, entry)
			}
		}
		reg.Sandboxes = kept
		return nil
	})
	if err != nil {
		return r.report(err)
	}
	return 0
}
