package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
	"github.com/ikigenba/ikigenba/opsctl/internal/services"
)

func runActivate(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		return writeOut(stdout, activateUsage)
	}
	if c := requireRoot(deps, stderr); c != exitOK {
		return c
	}
	message := ""
	switch {
	case len(args) == 0:
		message = "activate needs SHA"
	case len(args) > 2:
		message = "activate takes SHA and an optional LABEL"
	case !release.ValidSHA(args[0]):
		message = "activate takes a full commit sha, not '" + diagnosticArg(args[0]) + "'"
	case len(args) == 2 && !release.ValidLabel(args[1]):
		message = "label '" + diagnosticArg(args[1]) + "' may hold only letters, digits, '.', '_', '/' and '-'"
	}
	if message != "" {
		return releaseUsageError(stderr, "activate", message)
	}
	sha := args[0]
	executable, err := deps.Executable()
	if err != nil || !sameReleaseExecutable(deps.Root, executable, release.ReleasesDir+"/"+sha+"/"+release.OpsctlPath) {
		writeDiagnostic(stderr, fmt.Errorf("activate %s must be run by %s/%s/%s", sha, release.ReleasesDir, sha, release.OpsctlPath))
		return exitFail
	}
	r, err := checkedRelease(deps.Root, sha)
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	r.Label = ""
	if len(args) == 2 {
		r.Label = args[1]
	}
	return runReleaseTransition("activate", r, stdout, stderr, deps)
}

func checkedRelease(root, sha string) (release.Release, error) {
	r, err := release.Read(root, sha)
	if errors.Is(err, release.ErrNoMetadata) {
		return r, fmt.Errorf("%s/%s/release.json is missing; unpack the release again", release.ReleasesDir, sha)
	}
	if err != nil {
		return r, err
	}
	if r.SHA != sha {
		return r, fmt.Errorf("%s/%s/release.json names %s; unpack the release again", release.ReleasesDir, sha, r.SHA)
	}
	return r, nil
}
func sameReleaseExecutable(root, exe, target string) bool {
	base, err := filepath.Abs(root)
	if err != nil || !filepath.IsAbs(exe) || (base != "/" && !strings.HasPrefix(exe, base+string(filepath.Separator))) {
		return false
	}
	a, e := release.Resolve(root, exe)
	b, f := release.Resolve(root, filepath.Join(root, target))
	return e == nil && f == nil && a == b
}
func releaseUsageError(w io.Writer, command, message string) exitCode {
	_, _ = fmt.Fprintf(w, "opsctl: %s\n\nsee 'opsctl %s --help' for usage\n", message, command)
	return exitUsage
}
func releaseCount(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", n, noun)
}

type transition struct {
	ctx                context.Context
	env                host.Env
	deps               Deps
	files              *os.Root
	store              config.Store
	r                  release.Release
	folder             string
	name, apex, region string
	timing             apps.Timeouts
	trees              []string
	manifests          []apps.Manifest
	envs               map[string][]byte
	old                []apps.Service
	disabled           map[string]bool
	layout             apps.Layout
	current            release.Release
	hasCurrent         bool
	rollback           bool
	journal            []byte
	journalErr         error
}

