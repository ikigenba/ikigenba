// Package hardening establishes the host's login and state protection rules.
package hardening

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// Shared login policy names used by account management.
const (
	ShellGroup   = "shell"
	KeysDir      = "/etc/ssh/authorized_keys"
	AdminAccount = "ec2-user"
)

const sshConfig = "PubkeyAuthentication yes\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin no\nLoginGraceTime 30\nMaxAuthTries 3\nClientAliveInterval 300\nClientAliveCountMax 2\nAllowGroups " + AdminAccount + " " + ShellGroup + "\nUsePAM yes\n\nMatch Group " + ShellGroup + "\n  AllowTcpForwarding no\n  AllowStreamLocalForwarding no\n  AllowAgentForwarding no\n  X11Forwarding no\n  PermitTunnel no\n  AuthorizedKeysFile " + KeysDir + "/%u\n"
const journalConfig = "[Journal]\nStorage=persistent\n"
const jailConfig = "[sshd]\nenabled = true\nbackend = systemd\nbanaction = nftables-multiport\nmaxretry = 5\nfindtime = 10m\nbantime = 10m\nbantime.increment = true\nbantime.maxtime = 1w\n"
const sudoConfig = "ikigenba ALL=(root) NOPASSWD: /usr/local/bin/opsctl group *, /usr/local/bin/opsctl user *\n"

type operation struct {
	ctx     context.Context
	env     host.Env
	root    *os.Root
	changed bool
}
type savedFile struct {
	name           string
	data           []byte
	mode           os.FileMode
	exists         bool
	contentChanged bool
	modeChanged    bool
}

