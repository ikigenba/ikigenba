package cli

import (
	"fmt"
	"sort"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func (i *invocation) runLogs(app, lines string, follow, operandGiven bool) int {
	t, err := i.resolve("logs", "", false, false)
	if err != nil {
		return i.report(err)
	}
	defer t.unlock()
	var units []string
	if operandGiven {
		found := false
		for _, recorded := range t.entry.Apps {
			if recorded.Name == app {
				found = true
				break
			}
		}
		if !found {
			i.diagnostic(fmt.Sprintf("no app '%s' in sandbox '%s'", printedName(app), t.entry.Name))
			return 2
		}
		units = []string{socketUnit(t.entry.Name, app), serviceUnit(t.entry.Name, app)}
	} else {
		units = []string{nginxUnit(t.entry.Name)}
		apps := append([]registryApp(nil), t.entry.Apps...)
		sort.Slice(apps, func(a, b int) bool { return apps[a].Name < apps[b].Name })
		for _, recorded := range apps {
			units = append(units, socketUnit(t.entry.Name, recorded.Name), serviceUnit(t.entry.Name, recorded.Name))
		}
	}
	args := []string{"--user", "--no-pager", "--output=short", "--lines=" + lines}
	if follow {
		args = append(args, "--follow")
	}
	for _, unit := range units {
		args = append(args, "--unit="+unit)
	}
	result, err := i.deps.Stream(i.ctx, seam.Cmd{Path: "journalctl", Dir: "/", Args: args}, i.stdout)
	if follow && i.ctx.Err() != nil {
		return 0
	}
	if err != nil {
		return i.runnerError("journalctl --user", err)
	}
	if result.ExitCode != 0 {
		return i.externalFailure("journalctl --user", result, "")
	}
	return 0
}
