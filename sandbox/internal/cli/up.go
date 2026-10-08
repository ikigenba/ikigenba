package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

func (i invocation) runUp() (code int) {
	t, err := i.resolve("up", "", true, true)
	if err != nil {
		return i.report(err)
	}
	defer t.unlock()
	apps, err := discoverApps(t.worktree)
	if err != nil {
		return i.diagnostic(err.Error())
	}
	if err = checkSecrets(i.deps.Getenv, apps); err != nil {
		return i.diagnostic(err.Error())
	}
	recorded := make([]registryApp, 0, len(apps))
	for _, app := range apps {
		recorded = append(recorded, registryApp{Name: app.Name, Default: app.Default})
	}
	err = upAllocate(t.paths, func(reg *registry) error {
		var allocateErr error
		t.entry, allocateErr = allocateEntry(reg, t.entry.Name, t.worktree, recorded)
		return allocateErr
	})
	if err != nil {
		return i.report(err)
	}
	commit, code := i.upCommit(t.worktree)
	if code != 0 {
		return code
	}
	p, entry := t.paths, t.entry
	stage := p.stage(entry.Name)
	_, dataErr := os.Stat(p.data(entry.Name))
	dataNew := os.IsNotExist(dataErr)
	defer func() {
		if cleanupErr := os.RemoveAll(stage); cleanupErr != nil && code == 0 {
			code = i.fileError(stage, cleanupErr)
		}
		if dataNew {
			_ = os.Remove(p.data(entry.Name))
		}
	}()
	if err = os.RemoveAll(stage); err != nil {
		return i.fileError(stage, err)
	}
	if err = os.MkdirAll(filepath.Join(stage, "bin"), 0700); err != nil {
		return i.fileError(filepath.Join(stage, "bin"), err)
	}
	for _, app := range apps {
		if code = i.upExec(seam.Cmd{Path: "go", Args: []string{"build", "-o", p.stageBinary(entry.Name, app.Name), "./cmd/" + app.Name}, Dir: filepath.Join(t.worktree, app.Name)}, "build "+app.Name, ""); code != 0 {
			return code
		}
	}
	files := map[string][]byte{}
	for _, app := range apps {
		files[filepath.Join("env", app.Name+".env")] = renderAppEnv(p.data(entry.Name), entry.Name, commit, entry.Port, app)
		files[filepath.Join("units", socketUnit(entry.Name, app.Name))] = renderAppSocket(entry, app.Name, i.deps.EUID)
		files[filepath.Join("units", serviceUnit(entry.Name, app.Name))] = renderAppService(p, entry, app)
	}
	files["services.json"] = renderServices(entry.Name, entry.Port, i.deps.EUID, apps)
	files[filepath.Join("nginx", "nginx.conf")] = renderNginxConfig(p.data(entry.Name), t.worktree, entry.Name, entry.Port, i.deps.EUID, apps)
	files[filepath.Join("units", nginxUnit(entry.Name))] = renderNginxUnit(p.data(entry.Name), entry.Name)
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if code = i.upWrite(filepath.Join(stage, key), files[key], upFileMode(key)); code != 0 {
			return code
		}
	}
	_, nginxErr := os.Stat(p.nginx(entry.Name))
	nginxNew := os.IsNotExist(nginxErr)
	if err = os.MkdirAll(p.nginx(entry.Name), 0700); err != nil {
		return i.fileError(p.nginx(entry.Name), err)
	}
	code = i.upExec(upNginxTestCommand(p, entry.Name), "test nginx configuration", "")
	if code != 0 {
		if nginxNew {
			if err = os.RemoveAll(p.nginx(entry.Name)); err != nil {
				return i.fileError(p.nginx(entry.Name), err)
			}
		}
		return code
	}
	oldApps := append([]registryApp(nil), entry.Apps...)
	sort.Slice(oldApps, func(a, b int) bool { return oldApps[a].Name < oldApps[b].Name })
	for _, old := range oldApps {
		found := false
		for _, app := range apps {
			if app.Name == old.Name {
				found = true
				break
			}
		}
		if found {
			continue
		}
		for _, unit := range []string{socketUnit(entry.Name, old.Name), serviceUnit(entry.Name, old.Name)} {
			unitPath := filepath.Join(p.units, unit)
			if _, err = os.Lstat(unitPath); os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return i.fileError(unitPath, err)
			}
			if code = i.upSystemctl("stop", unit, "stop "+unit, ""); code != 0 {
				return code
			}
		}
		for _, path := range []string{filepath.Join(p.units, socketUnit(entry.Name, old.Name)), filepath.Join(p.units, serviceUnit(entry.Name, old.Name)), p.binary(entry.Name, old.Name), p.env(entry.Name, old.Name)} {
			if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
				return i.fileError(path, err)
			}
		}
	}
	err = withRegistry(p, func(reg *registry) error {
		for n := range reg.Sandboxes {
			if reg.Sandboxes[n].Name == entry.Name {
				reg.Sandboxes[n].Apps = recorded
				return nil
			}
		}
		return fmt.Errorf("sandbox '%s' disappeared", entry.Name)
	})
	if err != nil {
		return i.report(err)
	}
	for _, dir := range []string{filepath.Join(p.data(entry.Name), "bin"), filepath.Join(p.data(entry.Name), "env")} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return i.fileError(dir, err)
		}
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return i.fileError(dir, readErr)
		}
		for _, item := range entries {
			keep := false
			for _, app := range apps {
				wanted := app.Name
				if filepath.Base(dir) == "env" {
					wanted += ".env"
				}
				if item.Name() == wanted {
					keep = true
					break
				}
			}
			if !keep {
				path := filepath.Join(dir, item.Name())
				if err = os.RemoveAll(path); err != nil {
					return i.fileError(path, err)
				}
			}
		}
	}
	for _, app := range apps {
		if err = os.Rename(p.stageBinary(entry.Name, app.Name), p.binary(entry.Name, app.Name)); err != nil {
			return i.fileError(p.binary(entry.Name, app.Name), err)
		}
		if err = os.MkdirAll(p.appState(entry.Name, app.Name), 0700); err != nil {
			return i.fileError(p.appState(entry.Name, app.Name), err)
		}
	}
	for _, key := range keys {
		path := filepath.Join(p.data(entry.Name), key)
		if filepath.Dir(key) == "units" {
			path = filepath.Join(p.units, filepath.Base(key))
		}
		if code = i.upWrite(path, files[key], upFileMode(key)); code != 0 {
			return code
		}
	}
	if code = i.upExec(seam.Cmd{Path: "systemctl", Args: []string{"--user", "daemon-reload"}, Dir: "/"}, "systemctl --user daemon-reload", ""); code != 0 {
		return code
	}
	up, err := i.sandboxUp(entry)
	if err != nil {
		return i.report(err)
	}
	for _, app := range apps {
		unit := socketUnit(entry.Name, app.Name)
		if code = i.upSystemctl("start", unit, "start "+unit, "run 'sandbox logs "+app.Name+"' for its journal"); code != 0 {
			return code
		}
	}
	for _, app := range apps {
		unit := serviceUnit(entry.Name, app.Name)
		if code = i.upSystemctl("restart", unit, "start "+unit, "run 'sandbox logs "+app.Name+"' for its journal"); code != 0 {
			return code
		}
	}
	verb := "start"
	if up {
		verb = "reload"
	}
	unit := nginxUnit(entry.Name)
	if code = i.upSystemctl(verb, unit, verb+" "+unit, "run 'sandbox logs' for the journal"); code != 0 {
		return code
	}
	entry.Apps = recorded
	if _, err = fmt.Fprint(i.stdout, urlListing(entry)); err != nil {
		return i.runnerError("write stdout", err)
	}
	return 0
}

