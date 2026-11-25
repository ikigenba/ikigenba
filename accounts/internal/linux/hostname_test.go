package linux_test

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	hostnameFile   = "/etc/hostname"
	hostnameDropIn = "/etc/cloud/cloud.cfg.d/99-ikigenba.cfg"
	hostnameConfig = "preserve_hostname: true\n"
	hostName       = "host04.metaspot.org"
	setHostName    = "hostnamectl set-hostname " + hostName
)

func nameState() protocol.DesiredState {
	return protocol.DesiredState{Protocol: 1, Host: "host04", Version: 1, Domain: "metaspot.org"}
}

// R-ZP6W-9ZCG.
func TestApplyRejectsMalformedHostnameAfterLocks(t *testing.T) {
	state := nameState()
	for _, domain := range []string{
		"Example.com", "-x.org", "x-.org", "x_1.org", "é.org", ".org", "org.",
		"x..org", "x org", strings.Repeat("x", 58),
	} {
		t.Run(domain, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put("/etc/group", "shell:x:990:alice\n", 0o644)
			f.put("/etc/passwd", "alice:x:24001:24001::/home/alice:/bin/bash\n", 0o644)
			f.put("/etc/shadow", "alice:!:1::::::\n", 0o600)
			f.put(hostnameFile, "original\n", 0o644)
			state.Domain = domain
			err := f.host.Apply(t.Context(), state)
			if !errors.Is(err, machine.ErrInvalidState) {
				t.Fatalf("Apply domain %q = %v", domain, err)
			}
			if len(f.commands) == 0 || f.commands[0] != "usermod --expiredate 1 alice" ||
				f.commands[len(f.commands)-1] != "systemctl reload fail2ban.service" {
				t.Fatalf("commands before refusal = %v", f.commands)
			}
			f.expectAbsent(hostnameDropIn)
			f.expectFile(hostnameFile, "original\n", 0o644)
			checkNoHostnameCommand(t, f.commands)
		})
	}
	t.Run("64 bytes", func(t *testing.T) {
		f := newHardeningFixture(t)
		state.Host, state.Domain = "a", "b-2."+strings.Repeat("x", 58)
		f.apply(state)
		if !slices.Contains(f.commands, "hostnamectl set-hostname "+state.Host+"."+state.Domain) {
			t.Fatalf("missing hostname command in %v", f.commands)
		}
	})
}

// R-ZNYZ-W7LR R-ZQES-NR35.
func TestApplyHostnameWithoutDataOrUsers(t *testing.T) {
	f := newHardeningFixture(t)
	f.put("/etc/group", "", 0o644)
	f.put(hostnameFile, "ip-172-31-2-153.us-east-2.compute.internal\n", 0o644)
	f.observe = func(c linux.Command) {
		if c.Name == "hostnamectl" {
			f.expectFile(hostnameDropIn, hostnameConfig, 0o644)
			f.expectMode(linux.KeysDir, 0o755)
			f.expectMode(linux.SkelDir, 0o755)
		}
	}
	f.apply(nameState())
	group := slices.Index(f.commands, "groupadd --system shell")
	hostname := slices.Index(f.commands, setHostName)
	grow := slices.Index(f.commands, "lsblk --pairs --noheadings --output NAME,PKNAME,FSTYPE,MOUNTPOINT")
	if group < 0 || hostname != group+1 || grow != hostname+1 {
		t.Fatalf("hostname command ran out of order: %v", f.commands)
	}
	f.expectMode(filepath.Dir(hostnameDropIn), 0o755)
	before := f.stat(hostnameDropIn)
	f.setMode(hostnameDropIn, 0o600)
	f.put(hostnameFile, " \t\r\n"+hostName+" \t\r\n", 0o644)
	f.commands = nil
	f.apply(nameState())
	f.expectFile(hostnameDropIn, hostnameConfig, 0o644)
	assertUnchanged(t, f.path(hostnameDropIn), before)
	checkNoHostnameCommand(t, f.commands)
}

// R-ZQES-NR35.
func TestHostnameDropInIsStagedWithoutConfigSuffix(t *testing.T) {
	f := newHardeningFixture(t)
	f.put(hostnameDropIn, "preserve_hostname: false\n", 0o644)
	dir := f.path(filepath.Dir(hostnameDropIn))
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(watch) }()
	if _, err := syscall.InotifyAddWatch(watch, dir, syscall.IN_CREATE|syscall.IN_MOVED_TO); err != nil {
		t.Fatal(err)
	}
	f.apply(nameState())
	var events [4096]byte
	n, err := syscall.Read(watch, events[:])
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Base(hostnameDropIn)
	var staged, renamed bool
	for remaining := events[:n]; len(remaining) > 0; {
		var event syscall.InotifyEvent
		size, err := binary.Decode(remaining, binary.NativeEndian, &event)
		if err != nil || len(remaining)-size < int(event.Len) {
			t.Fatalf("invalid directory event: %v", err)
		}
		end := size + int(event.Len)
		name := strings.TrimRight(string(remaining[size:end]), "\x00")
		if event.Mask&syscall.IN_CREATE != 0 {
			if strings.HasSuffix(name, ".cfg") {
				t.Fatalf("staged cloud-init config at %q", name)
			}
			staged = true
		}
		if name == cfg && event.Mask&syscall.IN_MOVED_TO != 0 {
			renamed = true
		}
		remaining = remaining[end:]
	}
	entries, err := os.ReadDir(dir)
	if err != nil || !staged || !renamed || len(entries) != 1 || entries[0].Name() != cfg {
		t.Fatalf("staged %t, renamed %t, remaining %v, read error %v", staged, renamed, entries, err)
	}
	f.expectFile(hostnameDropIn, hostnameConfig, 0o644)
}

