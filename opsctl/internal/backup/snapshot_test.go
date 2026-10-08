package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var _ func(context.Context, host.Env, cloud.Env, config.Store, string) ([]backup.SnapshotResult, error) = backup.Snapshot

func TestSnapshotArchivesReplicaAndOrdinaryFiles(t *testing.T) {
	// R-1G7W-5X2V R-1HFS-JOTK R-D33X-4DF9 R-D4BT-I55Y R-1OR6-UB9Q
	for _, live := range []bool{true, false} {
		t.Run(map[bool]string{true: "live", false: "absent"}[live], func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			region := "us-east-2"
			if !live {
				region = "eu-west-1"
			}
			if err := store.Set("aws.region", region); err != nil {
				t.Fatal(err)
			}
			replica := "s3://bucket/host/crm/?region=" + region
			writeFile(t, root, "opt/crm/etc/manifest.toml", "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
			writeFile(t, root, "opt/crm/etc/env", "SECRET=secret", 0o000)
			writeFile(t, root, "etc/opt/ikigenba/crm/env", "NEW_SECRET=secret", 0o000)
			writeFile(t, root, "var/opt/ikigenba/crm/cache/item", "excluded", 0o600)
			writeFile(t, root, "var/opt/ikigenba/crm/state/outbox", "ordinary", 0o640)
			writeFile(t, root, "var/opt/ikigenba/crm/state/nested/value", "keep nested", 0o640)
			writeFile(t, root, "var/opt/ikigenba/crm/state/nested/cache/item", "nested cache", 0o000)
			writeFile(t, root, "var/opt/ikigenba/crm/state/cache/item", "state cache", 0o000)
			writeFile(t, root, "var/opt/ikigenba/crm/state/files/cache", "regular file", 0o640)
			if live {
				writeFile(t, root, "var/opt/ikigenba/crm/state/app.db", "live must not read", 0o400)
			}
			writeFile(t, root, "var/opt/ikigenba/crm/state/app.db-wal", "wal", 0o600)
			writeFile(t, root, "var/opt/ikigenba/crm/state/app.db-shm", "shm", 0o600)
			writeFile(t, root, "var/opt/ikigenba/crm/state/.app.db-litestream/item", "metadata", 0o600)
			for _, tree := range []string{"cache", "bin", "share"} {
				writeFile(t, root, "opt/crm/"+tree+"/item", "excluded", 0o600)
			}
			writeFile(t, root, "opt/other/etc/manifest.toml", "broken", 0o600)
			if err := os.Symlink("/outside", filepath.Join(root, "var/opt/ikigenba/crm/state/link")); err != nil {
				t.Fatal(err)
			}
			before := fileTreeSnapshot(t, root)
			watches := newFileAccessWatch(t, filepath.Join(root, "etc/opt/ikigenba/crm"), filepath.Join(root, "var/opt/ikigenba/crm/state/cache"), filepath.Join(root, "var/opt/ikigenba/crm/state/nested/cache"), filepath.Join(root, "opt/other/etc/manifest.toml"), filepath.Join(root, "var/opt/ikigenba/crm/state/app.db-wal"), filepath.Join(root, "var/opt/ikigenba/crm/state/app.db-shm"), filepath.Join(root, "var/opt/ikigenba/crm/state/.app.db-litestream/item"))
			var liveWatch *fileAccessWatch
			if live {
				liveWatch = newFileAccessWatch(t, filepath.Join(root, "var/opt/ikigenba/crm/state/app.db"))
			}
			executor := &fileExecutor{uid: os.Getuid(), gid: os.Getgid(), user: "ikigenba", group: "ikigenba"}
			var commands []host.Command
			var temporary string
			execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
				if cmd.Name != "litestream" {
					return executor.execute(ctx, cmd)
				}
				commands = append(commands, cmd)
				if cmd.Args[len(cmd.Args)-1] != replica {
					t.Fatalf("replica URL = %q, want %q", cmd.Args[len(cmd.Args)-1], replica)
				}
				if cmd.Args[0] == "ltx" {
					return host.Result{Stdout: []byte("[{}]")}, nil
				}
				temporary = cmd.Args[2]
				if _, err := os.Lstat(temporary); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("output already exists: %v", err)
				}
				if !strings.HasPrefix(temporary, root+"/") || strings.HasPrefix(temporary, root+"/opt/") || strings.HasPrefix(temporary, root+"/var/opt/") {
					t.Fatalf("output path %s", temporary)
				}
				if err := os.WriteFile(temporary, []byte("replica bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				return host.Result{}, nil
			}
			client := newFileCloud()
			results, err := backup.Snapshot(context.Background(), fileHostEnv(root, execute), cloud.Env{Open: func(_ context.Context, gotRegion string) (cloud.Client, error) {
				if gotRegion != region {
					t.Fatalf("cloud region = %q, want %q", gotRegion, region)
				}
				return client, nil
			}}, store, "crm")
			watches.assertQuiet(t)
			if liveWatch != nil {
				liveWatch.assertQuiet(t)
			}
			if err != nil || len(results) != 1 || results[0].Err != nil {
				t.Fatalf("Snapshot = %+v, %v", results, err)
			}
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(before[1:], after[1:]) {
				t.Fatalf("persistent filesystem changed: before %v after %v", before, after)
			}
			result := results[0]
			if result.Service != "crm" || result.URI != "s3://bucket/host/snapshots/crm/1970-01-01T00:00:01Z.tar.zst" || result.Size != int64(len(client.objects[result.URI])) {
				t.Fatalf("result %+v", result)
			}
			if len(commands) != 2 || !reflect.DeepEqual(commands[0].Args, []string{"ltx", "-level", "all", "-json", replica}) || !reflect.DeepEqual(commands[1].Args, []string{"restore", "-o", temporary, replica}) {
				t.Fatalf("commands %+v", commands)
			}
			members := readTestArchive(t, client.objects[result.URI])
			if !reflect.DeepEqual(sortedHeaderNames(members), []string{"state/", "state/app.db", "state/files/", "state/files/cache", "state/link", "state/nested/", "state/nested/value", "state/outbox"}) {
				t.Fatalf("members %v", sortedHeaderNames(members))
			}
			db := members["state/app.db"]
			if string(db.data) != "replica bytes" || db.header.Typeflag != tar.TypeReg || db.header.Mode != 0o600 || db.header.Uid != os.Getuid() || db.header.Gid != os.Getgid() || db.header.Uname != "ikigenba" || db.header.Gname != "ikigenba" {
				t.Fatalf("database %+v", db)
			}
			if string(members["state/outbox"].data) != "ordinary" || members["state/outbox"].header.Mode != 0o640 || members["state/link"].header.Linkname != "/outside" {
				t.Fatalf("ordinary members %+v", members)
			}
			if string(members["state/nested/value"].data) != "keep nested" || string(members["state/files/cache"].data) != "regular file" {
				t.Fatalf("ordinary files near cache directories: %+v", members)
			}
			for _, name := range []string{"state/", "state/outbox", "state/link"} {
				member := members[name]
				if member.header.Uid != os.Getuid() || member.header.Gid != os.Getgid() || member.header.Uname != "ikigenba" || member.header.Gname != "ikigenba" {
					t.Fatalf("ownership of %s: %+v", name, member.header)
				}
			}
			if members["state/"].header.Typeflag != tar.TypeDir || members["state/"].header.Mode != 0o750 || members["state/link"].header.Typeflag != tar.TypeSymlink {
				t.Fatalf("directory or symlink metadata: %+v", members)
			}
			if _, err := os.Lstat(filepath.Dir(temporary)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("temporary directory remains: %v", err)
			}
		})
	}
}