// Apply brings the host to its rules, preserving completed earlier parts on failure.
func Apply(ctx context.Context, env host.Env) (bool, error) {
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return false, fmt.Errorf("/: %s", hostFileError(err))
	}
	defer func() { _ = root.Close() }()
	o := operation{ctx: ctx, env: env, root: root}
	for _, part := range []func() error{o.account, o.pool, o.journal, o.shell, o.keys, o.sshd, o.packages, o.fail2ban, o.releaseLock, o.automatic, o.sudo, o.state} {
		if err := part(); err != nil {
			return o.changed, err
		}
	}
	return o.changed, nil
}
func hostFileError(err error) string {
	var p *os.PathError
	if errors.As(err, &p) {
		return p.Op + ": " + p.Err.Error()
	}
	return err.Error()
}
func fileError(name string, err error) error {
	return fmt.Errorf("/%s: %s", strings.TrimPrefix(name, "/"), hostFileError(err))
}
func (o *operation) path(name string) string {
	return filepath.Join(o.env.Root, strings.TrimPrefix(name, "/"))
}
func (o *operation) command(label, name string, accepted []int, args ...string) (host.Result, error) {
	if o.env.Execute == nil {
		return host.Result{}, &host.CommandError{Label: label, Err: errors.New("host execution is not configured")}
	}
	r, e := o.env.Execute(o.ctx, host.Command{Name: name, Args: args})
	if e == nil {
		for _, code := range accepted {
			if r.ExitCode == code {
				return r, nil
			}
		}
	}
	return r, &host.CommandError{Label: label, Result: r, Err: e}
}
func (o *operation) run(label, name string, args ...string) error {
	_, err := o.command(label, name, []int{0}, args...)
	return err
}
func (o *operation) account() error {
	r, err := o.command("id", "id", []int{0, 1}, "--user", "ikigenba")
	if err != nil {
		return err
	}
	if err := apps.EnsureAccount(o.ctx, o.env); err != nil {
		var ce *host.CommandError
		if errors.As(err, &ce) {
			return ce
		}
		return err
	}
	o.changed = o.changed || r.ExitCode == 1
	return nil
}
func (o *operation) directory(name string, fix bool) error {
	mode := os.FileMode(0o755)
	info, err := o.root.Lstat(strings.TrimPrefix(name, "/"))
	if err == nil {
		if !info.IsDir() {
			return fileError(name, errors.New("not a directory"))
		}
		if fix && info.Mode().Perm() != mode {
			if err := o.root.Chmod(strings.TrimPrefix(name, "/"), mode); err != nil {
				return fileError(name, err)
			}
			o.changed = true
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fileError(name, err)
	}
	if err := o.root.Mkdir(strings.TrimPrefix(name, "/"), mode); err != nil {
		return fileError(name, err)
	}
	if err := o.root.Chmod(strings.TrimPrefix(name, "/"), mode); err != nil {
		return fileError(name, err)
	}
	o.changed = true
	return nil
}
func (o *operation) read(name string) (savedFile, error) {
	s := savedFile{name: strings.TrimPrefix(name, "/")}
	info, err := o.root.Lstat(s.name)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fileError(name, err)
	}
	if !info.Mode().IsRegular() {
		return s, fileError(name, errors.New("not a regular file"))
	}
	s.exists = true
	s.mode = info.Mode()
	s.data, err = o.root.ReadFile(s.name)
	if err != nil {
		return s, fileError(name, err)
	}
	return s, nil
}
func (o *operation) atomic(name string, data []byte, mode os.FileMode) error {
	// An exclusive deterministic candidate is sufficient; interrupted candidates are never reused.
	var f *os.File
	var err error
	var temp string
	for n := 0; n < 1000; n++ {
		temp = filepath.Join(filepath.Dir(name), fmt.Sprintf(".ikigenba-tmp.%d", n))
		f, err = o.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if !errors.Is(err, os.ErrExist) {
			break
		}
	}
	if err != nil {
		return fileError(name, err)
	}
	defer func() { _ = o.root.Remove(temp) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return fileError(name, err)
	}
	if err = f.Chmod(mode); err != nil {
		_ = f.Close()
		return fileError(name, err)
	}
	if err = f.Close(); err != nil {
		return fileError(name, err)
	}
	if err = o.root.Rename(temp, name); err != nil {
		return fileError(name, err)
	}
	return nil
}
func (o *operation) put(name string, data []byte, mode os.FileMode) (savedFile, error) {
	s, err := o.read(name)
	if err != nil {
		return s, err
	}
	if s.exists && bytes.Equal(s.data, data) {
		if s.mode.Perm() != mode {
			if err := o.root.Chmod(s.name, mode); err != nil {
				return s, fileError(name, err)
			}
			s.modeChanged = true
		}
		return s, nil
	}
	if err := o.atomic(s.name, data, mode); err != nil {
		return s, err
	}
	s.contentChanged = true
	return s, nil
}
func (o *operation) keep(s savedFile) { o.changed = o.changed || s.contentChanged || s.modeChanged }
func (o *operation) restore(s savedFile) error {
	if !s.contentChanged {
		return nil
	}
	if s.exists {
		return o.atomic(s.name, s.data, s.mode)
	}
	if err := o.root.Remove(s.name); err != nil {
		return fileError(s.name, err)
	}
	return nil
}
func (o *operation) pool() (runErr error) {
	names := []string{"etc/systemd/system/user.slice.d/ikigenba.conf", "etc/systemd/system/user-.slice.d/ikigenba.conf"}
	data := []string{"[Slice]\nMemoryHigh=1.5G\nMemoryMax=2G\nTasksMax=512\nCPUWeight=50\n", "[Slice]\nMemoryHigh=600M\nMemoryMax=1G\nTasksMax=256\n"}
	var saved []savedFile
	defer func() {
		for _, s := range saved {
			if runErr != nil && s.contentChanged {
				_ = o.restore(s)
			} else {
				o.keep(s)
			}
		}
	}()
	for i, name := range names {
		if err := o.directory(filepath.Dir(name), false); err != nil {
			return err
		}
		s, err := o.put(name, []byte(data[i]), 0o644)
		if err != nil {
			return err
		}
		saved = append(saved, s)
	}
	if saved[0].contentChanged || saved[1].contentChanged {
		return o.run("systemctl daemon-reload", "systemctl", "daemon-reload")
	}
	return nil
}

func (o *operation) journal() (runErr error) {
	if err := o.directory("etc/systemd/journald.conf.d", false); err != nil {
		return err
	}
	s, err := o.put("etc/systemd/journald.conf.d/ikigenba.conf", []byte(journalConfig), 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if runErr != nil && s.contentChanged {
			_ = o.restore(s)
		} else {
			o.keep(s)
		}
	}()
	_, err = o.root.Lstat("var/log/journal")
	created := errors.Is(err, os.ErrNotExist)
	if err != nil && !created {
		return fileError("var/log/journal", err)
	}
	if created {
		if err := o.directory("var/log", false); err != nil {
			return err
		}
		if err := o.directory("var/log/journal", false); err != nil {
			return err
		}
	}
	if !s.contentChanged && !created {
		return nil
	}
	for _, command := range []struct {
		label, name string
		args        []string
	}{
		{"systemd-tmpfiles --create", "systemd-tmpfiles", []string{"--create", "--prefix", o.path("var/log/journal")}},
		{"systemctl restart", "systemctl", []string{"restart", "systemd-journald.service"}},
		{"journalctl --flush", "journalctl", []string{"--flush"}},
	} {
		if err := o.run(command.label, command.name, command.args...); err != nil {
			if !s.contentChanged {
				_ = o.root.Remove(s.name)
			}
			return err
		}
	}
	return nil
}

