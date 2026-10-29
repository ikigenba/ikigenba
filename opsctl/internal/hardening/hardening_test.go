package hardening

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type fixture struct {
	t              *testing.T
	root           string
	commands       []string
	account, group bool
	packages       map[string]bool
	services       map[string]bool
	owners         map[string]string
	acl            bool
	fail           string
	spawnErr       error
	hook           func(host.Command)
	fault          host.Result
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, root: t.TempDir(), packages: map[string]bool{}, services: map[string]bool{}, owners: map[string]string{}, fault: host.Result{ExitCode: 9, Stdout: []byte("out\n"), Stderr: []byte("err\n")}}
	for _, dir := range []string{"etc/ssh/sshd_config.d", "etc/sudoers.d", "etc/dnf", "etc/systemd/system", "etc/fail2ban"} {
		f.mkdir(dir, 0o755)
	}
	f.write("etc/dnf/automatic.conf", "# packaged\n[commands]\nupgrade_type = default\napply_updates = no\ndownload_updates = yes\n[emitters]\nemit_via = stdio\n", 0o640)
	return f
}
func (f *fixture) mkdir(name string, mode os.FileMode) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Join(f.root, name), mode); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(f.root, name), mode); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) write(name, data string, mode os.FileMode) {
	f.t.Helper()
	f.mkdir(filepath.Dir(name), 0o755)
	if err := os.WriteFile(filepath.Join(f.root, name), []byte(data), mode); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(f.root, name), mode); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) read(name string) string {
	f.t.Helper()
	root, err := os.OpenRoot(f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}
func (f *fixture) mode(name string) os.FileMode {
	f.t.Helper()
	info, err := os.Lstat(filepath.Join(f.root, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return info.Mode().Perm()
}
func (f *fixture) env() host.Env {
	return host.Env{Root: f.root, Getenv: func(string) string { f.t.Fatal("unexpected environment read"); return "" }, Execute: f.execute}
}
func (f *fixture) execute(_ context.Context, c host.Command) (host.Result, error) {
	text := strings.Join(append([]string{c.Name}, c.Args...), " ")
	f.commands = append(f.commands, text)
	if f.hook != nil {
		f.hook(c)
	}
	if text == f.fail {
		return f.fault, f.spawnErr
	}
	for _, arg := range c.Args {
		if strings.HasPrefix(arg, "/") && arg != "/usr/sbin/nologin" && !strings.HasPrefix(arg, f.root+"/") {
			f.t.Fatalf("unrooted command argument: %s", arg)
		}
	}
	switch c.Name {
	case "id":
		if c.Args[0] == "--user" {
			if !f.account {
				return host.Result{ExitCode: 1}, nil
			}
			return host.Result{Stdout: []byte("1001\n")}, nil
		}
		return host.Result{Stdout: []byte("ikigenba\n")}, nil
	case "useradd":
		f.account = true
	case "getent":
		if !f.group {
			return host.Result{ExitCode: 2}, nil
		}
		return host.Result{Stdout: []byte("shell:x:1002:\n")}, nil
	case "groupadd":
		f.group = true
	case "rpm":
		if !f.packages[c.Args[1]] {
			return host.Result{ExitCode: 1}, nil
		}
	case "dnf":
		for _, p := range c.Args[2:] {
			f.packages[p] = true
		}
	case "systemctl":
		if c.Args[0] == "is-enabled" || c.Args[0] == "is-active" {
			if !f.services[c.Args[1]] {
				return host.Result{ExitCode: 3}, nil
			}
		}
		if c.Args[0] == "enable" {
			f.services[c.Args[2]] = true
		}
	case "stat":
		var out strings.Builder
		for _, p := range c.Args[1:] {
			owner := f.owners[p]
			if owner == "" {
				owner = "nobody:nobody"
			}
			out.WriteString(owner + "\n")
		}
		return host.Result{Stdout: []byte(out.String())}, nil
	case "chown":
		f.owners[c.Args[1]] = c.Args[0]
	case "getfacl":
		if f.acl {
			return host.Result{Stdout: []byte("user::rwx\nuser:nginx:--x\t#effective:--x\n")}, nil
		}
	case "setfacl":
		f.acl = true
	case "sshd", "visudo":
	default:
		f.t.Fatalf("unexpected command %s", text)
	}
	return host.Result{}, nil
}
func (f *fixture) apply() (bool, error) { return Apply(context.Background(), f.env()) }
func (f *fixture) requireApply() {
	f.t.Helper()
	changed, err := f.apply()
	if err != nil || !changed {
		f.t.Fatalf("Apply = %v, %v", changed, err)
	}
}
func (f *fixture) noTemporary() {
	f.t.Helper()
	_ = filepath.WalkDir(f.root, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			f.t.Fatal(err)
		}
		if strings.Contains(d.Name(), ".ikigenba-tmp.") || strings.HasSuffix(d.Name(), ".new") {
			f.t.Fatalf("temporary remains: %s", name)
		}
		return nil
	})
}

func TestApplyContractAndIdempotence(t *testing.T) {
	// R-QP1Z-BOQ4 R-QQ9V-PGGT R-ZC9B-Q97J R-ZB1F-CHGU R-SALL-UGL5 R-QV5H-8JFL R-QWDD-MB6A
	// R-3929-UROK R-3AA6-8JF9 R-3BI2-MB5Y R-3CPZ-02WN R-SD1E-M02J R-3DXV-DUNC R-R4WO-APD5 R-SE9A-ZRT8
	// R-R7CH-28UJ R-98VK-1BB5 R-RC82-LBTB R-3HLK-J5VF R-3ITG-WXM4 R-SLKP-AE9E
	if ShellGroup != "shell" || KeysDir != "/etc/ssh/authorized_keys" || AdminAccount != "ec2-user" {
		t.Fatal("public constants")
	}
	f := newFixture(t)
	f.write("etc/ssh/authorized_keys/existing", "key", 0o600)
	f.write("opt/release/keep", "release", 0o644)
	f.hook = func(c host.Command) {
		if c.Name == "sshd" {
			if f.read("etc/ssh/sshd_config.d/10-ikigenba.conf") != "PubkeyAuthentication yes\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin no\nLoginGraceTime 30\nMaxAuthTries 3\nClientAliveInterval 300\nClientAliveCountMax 2\nAllowGroups ec2-user shell\nUsePAM yes\n\nMatch Group shell\n  AllowTcpForwarding no\n  AllowAgentForwarding no\n  X11Forwarding no\n  PermitTunnel no\n  AuthorizedKeysFile /etc/ssh/authorized_keys/%u\n" {
				t.Fatal("sshd candidate not installed")
			}
		}
		if c.Name == "visudo" {
			if f.read("etc/sudoers.d/ikigenba-accounts.new") != "ikigenba ALL=(root) NOPASSWD: /usr/local/bin/opsctl group *, /usr/local/bin/opsctl user *\n" {
				t.Fatal("sudo candidate")
			}
			if _, err := os.Lstat(filepath.Join(f.root, "etc/sudoers.d/ikigenba-accounts")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("sudo published before validation")
			}
		}
	}
	f.requireApply()
	f.noTemporary()
	for name, want := range map[string]string{"etc/systemd/system/user.slice.d/ikigenba.conf": "[Slice]\nMemoryHigh=1.5G\nMemoryMax=2G\nTasksMax=512\nCPUWeight=50\n", "etc/systemd/system/user-.slice.d/ikigenba.conf": "[Slice]\nMemoryHigh=600M\nMemoryMax=1G\nTasksMax=256\n", "etc/fail2ban/jail.d/ikigenba.local": "[sshd]\nenabled = true\nbackend = systemd\nbanaction = nftables-multiport\nmaxretry = 5\nfindtime = 10m\nbantime = 10m\nbantime.increment = true\nbantime.maxtime = 1w\n", "etc/dnf/vars/releasever": "latest\n", "etc/dnf/automatic.conf": "# packaged\n[commands]\nupgrade_type = security\napply_updates = yes\ndownload_updates = yes\n[emitters]\nemit_via = stdio\n"} {
		if got := f.read(name); got != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
	for name, mode := range map[string]os.FileMode{"etc/systemd/system/user.slice.d": 0o755, "etc/systemd/system/user.slice.d/ikigenba.conf": 0o644, "etc/systemd/system/user-.slice.d/ikigenba.conf": 0o644, "etc/ssh/authorized_keys": 0o755, "etc/ssh/sshd_config.d/10-ikigenba.conf": 0o600, "etc/fail2ban/jail.d/ikigenba.local": 0o644, "etc/dnf/vars/releasever": 0o644, "etc/dnf/automatic.conf": 0o640, "etc/sudoers.d/ikigenba-accounts": 0o440} {
		if got := f.mode(name); got != mode {
			t.Fatalf("%s mode %o", name, got)
		}
	}
	wantCommands := []string{"id --user ikigenba", "id --user ikigenba", "useradd --system --no-create-home --shell /usr/sbin/nologin --user-group ikigenba", "systemctl daemon-reload", "getent group shell", "groupadd --system shell", "sshd -t -f " + filepath.Join(f.root, "etc/ssh/sshd_config"), "systemctl reload sshd.service", "rpm -q fail2ban-server", "rpm -q dnf-automatic", "dnf install -y fail2ban-server dnf-automatic", "systemctl is-enabled fail2ban.service", "systemctl is-active fail2ban.service", "systemctl enable --now fail2ban.service", "systemctl is-enabled dnf-automatic.timer", "systemctl is-active dnf-automatic.timer", "systemctl enable --now dnf-automatic.timer", "visudo -c -f " + filepath.Join(f.root, "etc/sudoers.d/ikigenba-accounts.new")}
	// EnsureAccount's shell is a program setting rather than a filesystem operand.
	if !reflect.DeepEqual(f.commands, wantCommands) {
		t.Fatalf("commands = %q", f.commands)
	}
	f.hook = nil
	f.commands = nil
	changed, err := f.apply()
	if err != nil || changed {
		t.Fatalf("second Apply = %v %v", changed, err)
	}
	wantCommands = []string{"id --user ikigenba", "id --user ikigenba", "id --group --name ikigenba", "getent group shell", "rpm -q fail2ban-server", "rpm -q dnf-automatic", "systemctl is-enabled fail2ban.service", "systemctl is-active fail2ban.service", "systemctl is-enabled dnf-automatic.timer", "systemctl is-active dnf-automatic.timer"}
	if !reflect.DeepEqual(f.commands, wantCommands) {
		t.Fatalf("idempotent commands = %q", f.commands)
	}
	if f.read("etc/ssh/authorized_keys/existing") != "key" || f.read("opt/release/keep") != "release" {
		t.Fatal("unrelated file changed")
	}
}

func TestApplyRollbackAndCommandErrors(t *testing.T) {
	// R-SALL-UGL5 R-3AA6-8JF9 R-SD1E-M02J R-SE9A-ZRT8 R-98VK-1BB5 R-RFVR-QN1E R-RC82-LBTB
	for _, test := range []struct{ command, file, label string }{{"systemctl daemon-reload", "etc/systemd/system/user.slice.d/ikigenba.conf", "systemctl daemon-reload"}, {"sshd -t -f ROOT/etc/ssh/sshd_config", "etc/ssh/sshd_config.d/10-ikigenba.conf", "sshd -t"}, {"systemctl reload sshd.service", "etc/ssh/sshd_config.d/10-ikigenba.conf", "systemctl reload"}, {"systemctl enable --now fail2ban.service", "etc/fail2ban/jail.d/ikigenba.local", "systemctl enable"}, {"systemctl reload fail2ban.service", "etc/fail2ban/jail.d/ikigenba.local", "systemctl reload"}, {"systemctl enable --now dnf-automatic.timer", "etc/dnf/automatic.conf", "systemctl enable"}, {"visudo -c -f ROOT/etc/sudoers.d/ikigenba-accounts.new", "etc/sudoers.d/ikigenba-accounts", "visudo -c"}} {
		for _, exists := range []bool{false, true} {
			t.Run(test.label+fmt.Sprint(exists)+test.file, func(t *testing.T) {
				f := newFixture(t)
				if test.command == "systemctl reload fail2ban.service" {
					f.services["fail2ban.service"] = true
				}
				if exists {
					f.write(test.file, "old\n", 0o620)
				} else if err := os.Remove(filepath.Join(f.root, test.file)); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				f.fail = strings.ReplaceAll(test.command, "ROOT", f.root)
				_, err := f.apply()
				var ce *host.CommandError
				if !errors.As(err, &ce) || ce.Label != test.label || ce.Error() != err.Error() || ce.Result.ExitCode != 9 || !reflect.DeepEqual(ce.Result, f.fault) {
					t.Fatalf("error=%#v", err)
				}
				if f.commands[len(f.commands)-1] != f.fail {
					t.Fatalf("continued after failure: %q", f.commands)
				}
				if exists {
					if f.read(test.file) != "old\n" || f.mode(test.file) != 0o620 {
						t.Fatal("old file not restored")
					}
				} else if _, err := os.Lstat(filepath.Join(f.root, test.file)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed candidate remains %v", err)
				}
				f.noTemporary()
				f.fail = ""
				f.requireApply()
			})
		}
	}
}

func TestApplyStateDirectoryPermissionsOwnershipAndACL(t *testing.T) {
	// R-R9S9-TSBX R-YEOK-DJ3V R-3HLK-J5VF R-3ITG-WXM4
	f := newFixture(t)
	for _, name := range []string{"etc/ikigenba", "etc/opt/ikigenba/zulu", "etc/opt/ikigenba/alpha", "var/opt/ikigenba", "var/lib/ikigenba", "run/ikigenba"} {
		f.mkdir(name, 0o777)
	}
	f.write("var/opt/ikigenba/data/file", "payload", 0o677)
	f.write("outside", "outside", 0o777)
	if err := os.Symlink(filepath.Join(f.root, "outside"), filepath.Join(f.root, "var/opt/ikigenba/link")); err != nil {
		t.Fatal(err)
	}
	paths := []string{"etc/ikigenba", "etc/opt/ikigenba", "etc/opt/ikigenba/alpha", "etc/opt/ikigenba/zulu", "var/opt/ikigenba", "var/lib/ikigenba", "run/ikigenba"}
	f.hook = func(c host.Command) {
		if c.Name == "chown" || c.Name == "setfacl" {
			name := c.Args[len(c.Args)-1]
			if info, err := os.Lstat(name); err != nil || info.Mode().Perm()&7 == 0 {
				t.Fatalf("closed before owner/ACL established: %s %v %v", name, info, err)
			}
		}
	}
	f.requireApply()
	for i, p := range paths {
		mode := os.FileMode(0o750)
		owner := "root:ikigenba"
		if i < 4 {
			mode = 0o700
			owner = "root:root"
		}
		if f.mode(p) != mode || f.owners[filepath.Join(f.root, p)] != owner {
			t.Fatalf("state %s mode/owner", p)
		}
	}
	if f.mode("var/opt/ikigenba/data/file") != 0o670 || f.read("var/opt/ikigenba/data/file") != "payload" || f.mode("outside") != 0o777 {
		t.Fatal("descendant bits/bytes or link target changed")
	}
	var stat []string
	for _, c := range f.commands {
		if strings.HasPrefix(c, "stat ") {
			stat = append(stat, c)
		}
	}
	args := []string{"stat --format=%U:%G"}
	for _, p := range paths {
		args = append(args, filepath.Join(f.root, p))
	}
	if !reflect.DeepEqual(stat, []string{strings.Join(args, " ")}) {
		t.Fatalf("stat order = %q", stat)
	}
	f.hook = nil
	f.commands = nil
	changed, err := f.apply()
	if err != nil || changed {
		t.Fatalf("stable state %v %v", changed, err)
	}
	for _, c := range f.commands {
		if strings.HasPrefix(c, "chown ") || strings.HasPrefix(c, "setfacl ") {
			t.Fatal("repeated state mutation")
		}
	}
}

func TestAutomaticSectionEditing(t *testing.T) {
	// R-R4WO-APD5
	for _, test := range []struct{ in, want string }{{"", "[commands]\nupgrade_type = security\napply_updates = yes\n"}, {"other = yes", "other = yes\n[commands]\nupgrade_type = security\napply_updates = yes\n"}, {"[commands]\n;apply_updates=no\n#upgrade_type=default\nkeep = here\n[next]\napply_updates = no\n", "[commands]\nupgrade_type = security\napply_updates = yes\n;apply_updates=no\n#upgrade_type=default\nkeep = here\n[next]\napply_updates = no\n"}, {" [commands] \n upgrade_type = default\nupgrade_type=default\n apply_updates=no\n[next]\nupgrade_type=x", " [commands] \nupgrade_type = security\nupgrade_type = security\napply_updates = yes\n[next]\nupgrade_type=x"}} {
		t.Run(test.in, func(t *testing.T) {
			f := newFixture(t)
			f.write("etc/dnf/automatic.conf", test.in, 0o620)
			f.requireApply()
			if got := f.read("etc/dnf/automatic.conf"); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
			if f.mode("etc/dnf/automatic.conf") != 0o620 {
				t.Fatal("mode not preserved")
			}
		})
	}
}

func TestApplyModeOnlyKeepsInodesAndRunsNoReload(t *testing.T) {
	// R-SALL-UGL5 R-QWDD-MB6A R-3AA6-8JF9 R-3CPZ-02WN R-3DXV-DUNC R-98VK-1BB5 R-3ITG-WXM4
	f := newFixture(t)
	f.requireApply()
	var infos = map[string]os.FileInfo{}
	for _, name := range []string{"etc/systemd/system/user.slice.d/ikigenba.conf", "etc/systemd/system/user-.slice.d/ikigenba.conf", "etc/ssh/sshd_config.d/10-ikigenba.conf", "etc/fail2ban/jail.d/ikigenba.local", "etc/dnf/vars/releasever", "etc/sudoers.d/ikigenba-accounts"} {
		p := filepath.Join(f.root, name)
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		infos[name] = info
		rootFS, err := os.OpenRoot(f.root)
		if err != nil {
			t.Fatal(err)
		}
		if err := rootFS.Chmod(name, 0o666); err != nil {
			t.Fatal(err)
		}
		_ = rootFS.Close()
	}
	f.mkdir("etc/ssh/authorized_keys", 0o700)
	f.write("etc/sudoers.d/ikigenba-accounts.new", "interrupted", 0o440)
	f.commands = nil
	f.requireApply()
	for name, info := range infos {
		after, err := os.Lstat(filepath.Join(f.root, name))
		if err != nil || !os.SameFile(info, after) {
			t.Fatalf("mode-only rewrite %s", name)
		}
	}
	for _, c := range f.commands {
		if strings.Contains(c, "reload") || strings.HasPrefix(c, "sshd ") || strings.HasPrefix(c, "visudo ") {
			t.Fatalf("mode-only command %s", c)
		}
	}
	f.noTemporary()
}

func TestApplyStopsOnUnexpectedQueryAndFilesystemFailure(t *testing.T) {
	// R-RFVR-QN1E R-RH3O-4ES3 R-ZB1F-CHGU R-3BI2-MB5Y
	for _, command := range []string{"id --user ikigenba", "getent group shell", "rpm -q fail2ban-server", "rpm -q dnf-automatic", "dnf install -y fail2ban-server dnf-automatic"} {
		for _, spawn := range []bool{false, true} {
			t.Run(command+fmt.Sprint(spawn), func(t *testing.T) {
				f := newFixture(t)
				f.fail = command
				if spawn {
					f.spawnErr = errors.New("spawn failed")
				}
				_, err := f.apply()
				var ce *host.CommandError
				if !errors.As(err, &ce) || err.Error() != ce.Error() || !reflect.DeepEqual(ce.Result, f.fault) {
					t.Fatalf("error %v", err)
				}
				if got := f.commands[len(f.commands)-1]; got != command {
					t.Fatalf("did not stop: %s", got)
				}
				if strings.HasPrefix(command, "dnf ") {
					if _, err := os.Lstat(filepath.Join(f.root, "etc/fail2ban/jail.d/ikigenba.local")); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("jail installed after package failure")
					}
				}
			})
		}
	}
	f := newFixture(t)
	f.write("etc/systemd/system/user.slice.d/ikigenba.conf", "old", 0o644)
	if err := os.RemoveAll(filepath.Join(f.root, "etc/ssh/sshd_config.d")); err != nil {
		t.Fatal(err)
	}
	_, err := f.apply()
	if err == nil || !strings.Contains(err.Error(), "/etc/ssh/sshd_config.d/10-ikigenba.conf") || strings.Contains(err.Error(), f.root) {
		t.Fatalf("filesystem error %v", err)
	}
	if f.read("etc/systemd/system/user.slice.d/ikigenba.conf") == "old" {
		t.Fatal("earlier part rolled back")
	}
	f.noTemporary()
}

func TestPoolFilesystemFailureRestoresCandidatesAndRetainedModesCount(t *testing.T) {
	// R-RH3O-4ES3 R-SALL-UGL5 R-3ITG-WXM4 R-RFVR-QN1E R-3HLK-J5VF
	for _, modeOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(modeOnly), func(t *testing.T) {
			f := newFixture(t)
			f.requireApply()
			name := "etc/systemd/system/user.slice.d/ikigenba.conf"
			original := f.read(name)
			if modeOnly {
				root, err := os.OpenRoot(f.root)
				if err != nil {
					t.Fatal(err)
				}
				if err := root.Chmod(name, 0o600); err != nil {
					t.Fatal(err)
				}
				_ = root.Close()
			} else {
				f.write(name, "old\n", 0o620)
			}
			if err := os.RemoveAll(filepath.Join(f.root, "etc/systemd/system/user-.slice.d")); err != nil {
				t.Fatal(err)
			}
			f.write("etc/systemd/system/user-.slice.d", "blocking file", 0o600)
			f.commands = nil
			changed, err := f.apply()
			if err == nil || changed != modeOnly {
				t.Fatalf("mode-only retained changed=%v error=%v", changed, err)
			}
			want := "old\n"
			mode := os.FileMode(0o620)
			if modeOnly {
				want = original
				mode = 0o644
			}
			if f.read(name) != want || f.mode(name) != mode {
				t.Fatal("pool left unreloaded candidate")
			}
			for _, c := range f.commands {
				if c == "systemctl daemon-reload" {
					t.Fatal("reload after filesystem failure")
				}
			}
			f.noTemporary()
		})
	}
	f := newFixture(t)
	if err := os.RemoveAll(filepath.Join(f.root, "etc/systemd")); err != nil {
		t.Fatal(err)
	}
	_, err := f.apply()
	if err == nil {
		t.Fatal("missing required parent accepted")
	}
	if _, err := os.Lstat(filepath.Join(f.root, "etc/systemd")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created forbidden ancestors")
	}
}

