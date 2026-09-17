package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestDatabaseFiles(t *testing.T) {
	// R-JMWT-CBFN R-JO4P-Q36C
	database := apps.Database{Engine: "sqlite", Path: "state/nested/app.db"}
	want := []string{"state/nested/app.db", "state/nested/app.db-wal", "state/nested/app.db-shm", "state/nested/.app.db-litestream"}
	if got := backup.DatabaseFiles(database); !reflect.DeepEqual(got, want) {
		t.Fatalf("DatabaseFiles() = %#v, want %#v", got, want)
	}
}

func TestRegenerateRendersDiscoveredDatabases(t *testing.T) {
	// R-JKH0-KRY9 R-JPCM-3UX1 R-MKUD-39PW
	root, store := regenerationFixture(t)
	writeService(t, root, "zeta", "app = \"zeta\"\nport = 9000\n[database]\nengine = \"sqlite\"\npath = \"state/zeta.db\"\n")
	writeService(t, root, "empty", "app = \"empty\"\n")
	writeService(t, root, "alpha", "[database]\nengine = \"sqlite\"\npath = \"state/nested/alpha.db\"\n")
	if err := os.MkdirAll(filepath.Join(root, "opt", "state-only", "state"), 0o750); err != nil {
		t.Fatal(err)
	}

	changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
	if err != nil || !changed {
		t.Fatalf("Regenerate() = %v, %v, want true, nil", changed, err)
	}
	want := "region: 'us-west-2'\n" +
		"snapshot:\n  interval: 3600s\n" +
		"sync-interval: 5s\n" +
		"socket:\n  enabled: true\n  path: '" + filepath.ToSlash(filepath.Join(root, "var/run/litestream.sock")) + "'\n  permissions: 0600\n" +
		"retention:\n  enabled: false\n" +
		"dbs:\n" +
		"  - path: '" + filepath.ToSlash(filepath.Join(root, "opt/alpha/state/nested/alpha.db")) + "'\n" +
		"    replicas:\n      - url: 's3://backups/hosts/example/alpha/'\n" +
		"  - path: '" + filepath.ToSlash(filepath.Join(root, "opt/zeta/state/zeta.db")) + "'\n" +
		"    replicas:\n      - url: 's3://backups/hosts/example/zeta/'\n"
	if got := readLitestream(t, root); got != want {
		t.Fatalf("litestream.yml =\n%s\nwant:\n%s", got, want)
	}
	info, err := os.Stat(filepath.Join(root, "etc/litestream.yml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("litestream.yml mode = %v, %v, want 0600", info, err)
	}
}

func TestRegenerateEmptyDatabaseSequence(t *testing.T) {
	root, store := regenerationFixture(t)
	writeService(t, root, "notes", "app = \"notes\"\n")
	if _, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store); err != nil {
		t.Fatal(err)
	}
	want := "region: 'us-west-2'\n" +
		"snapshot:\n  interval: 3600s\n" +
		"sync-interval: 5s\n" +
		"socket:\n  enabled: true\n  path: '" + filepath.ToSlash(filepath.Join(root, "var/run/litestream.sock")) + "'\n  permissions: 0600\n" +
		"retention:\n  enabled: false\n" +
		"dbs: []\n"
	if got := readLitestream(t, root); got != want {
		t.Fatalf("litestream.yml = %q, want %q", got, want)
	}
}

func TestRegenerateIsDeterministicAndNoOpWhenIdentical(t *testing.T) {
	// R-JT0B-9654
	root, store := regenerationFixture(t)
	manifest := "app = \"notes\"\nport = 3000\n[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n"
	writeService(t, root, "notes", manifest)
	if changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store); err != nil || !changed {
		t.Fatalf("first Regenerate() = %v, %v", changed, err)
	}
	before := readLitestream(t, root)
	writeService(t, root, "notes", strings.Replace(manifest, "port = 3000", "port = 4000\ndefault = true", 1))
	changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
	if err != nil || changed {
		t.Fatalf("second Regenerate() = %v, %v, want false, nil", changed, err)
	}
	if after := readLitestream(t, root); after != before {
		t.Fatalf("no-op changed bytes: before %q, after %q", before, after)
	}
}