func TestSnapshotSharedSelectionAndConfiguration(t *testing.T) {
	// R-DGIT-BUKW R-1OR6-UB9Q
	for _, item := range []struct{ key, value, want string }{{"backup.s3_uri", "", "backup.s3_uri not set"}, {"aws.region", "", "aws.region not set"}, {"backup.s3_uri", "s3://bucket/../bad", "backup.s3_uri"}} {
		t.Run(item.want, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			if err := store.Set(item.key, item.value); err != nil {
				t.Fatal(err)
			}
			got, err := backup.Snapshot(context.Background(), host.Env{Root: filepath.Join(root, "missing")}, cloud.Env{}, store, "crm")
			if err == nil || !strings.Contains(err.Error(), item.want) || len(got) != 0 {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
	root := t.TempDir()
	store := configuredFileStore(t, root)
	for _, name := range []string{"seed", "snapshots", "host", "deploy", "bad_name", "zeta"} {
		writeFile(t, root, "var/opt/ikigenba/"+name+"/state/value", name, 0o600)
	}
	executor := &fileExecutor{unmapped: true}
	client := newFileCloud()
	env := fileHostEnv(root, executor.execute)
	for _, name := range []string{"seed", "snapshots", "host", "deploy", ".", "..", "a/b", "a\x00b"} {
		watch := newFileAccessWatch(t, filepath.Join(root, "var/opt/ikigenba"))
		got, err := backup.Snapshot(context.Background(), env, cloud.Env{Open: client.open}, store, name)
		watch.assertQuiet(t)
		watch.close()
		if err == nil || len(got) != 0 {
			t.Fatalf("%s: %+v, %v", name, got, err)
		}
	}
	got, err := backup.Snapshot(context.Background(), env, cloud.Env{Open: client.open}, store, "missing")
	if err == nil || err.Error() != "no service 'missing'" || len(got) != 0 {
		t.Fatalf("missing: %+v %v", got, err)
	}
	got, err = backup.Snapshot(context.Background(), env, cloud.Env{Open: client.open}, store, "")
	if err != nil || len(got) != 6 {
		t.Fatalf("all: %+v %v", got, err)
	}
	for i, name := range []string{"bad_name", "deploy", "host", "seed", "snapshots", "zeta"} {
		if got[i].Service != name || (got[i].Err == nil) != (name == "bad_name" || name == "zeta") {
			t.Fatalf("result %+v", got[i])
		}
	}
	empty := t.TempDir()
	emptyStore := configuredFileStore(t, empty)
	got, err = backup.Snapshot(context.Background(), fileHostEnv(empty, executor.execute), cloud.Env{}, emptyStore, "")
	if err != nil || len(got) != 0 {
		t.Fatalf("empty: %+v %v", got, err)
	}
}

func TestSnapshotTimestampAndCollision(t *testing.T) {
	// R-1JVL-B8AY R-D33X-4DF9
	root := t.TempDir()
	store := configuredFileStore(t, root)
	for _, name := range []string{"alpha", "zeta"} {
		writeFile(t, root, "var/opt/ikigenba/"+name+"/state/value", name, 0o600)
	}
	executor := &fileExecutor{unmapped: true}
	client := newFileCloud()
	calls := 0
	times := []time.Time{time.Unix(1, 0), time.Unix(2, 123), time.Unix(2, 123)}
	env := fileHostEnv(root, executor.execute)
	env.Now = func() time.Time { v := times[calls]; calls++; return v }
	for run := range 3 {
		before := cloneObjects(client.objects)
		got, err := backup.Snapshot(context.Background(), env, cloud.Env{Open: client.open}, store, "")
		if err != nil || len(got) != 2 || calls != run+1 {
			t.Fatalf("got %+v %v calls %d", got, err, calls)
		}
		for _, result := range got {
			if run == 2 {
				if !errors.Is(result.Err, cloud.ErrAlreadyExists) {
					t.Fatalf("collision %+v", result)
				}
			} else if result.Err != nil || result.URI != "s3://bucket/host/snapshots/"+result.Service+"/"+times[run].UTC().Format(time.RFC3339Nano)+".tar.zst" {
				t.Fatalf("result %+v", result)
			}
		}
		if run == 2 && !reflect.DeepEqual(before, client.objects) {
			t.Fatal("collision mutated object")
		}
	}
}

func TestSnapshotReplicaFailuresContinueAndClean(t *testing.T) {
	// R-D5JP-VWWN R-FNIS-52UH R-1OR6-UB9Q
	for _, kind := range []string{"empty", "missing", "execute", "exit", "restore-execute", "restore-exit", "compression", "upload", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			writeFile(t, root, "opt/alpha/etc/manifest.toml", "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
			writeFile(t, root, "var/opt/ikigenba/alpha/state/value", "alpha", 0o600)
			writeFile(t, root, "var/opt/ikigenba/beta/state/value", "beta", 0o600)
			executor := &fileExecutor{unmapped: true}
			client := newFileCloud()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			temporary := ""
			if kind == "compression" {
				executor.compressionResponses = []host.Result{{ExitCode: 1}}
			}
			if kind == "upload" {
				client.fail = func(uri string, _ []byte) error {
					if strings.Contains(uri, "/alpha/") {
						return errors.New("upload failed")
					}
					return nil
				}
			}
			execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
				if cmd.Name != "litestream" {
					return executor.execute(ctx, cmd)
				}
				if got := cmd.Args[len(cmd.Args)-1]; got != "s3://bucket/host/alpha/?region=us-east-2" {
					t.Fatalf("replica URL = %q", got)
				}
				if cmd.Args[0] == "ltx" {
					switch kind {
					case "empty":
						return host.Result{Stdout: []byte("[]")}, nil
					case "execute":
						return host.Result{}, errors.New("cannot execute")
					case "exit":
						return host.Result{ExitCode: 3, Stderr: []byte("failure")}, nil
					}
					return host.Result{Stdout: []byte("[{}]")}, nil
				}
				temporary = cmd.Args[2]
				if err := os.WriteFile(temporary, []byte("copy"), 0o600); err != nil {
					t.Fatal(err)
				}
				if kind == "restore-execute" {
					return host.Result{}, errors.New("cannot restore")
				}
				if kind == "restore-exit" {
					return host.Result{ExitCode: 3, Stderr: []byte("restore failure")}, nil
				}
				if kind == "missing" {
					return host.Result{ExitCode: 1, Stderr: []byte("no matching backup files")}, nil
				}
				if kind == "cancel" {
					cancel()
					return host.Result{}, context.Canceled
				}
				return host.Result{}, nil
			}
			before := fileTreeSnapshot(t, root)
			got, err := backup.Snapshot(ctx, fileHostEnv(root, execute), cloud.Env{Open: client.open}, store, "")
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(before[1:], after[1:]) {
				t.Fatalf("persistent filesystem changed: before %v after %v", before, after)
			}
			if kind == "cancel" {
				if !errors.Is(err, context.Canceled) || len(got) != 1 || !errors.Is(got[0].Err, context.Canceled) {
					t.Fatalf("cancel %+v %v", got, err)
				}
			} else {
				if err != nil || len(got) != 2 || got[0].Err == nil || got[1].Err != nil {
					t.Fatalf("got %+v %v", got, err)
				}
				if (kind == "empty" || kind == "missing") && got[0].Err.Error() != "no replica under the prefix" {
					t.Fatalf("reason %v", got[0].Err)
				}
				if kind == "execute" || kind == "exit" || kind == "restore-execute" || kind == "restore-exit" {
					var ce *host.CommandError
					if !errors.As(got[0].Err, &ce) {
						t.Fatalf("command error lost: %v", got[0].Err)
					}
				}
			}
			if temporary != "" {
				if _, err := os.Stat(filepath.Dir(temporary)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("temporary remains %v", err)
				}
			}
			for uri := range client.objects {
				if strings.Contains(uri, "/alpha/") {
					t.Fatalf("failed snapshot published: %s", uri)
				}
			}
		})
	}
}

func TestSnapshotCanonicalReadFailureAndManifestFailure(t *testing.T) {
	// R-FNIS-52UH
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "var/opt/ikigenba/alpha/state/outbox", "private", 0o000)
	writeFile(t, root, "opt/beta/etc/manifest.toml", "[broken", 0o600)
	writeFile(t, root, "var/opt/ikigenba/zeta/state/value", "later", 0o600)
	executor := &fileExecutor{unmapped: true}
	client := newFileCloud()
	got, err := backup.Snapshot(context.Background(), fileHostEnv(root, executor.execute), cloud.Env{Open: client.open}, store, "")
	if err != nil || len(got) != 3 || got[0].Err == nil || got[0].Err.Error() != "/var/opt/ikigenba/alpha/state/outbox: permission denied" || got[1].Err == nil || got[2].Err != nil {
		t.Fatalf("got %+v %v", got, err)
	}
	if len(client.puts) != 1 || !strings.Contains(client.puts[0], "/zeta/") {
		t.Fatalf("uploads %v", client.puts)
	}
}

