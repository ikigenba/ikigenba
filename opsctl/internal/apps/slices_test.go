package apps_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var sliceNames = []string{"ikigenba.slice", "ikigenba-core.slice", "ikigenba-apps.slice", "nginx.service.d/ikigenba.conf"}

func slicePath(root, name string) string {
	return filepath.Join(root, "etc/systemd/system", name)
}

func sliceMeminfo(t *testing.T, root, data string) {
	t.Helper()
	writeFixture(t, filepath.Join(root, "proc/meminfo"), []byte(data), 0o644)
}

func sliceRecorder(root string, commands *[]host.Command) host.Env {
	return host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		*commands = append(*commands, command)
		return host.Result{}, nil
	}}
}

func assertSliceCommands(t *testing.T, commands []host.Command, args ...[]string) {
	t.Helper()
	if len(commands) != len(args) {
		t.Fatalf("commands = %#v; want %v", commands, args)
	}
	for i := 0; i < len(commands) && i < len(args); i++ {
		command := commands[i]
		if command.Name != "systemctl" || !reflect.DeepEqual(command.Args, args[i]) {
			t.Fatalf("command %d = %#v; want systemctl %v", i, command, args[i])
		}
	}
}

// R-NN3P-UABM
func TestSetupSlicesDirectoryModeIgnoresUmask(t *testing.T) {
	const childKey = "OPSCTL_SLICES_UMASK_CHILD"
	if os.Getenv(childKey) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		result, err := host.Exec(t.Context(), host.Command{
			Name: binary, Args: []string{"-test.run=^TestSetupSlicesDirectoryModeIgnoresUmask$"},
			Env: []string{childKey + "=1"},
		})
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("umask fixture: %v, exit %d\n%s%s", err, result.ExitCode, result.Stdout, result.Stderr)
		}
		return
	}
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	root := t.TempDir()
	sliceMeminfo(t, root, "MemTotal: 1954816 kB\n")
	var commands []host.Command
	env := sliceRecorder(root, &commands)
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	directory := slicePath(root, "nginx.service.d")
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("new directory mode: %v, %v", info, err)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	if err := filesystem.Chmod("etc/systemd/system/nginx.service.d", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("existing directory mode changed: %v, %v", info, err)
	}
}

// R-02VD-34HM R-NN3P-UABM
func TestSetupSlicesSizesAndExactFiles(t *testing.T) {
	for _, test := range []struct {
		meminfo string
		t, a, h int64
	}{
		{"MemTotal:       1954816 kB\n", 1536, 1024, 960},
		{"MemFree: 100 kB\nMemTotal: 3964928 kB\n", 3072, 2048, 1920},
		{"MemTotal: 40960 kB\n", 64, 64, 60},
		{"MemTotal: 122880 kB\n", 128, 64, 60},
		{"MemTotal: 204800 kB\n", 192, 128, 120},
		{"MemTotal: 9007199254740991 kB\n", 7036874417792, 4691249611840, 4398046511100},
	} {
		t.Run(strings.TrimSpace(test.meminfo), func(t *testing.T) {
			root := t.TempDir()
			sliceMeminfo(t, root, test.meminfo)
			var commands []host.Command
			if err := apps.SetupSlices(t.Context(), sliceRecorder(root, &commands)); err != nil {
				t.Fatal(err)
			}
			want := []string{
				fmt.Sprintf("[Unit]\nDescription=Ikigenba suite\n\n[Slice]\nCPUWeight=100\nMemoryMax=%dM\n", test.t),
				"[Unit]\nDescription=Ikigenba core\n\n[Slice]\nCPUWeight=300\n",
				fmt.Sprintf("[Unit]\nDescription=Ikigenba apps\n\n[Slice]\nCPUWeight=100\nMemoryMax=%dM\nMemoryHigh=%dM\n", test.a, test.h),
				"[Service]\nSlice=ikigenba-core.slice\nCPUWeight=100\nMemoryMax=128M\nMemoryLow=32M\n",
			}
			for i, name := range sliceNames {
				assertFile(t, slicePath(root, name), want[i])
				info, err := os.Stat(slicePath(root, name))
				if err != nil || info.Mode() != 0o644 {
					t.Fatalf("%s mode = %v, %v", name, info, err)
				}
			}
			info, err := os.Stat(slicePath(root, "nginx.service.d"))
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("drop-in directory mode = %v, %v", info, err)
			}
			assertSliceCommands(t, commands, []string{"daemon-reload"}, []string{"restart", "nginx"})
		})
	}
}