func runReleaseTransition(command string, r release.Release, stdout, stderr io.Writer, deps Deps) exitCode {
	t := &transition{ctx: context.Background(), env: host.Env{Root: deps.Root, Execute: deps.Execute, Getenv: deps.Getenv, Now: deps.Now}, deps: deps, store: config.Store{Root: deps.Root}, r: r, folder: strings.TrimPrefix(release.ReleasesDir, "/") + "/" + r.SHA, envs: map[string][]byte{}, disabled: map[string]bool{}, rollback: command == "rollback"}
	if err := t.configuration(); err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	files, err := os.OpenRoot(deps.Root)
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	t.files = files
	defer func() { _ = files.Close() }()
	steps := []struct {
		name string
		run  func() (string, error)
	}{{"release", t.releaseStep}}
	if !t.rollback {
		steps = append(steps, struct {
			name string
			run  func() (string, error)
		}{"layout", t.layoutStep})
	}
	steps = append(steps, struct {
		name string
		run  func() (string, error)
	}{"manifests", t.manifestsStep}, struct {
		name string
		run  func() (string, error)
	}{"secrets", t.secretsStep}, struct {
		name string
		run  func() (string, error)
	}{"resources", t.resourcesStep})
	report := func(name string, fn func() (string, error)) bool {
		detail, err := fn()
		if err != nil {
			_ = writeInstallReport(stdout, name, err.Error(), false)
			writeDiagnostic(stderr, &transitionError{message: command + " failed", cause: err})
			if t.journalErr != nil {
				_, _ = fmt.Fprintf(stderr, "\ncould not read the journal: %s\n", t.journalErr)
			} else if len(t.journal) > 0 {
				_, _ = io.WriteString(stderr, "\n")
				quoteCapture(stderr, t.journal)
			}
			return false
		}
		return writeInstallReport(stdout, name, detail, true) == nil
	}
	for _, step := range steps {
		if !report(step.name, step.run) {
			return exitFail
		}
	}
	if !t.rollback && t.layout == apps.PerApp {
		if !report("snapshot", t.snapshotStep) || !report("cutover", t.cutoverStep) {
			return exitFail
		}
	}
	if !t.rollback {
		if !report("label", t.labelStep) {
			return exitFail
		}
	}
	for _, step := range []struct {
		name string
		run  func() (string, error)
	}{{"env", t.envStep}, {"units", t.unitsStep}, {"links", t.linksStep}, {"systemd", t.systemdStep}, {"nginx", t.nginxStep}, {"services", t.servicesStep}, {"litestream", t.litestreamStep}} {
		if !report(step.name, step.run) {
			return exitFail
		}
	}
	ordered := append([]apps.Manifest(nil), t.manifests...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Resources.Slice == "core" && ordered[j].Resources.Slice != "core"
	})
	for _, m := range ordered {
		if !report("service", func() (string, error) { return t.serviceStep(m) }) {
			return exitFail
		}
	}
	if !report("retention", t.retentionStep) {
		return exitFail
	}
	return exitOK
}

type transitionError struct {
	message string
	cause   error
}