func TestSnapshotUploadInterruptionRetainsEarlierResults(t *testing.T) {
	// R-FNIS-52UH
	root := t.TempDir()
	store := configuredFileStore(t, root)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		writeFile(t, root, "var/opt/ikigenba/"+name+"/state/value", name, 0o600)
	}
	executor := &fileExecutor{unmapped: true}
	client := newFileCloud()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.fail = func(uri string, _ []byte) error {
		if strings.Contains(uri, "/beta/") {
			cancel()
			return context.Canceled
		}
		return nil
	}
	got, err := backup.Snapshot(ctx, fileHostEnv(root, executor.execute), cloud.Env{Open: client.open}, store, "")
	if !errors.Is(err, context.Canceled) || len(got) != 2 || got[0].Service != "alpha" || got[0].Err != nil || got[1].Service != "beta" || !errors.Is(got[1].Err, context.Canceled) {
		t.Fatalf("got %+v %v", got, err)
	}
	if len(client.puts) != 2 || len(client.objects) != 1 {
		t.Fatalf("puts %v objects %v", client.puts, client.objects)
	}
}

func TestFilesReservedDiscoveredNamesFailBeforeDataReads(t *testing.T) {
	// R-FHFA-8850
	root := t.TempDir()
	store := configuredFileStore(t, root)
	var names []string
	for _, service := range []string{"seed", "snapshots"} {
		name := filepath.Join(root, "var/opt/ikigenba", service, "state/value")
		names = append(names, name)
		writeFile(t, root, "var/opt/ikigenba/"+service+"/state/value", "reserved", 0o600)
	}
	watch := newFileAccessWatch(t, names...)
	client := newFileCloud()
	executor := &fileExecutor{unmapped: true}
	got, err := backup.Files(context.Background(), fileHostEnv(root, executor.execute), cloud.Env{Open: client.open}, store, "")
	watch.assertQuiet(t)
	if err != nil || len(got) != 2 || got[0].Service != "seed" || got[1].Service != "snapshots" || got[0].Err == nil || got[1].Err == nil || len(client.puts) != 0 || len(executor.commands) != 0 {
		t.Fatalf("got %+v %v puts %v commands %v", got, err, client.puts, executor.commands)
	}
}