// R-IEQF-0ZI8
func TestSetupSlicesRerunsAndModeRepair(t *testing.T) {
	root := t.TempDir()
	sliceMeminfo(t, root, "MemTotal: 1954816 kB\n")
	var commands []host.Command
	env := sliceRecorder(root, &commands)
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	for _, name := range sliceNames {
		setTimeoutFileTimes(t, slicePath(root, name))
	}
	before := snapshotTree(t, root)
	commands = nil
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, root)) {
		t.Fatal("unchanged rerun touched files")
	}
	assertSliceCommands(t, commands)
	for _, name := range sliceNames {
		if err := os.Chmod(slicePath(root, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, root)) {
		t.Fatal("mode repair changed bytes or modification times, or did not restore modes")
	}
	assertSliceCommands(t, commands)
	sliceMeminfo(t, root, "MemTotal: 1954817 kB\n")
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	assertSliceCommands(t, commands)
	sliceMeminfo(t, root, "MemTotal: 3964928 kB\n")
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	assertSliceCommands(t, commands, []string{"daemon-reload"})
	for _, name := range sliceNames {
		info, err := os.Stat(slicePath(root, name))
		if err != nil {
			t.Fatal(err)
		}
		unchanged := name == "ikigenba-core.slice" || strings.HasPrefix(name, "nginx.")
		if info.ModTime().Equal(time.Unix(946684800, 0)) != unchanged {
			t.Fatalf("unexpected rewrite of %s", name)
		}
	}
	commands = nil
	writeFixture(t, slicePath(root, sliceNames[3]), []byte("old drop-in\n"), 0o600)
	if err := apps.SetupSlices(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	assertSliceCommands(t, commands, []string{"daemon-reload"}, []string{"restart", "nginx"})
}

// R-Y2EV-URH5
func TestSetupSlicesBadMeminfoHasNoEffects(t *testing.T) {
	for _, data := range []string{"", "MemFree: 1954816 kB\n", "MemTotal:1954816 kB\n", "MemTotal:\t1954816 kB\n", "MemTotal: 1954816 KB\n", "MemTotal: -1 kB\n", "MemTotal: 1.5 kB\n", "MemTotal: 1954816 kB trailing\n", "MemTotal: 9007199254740992 kB\n", "MemTotal: 999999999999999999999 kB\n", "MemTotal: 0 kB\n", "MemTotal: 40959 kB\n"} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			sliceMeminfo(t, root, data)
			assertBadSliceMeminfo(t, root)
		})
	}
	t.Run("missing", func(t *testing.T) { assertBadSliceMeminfo(t, t.TempDir()) })
	t.Run("unreadable", func(t *testing.T) {
		root := t.TempDir()
		mkdirAll(t, filepath.Join(root, "proc/meminfo"))
		assertBadSliceMeminfo(t, root)
	})
}

func assertBadSliceMeminfo(t *testing.T, root string) {
	t.Helper()
	writeFixture(t, slicePath(root, sliceNames[0]), []byte("existing slice"), 0o600)
	before := snapshotTree(t, root)
	var commands []host.Command
	err := apps.SetupSlices(t.Context(), sliceRecorder(root, &commands))
	if err == nil || !strings.Contains(err.Error(), "/proc/meminfo") {
		t.Fatalf("error = %v", err)
	}
	assertSliceCommands(t, commands)
	if !reflect.DeepEqual(before, snapshotTree(t, root)) {
		t.Fatal("invalid meminfo changed files")
	}
}

// R-IEQF-0ZI8 R-Y2EV-URH5
func TestSetupSlicesWriteFailureOrder(t *testing.T) {
	for failed, name := range sliceNames {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			sliceMeminfo(t, root, "MemTotal: 1954816 kB\n")
			mkdirAll(t, slicePath(root, name))
			var commands []host.Command
			err := apps.SetupSlices(t.Context(), sliceRecorder(root, &commands))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error = %v", err)
			}
			assertSliceCommands(t, commands)
			for i, other := range sliceNames {
				info, statErr := os.Stat(slicePath(root, other))
				if i < failed && (statErr != nil || !info.Mode().IsRegular()) {
					t.Fatalf("earlier file %s not preserved: %v", other, statErr)
				}
				if i > failed && !os.IsNotExist(statErr) {
					t.Fatalf("later file %s touched: %v", other, statErr)
				}
			}
		})
	}
}

// R-Y2EV-URH5
func TestSetupSlicesCommandFailuresPreserveFiles(t *testing.T) {
	cause := errors.New("cannot execute fixture")
	for _, test := range []struct {
		failure int
		start   bool
	}{{0, true}, {0, false}, {1, true}, {1, false}} {
		t.Run(fmt.Sprintf("%d/%t", test.failure, test.start), func(t *testing.T) {
			root := t.TempDir()
			sliceMeminfo(t, root, "MemTotal: 1954816 kB\n")
			writeFixture(t, filepath.Join(root, "var/lib/ikigenba/config.json"), []byte("store"), 0o600)
			writeFixture(t, filepath.Join(root, "opt/app/etc/env"), []byte("app"), 0o600)
			writeFixture(t, slicePath(root, "ikigenba-app.service"), []byte("unit"), 0o644)
			before := snapshotTree(t, root)
			var commands []host.Command
			result := host.Result{ExitCode: 7, Stdout: []byte("output\n"), Stderr: []byte("detail\n")}
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				commands = append(commands, command)
				if len(commands)-1 == test.failure {
					if test.start {
						return result, cause
					}
					return result, nil
				}
				return host.Result{}, nil
			}}
			err := apps.SetupSlices(t.Context(), env)
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) || !reflect.DeepEqual(commandErr.Result, result) || errors.Is(err, cause) != test.start {
				t.Fatalf("command error = %#v, %v", commandErr, err)
			}
			wantArgs := [][]string{{"daemon-reload"}, {"restart", "nginx"}}
			assertSliceCommands(t, commands, wantArgs[:test.failure+1]...)
			if commandErr.Label != "systemctl "+strings.Join(wantArgs[test.failure], " ") {
				t.Fatalf("label = %q", commandErr.Label)
			}
			for _, name := range sliceNames {
				if _, err := os.Stat(slicePath(root, name)); err != nil {
					t.Fatalf("written file lost: %v", err)
				}
			}
			after := snapshotTree(t, root)
			for _, name := range []string{"var/lib/ikigenba/config.json", "opt/app/etc/env", "etc/systemd/system/ikigenba-app.service", "proc/meminfo"} {
				if before[name] != after[name] {
					t.Fatalf("unrelated file changed: %s", name)
				}
			}
			for name, entry := range after {
				if entry.mode.IsRegular() {
					if _, existed := before[name]; !existed {
						allowed := false
						for _, slice := range sliceNames {
							allowed = allowed || name == "etc/systemd/system/"+slice
						}
						if !allowed {
							t.Fatalf("unexpected file created: %s", name)
						}
					}
				}
			}
		})
	}
}
