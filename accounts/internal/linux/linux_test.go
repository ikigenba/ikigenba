package linux_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

var _ machine.Machine = (*linux.Host)(nil)

func TestLinuxSeam(t *testing.T) {
	// R-XUKR-ZUNU R-BNWV-O81H R-BP4S-1ZS6 R-BQCO-FRIV
	var host machine.Machine = linux.New(linux.Env{
		Root:    t.TempDir(),
		Execute: func(context.Context, linux.Command) (linux.Result, error) { return linux.Result{}, nil },
		Now:     func() time.Time { return time.Unix(0, 0) },
		Sleep:   func(time.Duration) {},
		Fetch:   func(context.Context, string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil },
		Stderr:  io.Discard,
	})
	if host == (*linux.Host)(nil) {
		t.Fatal("New returned a nil host")
	}
	if linux.ShellGroup != "shell" || linux.KeysDir != "/etc/ssh/authorized_keys" ||
		linux.SkelDir != "/var/lib/accountsd/skel" || linux.DataDir != "/data" || linux.HostKeyDir != "/data/ssh" ||
		linux.DaemonPath != "/usr/local/bin/accountsd" || linux.PoolSlice != "acct.slice" || linux.MaxCapture != 64<<10 ||
		linux.ChownBatch != 100 || linux.MaxJournalLine != 64<<10 || linux.UpdatesInterval != time.Hour {
		t.Fatal("host seam constants differ from the declared values")
	}
	if got := linux.AccountSlice("one-a_b"); got != `acct-one\x2da_b.slice` {
		t.Fatalf("AccountSlice = %q", got)
	}
	if got := linux.PersonSlice("one-a_b", "two-a_b"); got != `acct-one\x2da_b-two\x2da_b.slice` {
		t.Fatalf("PersonSlice = %q", got)
	}
	injected := errors.New("missing program")
	cause := &linux.CommandError{Label: "program", Result: linux.Result{ExitCode: 9}, Err: injected}
	if cause.Error() != "program: "+injected.Error() || !errors.Is(cause, injected) {
		t.Fatalf("CommandError with cause = %v", cause)
	}
	cause.Err = nil
	if cause.Error() != "program: exit status 9" || cause.Unwrap() != nil {
		t.Fatalf("CommandError with status = %v", cause)
	}
}