func TestSnapshotWithoutDatabasePreservesCompleteState(t *testing.T) {
	// R-D33X-4DF9 R-1OR6-UB9Q
	for _, manifest := range []bool{false, true} {
		t.Run(map[bool]string{false: "no manifest", true: "no database"}[manifest], func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			if manifest {
				writeFile(t, root, "opt/notes/etc/manifest.toml", "app = \"notes\"\n", 0o600)
			}
			writeFile(t, root, "opt/notes/etc/env", "secret", 0o600)
			state := map[string]string{"app.db": "ordinary database name", "app.db-wal": "ordinary wal", "app.db-shm": "ordinary shm", ".app.db-litestream/item": "ordinary metadata", "nested/value": "ordinary nested file"}
			for name, data := range state {
				writeFile(t, root, "var/opt/ikigenba/notes/state/"+name, data, 0o640)
			}
			writeFile(t, root, "var/opt/ikigenba/notes/state/cache/item", "cache", 0o000)
			writeFile(t, root, "var/opt/ikigenba/notes/state/nested/cache/item", "nested cache", 0o000)
			before := fileTreeSnapshot(t, root)
			watch := newFileAccessWatch(t, filepath.Join(root, "var/opt/ikigenba/notes/state/cache"), filepath.Join(root, "var/opt/ikigenba/notes/state/nested/cache"))
			executor := &fileExecutor{unmapped: true}
			client := newFileCloud()
			got, err := backup.Snapshot(context.Background(), fileHostEnv(root, executor.execute), cloud.Env{Open: client.open}, store, "notes")
			watch.assertQuiet(t)
			if err != nil || len(got) != 1 || got[0].Err != nil {
				t.Fatalf("got %+v %v", got, err)
			}
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatalf("filesystem changed: before %v after %v", before, after)
			}
			for _, command := range executor.commands {
				if strings.HasPrefix(command, "litestream ") {
					t.Fatalf("unexpected Litestream: %s", command)
				}
			}
			members := readTestArchive(t, client.objects[got[0].URI])
			want := []string{"state/", "state/.app.db-litestream/", "state/.app.db-litestream/item", "state/app.db", "state/app.db-shm", "state/app.db-wal", "state/nested/", "state/nested/value"}
			sort.Strings(want)
			if !reflect.DeepEqual(sortedHeaderNames(members), want) {
				t.Fatalf("members %v want %v", sortedHeaderNames(members), want)
			}
			for name, data := range state {
				member := members["state/"+name]
				if string(member.data) != data || member.header.Mode != 0o640 {
					t.Fatalf("member %s %+v", name, member)
				}
			}
		})
	}
}