func (e *transitionError) Error() string { return e.message }
func (e *transitionError) Unwrap() error { return e.cause }
func (t *transition) configuration() error {
	var err error
	for _, item := range []struct {
		key string
		out *string
	}{{"aws.region", &t.region}, {"host.name", &t.name}, {"host.apex", &t.apex}} {
		v, e := t.store.Get(item.key)
		if errors.Is(e, config.ErrNotSet) {
			v = ""
		} else if e != nil {
			return fmt.Errorf("read %s: %w", item.key, e)
		}
		if item.key == "host.name" {
			v = host.NormalizeName(v)
		}
		if v == "" && item.key != "host.apex" {
			return fmt.Errorf("%s not set", item.key)
		}
		*item.out = v
	}
	if t.apex != "" {
		if _, err = host.Apex(t.name); err != nil {
			return err
		}
	}
	t.timing, err = apps.ReadTimeouts(t.store)
	return err
}
func (t *transition) command(name string, args ...string) (host.Result, error) {
	label := name + " " + strings.Join(args, " ")
	if t.env.Execute == nil {
		return host.Result{}, &host.CommandError{Label: label, Err: errors.New("host execution dependency not set")}
	}
	result, err := t.env.Execute(t.ctx, host.Command{Name: name, Args: args})
	if err != nil || result.ExitCode != 0 {
		return result, &host.CommandError{Label: label, Result: result, Err: err}
	}
	return result, nil
}
func (t *transition) releaseStep() (string, error) {
	f, err := t.files.Open(t.folder)
	if err != nil {
		return "", err
	}
	entries, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "opsctl" {
			t.trees = append(t.trees, entry.Name())
		}
	}
	for _, app := range t.trees {
		f, err := t.files.Open(t.folder + "/" + app)
		if err != nil {
			return "", err
		}
		entries, err := f.ReadDir(-1)
		_ = f.Close()
		if err != nil {
			return "", err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			allowed := entry.Name() == "bin" || entry.Name() == "libexec" || entry.Name() == "lib" || entry.Name() == "share" || entry.Name() == "etc"
			if !allowed || !entry.IsDir() {
				return "", fmt.Errorf("%s: %s is not allowed; an app holds only bin, libexec, lib, share and etc", app, entry.Name())
			}
		}
		bin := t.folder + "/" + app + "/bin"
		info, err := t.files.Lstat(bin + "/" + app)
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s: bin/%s is missing", app, app)
		}
		f, err = t.files.Open(bin)
		if err != nil {
			return "", err
		}
		entries, err = f.ReadDir(-1)
		_ = f.Close()
		if err != nil {
			return "", err
		}
		if len(entries) != 1 {
			return "", fmt.Errorf("%s: bin holds more than %s", app, app)
		}
	}
	if _, err = t.command("chown", "--recursive", "--no-dereference", "root:root", filepath.Join(t.deps.Root, t.folder)); err != nil {
		return "", err
	}
	if err = fs.WalkDir(t.files.FS(), t.folder, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		return t.files.Chmod(p, info.Mode()&^0o022)
	}); err != nil {
		return "", err
	}
	detail := t.r.Short()
	if t.r.Label != "" {
		detail += ", " + t.r.Label
	}
	return detail, nil
}
func (t *transition) captureHost() error {
	var err error
	t.current, t.hasCurrent, err = release.Current(t.deps.Root)
	if err != nil {
		return err
	}
	t.old, err = apps.Discover(t.deps.Root)
	if err != nil {
		return err
	}
	for _, s := range t.old {
		if s.Dir != "" {
			d, e := apps.Disabled(t.ctx, t.env, s.Name)
			if e != nil {
				return e
			}
			t.disabled[s.Name] = d
		}
	}

	return err
}
func (t *transition) layoutStep() (string, error) {
	for _, p := range []string{"etc/systemd/system/ikigenba.slice", "etc/systemd/system/ikigenba-core.slice", "etc/systemd/system/ikigenba-apps.slice", "etc/letsencrypt/live/" + t.name + "/fullchain.pem"} {
		if _, err := t.files.Stat(p); errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("host is not initialised; run '%s/%s/%s init' first", release.ReleasesDir, t.r.SHA, release.OpsctlPath)
		} else if err != nil {
			return "", err
		}
	}
	var err error
	t.layout, err = apps.ReadLayout(t.deps.Root)
	if err != nil {
		return "", err
	}
	if err = t.captureHost(); err != nil {
		return "", err
	}
	n := 0
	if t.layout == apps.PerApp {
		for _, s := range t.old {
			if s.Dir == "" {
				continue
			}
			n++
			if _, e := t.files.Lstat("opt/" + s.Name + "/state"); e == nil {
				return "", fmt.Errorf("%s: /opt/%s/state has not moved; install %s first", s.Name, s.Name, s.Name)
			} else if !errors.Is(e, os.ErrNotExist) {
				return "", e
			}
			if _, e := t.files.Lstat("etc/opt/ikigenba/" + s.Name + "/env"); errors.Is(e, os.ErrNotExist) {
				return "", fmt.Errorf("%s: /opt/%s/etc/env has not moved; install %s first", s.Name, s.Name, s.Name)
			} else if e != nil {
				return "", e
			}
		}
		return "per app; cutover of " + releaseCount(n, "app"), nil
	}
	return string(t.layout), nil
}
func (t *transition) manifestsStep() (string, error) {
	if t.rollback {
		if err := t.captureHost(); err != nil {
			return "", err
		}
	}
	defaultApp := ""
	for _, app := range t.trees {
		data, err := t.files.ReadFile(t.folder + "/" + app + "/etc/manifest.toml")
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%s: no etc/manifest.toml", app)
		}
		if err != nil {
			return "", err
		}
		m, err := apps.ParseManifest(data)
		if err != nil {
			if strings.HasSuffix(err.Error(), " is not a usable app name") {
				return "", fmt.Errorf("%s: %s", app, strings.TrimPrefix(err.Error(), "invalid manifest: "))
			}
			return "", fmt.Errorf("%s: etc/manifest.toml: %w", app, err)
		}
		if m.App != app {
			return "", fmt.Errorf("%s: etc/manifest.toml: app must be '%s'", app, app)
		}
		if m.Default {
			if defaultApp != "" {
				return "", fmt.Errorf("%s: %s is already the default app", app, defaultApp)
			}
			defaultApp = app
		}
		t.manifests = append(t.manifests, m)
	}
	return releaseCount(len(t.manifests), "app"), nil
}
func (t *transition) secretsStep() (string, error) {
	n := 0
	for _, m := range t.manifests {
		secrets, err := apps.ReadSecrets(t.ctx, t.deps.Cloud, t.region, t.name, m)
		if err != nil {
			return "", err
		}
		data, err := apps.ReleaseEnv(m, secrets, t.timing, t.r)
		if err != nil {
			return "", err
		}
		t.envs[m.App] = data
		distinct := map[string]bool{}
		for _, k := range m.Secrets {
			distinct[k] = true
		}
		n += len(distinct)
	}
	return releaseCount(n, "key"), nil
}
func (t *transition) resourcesStep() (string, error) {
	warnings, err := apps.CheckReleaseResources(t.deps.Root, t.manifests)
	return releaseCount(len(t.manifests), "app") + warnings, err
}
func (t *transition) snapshotStep() (string, error) {
	results, err := backup.Snapshot(t.ctx, t.env, t.deps.Cloud, t.store, "")
	if err != nil {
		return "", err
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Service < results[j].Service })
	for _, r := range results {
		if r.Err != nil {
			return "", fmt.Errorf("%s: %w", r.Service, r.Err)
		}
	}
	return releaseCount(len(results), "service"), nil
}
func (t *transition) cutoverStep() (string, error) {
	n := 0
	for _, s := range t.old {
		if s.Dir != "" {
			if err := t.files.RemoveAll("opt/" + s.Name); err != nil {
				return "", err
			}
			n++
		}
	}
	if err := t.remove("var/lib/ikigenba/services.json"); err != nil {
		return "", err
	}
	return "removed " + releaseCount(n, "app") + " from /opt", nil
}
func (t *transition) remove(p string) error {
	err := t.files.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (t *transition) labelStep() (string, error) {
	p := t.folder + "/label"
	if t.r.Label == "" {
		return "none", t.remove(p)
	}
	if err := t.remove(p); err != nil {
		return "", err
	}
	if err := t.files.WriteFile(p, []byte(t.r.Label+"\n"), 0o644); err != nil {
		return "", err
	}
	return t.r.Label, t.files.Chmod(p, 0o644)
}
func (t *transition) envStep() (string, error) {
	for _, m := range t.manifests {
		if err := apps.WriteEnv(t.env, m.App, t.envs[m.App]); err != nil {
			return "", err
		}
	}
	return releaseCount(len(t.manifests), "app"), nil
}
func (t *transition) unitsStep() (string, error) {
	if err := apps.EnsureAccount(t.ctx, t.env); err != nil {
		return "", err
	}
	present := map[string]bool{}
	for _, m := range t.manifests {
		present[m.App] = true
		p := "var/opt/ikigenba/" + m.App
		if _, err := t.files.Lstat(p); errors.Is(err, os.ErrNotExist) {
			for _, dir := range []string{"var/opt", "var/opt/ikigenba", p} {
				if _, e := t.files.Lstat(dir); errors.Is(e, os.ErrNotExist) {
					if e = t.files.MkdirAll(dir, 0o755); e != nil {
						return "", e
					}
					mode := os.FileMode(0o755)
					if dir == p {
						mode = 0o750
					}
					if e = t.files.Chmod(dir, mode); e != nil {
						return "", e
					}
				} else if e != nil {
					return "", e
				}
			}
			if _, err = t.command("chown", "ikigenba:ikigenba", filepath.Join(t.deps.Root, p)); err != nil {
				return "", err
			}
		} else if err != nil {
			return "", err
		}
		if err := apps.WriteReleaseUnits(t.env, m.App, m, t.timing); err != nil {
			return "", err
		}
	}
	if err := apps.WriteServicesUnit(t.env); err != nil {
		return "", err
	}
	dropped := map[string]bool{}
	for _, s := range t.old {
		if s.Dir != "" && !present[s.Name] {
			dropped[s.Name] = true
		}
	}
	f, err := t.files.Open("etc/systemd/system")
	if err != nil {
		return "", err
	}
	entries, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		n := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "ikigenba-"), ".service")
		if entry.Name() != "ikigenba-"+n+".service" || present[n] || apps.ValidateName(n) != nil {
			continue
		}
		data, e := t.files.ReadFile("etc/systemd/system/" + entry.Name())
		if e != nil {
			return "", e
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "ExecStart="+filepath.Join(t.deps.Root, "opt", n)+"/") {
				dropped[n] = true
			}
		}
	}
	names := []string{}
	for n := range dropped {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, err := t.command("systemctl", "disable", "--now", "ikigenba-"+n+".socket", "ikigenba-"+n+".service"); err != nil {
			return "", fmt.Errorf("%s: %w", n, err)
		}
		for _, p := range []string{"etc/systemd/system/ikigenba-" + n + ".socket", "etc/systemd/system/ikigenba-" + n + ".service"} {
			if err := t.remove(p); err != nil {
				return "", err
			}
		}
		if err := t.files.RemoveAll("etc/opt/ikigenba/" + n); err != nil {
			return "", err
		}
	}
	detail := releaseCount(len(t.manifests), "app")
	if len(names) > 0 {
		detail += "; " + strings.Join(names, ", ") + " stopped and removed, state kept"
	}
	return detail, nil
}
func (t *transition) link(name, target string) error {
	f, err := os.CreateTemp(filepath.Join(t.deps.Root, "opt/ikigenba"), ".link-")
	if err != nil {
		return err
	}
	p := f.Name()
	_ = f.Close()
	rel, err := filepath.Rel(t.deps.Root, p)
	if err != nil {
		return err
	}
	defer func() { _ = t.remove(rel) }()
	if err = t.files.Remove(rel); err != nil {
		return err
	}
	if err = t.files.Symlink(target, rel); err != nil {
		return err
	}
	return t.files.Rename(rel, "opt/ikigenba/"+name)
}
func (t *transition) linksStep() (string, error) {
	kept := t.hasCurrent && t.current.SHA == t.r.SHA && !t.rollback
	if t.rollback {
		if err := t.link("current", "releases/"+t.r.SHA); err != nil {
			return "", err
		}
		if err := t.remove("opt/ikigenba/previous"); err != nil {
			return "", err
		}
	} else if !kept {
		if t.hasCurrent {
			if err := t.link("previous", "releases/"+t.current.SHA); err != nil {
				return "", err
			}
		} else if err := t.remove("opt/ikigenba/previous"); err != nil {
			return "", err
		}
		if err := t.link("current", "releases/"+t.r.SHA); err != nil {
			return "", err
		}
	}
	if err := release.LinkOpsctl(t.deps.Root, release.CurrentOpsctl); err != nil {
		return "", err
	}
	prev, exists, err := release.Previous(t.deps.Root)
	if err != nil {
		return "", err
	}
	p := "none"
	if exists {
		p = prev.Short()
		if kept {
			p += " kept"
		}
	}
	return "current " + t.r.Short() + ", previous " + p, nil
}
func (t *transition) systemdStep() (string, error) {
	if _, err := t.command("systemctl", "daemon-reload"); err != nil {
		return "", err
	}
	for _, m := range t.manifests {
		if t.disabled[m.App] {
			continue
		}
		if _, err := t.command("systemctl", "enable", "ikigenba-"+m.App+".service"); err != nil {
			return "", err
		}
		if _, err := t.command("systemctl", "enable", "--now", "ikigenba-"+m.App+".socket"); err != nil {
			return "", err
		}
	}
	_, err := t.command("systemctl", "enable", apps.ServicesUnit)
	return "daemon-reload", err
}
func (t *transition) nginxStep() (string, error) {
	err := nginx.Apply(t.ctx, t.env, t.name, t.apex)
	return releaseCount(len(t.manifests), "app"), err
}
func (t *transition) servicesStep() (string, error) {
	_, err := services.Write(t.ctx, t.env, t.name)
	if err != nil {
		return "", err
	}
	data, err := t.files.ReadFile("run/ikigenba/services.json")
	if err != nil {
		return "", err
	}
	return releaseCount(servicesCount(data), "service"), nil
}
func (t *transition) litestreamStep() (string, error) {
	changed, err := backup.Regenerate(t.ctx, t.env, t.store)
	if err != nil {
		return "", err
	}
	if changed {
		_, err = t.command("systemctl", "restart", "litestream.service")
		return "updated", err
	}
	return "unchanged", nil
}
func (t *transition) serviceStep(m apps.Manifest) (string, error) {
	detail := m.App + " " + t.r.Short()
	if t.disabled[m.App] {
		return detail + " disabled", nil
	}
	unit := "ikigenba-" + m.App + ".service"
	r, err := t.command("systemctl", "show", "--property=ActiveState", unit)
	if err != nil {
		return "", err
	}
	action := "start"
	if activeReleaseResult(r) {
		action = "restart"
	}
	_, err = t.command("systemctl", action, unit)
	if err == nil {
		r, err = t.command("systemctl", "show", "--property=ActiveState", unit)
	}
	if err != nil || !activeReleaseResult(r) {
		journal, e := t.command("journalctl", "--unit", unit, "--no-pager", "--lines", "50")
		if e != nil {
			var ce *host.CommandError
			if errors.As(e, &ce) {
				ce.Label = "journalctl"
			}
			t.journalErr = e
		} else {
			t.journal = append(append([]byte{}, journal.Stdout...), journal.Stderr...)
		}
		return "", fmt.Errorf("%s: service failed to start", m.App)
	}
	return detail + " active", nil
}
func activeReleaseResult(r host.Result) bool {
	for _, line := range strings.Split(string(r.Stdout), "\n") {
		if line == "ActiveState=active" {
			return true
		}
	}
	return false
}
func (t *transition) retentionStep() (string, error) {
	current, _, err := release.Current(t.deps.Root)
	if err != nil {
		return "", err
	}
	previous, _, err := release.Previous(t.deps.Root)
	if err != nil {
		return "", err
	}
	f, err := t.files.Open("opt/ikigenba/releases")
	if err != nil {
		return "", err
	}
	entries, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil {
		return "", err
	}
	n := 0
	for _, entry := range entries {
		if release.ValidSHA(entry.Name()) && entry.Name() != current.SHA && entry.Name() != previous.SHA {
			if err := t.files.RemoveAll(path.Join("opt/ikigenba/releases", entry.Name())); err != nil {
				return "", err
			}
			n++
		}
	}
	if n == 0 {
		return "nothing removed", nil
	}
	return "removed " + releaseCount(n, "release"), nil
}