// R-ZRMP-1ITU.
func TestMissingDomainLeavesHostnameAlone(t *testing.T) {
	f := newHardeningFixture(t)
	state := nameState()
	state.Domain = ""
	f.apply(state)
	f.expectAbsent(hostnameDropIn)
	f.expectAbsent(hostnameFile)
	checkNoHostnameCommand(t, f.commands)
	f.put(hostnameDropIn, "another config\n", 0o600)
	f.put(hostnameFile, "existing name\n", 0o600)
	config, host := f.stat(hostnameDropIn), f.stat(hostnameFile)
	f.commands = nil
	f.apply(state)
	assertUnchanged(t, f.path(hostnameDropIn), config)
	assertUnchanged(t, f.path(hostnameFile), host)
	f.expectFile(hostnameDropIn, "another config\n", 0o600)
	f.expectFile(hostnameFile, "existing name\n", 0o600)
	checkNoHostnameCommand(t, f.commands)
}

// R-ZSUL-FAKJ.
func TestApplyHostnameFailuresStopAndRetry(t *testing.T) {
	t.Run("cannot write config", func(t *testing.T) {
		f := newHardeningFixture(t)
		if err := os.MkdirAll(f.path(hostnameDropIn), 0o700); err != nil {
			t.Fatal(err)
		}
		err := f.host.Apply(t.Context(), nameState())
		if err == nil || !strings.Contains(err.Error(), hostnameDropIn) {
			t.Fatalf("Apply with unwritable drop-in = %v", err)
		}
		checkNoHostnameCommand(t, f.commands)
		if got := f.commands[len(f.commands)-1]; got != "systemctl reload fail2ban.service" {
			t.Fatalf("last command after drop-in failure = %q", got)
		}
	})
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "exit status"},
		{name: "cannot start", err: errors.New("cannot start hostnamectl")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			var attempts int
			result := linux.Result{ExitCode: 1, Stderr: []byte("fixture status")}
			execute := f.env.Execute
			f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
				got, err := execute(ctx, c)
				if c.Name == "hostnamectl" {
					attempts++
					if attempts == 1 {
						return result, tt.err
					}
				}
				return got, err
			}
			f.host = linux.New(f.env)
			err := f.host.Apply(t.Context(), nameState())
			var ce *linux.CommandError
			if !errors.As(err, &ce) || ce.Label != "hostnamectl" ||
				!reflect.DeepEqual(ce.Result, result) || !errors.Is(ce.Err, tt.err) {
				t.Fatalf("Apply with failing hostnamectl = %v, command error %+v", err, ce)
			}
			if got := f.commands[len(f.commands)-1]; got != setHostName {
				t.Fatalf("last command after hostnamectl failed = %q", got)
			}
			f.expectFile(hostnameDropIn, hostnameConfig, 0o644)
			f.apply(nameState())
			if attempts != 2 {
				t.Fatalf("hostnamectl attempts after retry = %d", attempts)
			}
		})
	}
}

// R-ZU2H-T2B8 R-ZQES-NR35.
func TestHostnameReadIsBoundedAndTrimsOnlyASCIIWhitespace(t *testing.T) {
	limit := hostName + strings.Repeat(" ", linux.MaxCapture-len(hostName))
	for _, tt := range []struct {
		name     string
		contents string
		want     string
	}{
		{name: "missing"},
		{name: "unreadable"},
		{name: "trimmed", contents: " \t\n\r" + hostName + "\r\n\t ", want: hostName},
		{name: "nonbreaking space", contents: " \u00a0" + hostName + "\n", want: "\u00a0" + hostName},
		{name: "at limit", contents: limit, want: hostName},
		{name: "over limit", contents: limit + " "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			switch tt.name {
			case "unreadable":
				if err := os.Mkdir(f.path(hostnameFile), 0o700); err != nil {
					t.Fatal(err)
				}
			case "missing":
			default:
				f.put(hostnameFile, tt.contents, 0o644)
			}
			report, err := f.host.Inspect(t.Context(), nameState())
			if err != nil || report.Hostname != tt.want {
				t.Fatalf("inspect hostname = %q, %v, want %q", report.Hostname, err, tt.want)
			}
			f.commands = nil
			f.apply(nameState())
			if got := slices.Contains(f.commands, setHostName); got != (tt.want != hostName) {
				t.Fatalf("hostnamectl ran = %t for reported hostname %q", got, tt.want)
			}
		})
	}
}

func checkNoHostnameCommand(t *testing.T, commands []string) {
	t.Helper()
	for _, command := range commands {
		if command == "hostnamectl" || strings.HasPrefix(command, "hostnamectl ") {
			t.Fatalf("unexpected command %q", command)
		}
	}
}