func TestExec(t *testing.T) {
	// R-SCRB-B1BJ
	env := linux.Real()
	if env.Root != "/" || reflect.ValueOf(env.Execute).Pointer() != reflect.ValueOf(linux.Exec).Pointer() ||
		reflect.ValueOf(env.Now).Pointer() != reflect.ValueOf(time.Now).Pointer() ||
		reflect.ValueOf(env.Sleep).Pointer() != reflect.ValueOf(time.Sleep).Pointer() ||
		reflect.ValueOf(env.Fetch).Pointer() != reflect.ValueOf(linux.Get).Pointer() || env.Stderr != os.Stderr {
		t.Fatal("Real returned unexpected seams")
	}
	script := filepath.Join(t.TempDir(), "probe.sh")
	const scriptBody = `IFS= read -r line
printf '%s\n%s\n' "$line" "$1"
printf '%s\n' "$line" >&2
i=0
while [ "$i" -lt 200 ]; do
  printf '%0400d' 0
  printf '%0400d' 1 >&2
  i=$((i + 1))
done
exit 19
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o600); err != nil {
		t.Fatal(err)
	}
	const input = "test line\n"
	const literal = "`false` $(false) ; *"
	var stream bytes.Buffer
	for _, streamed := range []bool{false, true} {
		c := linux.Command{Name: "/bin/sh", Args: []string{script, literal}, Stdin: strings.NewReader(input)}
		if streamed {
			c.Stdout = &stream
		}
		got, err := linux.Exec(t.Context(), c)
		if err != nil || got.ExitCode != 19 || len(got.Stderr) != linux.MaxCapture ||
			!bytes.HasPrefix(got.Stderr, []byte(input)) {
			t.Fatalf("Exec: error %v, status %d, stderr size %d", err, got.ExitCode, len(got.Stderr))
		}
		prefix := []byte(input + literal + "\n")
		if streamed {
			if len(got.Stdout) != 0 || stream.Len() != len(prefix)+80_000 || !bytes.HasPrefix(stream.Bytes(), prefix) {
				t.Fatal("command stdout was not sent whole to injected writer")
			}
		} else if len(got.Stdout) != linux.MaxCapture || !bytes.HasPrefix(got.Stdout, prefix) {
			t.Fatal("command stdout did not retain the correct capped prefix")
		}
	}
	if _, err := linux.Exec(t.Context(), linux.Command{Name: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Fatal("Exec accepted a path with no program")
	}
}

func TestExecUsesFixedPath(t *testing.T) {
	// R-XVSO-DMEJ
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	name := "accountsd-path-probe-" + filepath.Base(filepath.Dir(dir))
	script := filepath.Join(dir, name)
	marker := filepath.Join(dir, "caller-path-stub-ran")
	const body = `#!/bin/sh
if [ "$#" -ne 0 ]; then
  printf 'called\n' > "$1"
fi
printf '%s\n' "$PATH"
`
	if err := root.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Chmod(name, 0o700); err != nil {
		t.Fatal(err)
	}
	const want = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin\n"
	envScript := filepath.Join(dir, "environment.sh")
	const envBody = "printf '%s\\n' \"$PATH\" \"$ACCOUNTSD_EXEC_TEST\" \"$ACCOUNTSD_ADDED\"\n"
	if err := os.WriteFile(envScript, []byte(envBody), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := linux.Exec(t.Context(), linux.Command{
		Name: "/bin/sh", Args: []string{envScript},
		Env: []string{"ACCOUNTSD_EXEC_TEST=first", "ACCOUNTSD_EXEC_TEST=last", "ACCOUNTSD_ADDED=added"},
	})
	if err != nil || result.ExitCode != 0 || string(result.Stdout) != want+"last\nadded\n" {
		t.Fatalf("Exec environment probe = %+v, %v", result, err)
	}
	for _, callerPath := range []string{dir, ""} {
		t.Setenv("PATH", callerPath)
		if got, err := linux.Exec(t.Context(), linux.Command{Name: name, Args: []string{marker}}); err == nil {
			t.Fatalf("Exec found %q with caller PATH %q: %+v", name, callerPath, got)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("caller PATH stub marker was not absent: %v", err)
		}
		got, err := linux.Exec(t.Context(), linux.Command{Name: script})
		if err != nil || got.ExitCode != 0 || string(got.Stdout) != want {
			t.Fatalf("Exec with caller PATH %q returned PATH %q, status %d, error %v", callerPath, got.Stdout, got.ExitCode, err)
		}
	}
	for _, invalid := range []string{"PATH=/tmp", "MISSING_EQUAL", "=empty", "NUL=bad\x00value"} {
		if _, err := linux.Exec(t.Context(), linux.Command{Name: script, Args: []string{marker}, Env: []string{invalid}}); err == nil {
			t.Fatalf("Exec accepted invalid environment entry %q", invalid)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Exec started a command with invalid environment entry %q", invalid)
		}
	}
}

type endlessBytes struct{ read int }

func (b *endlessBytes) Read(p []byte) (int, error) {
	b.read += len(p)
	clear(p)
	return len(p), nil
}

func TestInstallDaemon(t *testing.T) {
	// R-D5KG-I4O1 R-XP6I-YEOF
	f := newHardeningFixture(t)
	const staging = "/usr/local/bin/.accountsd.new"
	const body = "\x7fELFhost replacement"
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body)))
	over := new(endlessBytes)
	for _, tc := range []struct {
		input io.Reader
		want  error
	}{
		{over, machine.ErrTooLarge},
		{strings.NewReader("plain text"), machine.ErrNotExecutable},
		{strings.NewReader(body + "!"), machine.ErrDigestMismatch},
	} {
		if err := f.host.InstallDaemon(t.Context(), tc.input, digest); !errors.Is(err, tc.want) {
			t.Fatalf("InstallDaemon = %v, want %v", err, tc.want)
		}
		f.expectFile(linux.DaemonPath, "daemon\n", 0o644)
		f.expectAbsent(staging)
	}
	if over.read != protocol.MaxBinaryBytes+1 {
		t.Fatalf("oversize read consumed %d bytes", over.read)
	}
	if err := f.host.InstallDaemon(t.Context(), strings.NewReader(body), digest); err != nil {
		t.Fatal(err)
	}
	f.expectFile(linux.DaemonPath, body, 0o755)
	f.expectAbsent(staging)
}

func TestInspectHost(t *testing.T) {
	// R-5DW4-003H R-D0OU-Z1P9 R-D1WR-CTFY R-D34N-QL6N R-D4CK-4CXC R-XP6I-YEOF
	f := newHardeningFixture(t)
	const cgroups = "/sys/fs/cgroup/acct.slice/"
	for path, text := range map[string]string{
		"/etc/os-release":            "NAME=ignored\nPRETTY_NAME=\"Example Host OS\"\n",
		"/proc/sys/kernel/osrelease": " host-kernel \n",
		"/proc/uptime":               "29.5 5\n",
		"/proc/loadavg":              "0.50 0.25 0.125 one two\n",
		"/proc/meminfo":              "MemTotal: 30 kB\nMemAvailable: 20 kB\nSwapTotal: 10 kB\nSwapFree: 1 kB\n",
		"/proc/stat":                 "cpu  10 0 0 30 10 0 0 0 0 0\n",
		cgroups + linux.AccountSlice("zebra") + "/memory.current":                 "256\n",
		cgroups + linux.AccountSlice("zebra") + "/cpu.stat":                       "usage_usec 1000\n",
		cgroups + linux.AccountSlice("zebra") + "/active.scope/cgroup.procs":      "1\n",
		cgroups + linux.AccountSlice("zebra") + "/deep/active.scope/cgroup.procs": "2\n",
		"/data/ordinary": "same device\n",
	} {
		f.put(path, text, 0o600)
	}
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.FixedZone("test", 3600))
	var updates, restarts, journals int
	var readFailed []string
	state := protocol.DesiredState{Protocol: 1, Host: "example", Version: 3, Daemon: "digest", Accounts: []protocol.Account{
		{Slug: "zebra", State: protocol.AccountActive}, {Slug: "alpaca", State: protocol.AccountActive}, {Slug: "gold", State: protocol.AccountRemoved},
	}, Users: []protocol.User{{Name: "amy"}, {Name: "whit"}}}
	host := linux.New(linux.Env{Root: f.root, Now: func() time.Time { return now }, Execute: func(_ context.Context, c linux.Command) (linux.Result, error) {
		switch c.Name {
		case "systemctl":
			readFailed = append(readFailed, strings.Join(c.Args, " "))
			lines := make([]string, 0, protocol.MaxFailedUnits)
			for i := protocol.MaxFailedUnits; i > 0; i-- {
				lines = append(lines, fmt.Sprintf("n%03d.service failed\n", i))
			}
			return hostAnswer(strings.Join(lines, "") + "excluded.service extra line\n"), nil
		case "dnf":
			if strings.Join(c.Args, " ") == "-q updateinfo list --security --available" {
				if c.Stdout == nil {
					t.Fatal("updateinfo did not stream output")
				}
				updates++
				_, err := io.WriteString(c.Stdout, "\nsecurity example-1\n\nsecurity example-2")
				return linux.Result{}, err
			}
			if strings.Join(c.Args, " ") != "needs-restarting -r" {
				t.Fatalf("unexpected dnf arguments: %v", c.Args)
			}
			restarts++
			return linux.Result{ExitCode: 1}, nil
		case "journalctl":
			base := []string{"--unit=sshd.service", "--output=json", "--no-pager", "--quiet", "--grep=^Accepted publickey for "}
			journals++
			if journals > 1 {
				base = append(base, "--after-cursor=last")
			}
			if !reflect.DeepEqual(c.Args, base) || c.Stdout == nil {
				t.Fatalf("journalctl = %+v", c)
			}
			if journals == 1 {
				var output strings.Builder
				for _, line := range []string{
					`{"__CURSOR":"001","MESSAGE":"Accepted publickey for amy from test","__REALTIME_TIMESTAMP":"8000001"}`,
					`{"__CURSOR":"002","MESSAGE":"Accepted publickey for amy from test","__REALTIME_TIMESTAMP":"7000000"}`,
					`{"__CURSOR":"003","MESSAGE":"Accepted publickey for unlisted from test","__REALTIME_TIMESTAMP":"10000000"}`,
					`{"__CURSOR":"long","MESSAGE":"Accepted publickey for amy from ` + strings.Repeat("a", linux.MaxJournalLine) + `","__REALTIME_TIMESTAMP":"11000000"}`,
					`{"__CURSOR":"last","MESSAGE":"Accepted publickey for whit from test","__REALTIME_TIMESTAMP":"9000001"}`,
				} {
					output.WriteString(line + "\n")
				}
				if _, err := io.WriteString(c.Stdout, output.String()); err != nil {
					return linux.Result{}, err
				}
			}
			return linux.Result{ExitCode: 1}, nil
		case "fail2ban-client":
			return linux.Result{}, nil
		}
		t.Fatalf("unexpected command %s", c.Name)
		return linux.Result{}, nil
	}})
	got, err := host.Inspect(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	var disk syscall.Statfs_t
	if err := syscall.Statfs(f.root, &disk); err != nil {
		t.Fatal(err)
	}
	diskSize := new(big.Int).SetUint64(disk.Blocks)
	diskSize.Mul(diskSize, big.NewInt(disk.Bsize))
	if got.OS != "Example Host OS" || got.Kernel != "host-kernel" ||
		got.Booted != now.Add(-29500*time.Millisecond).UTC().Truncate(time.Second) ||
		got.Load != [3]float64{0.5, 0.25, 0.125} || got.CPUPercent != 0 ||
		got.MemoryUsed != 10<<10 || got.MemoryAvailable != 20<<10 || got.SwapUsed != 9<<10 || got.SwapTotal != 10<<10 ||
		strconv.FormatInt(got.Root.SizeBytes, 10) != diskSize.String() ||
		strconv.FormatInt(got.Root.InodesTotal, 10) != strconv.FormatUint(disk.Files, 10) ||
		got.Root.UsedBytes < 0 || got.Root.InodesUsed < 0 || got.Data != (protocol.Disk{}) || got.Sessions != 2 ||
		!reflect.DeepEqual(got.Slices, []protocol.Slice{{Account: "alpaca"}, {Account: "zebra"}}) ||
		got.Updates != 2 || !got.Reboot || updates != 1 || restarts != 1 ||
		len(got.FailedUnits) != protocol.MaxFailedUnits || got.FailedUnits[0] != "n001.service" ||
		got.FailedUnits[len(got.FailedUnits)-1] != fmt.Sprintf("n%03d.service", protocol.MaxFailedUnits) ||
		readFailed[0] != "list-units --state=failed --plain --no-legend --no-pager" ||
		!reflect.DeepEqual(got.SignIns, []protocol.SignIn{{User: "amy", At: time.Unix(8, 0).UTC()}, {User: "whit", At: time.Unix(9, 0).UTC()}}) ||
		got.Protocol != 0 || got.Attempted != 0 || got.Applied != 0 || got.Error != "" ||
		got.InstanceID != "" || got.Type != "" || got.AMI != "" || got.DaemonVersion != "" {
		t.Fatalf("first Inspect = %+v, dnf counts %d %d, failed queries %v", got, updates, restarts, readFailed)
	}
	now = now.Add(time.Second)
	f.put("/proc/stat", "cpu  20 0 0 35 15 0 0 0 0 0\n", 0o600)
	f.put(cgroups+linux.AccountSlice("zebra")+"/cpu.stat", "usage_usec 501000\n", 0o600)
	state.Users = state.Users[1:]
	got, err = host.Inspect(t.Context(), state)
	if err != nil || got.CPUPercent != 50 || got.Updates != 2 || !got.Reboot || updates != 1 || restarts != 1 ||
		got.Slices[1] != (protocol.Slice{Account: "zebra", CPUPercent: 50, MemoryBytes: 256}) ||
		!reflect.DeepEqual(got.SignIns, []protocol.SignIn{{User: "whit", At: time.Unix(9, 0).UTC()}}) {
		t.Fatalf("second Inspect = %+v, %v, dnf counts %d %d", got, err, updates, restarts)
	}
	now = now.Add(linux.UpdatesInterval)
	state.Users = append(state.Users, protocol.User{Name: "amy"})
	got, err = host.Inspect(t.Context(), state)
	if err != nil || updates != 2 || restarts != 2 || len(got.SignIns) != 1 || journals != 3 {
		t.Fatalf("third Inspect = %+v, %v, dnf counts %d %d, journal count %d", got, err, updates, restarts, journals)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := host.Inspect(ctx, state); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Inspect = %v", err)
	}
}

func assertUnchanged(t *testing.T, path string, previous fs.FileInfo) {
	t.Helper()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(previous, after) {
		t.Fatalf("rewrote unchanged file %s", path)
	}
}

type hostFile struct {
	bytes    []byte
	mode     fs.FileMode
	target   string
	uid, gid uint32
	inode    uint64
}

func snapshotHost(t *testing.T, path string) map[string]hostFile {
	t.Helper()
	output := map[string]hostFile{}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := filepath.WalkDir(path, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(full, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("missing ownership for %s", name)
		}
		value := hostFile{mode: info.Mode(), uid: stat.Uid, gid: stat.Gid, inode: stat.Ino}
		switch {
		case info.Mode().IsRegular():
			value.bytes, err = root.ReadFile(strings.TrimPrefix(name, "/"))
		case info.Mode()&fs.ModeSymlink != 0:
			value.target, err = root.Readlink(strings.TrimPrefix(name, "/"))
		}
		output[name] = value
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return output
}

type applyHostFile struct {
	hostFile
	modified int64
}

func snapshotApplyHost(t *testing.T, root string) map[string]applyHostFile {
	t.Helper()
	files := snapshotHost(t, root)
	output := make(map[string]applyHostFile, len(files))
	for path, file := range files {
		value := applyHostFile{hostFile: file}
		if file.mode.IsRegular() || path == "/etc/sudoers.d" {
			info, err := os.Lstat(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			value.modified = info.ModTime().UnixNano()
		}
		output[path] = value
	}
	return output
}

func assertHostChanges(t *testing.T, before, after map[string]applyHostFile, managed []string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, path := range managed {
		allowed[path] = true
		for dir := filepath.Dir(path); dir != "/"; dir = filepath.Dir(dir) {
			if _, exists := before[dir]; !exists {
				allowed[dir] = true
			}
		}
	}
	for path, prior := range before {
		if !allowed[path] && !reflect.DeepEqual(after[path], prior) {
			t.Errorf("unexpected change at %s", path)
		}
	}
	for path := range after {
		if _, present := before[path]; !present && !allowed[path] {
			t.Errorf("unexpected new path %s", path)
		}
	}
}

func TestApplyChangesOnlyManagedFiles(t *testing.T) {
	// R-ZWIA-KLSM
	f := newHardeningFixture(t)
	seedApplySudoers(f)
	before := snapshotApplyHost(t, f.root)
	commands := applyRecordingCommands(f, protocol.DesiredState{Host: "host04", Domain: "metaspot.org"})
	after := snapshotApplyHost(t, f.root)
	want := append(initialHardeningCommands(f), []string{"hostnamectl", "set-hostname", "host04.metaspot.org"})
	want = append(want, initialVolumeCommands(f)...)
	want = append(want, []string{"systemctl", "daemon-reload"})
	want = appendSecureCommands(f, want, after, linux.MCPHelper)
	expectApplyCommands(t, commands, want...)
	assertHostChanges(t, before, after, append(hardeningPaths(), "/var/log/journal", linux.SkelDir, linux.KeysDir,
		linux.SessionHelper, linux.MCPHelper, hostnameDropIn))
}

func seedApplySudoers(f *hardeningFixture) {
	f.t.Helper()
	f.put("/etc/sudoers", "root ALL=(ALL) ALL\n", 0o600)
	f.put("/etc/sudoers.d/retained", "other existing configuration\n", 0o600)
	stamp := time.Unix(1, 0)
	for _, path := range []string{"/etc/sudoers", "/etc/sudoers.d", "/etc/sudoers.d/retained"} {
		if err := os.Chtimes(f.path(path), stamp, stamp); err != nil {
			f.t.Fatal(err)
		}
	}
}

func applyRecordingCommands(f *hardeningFixture, state protocol.DesiredState) []linux.Command {
	f.t.Helper()
	var commands []linux.Command
	f.observe = func(command linux.Command) {
		commands = append(commands, linux.Command{Name: command.Name, Args: slices.Clone(command.Args)})
	}
	f.apply(state)
	return commands
}

func expectApplyCommands(t *testing.T, got []linux.Command, want ...[]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("applied %d commands, want %d: %+v", len(got), len(want), got)
	}
	for i := range min(len(got), len(want)) {
		if got[i].Name != want[i][0] || !slices.Equal(got[i].Args, want[i][1:]) {
			t.Errorf("command %d = %q %q, want %q %q", i+1, got[i].Name, got[i].Args, want[i][0], want[i][1:])
		}
	}
}

func initialHardeningCommands(f *hardeningFixture) [][]string {
	return [][]string{
		{"rpm", "-q", "nftables"}, {"rpm", "-q", "fail2ban-server"}, {"rpm", "-q", "dnf-automatic"}, {"rpm", "-q", "git"},
		{"rpm", "-q", "cronie"}, {"rpm", "-q", "at"},
		{"systemctl", "daemon-reload"}, {"nft", "list", "table", "inet", "ikigenba_imds"},
		{"nft", "-f", f.path(hardenRules)}, {"systemctl", "is-enabled", "ikigenba-imds.service"},
		{"systemd-tmpfiles", "--create", "--prefix", f.path("/var/log/journal")},
		{"systemctl", "restart", "systemd-journald.service"}, {"journalctl", "--flush"},
		{"systemctl", "is-enabled", "dnf-automatic.timer"}, {"systemctl", "is-active", "dnf-automatic.timer"},
		{"systemctl", "is-enabled", "fail2ban.service"}, {"systemctl", "is-active", "fail2ban.service"},
		{"systemctl", "reload", "fail2ban.service"},
	}
}

func initialVolumeCommands(f *hardeningFixture) [][]string {
	return [][]string{
		{"lsblk", "--pairs", "--noheadings", "--output", "NAME,PKNAME,FSTYPE,MOUNTPOINT"},
		{"blockdev", "--getsize64", f.path("/dev/nvme0n1")},
		{"growpart", f.path("/dev/nvme0n1"), "1"},
		{"xfs_growfs", "-d", f.path("/")},
		{"lsblk", "--nodeps", "--pairs", "--noheadings", "--output", "NAME,SERIAL"},
	}
}

func hardeningPaths() []string {
	return []string{
		hardenRules, hardenIMDSUnit, hardenJournal,
		"/etc/dnf/vars/releasever", hardenUpdates, hardenJail,
		"/etc/systemd/system/" + linux.PoolSlice, "/etc/pam.d/sshd",
	}
}

func TestApplyChangesOnlyManagedFilesWithUsersAndData(t *testing.T) {
	// R-ZWIA-KLSM R-ZNYZ-W7LR R-XP6I-YEOF
	f := newHardeningFixture(t)
	const unitDir = "/etc/systemd/system/"
	const folder = "/srv/citron"
	const home = "/home/alice"
	const aliceKeys = linux.KeysDir + "/alice"
	const retiredKeys = linux.KeysDir + "/retired"
	const staleSlice = unitDir + "acct-pear-retired.slice"
	const sharedPAMPath = "/etc/pam.d/password-auth"
	seedApplySudoers(f)
	for _, path := range []string{
		"/etc/untouched", "/usr/local/bin/other", "/etc/systemd/system/unrelated.service",
		folder + "/.git/config", folder + "/project", home + "/private", "/srv/unlisted/project", "/home/unlisted/private", retiredKeys, staleSlice,
	} {
		f.put(path, "fixture for "+path, 0o664)
	}
	f.setMode(home, 0o755)
	f.put("/etc/group", "shell:x:990:alice,unlisted\ncitron:x:24000:alice\nalice:x:24001:\npear:x:24010:\n", 0o644)
	f.put("/etc/passwd", "citron:x:24000:24000::/srv/citron:/usr/sbin/nologin\nalice:x:24001:24001::/home/alice:/bin/bash\n"+
		"unlisted:x:24002:24002::/home/unlisted:/bin/bash\npear:x:24010:24010::/srv/pear:/usr/sbin/nologin\n", 0o644)
	f.put("/etc/shadow", "alice:!:1:::::1:\nunlisted:!:1::::::\n", 0o600)
	f.put("/etc/pam.d/sshd", "session include password-auth\n", 0o644)
	f.put(sharedPAMPath, "-session optional pam_systemd.so\n", 0o644)
	gitQuery := []string{"git", "config", "--file", f.path("/etc/gitconfig"), "--get-all", "safe.directory"}
	gitCall := strings.Join(gitQuery, " ")
	f.answer = func(call string) (linux.Result, bool) {
		return hostAnswer(folder + "\n"), call == gitCall
	}
	before := snapshotApplyHost(t, f.root)
	commands := applyRecordingCommands(f, protocol.DesiredState{
		Host: "host04", Domain: "metaspot.org",
		DataVolume: "vol-12345678", Unban: []string{"192.0.2.7"},
		Accounts: []protocol.Account{
			{Slug: "citron", GID: 24000, State: protocol.AccountActive},
			{Slug: "pear", GID: 24010, State: protocol.AccountRemoved},
		},
		Users: []protocol.User{
			{Name: "alice", Account: "citron", UID: 24001, State: protocol.UserActive},
			{Name: "retired", Account: "pear", UID: 24011, State: protocol.UserDeleted},
		},
	})
	after := snapshotApplyHost(t, f.root)
	volumeFiles := []string{unitDir + "data.mount", unitDir + "home.mount", unitDir + "srv.mount", sshUnitPath, sshDropInPath,
		unitDir + linux.AccountSlice("citron"), unitDir + linux.PersonSlice("citron", "alice"), aliceKeys}
	secureFirst := []string{linux.DaemonPath, linux.SessionHelper, linux.HostKeyDir, dataPrivateKey, dataPublicKey, linux.KeysDir}
	managed := slices.Concat(hardeningPaths(), volumeFiles, secureFirst,
		[]string{"/var/log/journal", linux.SkelDir, "/data/home", "/data/srv", folder, home, retiredKeys, staleSlice,
			sharedPAMPath, linux.MCPHelper, claudeRepoPath, linux.ClaudeStamp, hostnameDropIn}, claudeFilePaths())
	assertHostChanges(t, before, after, managed)

	dataDevice := f.path("/dev/nvme1n1")
	want := [][]string{
		{"usermod", "--expiredate", "1", "unlisted"},
		{"systemctl", "is-active", "user-24002.slice"},
		{"systemctl", "stop", "user-24002.slice"},
	}
	want = append(want, initialHardeningCommands(f)...)
	want = append(want, []string{"fail2ban-client", "set", "sshd", "unbanip", "192.0.2.7"})
	want = append(want, []string{"hostnamectl", "set-hostname", "host04.metaspot.org"})
	want = append(want, initialVolumeCommands(f)...)
	want = append(want,
		[]string{"blkid", "-p", "--output", "export", dataDevice}, []string{"systemctl", "daemon-reload"},
		[]string{"systemctl", "is-enabled", "data.mount"}, []string{"systemctl", "is-active", "data.mount"},
		[]string{"systemctl", "is-enabled", "home.mount"}, []string{"systemctl", "is-active", "home.mount"},
		[]string{"systemctl", "is-enabled", "srv.mount"}, []string{"systemctl", "is-active", "srv.mount"},
		[]string{"blockdev", "--getsize64", dataDevice}, []string{"xfs_growfs", "-d", f.path("/data")}, []string{"systemctl", "daemon-reload"},
		[]string{"sshd", "-t", "-f", f.path("/etc/ssh/sshd_config")}, []string{"systemctl", "reload", "sshd.service"}, []string{"systemctl", "daemon-reload"})
	want = appendSecureCommands(f, want, after, linux.MCPHelper)
	if before[folder].uid != 24000 || before[folder].gid != 24000 {
		want = append(want, []string{"chown", "citron:citron", f.path(folder)}, []string{"setfacl", "-d", "-m", "g::rwx,o::-", f.path(folder)})
	}
	want = append(want, gitQuery, []string{"usermod", "--expiredate", "", "alice"},
		[]string{"userdel", "pear"}, []string{"groupdel", "pear"})
	secureLater := slices.Concat(hardeningPaths(), volumeFiles, []string{dataPrivateKey, dataPublicKey, sharedPAMPath, hostnameDropIn})
	slices.Sort(secureLater)
	want = appendSecureCommands(f, want, after, slices.Concat(secureFirst, secureLater)...)
	want = appendSecureCommands(f, want, after, claudeDirectories...)
	want = appendSecureCommands(f, want, after, claudeFilePaths()...)
	want = append(want, strings.Fields(claudeKeyProbe))
	want = appendSecureCommands(f, want, after, claudeRepoPath)
	want = append(want, []string{"rpm", "-q", "claude-code"})
	if info := after[linux.ClaudeStamp]; info.uid != 0 || info.gid != 0 {
		found := slices.IndexFunc(commands, func(c linux.Command) bool {
			return stagedStampPath(f, c) != ""
		})
		if found < 0 {
			t.Fatal("did not prepare stamp ownership")
		}
		want = append(want, []string{"chown", "--no-dereference", "root:root", stagedStampPath(f, commands[found])})
	}
	want = append(want, strings.Fields(claudeUpgrade))
	expectApplyCommands(t, commands, want...)
}

func appendSecureCommands(f *hardeningFixture, want [][]string, after map[string]applyHostFile, paths ...string) [][]string {
	for _, path := range paths {
		if info := after[path]; info.uid != 0 || info.gid != 0 {
			want = append(want, []string{"chown", "--no-dereference", "root:root", f.path(path)})
		}
	}
	return want
}