func TestFail2banModeOnlyFailureReportsRetainedChange(t *testing.T) {
	// R-3ITG-WXM4 R-RFVR-QN1E R-SD1E-M02J
	f := newFixture(t)
	f.requireApply()
	root, err := os.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := root.Chmod("etc/fail2ban/jail.d/ikigenba.local", 0o600); err != nil {
		t.Fatal(err)
	}
	f.fail = "systemctl is-enabled fail2ban.service"
	f.spawnErr = errors.New("unavailable")
	changed, err := f.apply()
	var ce *host.CommandError
	if !changed || !errors.As(err, &ce) || f.mode("etc/fail2ban/jail.d/ikigenba.local") != 0o644 {
		t.Fatalf("retained mode changed=%v err=%v", changed, err)
	}
}

func TestApplyReplacesWholeFilesWithoutChangingOpenReaders(t *testing.T) {
	// R-RC82-LBTB R-SALL-UGL5 R-3AA6-8JF9 R-3CPZ-02WN R-3DXV-DUNC R-R4WO-APD5 R-98VK-1BB5
	f := newFixture(t)
	f.requireApply()
	names := []string{"etc/systemd/system/user.slice.d/ikigenba.conf", "etc/systemd/system/user-.slice.d/ikigenba.conf", "etc/ssh/sshd_config.d/10-ikigenba.conf", "etc/fail2ban/jail.d/ikigenba.local", "etc/dnf/vars/releasever", "etc/dnf/automatic.conf", "etc/sudoers.d/ikigenba-accounts"}
	readers := map[string]*os.File{}
	previous := map[string]os.FileInfo{}
	filesystem, err := os.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	for _, name := range names {
		if err := filesystem.Chmod(name, 0o600); err != nil {
			t.Fatal(err)
		}
		f.write(name, "previous "+name+"\n", 0o600)
		reader, err := filesystem.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = reader.Close() }()
		readers[name] = reader
		previous[name], err = reader.Stat()
		if err != nil {
			t.Fatal(err)
		}
	}
	f.hook = func(_ host.Command) {
		_ = filepath.WalkDir(f.root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(d.Name(), "ikigenba-tmp") {
				if strings.Contains(name, "sudoers.d") {
					if !strings.Contains(d.Name(), ".") {
						t.Fatal("sudo reads temporary")
					}
				} else if strings.HasSuffix(name, ".conf") || strings.HasSuffix(name, ".local") {
					t.Fatal("daemon reads temporary")
				}
			}
			return nil
		})
	}
	f.requireApply()
	for _, name := range names {
		after, err := os.Lstat(filepath.Join(f.root, name))
		if err != nil || os.SameFile(previous[name], after) {
			t.Fatalf("in-place publication %s", name)
		}
		old, err := io.ReadAll(readers[name])
		if err != nil || string(old) != "previous "+name+"\n" {
			t.Fatalf("open reader changed for %s: %q %v", name, old, err)
		}
		if f.read(name) == string(old) {
			t.Fatal("candidate not published")
		}
	}
	f.noTemporary()
}