func (i invocation) upCommit(worktree string) (string, int) {
	const headAction = "git rev-parse HEAD"
	head, err := i.deps.Exec(i.ctx, seam.Cmd{Path: "git", Args: []string{"rev-parse", "HEAD"}, Dir: worktree})
	if err != nil {
		return "", i.runnerError(headAction, err)
	}
	if head.ExitCode != 0 {
		return "", i.externalFailure(headAction, head, "")
	}
	commit := strings.TrimSuffix(string(head.Stdout), "\n")
	valid := len(commit) == 40 || len(commit) == 64
	for _, c := range []byte(commit) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			valid = false
			break
		}
	}
	if !valid {
		if _, err = fmt.Fprintln(i.stderr, "sandbox: git rev-parse HEAD: printed no commit name"); err != nil {
			return "", 1
		}
		if len(head.Output) > 0 {
			if _, err = fmt.Fprintln(i.stderr); err != nil {
				return "", 1
			}
			for _, line := range strings.Split(strings.TrimSuffix(string(head.Output), "\n"), "\n") {
				if _, err = fmt.Fprintf(i.stderr, "> %s\n", line); err != nil {
					return "", 1
				}
			}
		}
		return "", 1
	}
	const statusAction = "git status --porcelain"
	status, err := i.deps.Exec(i.ctx, seam.Cmd{Path: "git", Args: []string{"--no-optional-locks", "status", "--porcelain", "--untracked-files=normal"}, Dir: worktree})
	if err != nil {
		return "", i.runnerError(statusAction, err)
	}
	if status.ExitCode != 0 {
		return "", i.externalFailure(statusAction, status, "")
	}
	if len(status.Stdout) != 0 {
		commit += "-dirty"
	}
	return commit, 0
}

