package services

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func writeEnv(t *testing.T, root string, commands *[]host.Command) host.Env {
	t.Helper()
	return host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if commands != nil {
			*commands = append(*commands, command)
		}
		switch {
		case command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--user", "ikigenba"}):
			return host.Result{Stdout: []byte("1000\n")}, nil
		case command.Name == "id" && reflect.DeepEqual(command.Args, []string{"--group", "--name", "ikigenba"}):
			return host.Result{Stdout: []byte("ikigenba\n")}, nil
		case command.Name == "chown":
			return host.Result{}, nil
		default:
			t.Errorf("unexpected command: %+v", command)
			return host.Result{}, errors.New("unexpected command")
		}
	}}
}

func servicesFile(root string) string {
	return filepath.Join(root, strings.TrimPrefix(apps.ServicesPath, "/"))
}

func writeLauncher(t *testing.T, root, name string) {
	t.Helper()
	base := filepath.Join(root, "opt", name)
	for _, directory := range []string{"etc", "bin", "share"} {
		if err := os.MkdirAll(filepath.Join(base, directory), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{
		filepath.Join(base, "etc", "manifest.toml"): "app = \"" + name + "\"\n",
		filepath.Join(base, "bin", name):            "binary",
		filepath.Join(base, apps.IconPath):          "<svg/>\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeLauncherEnv(t *testing.T, root string, commands *[]host.Command, disabled bool) host.Env {
	t.Helper()
	env := writeEnv(t, root, commands)
	baseExecute := env.Execute
	env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		if command.Name == "systemctl" {
			if commands != nil {
				*commands = append(*commands, command)
			}
			state := "enabled"
			if disabled {
				state = "disabled"
			}
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\n")}, nil
		}
		return baseExecute(ctx, command)
	}
	return env
}

func TestWriteOwnsServicesPublication(t *testing.T) {
	// R-M0FZ-4B5W
	root := t.TempDir()
	_, err := Write(context.Background(), writeEnv(t, root, nil), "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(servicesFile(root)); err != nil {
		t.Fatalf("services publication at apps-owned path: %v", err)
	}
}

// R-89D7-WNJZ
var _ func(context.Context, host.Env, string) (Changes, error) = Write

func TestWritePreflightLeavesExistingFileAlone(t *testing.T) {
	// R-8GOM-7A05
	root := t.TempDir()
	file := servicesFile(root)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	var commands []host.Command
	changes, err := Write(context.Background(), writeEnv(t, root, &commands), "")
	if err == nil || err.Error() != "host.name not set" || changes != nil {
		t.Fatalf("changes=%v, error=%v", changes, err)
	}
	if len(commands) != 0 {
		t.Fatalf("commands before entries: %+v", commands)
	}
	data := readPublishedServices(t, root)
	if string(data) != "previous" {
		t.Fatalf("previous file: %q", data)
	}
	for path, want := range map[string]os.FileMode{filepath.Dir(file): 0o700, file: 0o600} {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode: %v, %v; want %v", path, info, statErr, want)
		}
	}
}

func TestWritePreflightFailuresLeaveFileAlone(t *testing.T) {
	// R-8GOM-7A05
	for _, cause := range []string{"discover", "manifest", "disabled"} {
		t.Run(cause, func(t *testing.T) {
			root := t.TempDir()
			file := servicesFile(root)
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}
			var commands []host.Command
			env := writeEnv(t, root, &commands)
			switch cause {
			case "discover":
				if err := os.WriteFile(filepath.Join(root, "opt"), []byte("not directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				writeLauncher(t, root, "broken")
				if err := os.WriteFile(filepath.Join(root, "opt", "broken", "etc", "manifest.toml"), []byte("invalid =\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				writeLauncher(t, root, "running")
				baseExecute := env.Execute
				env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
					if command.Name == "systemctl" {
						commands = append(commands, command)
						return host.Result{ExitCode: 1}, nil
					}
					return baseExecute(ctx, command)
				}
			}
			changes, err := Write(context.Background(), env, "example.test")
			if err == nil || changes != nil {
				t.Fatalf("changes=%v, error=%v", changes, err)
			}
			if cause == "manifest" {
				services, discoverErr := apps.Discover(root)
				if discoverErr != nil || len(services) != 1 || services[0].ManifestError == nil ||
					!strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), services[0].ManifestError.Error()) {
					t.Fatalf("manifest error lost service or failure: %v; discovered=%v, %v", err, services, discoverErr)
				}
			}
			if cause == "disabled" {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) {
					t.Fatalf("disabled error lost command: %v", err)
				}
			}
			for _, command := range commands {
				if command.Name != "systemctl" {
					t.Fatalf("command before entries: %+v", command)
				}
			}
			data := readPublishedServices(t, root)
			info, statErr := os.Lstat(file)
			directory, dirErr := os.Lstat(filepath.Dir(file))
			if statErr != nil || dirErr != nil || string(data) != "previous" || info.Mode().Perm() != 0o600 || directory.Mode().Perm() != 0o700 {
				t.Fatalf("changed before entries: %q, %v, %v, %v", data, info, directory, errors.Join(statErr, dirErr))
			}
		})
	}
}

