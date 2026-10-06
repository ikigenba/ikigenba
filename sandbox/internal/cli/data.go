package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

type failure struct {
	code    int
	message string
}

func (f failure) Error() string { return f.message }
func (r *invocation) report(err error) int {
	var f failure
	if errors.As(err, &f) {
		_, _ = fmt.Fprintf(r.stderr, "sandbox: %s\n", f.message)
		return f.code
	}
	_, _ = fmt.Fprintf(r.stderr, "sandbox: %s\n", err)
	return 1
}
func dataFileError(p string, err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		err = le.Err
	}
	return failure{1, p + ": " + err.Error()}
}
func dataPrinted(s string, invalid bool) string {
	var b strings.Builder
	for len(s) > 0 {
		c := s[0]
		_, n := utf8.DecodeRuneInString(s)
		if c < 32 || c == 127 || (invalid && n == 1 && c >= 128) {
			fmt.Fprintf(&b, "\\x%02x", c)
			s = s[1:]
			continue
		}
		b.WriteString(s[:n])
		s = s[n:]
	}
	return b.String()
}
func hasControl(s string) bool {
	for i := range len(s) {
		if s[i] < 32 || s[i] == 127 {
			return true
		}
	}
	return false
}
func stateRejected(s string) bool {
	if hasControl(s) || !utf8.ValidString(s) || strings.ContainsAny(s, "\"'\\") {
		return true
	}
	for _, c := range s {
		if c == 0xfeff || c >= 0xfdd0 && c <= 0xfdef || c&0xffff == 0xfffe || c&0xffff == 0xffff {
			return true
		}
	}
	return false
}
func sandboxName(s string) bool { return usableAppName(s) && s != "nginx" || s == "nginx" }
func usableAppName(s string) bool {
	if len(s) < 1 || len(s) > 63 || s == "nginx" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
func validSandboxName(s string) bool { return sandboxName(s) && !strings.Contains(s, "--") }
func derivedName(s string) string {
	var b strings.Builder
	hyphen := false
	for _, c := range path.Base(s) {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(c)
			hyphen = false
		} else {
			hyphen = true
		}
	}
	return b.String()
}

type paths struct{ state, config, root, units string }

func (r *invocation) roots(needConfig bool) (paths, error) {
	var p paths
	root := func(key, suffix string) (string, error) {
		v := r.deps.Getenv(key)
		if !path.IsAbs(v) {
			h := r.deps.Getenv("HOME")
			if !path.IsAbs(h) {
				return "", failure{2, "HOME is not an absolute path"}
			}
			v = path.Join(h, suffix)
		}
		return path.Clean(v), nil
	}
	var err error
	p.state, err = root("XDG_STATE_HOME", ".local/state")
	if err != nil {
		return p, err
	}
	if needConfig {
		p.config, err = root("XDG_CONFIG_HOME", ".config")
		if err != nil {
			return p, err
		}
	}
	if stateRejected(p.state) {
		return p, failure{2, "state directory '" + dataPrinted(p.state, true) + "' holds a control character, quote, backslash or a character systemd rejects"}
	}
	if needConfig && hasControl(p.config) {
		return p, failure{2, "config directory '" + dataPrinted(p.config, true) + "' holds a control character, quote, backslash or a character systemd rejects"}
	}
	p.root = path.Join(p.state, "ikigenba/sandbox")
	if needConfig {
		p.units = path.Join(p.config, "systemd/user")
	}
	return p, nil
}
func (p paths) registryPath() string                { return path.Join(p.root, "registry.json") }
func (p paths) registryLock() string                { return path.Join(p.root, "registry.json.lock") }
func (p paths) lock(name string) string             { return path.Join(p.root, name+".lock") }
func (p paths) data(name string) string             { return path.Join(p.root, name) }
func (p paths) binary(name, app string) string      { return path.Join(p.data(name), "bin", app) }
func (p paths) stage(name string) string            { return path.Join(p.data(name), "stage") }
func (p paths) stageBinary(name, app string) string { return path.Join(p.stage(name), "bin", app) }
func (p paths) stageNginx(name string) string       { return path.Join(p.stage(name), "nginx/nginx.conf") }
func (p paths) stageEnv(name, app string) string    { return path.Join(p.stage(name), "env", app+".env") }
func (p paths) env(name, app string) string         { return path.Join(p.data(name), "env", app+".env") }
func (p paths) services(name string) string         { return path.Join(p.data(name), "services.json") }
func (p paths) nginx(name string) string            { return path.Join(p.data(name), "nginx") }
func (p paths) nginxConf(name string) string        { return path.Join(p.nginx(name), "nginx.conf") }
func (p paths) nginxPID(name string) string         { return path.Join(p.nginx(name), "nginx.pid") }
func (p paths) appDir(name, app string) string      { return path.Join(p.data(name), "apps", app) }
func (p paths) appState(name, app string) string    { return path.Join(p.appDir(name, app), "state") }
func (p paths) token(name string) string            { return path.Join(p.data(name), "token") }
func socketPath(uid, port int, app string) string {
	return fmt.Sprintf("/run/user/%d/sandbox/%d/%s.sock", uid, port, app)
}
func escapedSandboxName(name string) string { return strings.ReplaceAll(name, "-", `\x2d`) }
func sandboxSliceName(name, placement string) string {
	suffix := ""
	if placement != "" {
		suffix = "-" + placement
	}
	return "sandbox-" + escapedSandboxName(name) + suffix + ".slice"
}
func nginxUnit(name string) string        { return "sandbox-" + name + "-nginx.service" }
func serviceUnit(name, app string) string { return "sandbox-" + name + "-" + app + ".service" }
func socketUnit(name, app string) string  { return "sandbox-" + name + "-" + app + ".socket" }
func appOrigin(e registryEntry, app string) string {
	return fmt.Sprintf("http://%s.%s.localhost:%d", app, e.Name, e.Port)
}
func defaultOrigin(e registryEntry) string {
	return fmt.Sprintf("http://%s.localhost:%d", e.Name, e.Port)
}
func callbackOrigin(e registryEntry) string { return fmt.Sprintf("http://localhost:%d", e.Port) }
func unitArgument(s string) string {
	return "\"" + strings.NewReplacer("%", "%%", "$", "$$").Replace(s) + "\""
}
func unitExecutable(s string) string { return "\"" + strings.ReplaceAll(s, "%", "%%") + "\"" }
func unitPath(s string) string       { return strings.ReplaceAll(s, "%", "%%") }