func TestSnapshotConfigurationStopsBeforeServiceAccess(t *testing.T) {
	// R-DGIT-BUKW R-D4BT-I55Y
	for _, item := range []struct {
		name, key, want string
		unset, both     bool
	}{
		{"unset prefix", "backup.s3_uri", "backup.s3_uri not set", true, false},
		{"empty prefix", "backup.s3_uri", "backup.s3_uri not set", false, false},
		{"unset region", "aws.region", "aws.region not set", true, false},
		{"empty region", "aws.region", "aws.region not set", false, false},
		{"prefix before region", "backup.s3_uri", "backup.s3_uri not set", true, true},
	} {
		t.Run(item.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			var setupErr error
			if item.unset {
				setupErr = store.Del(item.key)
			} else {
				setupErr = store.Set(item.key, "")
			}
			if setupErr != nil {
				t.Fatal(setupErr)
			}
			if item.both {
				if err := store.Del("aws.region"); err != nil {
					t.Fatal(err)
				}
			}
			writeFile(t, root, "opt/crm/etc/manifest.toml", "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
			writeFile(t, root, "var/opt/ikigenba/crm/state/app.db", "live", 0o600)
			watch := newFileAccessWatch(t, filepath.Join(root, "opt/crm/etc/manifest.toml"), filepath.Join(root, "var/opt/ikigenba/crm/state/app.db"))
			env := host.Env{Root: root, Now: func() time.Time { t.Fatal("sampled time before validating configuration"); return time.Time{} }, Execute: unexpectedHostCommand(t)}
			cloudEnv := cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
				t.Fatal("opened cloud storage before validating configuration")
				return nil, nil
			}}
			got, err := backup.Snapshot(context.Background(), env, cloudEnv, store, "crm")
			watch.assertQuiet(t)
			if err == nil || err.Error() != item.want || len(got) != 0 {
				t.Fatalf("Snapshot = %+v, %v; want %q", got, err, item.want)
			}
		})
	}
}