func (o *operation) shell() error {
	r, err := o.command("getent group", "getent", []int{0, 2}, "group", ShellGroup)
	if err != nil {
		return err
	}
	if r.ExitCode == 2 {
		if err := o.run("groupadd", "groupadd", "--system", ShellGroup); err != nil {
			return err
		}
		o.changed = true
	}
	return nil
}
func (o *operation) keys() error {
	if err := o.directory("etc/ssh", false); err != nil {
		return err
	}
	return o.directory(KeysDir, true)
}
func (o *operation) sshd() error {
	s, err := o.put("etc/ssh/sshd_config.d/10-ikigenba.conf", []byte(sshConfig), 0o600)
	if err != nil {
		return err
	}
	if s.contentChanged {
		if err := o.run("sshd -t", "sshd", "-t", "-f", o.path("etc/ssh/sshd_config")); err != nil {
			_ = o.restore(s)
			return err
		}
		if err := o.run("systemctl reload", "systemctl", "reload", "sshd.service"); err != nil {
			_ = o.restore(s)
			return err
		}
	}
	o.keep(s)
	return nil
}
func (o *operation) packages() error {
	var missing []string
	for _, p := range []string{"fail2ban-server", "dnf-automatic"} {
		r, err := o.command("rpm -q", "rpm", []int{0, 1}, "-q", p)
		if err != nil {
			return err
		}
		if r.ExitCode == 1 {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		if err := o.run("dnf install", "dnf", append([]string{"install", "-y"}, missing...)...); err != nil {
			return err
		}
		o.changed = true
	}
	return nil
}
func (o *operation) service(unit string, reload bool) error {
	enabled, err := o.queryService("is-enabled", unit)
	if err != nil {
		return err
	}
	active, err := o.queryService("is-active", unit)
	if err != nil {
		return err
	}
	if !enabled || !active {
		if err := o.run("systemctl enable", "systemctl", "enable", "--now", unit); err != nil {
			return err
		}
		o.changed = true
	}
	if reload && active {
		return o.run("systemctl reload", "systemctl", "reload", unit)
	}
	return nil
}
func (o *operation) queryService(verb, unit string) (bool, error) {
	if o.env.Execute == nil {
		return false, &host.CommandError{Label: "systemctl " + verb, Err: errors.New("host execution is not configured")}
	}
	r, err := o.env.Execute(o.ctx, host.Command{Name: "systemctl", Args: []string{verb, unit}})
	if err != nil {
		return false, &host.CommandError{Label: "systemctl " + verb, Result: r, Err: err}
	}
	return r.ExitCode == 0, nil
}
func (o *operation) fail2ban() error {
	if err := o.directory("etc/fail2ban/jail.d", false); err != nil {
		return err
	}
	s, err := o.put("etc/fail2ban/jail.d/ikigenba.local", []byte(jailConfig), 0o644)
	if err != nil {
		return err
	}
	if err := o.service("fail2ban.service", s.contentChanged); err != nil {
		if !s.contentChanged {
			o.keep(s)
		}
		_ = o.restore(s)
		return err
	}
	o.keep(s)
	return nil
}
func (o *operation) releaseLock() error {
	if err := o.directory("etc/dnf/vars", false); err != nil {
		return err
	}
	s, err := o.put("etc/dnf/vars/releasever", []byte("latest\n"), 0o644)
	if err == nil {
		o.keep(s)
	}
	return err
}
func automaticBytes(data []byte) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if start < 0 {
			if trim == "[commands]" {
				start = i
			}
		} else if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			end = i
			break
		}
	}
	if start < 0 {
		out := append([]byte(nil), data...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		return append(out, []byte("[commands]\nupgrade_type = security\napply_updates = yes\n")...)
	}
	found := map[string]bool{}
	for i := start + 1; i < end; i++ {
		line := strings.TrimLeft(lines[i], " \t\r")
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if ok && (key == "upgrade_type" || key == "apply_updates") {
			value := "security"
			if key == "apply_updates" {
				value = "yes"
			}
			suffix := ""
			if strings.HasSuffix(lines[i], "\n") {
				suffix = "\n"
			}
			lines[i] = key + " = " + value + suffix
			found[key] = true
		}
	}
	var extra string
	for _, key := range []string{"upgrade_type", "apply_updates"} {
		if !found[key] {
			value := "security"
			if key == "apply_updates" {
				value = "yes"
			}
			extra += key + " = " + value + "\n"
		}
	}
	if extra != "" && !strings.HasSuffix(lines[start], "\n") {
		lines[start] += "\n"
	}
	lines[start] += extra
	return []byte(strings.Join(lines, ""))
}
func (o *operation) automatic() error {
	old, err := o.read("etc/dnf/automatic.conf")
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if old.exists {
		mode = old.mode
	}
	s, err := o.put(old.name, automaticBytes(old.data), mode)
	if err != nil {
		return err
	}
	if err := o.service("dnf-automatic.timer", false); err != nil {
		_ = o.restore(s)
		return err
	}
	o.keep(s)
	return nil
}
func (o *operation) sudo() error {
	const name = "etc/sudoers.d/ikigenba-accounts"
	const staged = name + ".new"
	defer func() { _ = o.root.Remove(staged) }()
	s, err := o.read(name)
	if err != nil {
		return err
	}
	if s.exists && bytes.Equal(s.data, []byte(sudoConfig)) {
		if err := o.root.Remove(staged); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fileError(staged, err)
		}
		s, err = o.put(name, []byte(sudoConfig), 0o440)
		if err == nil {
			o.keep(s)
		}
		return err
	}
	if err := o.atomic(staged, []byte(sudoConfig), 0o440); err != nil {
		return err
	}
	if err := o.run("visudo -c", "visudo", "-c", "-f", o.path(staged)); err != nil {
		return err
	}
	if err := o.root.Rename(staged, name); err != nil {
		return fileError(name, err)
	}
	o.changed = true
	return nil
}