func TestWriteEnsuresAccountThenDirectoryMode(t *testing.T) {
	// R-8J4E-YTHJ
	root := t.TempDir()
	env := writeEnv(t, root, nil)
	baseExecute := env.Execute
	env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		if command.Name == "id" {
			if _, err := os.Lstat(filepath.Join(root, servicesDirectory)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("directory changed before account: %v", err)
			}
		}
		return baseExecute(ctx, command)
	}
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, servicesDirectory))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o750 {
		t.Fatalf("services directory: %v, %v", info, err)
	}
}

func TestWriteAccountFailureAndExistingDirectory(t *testing.T) {
	// R-8J4E-YTHJ
	root := t.TempDir()
	env := writeEnv(t, root, nil)
	env.Execute = func(_ context.Context, _ host.Command) (host.Result, error) {
		return host.Result{ExitCode: 1}, errors.New("account unavailable")
	}
	if changes, err := Write(context.Background(), env, "example.test"); err == nil || changes != nil {
		t.Fatalf("account failure: %v, %v", changes, err)
	}
	if _, err := os.Lstat(filepath.Join(root, servicesDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("account failure created directory: %v", err)
	}
	for _, directory := range []string{"var", filepath.Join("var", "lib"), servicesDirectory} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env = writeEnv(t, root, nil)
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, servicesDirectory))
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("existing directory mode: %v, %v", info, err)
	}
	other := t.TempDir()
	if _, err := Write(context.Background(), writeEnv(t, other, nil), "example.test"); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"var", filepath.Join("var", "lib")} {
		info, err := os.Lstat(filepath.Join(other, directory))
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("ancestor %s mode: %v, %v", directory, info, err)
		}
	}
}

func TestWriteKeepsExactFileAndAtomicallyReplacesDifferentFile(t *testing.T) {
	// R-8KCB-CL88
	root := t.TempDir()
	env := writeEnv(t, root, nil)
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	file := servicesFile(root)
	first, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	second, err := os.Lstat(file)
	if err != nil || !os.SameFile(first, second) {
		t.Fatalf("same bytes replaced file: %v", err)
	}
	if err := os.WriteFile(file, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	third, err := os.Lstat(file)
	if err != nil || os.SameFile(second, third) {
		t.Fatalf("different bytes did not replace file: %v", err)
	}
	data := readPublishedServices(t, root)
	if bytes.Equal(data, []byte("stale")) {
		t.Fatalf("candidate not published: %q", data)
	}
}

func TestWriteRetainsPreviousFileUntilRename(t *testing.T) {
	// R-8KCB-CL88
	root := t.TempDir()
	file := servicesFile(root)
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(filepath.Dir(file), "unrelated")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := writeEnv(t, root, nil)
	baseExecute := env.Execute
	env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		if command.Name == "chown" {
			data := readPublishedServices(t, root)
			if string(data) != "previous" {
				t.Fatalf("candidate visible before rename: %q", data)
			}
		}
		return baseExecute(ctx, command)
	}
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	data := readRootFile(t, root, "var/lib/ikigenba/unrelated")
	if string(data) != "keep" {
		t.Fatalf("unrelated file changed: %q", data)
	}
	items, err := os.ReadDir(filepath.Dir(file))
	if err != nil || len(items) != 2 {
		t.Fatalf("publication left extra files: %v, %v", items, err)
	}
}

