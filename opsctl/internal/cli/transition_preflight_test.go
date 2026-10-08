package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// preflightEntry includes link targets and ownership as well as ordinary data.
// The root timestamp is excluded because Snapshot's allowed temporary work
// changes that containing directory's timestamp even after cleanup.
type preflightEntry struct {
	mode     os.FileMode
	uid, gid uint32
	modified int64
	content  string
}

func preflightTree(root, releaseDir string, temporary bool) (map[string]preflightEntry, error) {
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = files.Close() }()
	tree := map[string]preflightEntry{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if temporary && filepath.Dir(rel) == "." && strings.HasPrefix(rel, ".opsctl-snapshot-") && d.IsDir() {
			return filepath.SkipDir
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		entry := preflightEntry{mode: info.Mode(), uid: stat.Uid, gid: stat.Gid}
		if rel != "." {
			entry.modified = info.ModTime().UnixNano()
		}
		switch {
		case info.Mode().IsRegular():
			data, e := files.ReadFile(rel)
			if e != nil {
				return e
			}
			entry.content = string(data)
		case info.Mode()&os.ModeSymlink != 0:
			entry.content, err = os.Readlink(p)
			if err != nil {
				return err
			}
		}
		if rel == releaseDir || strings.HasPrefix(rel, releaseDir+"/") {
			entry.uid, entry.gid = 0, 0
			// This requirement permits release modes and owners to change.
			// The entry type, contents and link targets must still be preserved.
			entry.mode = entry.mode.Type()
		}
		tree[rel] = entry
		return nil
	})
	return tree, err
}

func checkPreflightTree(f *transitionFixture, before map[string]preflightEntry, temporary bool) error {
	after, err := preflightTree(f.root, "opt/ikigenba/releases/"+transitionSHA, temporary)
	if err != nil {
		return err
	}
	for p, want := range before {
		if got, exists := after[p]; !exists || got != want {
			return fmt.Errorf("host entry changed: %s", p)
		}
	}
	for p := range after {
		if _, exists := before[p]; !exists {
			return fmt.Errorf("host entry created: %s", p)
		}
	}
	return nil
}