type statePath struct {
	name, owner string
	mode        os.FileMode
}

func (o *operation) state() error {
	table := []statePath{{"etc/ikigenba", "root:root", 0o700}, {"etc/opt/ikigenba", "root:root", 0o700}, {"var/opt/ikigenba", "root:ikigenba", 0o750}, {"var/lib/ikigenba", "root:ikigenba", 0o750}, {"run/ikigenba", "root:ikigenba", 0o750}}
	var paths []statePath
	var bases []string
	for _, p := range table {
		info, err := o.root.Lstat(p.name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fileError(p.name, err)
		}
		if !info.IsDir() {
			continue
		}
		paths = append(paths, p)
		bases = append(bases, p.name)
		if p.name == "etc/opt/ikigenba" {
			entries, err := fs.ReadDir(o.root.FS(), p.name)
			if err != nil {
				return fileError(p.name, err)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					paths = append(paths, statePath{filepath.Join(p.name, entry.Name()), p.owner, p.mode})
				}
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	args := []string{"--format=%U:%G"}
	for _, p := range paths {
		args = append(args, o.path(p.name))
	}
	r, err := o.command("stat", "stat", []int{0}, args...)
	if err != nil {
		return err
	}
	text := strings.TrimSuffix(string(r.Stdout), "\n")
	owners := strings.Split(text, "\n")
	if len(owners) != len(paths) {
		return fileError(paths[0].name, errors.New("invalid stat owner output"))
	}
	for _, owner := range owners {
		parts := strings.Split(owner, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(owner, " \t\r") {
			return fileError(paths[0].name, errors.New("invalid stat owner output"))
		}
	}
	for i, p := range paths {
		if owners[i] != p.owner {
			if err := o.run("chown", "chown", p.owner, o.path(p.name)); err != nil {
				return err
			}
			o.changed = true
		}
		if p.name == "run/ikigenba" {
			r, err := o.command("getfacl", "getfacl", []int{0}, "--absolute-names", "--omit-header", o.path(p.name))
			if err != nil {
				return err
			}
			has := false
			for _, line := range strings.Split(string(r.Stdout), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 0 && fields[0] == "user:nginx:--x" {
					has = true
				}
			}
			if !has {
				if err := o.run("setfacl", "setfacl", "-m", "u:nginx:x", o.path(p.name)); err != nil {
					return err
				}
				o.changed = true
			}
		}
		info, err := o.root.Lstat(p.name)
		if err != nil {
			return fileError(p.name, err)
		}
		if info.Mode().Perm() != p.mode {
			if err := o.root.Chmod(p.name, (info.Mode()&^0o777)|p.mode); err != nil {
				return fileError(p.name, err)
			}
			o.changed = true
		}
	}
	for _, base := range bases {
		if err := fs.WalkDir(o.root.FS(), base, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return fileError(name, err)
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return fileError(name, err)
			}
			if info.Mode().Perm()&0o007 != 0 {
				if err := o.root.Chmod(name, info.Mode()&^0o007); err != nil {
					return fileError(name, err)
				}
				o.changed = true
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
