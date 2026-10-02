package cli

import (
	"fmt"
	"sort"
	"strings"
)

func (i *invocation) runStatus() int {
	t, err := i.resolve("status", "", false, false)
	if err != nil {
		return i.report(err)
	}
	defer t.unlock()
	apps := append([]registryApp(nil), t.entry.Apps...)
	sort.Slice(apps, func(a, b int) bool { return apps[a].Name < apps[b].Name })
	names := []string{"nginx"}
	units := []string{nginxUnit(t.entry.Name)}
	for _, app := range apps {
		names = append(names, app.Name)
		units = append(units, serviceUnit(t.entry.Name, app.Name))
	}
	states := make([]string, len(units))
	for n, unit := range units {
		states[n], err = i.unitState(unit, "systemctl --user")
		if err != nil {
			return i.report(err)
		}
	}
	width := len("UNIT")
	for _, name := range names {
		if len(name) > width {
			width = len(name)
		}
	}
	var report strings.Builder
	fmt.Fprintf(&report, "%-*s%s\n", width+2, "UNIT", "STATE")
	for n, name := range names {
		fmt.Fprintf(&report, "%-*s%s\n", width+2, name, states[n])
	}
	if _, err := fmt.Fprint(i.stdout, report.String()); err != nil {
		i.diagnostic("stdout: " + err.Error())
		return 1
	}
	return 0
}