const activateUsage = "Usage: opsctl activate SHA [LABEL]\n\nMake the release unpacked at /opt/ikigenba/releases/SHA/ the one this host\nruns. SHA is the full 40-character commit sha. LABEL, one token of letters,\ndigits, '.', '_', '/' and '-', is written to releases/SHA/label and given to\nevery app as IKIGENBA_RELEASE; without LABEL the label file is removed.\nMust be run by the opsctl inside that release,\n/opt/ikigenba/releases/SHA/opsctl/bin/opsctl.\n\nEvery check runs before anything outside the release changes: the release\ntree, the host's layout, every app's manifest, its secrets in\n/<host.name>/<app>, and the slices' room for the release. Then every app's\nenvironment file and units are written, previous is pointed at what current\nnamed and current at the release, nginx, /run/ikigenba/services.json and\n/etc/litestream.yml are regenerated, and every app is restarted one at a\ntime, core apps first, each active before the next. A disabled app stays\ndisabled and is not started. An app the host runs that the release lacks is\nstopped and its units removed; its data under /var/opt/ikigenba/APP/ is kept.\nThe first failure stops the run. After a run that succeeds, every release\nneither current nor previous names is removed.\n\nOn a host whose apps were installed one by one, every service is snapshotted\nand the apps' units and /opt/APP/ removed before the release's are written.\nActivating the release current already names redoes everything but moving\ncurrent and previous.\n\nConfiguration keys:\n  aws.region          the region this host's parameters live in\n  host.name           the fully-qualified name this host answers at\n  host.apex           the app that answers at the parent of host.name; unset means none\n  backup.s3_uri       the prefix the cutover's snapshots are written under\n  apps.drain_seconds  how long an app may drain when stopped (default 5)\n  apps.stop_seconds   how long systemd waits for an app to stop (default 10)\n"

const rollbackUsage = "Usage: opsctl rollback\n\nMake the release previous names the one this host runs again. current is\npointed at it and previous removed, then every app's environment file and\nunits are written from it, nginx, /run/ikigenba/services.json and\n/etc/litestream.yml are regenerated, and every app is restarted as 'opsctl\nactivate' does. The release keeps the label it last had. Run by the opsctl\ninside that release; any other opsctl hands the command to\n/opt/ikigenba/previous/opsctl/bin/opsctl. After a run that succeeds the\nrelease rolled away from is removed, so there is nothing for a second\nrollback to go back to.\n\nConfiguration keys:\n  aws.region          the region this host's parameters live in\n  host.name           the fully-qualified name this host answers at\n  host.apex           the app that answers at the parent of host.name; unset means none\n  apps.drain_seconds  how long an app may drain when stopped (default 5)\n  apps.stop_seconds   how long systemd waits for an app to stop (default 10)\n"