func TestWriteAppliesFileModeAndOwnership(t *testing.T) {
	// R-8LK7-QCYX
	root := t.TempDir()
	var commands []host.Command
	if _, err := Write(context.Background(), writeEnv(t, root, &commands), "example.test"); err != nil {
		t.Fatal(err)
	}
	file := servicesFile(root)
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o640 {
		t.Fatalf("services file: %v, %v", info, err)
	}
	var chowns []host.Command
	for _, command := range commands {
		if command.Name == "chown" {
			chowns = append(chowns, command)
		}
	}
	if len(chowns) != 1 || len(chowns[0].Args) != 3 || chowns[0].Args[0] != "root:ikigenba" || chowns[0].Args[1] != filepath.Dir(file) || chowns[0].Args[2] == file {
		t.Fatalf("ownership command: %+v", chowns)
	}
	if _, err := os.Lstat(chowns[0].Args[2]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file was not renamed: %v", err)
	}
}

func TestWriteOwnsStagedAndKeptFiles(t *testing.T) {
	// R-8LK7-QCYX
	root := t.TempDir()
	file := servicesFile(root)
	var owned []string
	env := writeEnv(t, root, nil)
	baseExecute := env.Execute
	env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		if command.Name == "chown" {
			if len(command.Args) != 3 || command.Args[0] != "root:ikigenba" || command.Args[1] != filepath.Dir(file) {
				t.Fatalf("ownership arguments: %+v", command.Args)
			}
			info, err := os.Lstat(command.Args[2])
			wantMode := os.FileMode(0o640)
			if len(owned) == 1 {
				wantMode = 0o600
			}
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != wantMode {
				t.Fatalf("owned file at chown: %v, %v", info, err)
			}
			owned = append(owned, command.Args[2])
		}
		return baseExecute(ctx, command)
	}
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	if len(owned) != 1 || owned[0] == file || filepath.Dir(owned[0]) != filepath.Dir(file) {
		t.Fatalf("replacement chown path: %v", owned)
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(context.Background(), env, "example.test"); err != nil {
		t.Fatal(err)
	}
	if len(owned) != 2 || owned[1] != file {
		t.Fatalf("kept file chown path: %v", owned)
	}
	info, err := os.Lstat(file)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("kept file mode: %v, %v", info, err)
	}
}

func TestWriteChownFailurePreservesPreviousFile(t *testing.T) {
	// R-8MS4-44PM
	root := t.TempDir()
	file := servicesFile(root)
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := writeEnv(t, root, nil)
	baseExecute := env.Execute
	env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		if command.Name == "chown" {
			return host.Result{ExitCode: 1}, nil
		}
		return baseExecute(ctx, command)
	}
	changes, err := Write(context.Background(), env, "example.test")
	var commandErr *host.CommandError
	if changes != nil || !errors.As(err, &commandErr) {
		t.Fatalf("changes=%v, error=%v", changes, err)
	}
	data := readPublishedServices(t, root)
	info, statErr := os.Lstat(file)
	if statErr != nil || string(data) != "previous" || info.Mode().Perm() != 0o600 {
		t.Fatalf("previous file: %q, %v, %v", data, info, statErr)
	}
	items, err := os.ReadDir(filepath.Dir(file))
	if err != nil || len(items) != 1 || items[0].Name() != filepath.Base(file) {
		t.Fatalf("temporary file remains: %v, %v", items, err)
	}
}