func TestSnapshotReservedDiscoveredNamesFailBeforeDataReads(t *testing.T) {
	// R-DGIT-BUKW
	root := t.TempDir()
	store := configuredFileStore(t, root)
	var names []string
	for _, service := range []string{"deploy", "host", "seed", "snapshots"} {
		writeFile(t, root, "var/opt/ikigenba/"+service+"/state/value", "reserved", 0o600)
		names = append(names, filepath.Join(root, "var/opt/ikigenba", service, "state/value"))
	}
	watch := newFileAccessWatch(t, names...)
	client := newFileCloud()
	executor := &fileExecutor{unmapped: true}
	got, err := backup.Snapshot(context.Background(), fileHostEnv(root, executor.execute), cloud.Env{Open: client.open}, store, "")
	watch.assertQuiet(t)
	if err != nil || len(got) != 4 || len(client.puts) != 0 || len(executor.commands) != 0 {
		t.Fatalf("Snapshot = %+v, %v; puts %v commands %v", got, err, client.puts, executor.commands)
	}
	for i, service := range []string{"deploy", "host", "seed", "snapshots"} {
		if got[i].Service != service || got[i].Err == nil || got[i].Err.Error() != "invalid service \""+service+"\"" {
			t.Fatalf("result = %+v", got[i])
		}
	}
}