func TestRegenerateRejectsInvalidConfigurationWithoutChangingFile(t *testing.T) {
	// R-RR46-VI3R
	tests := []struct {
		key   string
		value string
	}{
		{"backup.service_db_seconds", "-1"},
		{"backup.service_db_seconds", "+1"},
		{"backup.service_db_seconds", "1.5"},
		{"backup.service_wal_seconds", "abc"},
		{"backup.service_wal_seconds", "9223372037"},
		{"backup.service_wal_seconds", "18446744073709551616"},
		{"backup.s3_uri", "relative/path"},
		{"backup.s3_uri", "s3:///missing-bucket/"},
		{"backup.s3_uri", "s3://:80/path"},
		{"backup.s3_uri", "s3://user@:80/path"},
		{"backup.s3_uri", "s3://bucket/good/../bad/"},
		{"backup.s3_uri", "s3://bucket/path?query=yes"},
		{"backup.s3_uri", "s3://bucket/path#fragment"},
	}
	for _, test := range tests {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
			root, store := regenerationFixture(t)
			writeService(t, root, "notes", "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
			old := []byte("old configuration\n")
			if err := os.WriteFile(filepath.Join(root, "etc/litestream.yml"), old, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := store.Set(test.key, test.value); err != nil {
				t.Fatal(err)
			}
			changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
			if err == nil || changed || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("Regenerate() = %v, %v, want false and error naming %s", changed, err, test.key)
			}
			if got := readLitestream(t, root); got != string(old) {
				t.Fatalf("configuration changed to %q", got)
			}
		})
	}
}

func TestRegenerateAcceptsBucketWithUserinfoOrPort(t *testing.T) {
	// R-RR46-VI3R
	for _, prefix := range []string{
		"s3://user@bucket/path/",
		"s3://bucket:80/path/",
		"s3://user@bucket:80/path/",
	} {
		t.Run(prefix, func(t *testing.T) {
			root, store := regenerationFixture(t)
			if err := store.Set("backup.s3_uri", prefix); err != nil {
				t.Fatal(err)
			}
			writeService(t, root, "notes", "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")

			changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
			if err != nil || !changed {
				t.Fatalf("Regenerate() = %v, %v, want true, nil", changed, err)
			}
			want := "region: 'us-west-2'\n" +
				"snapshot:\n  interval: 3600s\n" +
				"sync-interval: 5s\n" +
				"socket:\n  enabled: true\n  path: '" + filepath.ToSlash(filepath.Join(root, "var/run/litestream.sock")) + "'\n  permissions: 0600\n" +
				"retention:\n  enabled: false\n" +
				"dbs:\n" +
				"  - path: '" + filepath.ToSlash(filepath.Join(root, "opt/notes/state/notes.db")) + "'\n" +
				"    replicas:\n      - url: '" + prefix + "notes/'\n"
			if got := readLitestream(t, root); got != want {
				t.Fatalf("litestream.yml = %q, want %q", got, want)
			}
		})
	}
}

func TestRegenerateAcceptsPeriodLimitAndSafeDatabaseOnlyName(t *testing.T) {
	// R-RSC3-99UG
	root, store := regenerationFixture(t)
	if err := store.Set("backup.service_db_seconds", "9223372036"); err != nil {
		t.Fatal(err)
	}
	writeService(t, root, "bad_name", "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")
	changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
	if err != nil || !changed {
		t.Fatalf("Regenerate() = %v, %v, want true, nil", changed, err)
	}
	want := "region: 'us-west-2'\n" +
		"snapshot:\n  interval: 9223372036s\n" +
		"sync-interval: 5s\n" +
		"socket:\n  enabled: true\n  path: '" + filepath.ToSlash(filepath.Join(root, "var/run/litestream.sock")) + "'\n  permissions: 0600\n" +
		"retention:\n  enabled: false\n" +
		"dbs:\n" +
		"  - path: '" + filepath.ToSlash(filepath.Join(root, "opt/bad_name/state/app.db")) + "'\n" +
		"    replicas:\n      - url: 's3://backups/hosts/example/bad_name/'\n"
	if got := readLitestream(t, root); got != want {
		t.Fatalf("litestream.yml = %q, want %q", got, want)
	}
}