func allowedPreflightCommand(root string, c host.Command, snapshot bool) bool {
	if c.Dir != "" || len(c.Env) != 0 {
		return false
	}
	if c.Name == "chown" {
		return reflect.DeepEqual(c.Args, []string{"--recursive", "--no-dereference", "root:root", filepath.Join(root, "opt/ikigenba/releases", transitionSHA)}) && c.Stdin == nil
	}
	if c.Name == "systemctl" {
		return reflect.DeepEqual(c.Args, []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-dummy.socket"}) && c.Stdin == nil
	}
	if !snapshot {
		return false
	}
	switch c.Name {
	case "getent":
		return len(c.Args) == 2 && (c.Args[0] == "passwd" || c.Args[0] == "group") && c.Stdin == nil
	case "zstd":
		return reflect.DeepEqual(c.Args, []string{"--quiet", "--stdout"}) && c.Stdin != nil
	case "litestream":
		if reflect.DeepEqual(c.Args, []string{"ltx", "-level", "all", "-json", "s3://bucket/host/dummy/"}) {
			return c.Stdin == nil
		}
		if len(c.Args) != 4 || c.Args[0] != "restore" || c.Args[1] != "-o" || c.Args[3] != "s3://bucket/host/dummy/" || c.Stdin != nil {
			return false
		}
		rel, err := filepath.Rel(root, c.Args[2])
		return err == nil && filepath.Base(rel) == "database" && filepath.Dir(filepath.Dir(rel)) == "." && strings.HasPrefix(filepath.Dir(rel), ".opsctl-snapshot-")
	}
	return false
}

type preflightWriter struct {
	out   bytes.Buffer
	check func(string)
	stop  string
}

func (w *preflightWriter) Write(p []byte) (int, error) {
	w.check(string(p))
	n, _ := w.out.Write(p)
	if strings.HasPrefix(string(p), w.stop+": ok (") {
		return n, errors.New("report fixture stops after successful preflight")
	}
	return n, nil
}

func seedPreflightHost(f *transitionFixture, layout string) {
	f.addRelease(transitionOld, "dummy")
	f.addRelease(transitionUnused, "dummy")
	f.link("previous", transitionSHA)
	if layout == "released" {
		f.link("current", transitionOld)
	}
	if layout == "per-app" {
		f.write("opt/dummy/etc/manifest.toml", "app='dummy'\n[database]\nengine='sqlite'\npath='state/app.db'\n", 0o644)
	}
	for p, data := range map[string]string{
		"etc/opt/ikigenba/dummy/env":                        "old secrets",
		"etc/systemd/system/ikigenba-dummy.service":         "old service",
		"etc/systemd/system/ikigenba-dummy.socket":          "old socket",
		"etc/nginx/conf.d/ikigenba.conf":                    "old nginx",
		"run/ikigenba/services.json":                        "old services",
		"var/lib/ikigenba/services.json":                    "per app services",
		"etc/litestream.yml":                                "old replication",
		"var/opt/ikigenba/dummy/state/app.db":               "live database",
		"var/opt/ikigenba/dummy/state/app.db-wal":           "live wal",
		"var/opt/ikigenba/dummy/state/app.db-shm":           "live shm",
		"var/opt/ikigenba/dummy/state/file":                 "ordinary state",
		"var/opt/ikigenba/dummy/cache/file":                 "cache",
		"opt/aws/keep":                                      "unrelated opt",
		"opt/ikigenba/releases/" + transitionSHA + "/label": "old label\n",
	} {
		f.write(p, data, 0o640)
	}
	if err := os.MkdirAll(filepath.Join(f.root, "usr/local/bin"), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink("/opt/ikigenba/current/opsctl/bin/opsctl", filepath.Join(f.root, "usr/local/bin/opsctl")); err != nil {
		f.t.Fatal(err)
	}
	f.active["dummy"] = true
	f.disabled["dummy"] = true
}

func TestReleasePreflightMutationBoundary(t *testing.T) {
	// R-4A4N-LJX5
	for _, command := range []string{"activate", "rollback"} {
		for _, layout := range []string{"fresh", "released", "per-app"} {
			for _, failure := range []string{"release", "chown", "layout", "disabled", "manifests", "secrets", "resources", "resources success"} {
				if (command == "rollback" && failure == "layout") || (layout == "fresh" && failure == "disabled") {
					continue
				}
				t.Run(command+"/"+layout+"/"+failure, func(t *testing.T) {
					f := newTransitionFixture(t)
					seedPreflightHost(f, layout)
					switch failure {
					case "release":
						f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/sbin/unallowed", "x", 0o644)
					case "chown":
						f.fail = "chown --recursive --no-dereference root:root " + filepath.Join(f.root, "opt/ikigenba/releases", transitionSHA)
					case "layout":
						if err := os.Remove(filepath.Join(f.root, "etc/systemd/system/ikigenba.slice")); err != nil {
							t.Fatal(err)
						}
					case "disabled":
						f.fail = "systemctl show --property=LoadState --property=UnitFileState ikigenba-dummy.socket"
					case "manifests":
						f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='wrong'", 0o644)
					case "secrets":
						f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='dummy'\nsecrets=['MISSING']", 0o644)
					case "resources":
						f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='dummy'\n[resources]\nmemory_max='8G'", 0o644)
					}
					runPreflightBoundary(t, f, command, failure, false)
				})
			}
		}
	}
	for _, failure := range []string{"snapshot config", "snapshot listing", "snapshot restore", "snapshot compression", "snapshot upload", "snapshot success"} {
		t.Run("activate/per-app/"+failure, func(t *testing.T) {
			f := newTransitionFixture(t)
			seedPreflightHost(f, "per-app")
			if failure == "snapshot config" {
				if err := (config.Store{Root: f.root}).Del("backup.s3_uri"); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "snapshot upload" {
				f.cloud.failNames = map[string]bool{"dummy": true}
			}
			if failure == "snapshot compression" {
				f.fail = "zstd --quiet --stdout"
			}
			runPreflightBoundary(t, f, "activate", failure, true)
		})
	}
}

func runPreflightBoundary(t *testing.T, f *transitionFixture, command, failure string, snapshot bool) {
	t.Helper()
	before, err := preflightTree(f.root, "opt/ikigenba/releases/"+transitionSHA, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshotStarted := false
	active := map[string]bool{"dummy": true}
	check := func(temporary bool) {
		t.Helper()
		if err := checkPreflightTree(f, before, temporary); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.active, active) || !f.disabled["dummy"] {
			t.Fatal("app process/enablement changed")
		}
	}
	baseExecute := f.deps.Execute
	f.deps.Execute = func(ctx context.Context, c host.Command) (host.Result, error) {
		if !allowedPreflightCommand(f.root, c, snapshotStarted) {
			t.Fatalf("forbidden preflight command %+v", c)
		}
		check(snapshotStarted)
		if c.Name == "litestream" {
			f.commands = append(f.commands, c)
			if c.Args[0] == "ltx" {
				if failure == "snapshot listing" {
					return host.Result{ExitCode: 1}, nil
				}
				return host.Result{Stdout: []byte(`[{"file":"replica"}]`)}, nil
			}
			if _, err := os.Lstat(c.Args[2]); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("restore destination already exists: %v", err)
			}
			if err := os.WriteFile(c.Args[2], []byte("replica database"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(c.Args[2]), "restore-sidecar"), []byte("temporary"), 0o600); err != nil {
				t.Fatal(err)
			}
			if failure == "snapshot restore" {
				return host.Result{ExitCode: 1}, nil
			}
			return host.Result{}, nil
		}
		return baseExecute(ctx, c)
	}
	stop := "resources"
	if snapshot {
		stop = "snapshot"
	}
	writer := &preflightWriter{stop: stop, check: func(line string) {
		check(false)
		if strings.HasPrefix(line, "resources: ok (") {
			snapshotStarted = snapshot
		}
	}}
	var stderr bytes.Buffer
	args := []string{command}
	if command == "activate" {
		args = append(args, transitionSHA, "new-label")
	}
	code := Run(args, nil, writer, &stderr, f.deps)
	if code != 1 {
		t.Fatalf("exit %d, %s %s", code, writer.out.String(), stderr.String())
	}
	expectedStep := strings.TrimSuffix(failure, " success")
	switch failure {
	case "chown":
		expectedStep = "release"
	case "disabled":
		if command == "rollback" {
			expectedStep = "manifests"
		} else {
			expectedStep = "layout"
		}
	}
	if strings.HasPrefix(failure, "snapshot ") {
		expectedStep = "snapshot"
	}
	outcome := ": failed:"
	if strings.HasSuffix(failure, " success") {
		outcome = ": ok ("
	}
	if !strings.Contains(writer.out.String(), expectedStep+outcome) {
		t.Fatalf("expected %s%s, got %s %s", expectedStep, outcome, writer.out.String(), stderr.String())
	}
	check(false)
	if !snapshot && len(f.cloud.putCalls) != 0 {
		t.Fatal("preflight published objects")
	}
	if snapshot && len(f.cloud.putCalls) > 0 && !snapshotStarted {
		t.Fatal("snapshot preceded resource checks")
	}
	if failure == "snapshot success" && len(f.cloud.objects) != 1 {
		t.Fatalf("snapshot objects %v", f.cloud.objects)
	}
}