func TestEnsureAccountErrorIsReturnedDirectlyAndStatRejectsMalformedOutput(t *testing.T) {
	// R-ZB1F-CHGU R-RFVR-QN1E R-RH3O-4ES3
	f := newFixture(t)
	f.account = true
	calls := 0
	env := f.env()
	base := env.Execute
	env.Execute = func(ctx context.Context, c host.Command) (host.Result, error) {
		if c.Name == "id" && c.Args[0] == "--user" {
			calls++
			if calls == 2 {
				return f.fault, nil
			}
		}
		return base(ctx, c)
	}
	changed, err := Apply(context.Background(), env)
	var ce *host.CommandError
	ok := errors.As(err, &ce)
	if changed || !ok || reflect.TypeOf(err) != reflect.TypeOf(ce) || ce.Label != "inspect ikigenba account" || !reflect.DeepEqual(ce.Result, f.fault) {
		t.Fatalf("EnsureAccount error %#v changed=%v", err, changed)
	}
	for _, out := range []string{"", "root\n", "root:root\nextra:line\n", "root:root:root\n"} {
		f := newFixture(t)
		f.mkdir("etc/ikigenba", 0o700)
		f.requireApply()
		f.mkdir("etc/ikigenba", 0o755)
		f.commands = nil
		env := f.env()
		base := env.Execute
		env.Execute = func(ctx context.Context, c host.Command) (host.Result, error) {
			if c.Name == "stat" {
				f.commands = append(f.commands, "stat")
				return host.Result{Stdout: []byte(out)}, nil
			}
			return base(ctx, c)
		}
		changed, err := Apply(context.Background(), env)
		if changed || err == nil || !strings.Contains(err.Error(), "/etc/ikigenba") || strings.Contains(err.Error(), f.root) || f.mode("etc/ikigenba") != 0o755 || f.commands[len(f.commands)-1] != "stat" {
			t.Fatalf("malformed stat %q: %v %v", out, changed, err)
		}
	}
}
