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
	// R-BMOZ-AGAS R-BNWV-O81H R-BP4S-1ZS6 R-BQCO-FRIV
	env := linux.Real()
	if env.Root != "/" || env.Execute == nil || env.Now == nil {
		t.Fatalf("Real returns root %q and incomplete seams", env.Root)
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
	// R-BRKK-TJ9K
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

type endlessBytes struct{ read int }

func (b *endlessBytes) Read(p []byte) (int, error) {
	b.read += len(p)
	clear(p)
	return len(p), nil
}

func TestInstallDaemon(t *testing.T) {
	// R-D5KG-I4O1
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
	// R-BSSH-7B09 R-5DW4-003H R-D0OU-Z1P9 R-D1WR-CTFY R-D34N-QL6N R-D4CK-4CXC
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

func TestApplyChangesOnlyManagedFiles(t *testing.T) {
	// R-SKZ3-MFVV
	f := newHardeningFixture(t)
	f.put("/etc/sudoers", "root ALL=(ALL) ALL\n", 0o600)
	f.put("/etc/sudoers.d/retained", "other existing configuration\n", 0o600)
	type file struct {
		bytes  []byte
		mode   fs.FileMode
		target string
	}
	snapshot := func() map[string]file {
		output := map[string]file{}
		root, err := os.OpenRoot(f.root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = root.Close() }()
		if err := filepath.WalkDir(f.root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := strings.TrimPrefix(path, f.root)
			info, err := d.Info()
			if err != nil {
				return err
			}
			value := file{mode: info.Mode()}
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
	before := snapshot()
	f.apply(protocol.DesiredState{})
	f.expectCalls(
		"rpm -q nftables", "rpm -q fail2ban-server", "rpm -q dnf-automatic",
		"systemctl daemon-reload", "nft list table inet ikigenba_imds",
		"nft -f "+f.path(hardenRules), "systemctl is-enabled ikigenba-imds.service",
		"systemd-tmpfiles --create --prefix "+f.path("/var/log/journal"),
		"systemctl restart systemd-journald.service", "journalctl --flush",
		"systemctl is-enabled dnf-automatic.timer", "systemctl is-active dnf-automatic.timer",
		"systemctl is-enabled fail2ban.service", "systemctl is-active fail2ban.service",
		"systemctl reload fail2ban.service",
		"lsblk --pairs --noheadings --output NAME,PKNAME,FSTYPE,MOUNTPOINT",
		"blockdev --getsize64 "+f.path("/dev/nvme0n1"),
		"growpart "+f.path("/dev/nvme0n1")+" 1",
		"xfs_growfs -d "+f.path("/"),
		"lsblk --nodeps --pairs --noheadings --output NAME,SERIAL",
		"systemctl daemon-reload",
	)
	allowed := map[string]bool{"/etc/pam.d/sshd": true}
	for _, managed := range []string{
		hardenRules, hardenIMDSUnit, hardenJournal, "/var/log/journal",
		"/etc/dnf/vars/releasever", hardenUpdates, hardenJail, linux.KeysDir, linux.SkelDir,
		"/etc/systemd/system/" + linux.PoolSlice, linux.SessionHelper,
	} {
		for path := managed; path != "/"; path = filepath.Dir(path) {
			allowed[path] = true
		}
	}
	after := snapshot()
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