// Allocation checks existing entries without rewriting an unchanged registry.
func upAllocate(p paths, fn func(*registry) error) error {
	unlock, err := lockFile(p.registryLock())
	if err != nil {
		return err
	}
	defer unlock()
	reg, err := readRegistry(p)
	if err != nil {
		return err
	}
	count := len(reg.Sandboxes)
	if err = fn(&reg); err != nil {
		return err
	}
	if len(reg.Sandboxes) != count {
		return writeRegistry(p, reg)
	}
	return nil
}

func (i invocation) upWrite(path string, content []byte, mode os.FileMode) int {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return i.fileError(filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		return i.fileError(path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return i.fileError(path, err)
	}
	return 0
}

func upFileMode(path string) os.FileMode {
	if filepath.Dir(path) == "env" {
		return 0600
	}
	return 0644
}

func upNginxTestCommand(p paths, name string) seam.Cmd {
	return seam.Cmd{Path: "nginx", Args: []string{"-t", "-p", p.nginx(name), "-c", p.stageNginx(name), "-e", "/dev/null"}, Dir: "/"}
}

func (i invocation) upExec(cmd seam.Cmd, action, detail string) int {
	result, err := i.deps.Exec(i.ctx, cmd)
	if err != nil {
		return i.runnerError(action, err)
	}
	if result.ExitCode != 0 {
		return i.externalFailure(action, result, detail)
	}
	return 0
}

func (i invocation) upSystemctl(verb, unit, action, detail string) int {
	return i.upExec(seam.Cmd{Path: "systemctl", Args: []string{"--user", verb, unit}, Dir: "/"}, action, detail)
}

func renderAppSocket(entry registryEntry, app string, uid int) []byte {
	return []byte(fmt.Sprintf("[Unit]\nDescription=sandbox %s: %s socket\n\n[Socket]\nListenStream=%s\nRemoveOnStop=yes\n", entry.Name, app, unitPath(socketPath(uid, entry.Port, app))))
}

func renderAppService(p paths, entry registryEntry, app appInfo) []byte {
	unit := socketUnit(entry.Name, app.Name)
	placement := app.Placement
	if placement == "" {
		placement = "apps"
	}
	text := fmt.Sprintf("[Unit]\nDescription=sandbox %s: %s\nRequires=%s\nAfter=%s\n\n[Service]\nType=notify\nExecStart=%s\nWorkingDirectory=%s\nEnvironmentFile=%s\nTimeoutStopSec=10\nSlice=%s\n", entry.Name, app.Name, unit, unit, unitExecutable(p.binary(entry.Name, app.Name)), unitPath(p.appDir(entry.Name, app.Name)), unitPath(p.env(entry.Name, app.Name)), sandboxSliceName(entry.Name, placement))
	if app.Delegate {
		text += "Delegate=yes\n"
	}
	return []byte(text)
}

func urlListing(entry registryEntry) string {
	apps := append([]registryApp(nil), entry.Apps...)
	sort.Slice(apps, func(a, b int) bool { return apps[a].Name < apps[b].Name })
	width := 2
	for _, app := range apps {
		if len(app.Name)+2 > width {
			width = len(app.Name) + 2
		}
	}
	text := ""
	for _, app := range apps {
		text += fmt.Sprintf("%-*s%s\n", width, app.Name, appOrigin(entry, app.Name))
		if app.Default {
			text += fmt.Sprintf("%-*s%s\n", width, app.Name, defaultOrigin(entry))
		}
	}
	return text
}

func (i invocation) runURL() int {
	t, err := i.resolve("url", "", false, false)
	if err != nil {
		return i.report(err)
	}
	defer t.unlock()
	up, err := i.sandboxUp(t.entry)
	if err != nil {
		return i.report(err)
	}
	if !up {
		return i.diagnostic(fmt.Sprintf("sandbox '%s' is down\n\nrun 'sandbox up' to start it", t.entry.Name))
	}
	if _, err = fmt.Fprint(i.stdout, urlListing(t.entry)); err != nil {
		return i.runnerError("write stdout", err)
	}
	return 0
}