func TestWriteFailurePreservesAbsenceAndCleansTemporary(t *testing.T) {
	// R-8MS4-44PM
	for _, cause := range []string{"account", "directory", "ownership", "rename"} {
		t.Run(cause, func(t *testing.T) {
			root := t.TempDir()
			file := servicesFile(root)
			if cause == "directory" {
				if err := os.WriteFile(filepath.Join(root, "var"), []byte("blocking ancestor"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if cause == "rename" {
				if err := os.MkdirAll(file, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			env := writeEnv(t, root, nil)
			baseExecute := env.Execute
			env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
				if cause == "account" && command.Name == "id" {
					return host.Result{}, errors.New("account unavailable")
				}
				if cause == "ownership" && command.Name == "chown" {
					return host.Result{ExitCode: 1}, nil
				}
				return baseExecute(ctx, command)
			}
			changes, err := Write(context.Background(), env, "example.test")
			if changes != nil || err == nil {
				t.Fatalf("failure result: %v, %v", changes, err)
			}
			if cause == "account" || cause == "ownership" {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) {
					t.Fatalf("command failure not wrapped: %v", err)
				}
			}
			if cause == "rename" {
				info, statErr := os.Lstat(file)
				if statErr != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					t.Fatalf("rename changed previous entry: %v, %v", info, statErr)
				}
			} else if _, statErr := os.Lstat(file); statErr == nil {
				t.Fatalf("failure published file: %v", statErr)
			}
			if cause == "ownership" || cause == "rename" {
				items, readErr := os.ReadDir(filepath.Dir(file))
				if readErr != nil || len(items) != map[string]int{"ownership": 0, "rename": 1}[cause] {
					t.Fatalf("temporary file remains: %v, %v", items, readErr)
				}
			}
		})
	}
}

func TestWriteExecutesOnlyAccountAndOwnershipCommands(t *testing.T) {
	// R-8O00-HWGB
	root := t.TempDir()
	writeLauncher(t, root, "running")
	var commands []host.Command
	if _, err := Write(context.Background(), writeLauncherEnv(t, root, &commands, false), "example.test"); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, command := range commands {
		names = append(names, command.Name)
	}
	if !reflect.DeepEqual(names, []string{"systemctl", "id", "id", "chown"}) {
		t.Fatalf("commands: %v", names)
	}
}

func TestWriteReturnsChangesFromPublishedEntries(t *testing.T) {
	// R-LY06-CROI
	root := t.TempDir()
	writeLauncher(t, root, "running")
	file := servicesFile(root)
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	prior := `{"services":[{"name":"retired","url":"https://retired.example.test","icon":"old","enabled":true}]}`
	if err := os.WriteFile(file, []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err := Write(context.Background(), writeLauncherEnv(t, root, nil, false), "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes, Changes{"retired": Removed, "running": Added}) {
		t.Fatalf("changes = %v", changes)
	}
	changes, err = Write(context.Background(), writeLauncherEnv(t, root, nil, true), "example.test")
	if err != nil || !reflect.DeepEqual(changes, Changes{"running": Disabled}) {
		t.Fatalf("disabled change = %v, %v", changes, err)
	}
	changes, err = Write(context.Background(), writeLauncherEnv(t, root, nil, false), "example.test")
	if err != nil || !reflect.DeepEqual(changes, Changes{"running": Enabled}) {
		t.Fatalf("enabled change = %v, %v", changes, err)
	}
	icon := filepath.Join(root, "opt", "running", apps.IconPath)
	if err := os.WriteFile(icon, []byte("<svg>updated</svg>"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err = Write(context.Background(), writeLauncherEnv(t, root, nil, false), "example.test")
	if err != nil || !reflect.DeepEqual(changes, Changes{"running": Updated}) {
		t.Fatalf("updated change = %v, %v", changes, err)
	}
	changes, err = Write(context.Background(), writeLauncherEnv(t, root, nil, false), "example.test")
	if err != nil || len(changes) != 0 {
		t.Fatalf("unchanged = %v, %v", changes, err)
	}
}

func TestWriteToleratesUnreadableAndMalformedPreviousFile(t *testing.T) {
	// R-8QFT-9FXP
	for _, prior := range []string{"absent", "unreadable", "malformed"} {
		t.Run(prior, func(t *testing.T) {
			root := t.TempDir()
			writeLauncher(t, root, "running")
			file := servicesFile(root)
			if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
				t.Fatal(err)
			}
			switch prior {
			case "unreadable":
				if err := os.Symlink("missing", file); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(file, []byte(`{"services":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			changes, err := Write(context.Background(), writeLauncherEnv(t, root, nil, false), "example.test")
			if err != nil || !reflect.DeepEqual(changes, Changes{"running": Added}) {
				t.Fatalf("changes=%v, error=%v", changes, err)
			}
		})
	}
}
