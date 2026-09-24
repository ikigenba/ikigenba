package apps_test

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestLifecycleAPITypes(t *testing.T) {
	// R-ES81-UK2R
	// R-V4LM-LI08 R-V3DQ-7Q9J R-V5TI-Z9QX
	// R-LTJ4-ANPF
	typeFieldsExactly(t, reflect.TypeFor[apps.UninstallHooks](), []fieldShape{
		{"Report", reflect.TypeFor[func(string, string, bool) error]()},
		{"Configure", reflect.TypeFor[func(context.Context, apps.Manifest) error]()},
	})
	typeFieldsExactly(t, reflect.TypeFor[apps.StatusRow](), []fieldShape{
		{"Name", reflect.TypeFor[string]()},
		{"Version", reflect.TypeFor[string]()},
		{"State", reflect.TypeFor[string]()},
		{"Socket", reflect.TypeFor[string]()},
		{"JournalMode", reflect.TypeFor[string]()},
	})
	typeFieldsExactly(t, reflect.TypeFor[apps.ServiceReport](), []fieldShape{
		{"Name", reflect.TypeFor[string]()},
		{"Version", reflect.TypeFor[string]()},
		{"State", reflect.TypeFor[string]()},
	})
	typeFieldsExactly(t, reflect.TypeFor[apps.LifecycleHooks](), []fieldShape{
		{"Report", reflect.TypeFor[func(string, string, bool) error]()},
		{"Configure", reflect.TypeFor[func(context.Context, apps.Manifest) error]()},
	})
	typeFieldsExactly(t, reflect.TypeFor[apps.LifecycleError](), []fieldShape{
		{"Code", reflect.TypeFor[int]()},
		{"Message", reflect.TypeFor[string]()},
		{"Cause", reflect.TypeFor[error]()},
	})
	cause := errors.New("cause")
	failure := &apps.LifecycleError{Code: 7, Message: "message", Cause: cause}
	if failure.Error() != "message" || !errors.Is(failure, cause) {
		t.Fatalf("LifecycleError = (%q, %v), want message and wrapped cause", failure.Error(), errors.Unwrap(failure))
	}
}

type fieldShape struct {
	name   string
	typeOf reflect.Type
}

func typeFieldsExactly(t *testing.T, actual reflect.Type, want []fieldShape) {
	t.Helper()
	if actual.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", actual, actual.NumField(), len(want))
	}
	for index, expected := range want {
		field := actual.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf {
			t.Fatalf("%s field %d = %s %s, want %s %s", actual, index, field.Name, field.Type, expected.name, expected.typeOf)
		}
	}
}

func TestStatusEmptyAndDiscoveryFailure(t *testing.T) {
	// R-LSB7-WVYQ
	root := t.TempDir()
	rows, err := apps.Status(context.Background(), host.Env{Root: root})
	if err != nil || len(rows) != 0 {
		t.Fatalf("Status without /opt = (%#v, %v), want empty success", rows, err)
	}

	missing := filepath.Join(root, "missing-root")
	rows, err = apps.Status(context.Background(), host.Env{Root: missing})
	var failure *apps.LifecycleError
	if rows != nil || !errors.As(err, &failure) || failure.Code != 1 || failure.Message != "status failed" || failure.Cause == nil {
		t.Fatalf("Status with missing root = (%#v, %#v), want lifecycle discovery failure", rows, err)
	}
}