func TestRegenerateRejectsReservedDatabaseService(t *testing.T) {
	// R-RSC3-99UG
	for _, name := range []string{"host", "deploy"} {
		t.Run(name, func(t *testing.T) {
			root, store := regenerationFixture(t)
			writeService(t, root, name, "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")
			old := "old\n"
			if err := os.WriteFile(filepath.Join(root, "etc/litestream.yml"), []byte(old), 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
			if err == nil || changed || !strings.Contains(err.Error(), name) {
				t.Fatalf("Regenerate() = %v, %v, want error naming %q", changed, err, name)
			}
			if got := readLitestream(t, root); got != old {
				t.Fatalf("configuration changed to %q", got)
			}
		})
	}
}

func TestRegenerateReportsDiscoveryAndManifestFailures(t *testing.T) {
	// R-JPCM-3UX1
	t.Run("discovery", func(t *testing.T) {
		storeRoot, store := regenerationFixture(t)
		missing := filepath.Join(storeRoot, "missing")
		changed, err := backup.Regenerate(context.Background(), host.Env{Root: missing}, store)
		if err == nil || changed || !strings.Contains(err.Error(), "discover") {
			t.Fatalf("Regenerate() = %v, %v", changed, err)
		}
	})
	t.Run("manifest", func(t *testing.T) {
		root, store := regenerationFixture(t)
		writeService(t, root, "broken", "[database\n")
		old := "old\n"
		if err := os.WriteFile(filepath.Join(root, "etc/litestream.yml"), []byte(old), 0o600); err != nil {
			t.Fatal(err)
		}
		changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
		if err == nil || changed || !strings.Contains(err.Error(), "broken") {
			t.Fatalf("Regenerate() = %v, %v, want error naming broken", changed, err)
		}
		if got := readLitestream(t, root); got != old {
			t.Fatalf("configuration changed to %q", got)
		}
	})
}

func TestRegenerateReturnsContextAndConfigurationIOFailures(t *testing.T) {
	// R-JU87-MXVT
	t.Run("cancelled", func(t *testing.T) {
		root, store := regenerationFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		changed, err := backup.Regenerate(ctx, host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			panic("Regenerate controlled a unit")
		}}, store)
		if !errors.Is(err, context.Canceled) || changed {
			t.Fatalf("Regenerate() = %v, %v, want false, context.Canceled", changed, err)
		}
		if _, statErr := os.Stat(filepath.Join(root, "etc/litestream.yml")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("litestream configuration exists after cancellation: %v", statErr)
		}
	})
	t.Run("configuration read", func(t *testing.T) {
		root, store := regenerationFixture(t)
		configPath := filepath.Join(root, "etc/ikigenba/config.json")
		if err := os.WriteFile(configPath, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
		if err == nil || changed || !strings.Contains(err.Error(), "read aws.region") {
			t.Fatalf("Regenerate() = %v, %v", changed, err)
		}
	})
	t.Run("configuration write", func(t *testing.T) {
		root, store := regenerationFixture(t)
		filesystem, err := os.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = filesystem.Close() }()
		if err := filesystem.Chmod("etc", 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupRoot, openErr := os.OpenRoot(root)
			if openErr == nil {
				_ = cleanupRoot.Chmod("etc", 0o700)
				_ = cleanupRoot.Close()
			}
		})
		changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
		if err == nil || changed || !strings.Contains(err.Error(), "write /etc/litestream.yml") {
			t.Fatalf("Regenerate() = %v, %v", changed, err)
		}
		if _, statErr := filesystem.Stat("etc/litestream.yml"); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed write created target: %v", statErr)
		}
	})
}

func TestRegenerateDoesNotTouchDatabaseOrExecuteCommands(t *testing.T) {
	// R-JU87-MXVT
	root, store := regenerationFixture(t)
	writeService(t, root, "notes", "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
	database := filepath.Join(root, "opt/notes/state/notes.db")
	if err := os.WriteFile(database, []byte("database marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		panic("Regenerate controlled systemd")
	}}
	if _, err := backup.Regenerate(context.Background(), env, store); err != nil {
		t.Fatal(err)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	data, err := filesystem.ReadFile("opt/notes/state/notes.db")
	if err != nil || string(data) != "database marker" {
		t.Fatalf("database = %q, %v; want unchanged marker", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(database))
	if err != nil || len(entries) != 1 || entries[0].Name() != "notes.db" {
		t.Fatalf("database directory = %v, %v; want only notes.db", entries, err)
	}
}

func regenerationFixture(t *testing.T) (string, config.Store) {
	t.Helper()
	root := t.TempDir()
	store := config.Store{Root: root}
	values := map[string]string{
		"aws.region":                 "us-west-2",
		"backup.s3_uri":              "s3://backups/hosts/example/",
		"backup.service_db_seconds":  "3600",
		"backup.service_wal_seconds": "5",
	}
	for key, value := range values {
		if err := store.Set(key, value); err != nil {
			t.Fatalf("Set(%q): %v", key, err)
		}
	}
	return root, store
}

func writeService(t *testing.T, root, name, manifest string) {
	t.Helper()
	directory := filepath.Join(root, "opt", name, "etc")
	if err := os.MkdirAll(filepath.Join(root, "opt", name, "state"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readLitestream(t *testing.T, root string) string {
	t.Helper()
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	data, err := filesystem.ReadFile("etc/litestream.yml")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
