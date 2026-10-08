package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestLifecycleFailureReportsStageOnceAndRetainsCause(t *testing.T) {
	// R-TEJL-RD5B
	restartRoot := cliRestartRoot(t)
	restartOut, restartErr, restartCode := invoke([]string{"restart", "notes"}, cli.Deps{Root: restartRoot, EUID: 0, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show" {
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
		}
		if command.Name == "journalctl" {
			return host.Result{Stdout: []byte("startup detail\n")}, nil
		}
		return host.Result{ExitCode: 7, Stderr: []byte("restart rejected\n")}, nil
	}})
	if restartCode != 1 || restartOut != "service: failed: notes: service failed to start\n" ||
		restartErr != "opsctl: restart failed\n\n> startup detail\n" {
		t.Fatalf("restart failure = exit %d stdout %q stderr %q", restartCode, restartOut, restartErr)
	}
}

func uninstallCommandRoot(t *testing.T, state bool) string {
	t.Helper()
	root := t.TempDir()
	setUninstallConfig(t, root)
	notesManifest := "app = \"notes\"\ndefault = true\n[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n"
	tasksManifest := "app = \"tasks\"\n[database]\nengine = \"sqlite\"\npath = \"state/tasks.db\"\n"
	writeUninstallFile(t, root, "opt/notes/bin/notes", "notes binary")
	writeUninstallFile(t, root, "opt/notes/etc/manifest.toml", notesManifest)
	writeUninstallFile(t, root, "opt/notes/share/asset", "notes asset")
	writeUninstallFile(t, root, "var/opt/ikigenba/notes/cache/item", "notes cache")
	writeUninstallFile(t, root, "etc/systemd/system/ikigenba-notes.service", "notes unit")
	writeUninstallFile(t, root, "etc/systemd/system/ikigenba-notes.socket", "notes socket")
	if state {
		writeUninstallFile(t, root, "var/opt/ikigenba/notes/state/notes.db", "last committed transaction")
		writeUninstallFile(t, root, "var/opt/ikigenba/notes/state/notes.db-wal", "wal")
		writeUninstallFile(t, root, "var/opt/ikigenba/notes/state/notes.db-shm", "shm")
		writeUninstallFile(t, root, "var/opt/ikigenba/notes/state/.notes.db-litestream/meta", "metadata")
		writeUninstallFile(t, root, "var/opt/ikigenba/notes/state/private/blob", "private retained state")
	}
	writeUninstallFile(t, root, "opt/tasks/bin/tasks", "other binary")
	writeUninstallFile(t, root, "opt/tasks/etc/manifest.toml", tasksManifest)
	writeUninstallFile(t, root, "var/opt/ikigenba/tasks/state/tasks.db", "other database")
	writeUninstallFile(t, root, "etc/systemd/system/ikigenba-tasks.service", "other unit")
	writeUninstallFile(t, root, "etc/systemd/system/ikigenba-tasks.socket", "other socket")
	writeUninstallFile(t, root, "etc/nginx/conf.d/ikigenba.conf", "old nginx\n")
	writeUninstallFile(t, root, "etc/litestream.yml", "old litestream\n")
	writeUninstallFile(t, root, "var/lib/ikigenba/services.json", "{\n  \"services\": [\n    { \"name\": \"notes\", \"url\": \"https://notes.example.test\", \"icon\": \"notes\", \"enabled\": true }\n  ]\n}\n")
	writeUninstallFile(t, root, "remote/parameters/notes", "owned by devctl")
	writeUninstallFile(t, root, "remote/releases/notes.tar.xz", "deployment artifact")
	return root
}

func setUninstallConfig(t *testing.T, root string) {
	t.Helper()
	store := config.Store{Root: root}
	for key, value := range map[string]string{
		"host.name":                  "example.test",
		"aws.region":                 "us-west-2",
		"backup.s3_uri":              "s3://backups/services/",
		"backup.service_db_seconds":  "3600",
		"backup.service_wal_seconds": "5",
	} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
}

func writeUninstallFile(t *testing.T, root, name, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readUninstallFile(t *testing.T, root, name string) string {
	t.Helper()
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	data, err := filesystem.ReadFile(filepath.ToSlash(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func snapshotUninstallPaths(t *testing.T, root string, names ...string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, name := range names {
		full := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			result[name] = readUninstallFile(t, root, name)
			continue
		}
		if err := filepath.WalkDir(full, func(pathname string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, pathname)
			if err != nil {
				return err
			}
			result[filepath.ToSlash(relative)] = readUninstallFile(t, root, relative)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

type failStepWriter struct {
	bytes.Buffer
	failStep           string
	err                error
	failed             bool
	failures           int
	writesAfterFailure int
}

func (writer *failStepWriter) Write(data []byte) (int, error) {
	if bytes.HasPrefix(data, []byte(writer.failStep+":")) {
		writer.failed = true
		writer.failures++
		return 0, writer.err
	}
	if writer.failed {
		writer.writesAfterFailure++
	}
	return writer.Buffer.Write(data)
}