type registryApp struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}
type registryEntry struct {
	Name     string        `json:"name"`
	Port     int           `json:"port"`
	Worktree string        `json:"worktree"`
	Apps     []registryApp `json:"apps"`
}
type registry struct {
	Sandboxes []registryEntry `json:"sandboxes"`
}

func (reg registry) find(name string) (registryEntry, bool) {
	for _, e := range reg.Sandboxes {
		if e.Name == name {
			return e, true
		}
	}
	return registryEntry{}, false
}
func requiredObject(raw json.RawMessage, members ...string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("expected object")
	}
	for _, k := range members {
		if _, ok := m[k]; !ok {
			return nil, fmt.Errorf("missing member %s", k)
		}
	}
	return m, nil
}
func rawArray(raw json.RawMessage) ([]json.RawMessage, error) {
	var a []json.RawMessage
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("expected array")
	}
	err := json.Unmarshal(raw, &a)
	return a, err
}
func readRegistry(p paths) (registry, error) {
	reg := registry{Sandboxes: []registryEntry{}}
	b, err := os.ReadFile(p.registryPath())
	if errors.Is(err, os.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return reg, dataFileError(p.registryPath(), err)
	}
	obj, err := requiredObject(b, "sandboxes")
	if err != nil {
		return reg, dataFileError(p.registryPath(), err)
	}
	entries, err := rawArray(obj["sandboxes"])
	if err != nil {
		return reg, dataFileError(p.registryPath(), err)
	}
	names := map[string]bool{}
	ports := map[int]bool{}
	for _, raw := range entries {
		m, e := requiredObject(raw, "name", "port", "worktree", "apps")
		if e != nil {
			return reg, dataFileError(p.registryPath(), e)
		}
		var entry registryEntry
		for _, k := range []string{"name", "port", "worktree"} {
			if bytes.Equal(bytes.TrimSpace(m[k]), []byte("null")) {
				return reg, dataFileError(p.registryPath(), errors.New("wrong member type"))
			}
		}
		if e = json.Unmarshal(raw, &entry); e != nil {
			return reg, dataFileError(p.registryPath(), e)
		}
		apps, e := rawArray(m["apps"])
		if e != nil {
			return reg, dataFileError(p.registryPath(), e)
		}
		if !validSandboxName(entry.Name) || entry.Port < 7400 || entry.Port > 7499 || names[entry.Name] || ports[entry.Port] {
			return reg, dataFileError(p.registryPath(), errors.New("invalid or duplicate sandbox"))
		}
		names[entry.Name] = true
		ports[entry.Port] = true
		seen := map[string]bool{}
		defaults := 0
		for _, app := range apps {
			am, e := requiredObject(app, "name", "default")
			if e != nil {
				return reg, dataFileError(p.registryPath(), e)
			}
			if bytes.Equal(bytes.TrimSpace(am["name"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(am["default"]), []byte("null")) {
				return reg, dataFileError(p.registryPath(), errors.New("wrong member type"))
			}
		}
		for _, app := range entry.Apps {
			if !usableAppName(app.Name) || seen[app.Name] {
				return reg, dataFileError(p.registryPath(), errors.New("invalid or duplicate app"))
			}
			seen[app.Name] = true
			if app.Default {
				defaults++
			}
		}
		if defaults > 1 {
			return reg, dataFileError(p.registryPath(), errors.New("multiple default apps"))
		}
		reg.Sandboxes = append(reg.Sandboxes, entry)
	}
	return reg, nil
}
func writeRegistry(p paths, reg registry) error {
	if reg.Sandboxes == nil {
		reg.Sandboxes = []registryEntry{}
	}
	sort.Slice(reg.Sandboxes, func(i, j int) bool { return reg.Sandboxes[i].Name < reg.Sandboxes[j].Name })
	for i := range reg.Sandboxes {
		if reg.Sandboxes[i].Apps == nil {
			reg.Sandboxes[i].Apps = []registryApp{}
		}
		sort.Slice(reg.Sandboxes[i].Apps, func(a, b int) bool { return reg.Sandboxes[i].Apps[a].Name < reg.Sandboxes[i].Apps[b].Name })
	}
	if err := os.MkdirAll(p.root, 0700); err != nil {
		return dataFileError(p.root, err)
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return dataFileError(p.registryPath(), err)
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(p.root, ".registry-")
	if err != nil {
		return dataFileError(p.registryPath(), err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return dataFileError(p.registryPath(), err)
	}
	if err = f.Close(); err != nil {
		return dataFileError(p.registryPath(), err)
	}
	if err = os.Rename(tmp, p.registryPath()); err != nil {
		return dataFileError(p.registryPath(), err)
	}
	return nil
}
func lockFile(p string) (func(), error) {
	if err := os.MkdirAll(path.Dir(p), 0700); err != nil {
		return nil, dataFileError(path.Dir(p), err)
	}
	f, err := os.OpenFile(filepath.Clean(p), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, dataFileError(p, err)
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, dataFileError(p, err)
	}
	return func() { _ = f.Close() }, nil
}
func withRegistry(p paths, fn func(*registry) error) error {
	unlock, err := lockFile(p.registryLock())
	if err != nil {
		return err
	}
	defer unlock()
	reg, err := readRegistry(p)
	if err != nil {
		return err
	}
	if err = fn(&reg); err != nil {
		return err
	}
	return writeRegistry(p, reg)
}
func unitNames(e registryEntry) []string {
	names := []string{nginxUnit(e.Name)}
	for _, a := range e.Apps {
		names = append(names, socketUnit(e.Name, a.Name), serviceUnit(e.Name, a.Name))
	}
	sort.Strings(names)
	return names
}
func checkUnitClash(reg registry, name string, apps []registryApp) error {
	ours := unitNames(registryEntry{Name: name, Apps: apps})
	sort.Slice(reg.Sandboxes, func(i, j int) bool { return reg.Sandboxes[i].Name < reg.Sandboxes[j].Name })
	for _, unit := range ours {
		for _, e := range reg.Sandboxes {
			if e.Name == name {
				continue
			}
			for _, other := range unitNames(e) {
				if other == unit {
					return failure{2, fmt.Sprintf("unit '%s' would also belong to sandbox '%s'\n\nrename this worktree or the app, or wipe sandbox '%s' once it is down", unit, e.Name, e.Name)}
				}
			}
		}
	}
	return nil
}
func allocateEntry(reg *registry, name, worktree string, apps []registryApp) (registryEntry, error) {
	if err := checkUnitClash(*reg, name, apps); err != nil {
		return registryEntry{}, err
	}
	if e, ok := reg.find(name); ok {
		return e, nil
	}
	used := map[int]bool{}
	for _, e := range reg.Sandboxes {
		used[e.Port] = true
	}
	for port := 7400; port <= 7499; port++ {
		if !used[port] {
			e := registryEntry{Name: name, Port: port, Worktree: worktree, Apps: []registryApp{}}
			reg.Sandboxes = append(reg.Sandboxes, e)
			return e, nil
		}
	}
	return registryEntry{}, failure{2, "no free port: every port from 7400 to 7499 belongs to a sandbox\n\nrun 'sandbox ls' and wipe a sandbox you no longer need"}
}

type target struct {
	paths    paths
	entry    registryEntry
	worktree string
	unlock   func()
}

func unknownSandbox(name string, explicit bool) error {
	detail := "run 'sandbox up' to create it"
	if explicit {
		detail = "run 'sandbox ls' to see every sandbox"
	}
	return failure{2, "no sandbox '" + dataPrinted(name, false) + "'\n\n" + detail}
}
func checkOwner(e registryEntry, worktree string) error {
	if worktree != "" && e.Worktree != worktree {
		return failure{2, "sandbox '" + e.Name + "' belongs to another worktree: " + dataPrinted(e.Worktree, false) + "\n\nrename this worktree, or wipe that sandbox with 'sandbox wipe " + e.Name + "' once it is down"}
	}
	return nil
}
func (r *invocation) resolve(command, explicitName string, allowNew, mutate bool) (target, error) {
	t := target{unlock: func() {}}
	name := explicitName
	explicit := explicitName != ""
	if explicit {
		if !validSandboxName(name) {
			return t, unknownSandbox(name, true)
		}
	} else {
		if r.deps.Dir == "" {
			return t, failure{2, "the current directory no longer exists"}
		}
		result, err := r.deps.Exec(r.ctx, seam.Cmd{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: r.deps.Dir})
		if err != nil {
			return t, failure{1, "git rev-parse --show-toplevel: " + dataPrinted(err.Error(), false)}
		}
		if result.ExitCode != 0 {
			return t, failure{2, "'" + dataPrinted(r.deps.Dir, false) + "' is not inside a git checkout"}
		}
		t.worktree = strings.TrimSuffix(string(result.Stdout), "\n")
		if hasControl(t.worktree) || !utf8.ValidString(t.worktree) {
			return t, failure{2, "worktree '" + dataPrinted(t.worktree, true) + "' holds a control character or is not valid UTF-8"}
		}
		name = derivedName(t.worktree)
		if !validSandboxName(name) {
			return t, failure{2, "worktree '" + path.Base(t.worktree) + "' does not give a usable sandbox name\n\na name needs a letter or digit and at most 63 characters"}
		}
	}
	p, err := r.roots(command == "up" || command == "down" || command == "wipe")
	if err != nil {
		return t, err
	}
	t.paths = p
	check := func() (registryEntry, error) {
		reg, err := readRegistry(p)
		if err != nil {
			return registryEntry{}, err
		}
		e, ok := reg.find(name)
		if !ok {
			if !allowNew {
				return e, unknownSandbox(name, explicit)
			}
			e = registryEntry{Name: name, Worktree: t.worktree, Apps: []registryApp{}}
		}
		if err = checkOwner(e, t.worktree); err != nil {
			return e, err
		}
		return e, nil
	}
	t.entry, err = check()
	if err != nil {
		return t, err
	}
	if mutate {
		t.unlock, err = lockFile(p.lock(name))
		if err != nil {
			t.unlock = func() {}
			return t, err
		}
		t.entry, err = check()
		if err != nil {
			t.unlock()
			t.unlock = func() {}
			return t, err
		}
	}
	return t, nil
}
func (r *invocation) unitState(unit, action string) (string, error) {
	res, err := r.deps.Exec(r.ctx, seam.Cmd{Path: "systemctl", Args: []string{"--user", "show", "--property=ActiveState", "--value", unit}, Dir: "/"})
	if err != nil {
		return "", failure{1, action + ": " + dataPrinted(err.Error(), false)}
	}
	if res.ExitCode != 0 {
		msg := action + ": exit status " + strconv.Itoa(res.ExitCode)
		if len(res.Output) > 0 {
			msg += "\n\n"
			lines := strings.Split(strings.TrimSuffix(string(res.Output), "\n"), "\n")
			for i, line := range lines {
				if i > 0 {
					msg += "\n"
				}
				msg += "> " + line
			}
		}
		return "", failure{1, msg}
	}
	return strings.TrimSuffix(string(res.Stdout), "\n"), nil
}
func (r *invocation) sandboxUp(e registryEntry) (bool, error) {
	state, err := r.unitState(nginxUnit(e.Name), "systemctl --user")
	return state == "active" || state == "reloading" || state == "refreshing", err
}
