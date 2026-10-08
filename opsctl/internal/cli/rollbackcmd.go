package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func runRollback(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		return writeOut(stdout, rollbackUsage)
	}
	if c := requireRoot(deps, stderr); c != exitOK {
		return c
	}
	if len(args) != 0 {
		return releaseUsageError(stderr, "rollback", "rollback takes no arguments")
	}
	previous, exists, err := release.Previous(deps.Root)
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	if !exists {
		writeDiagnostic(stderr, errors.New("no previous release to roll back to"))
		return exitFail
	}
	exe, err := deps.Executable()
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	if !sameReleaseExecutable(deps.Root, exe, release.PreviousLink+"/"+release.OpsctlPath) {
		target := filepath.Join(deps.Root, release.PreviousLink, release.OpsctlPath)
		err = deps.Exec(target, []string{target, "rollback"}, os.Environ())
		if err == nil {
			err = errors.New("exec returned without replacing the process")
		}
		writeDiagnostic(stderr, err)
		return exitFail
	}
	r, err := checkedRelease(deps.Root, previous.SHA)
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	r.Label = previous.Label
	return runReleaseTransition("rollback", r, stdout, stderr, deps)
}
func servicesCount(data []byte) int {
	var entries struct {
		Services []json.RawMessage `json:"services"`
	}
	if json.Unmarshal(data, &entries) != nil {
		return 0
	}
	return len(entries.Services)
}