func TestStatusReportsIndependentCurrentFactsInNameOrder(t *testing.T) {
	// R-MFHB-6J1X
	// R-VPBX-3LM1
	root := t.TempDir()
	writeStatusService(t, root, "zeta", "app = \"zeta\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 1)
	writeStatusService(t, root, "alpha", "app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 2)
	writeStatusService(t, root, "bad_name", "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 1)
	writeStatusService(t, root, "broken", "app = [\n", 0)

	var commands []host.Command
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		if command.Name == "systemctl" {
			switch command.Args[len(command.Args)-1] {
			case "ikigenba-alpha.service":
				return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=active\n")}, nil
			case "ikigenba-broken.service":
				return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\n")}, nil
			case "ikigenba-zeta.service":
				return host.Result{Stdout: []byte("ActiveState=failed\r\nLoadState=loaded\r\n")}, nil
			}
		}
		name := filepath.Base(command.Name)
		switch name {
		case "alpha":
			return host.Result{Stdout: []byte("v1.2.3\r\n")}, nil
		case "bad_name":
			return host.Result{Stdout: []byte("odd-version\n")}, nil
		case "broken":
			return host.Result{ExitCode: 1}, nil
		case "zeta":
			return host.Result{Stdout: []byte("v9\nkept")}, nil
		default:
			return host.Result{}, errors.New("unexpected command")
		}
	}}

	rows, err := apps.Status(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	want := []apps.StatusRow{
		{Name: "alpha", Version: "v1.2.3", State: "active", Socket: "-", JournalMode: "wal"},
		{Name: "bad_name", Version: "odd-version", State: "-", Socket: "-", JournalMode: "delete"},
		{Name: "broken", Version: "-", State: "-", Socket: "-", JournalMode: "-"},
		{Name: "zeta", Version: "v9\nkept", State: "failed", Socket: "-", JournalMode: "delete"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("Status rows = %#v, want %#v", rows, want)
	}
	for _, command := range commands {
		if command.Name == "systemctl" && slices.Contains(command.Args, "ikigenba-bad_name.service") {
			t.Fatalf("invalid name reached systemd: %#v", command)
		}
	}
}

func TestStatusIsReadOnlyAndKeepsManifestFailuresIndependent(t *testing.T) {
	// R-MHX3-Y2JB
	root := t.TempDir()
	writeStatusService(t, root, "notes", "this is not toml\n", 0)
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootFS.Close() })
	before, err := rootFS.ReadFile("opt/notes/etc/manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	var commands []host.Command
	rows, err := apps.Status(context.Background(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		if command.Name == "systemctl" {
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")}, nil
		}
		return host.Result{Stdout: []byte("v4\n")}, nil
	}})
	if err != nil || !reflect.DeepEqual(rows, []apps.StatusRow{{Name: "notes", Version: "v4", State: "inactive", Socket: "inactive", JournalMode: "-"}}) {
		t.Fatalf("Status = (%#v, %v)", rows, err)
	}
	after, err := rootFS.ReadFile("opt/notes/etc/manifest.toml")
	if err != nil || !slices.Equal(before, after) {
		t.Fatalf("manifest changed: %v", err)
	}
	for _, command := range commands {
		if command.Name == "systemctl" && (len(command.Args) == 0 || command.Args[0] != "show") {
			t.Fatalf("status issued service control: %#v", command)
		}
	}
}

func TestStatusReadsPersistentSQLiteJournalMode(t *testing.T) {
	// R-PWID-NYOW
	root := t.TempDir()
	wantModes := map[string]string{"missing": "-"}
	for _, tc := range []struct {
		name   string
		mode   byte
		mutate func([]byte) []byte
		want   string
	}{
		{name: "wal", mode: 2, want: "wal"},
		{name: "delete", mode: 1, want: "delete"},
		{name: "unsupported", mode: 3, want: "-"},
		{name: "mixed", mode: 2, mutate: func(data []byte) []byte { data[19] = 1; return data }, want: "-"},
		{name: "malformed", mode: 1, mutate: func(data []byte) []byte { return data[:40] }, want: "-"},
		{name: "header-only", mode: 1, mutate: func(data []byte) []byte { return data[:100] }, want: "-"},
		{name: "bad-header", mode: 1, mutate: func(data []byte) []byte { data[0] = 'X'; return data }, want: "-"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := "db-" + tc.name
			wantModes[service] = tc.want
			writeStatusService(t, root, service, "app = \""+service+"\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0)
			data := sqliteDatabase(tc.mode)
			if tc.mutate != nil {
				data = tc.mutate(data)
			}
			if err := os.WriteFile(filepath.Join(root, "opt", service, "state", "app.db"), data, 0o600); err != nil {
				t.Fatal(err)
			}
		})
	}
	writeStatusService(t, root, "missing", "app = \"missing\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0)

	rows, err := apps.Status(context.Background(), host.Env{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	modes := make(map[string]string, len(rows))
	for _, row := range rows {
		modes[row.Name] = row.JournalMode
	}
	for name, want := range wantModes {
		if modes[name] != want {
			t.Errorf("%s journal mode = %q, want %q", name, modes[name], want)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(root, "opt", "*", "state", "app.db-*")); err != nil || len(matches) != 0 {
		t.Fatalf("status created SQLite sidecars: %v, %v", matches, err)
	}
}

func writeStatusService(t *testing.T, root, name, manifest string, mode byte) {
	t.Helper()
	directory := filepath.Join(root, "opt", name)
	if err := os.MkdirAll(filepath.Join(directory, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "state"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "etc", "manifest.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if mode != 0 {
		if err := os.WriteFile(filepath.Join(directory, "state", "app.db"), sqliteDatabase(mode), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func sqliteDatabase(mode byte) []byte {
	data := make([]byte, 4096)
	copy(data, "SQLite format 3\x00")
	binary.BigEndian.PutUint16(data[16:18], 4096)
	data[18], data[19] = mode, mode
	data[21], data[22], data[23] = 64, 32, 32
	binary.BigEndian.PutUint32(data[24:28], 1)
	binary.BigEndian.PutUint32(data[28:32], 1)
	binary.BigEndian.PutUint32(data[44:48], 4)
	binary.BigEndian.PutUint32(data[56:60], 1)
	binary.BigEndian.PutUint32(data[92:96], 1)
	binary.BigEndian.PutUint32(data[96:100], 3046000)
	data[100] = 13
	binary.BigEndian.PutUint16(data[105:107], 4096)
	return data
}
