package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var _ func(context.Context, host.Env, cloud.Env, config.Store, string) ([]backup.FileResult, error) = backup.Files

func TestFilesAPIAndConfigurationBoundary(t *testing.T) {
	// R-D7XD-PK5R R-D95A-3BWG R-DAD6-H3N5 R-ANMR-IAT5
	wantFields := []struct {
		name string
		typ  reflect.Type
	}{
		{"Service", reflect.TypeFor[string]()},
		{"Object", reflect.TypeFor[string]()},
		{"Size", reflect.TypeFor[int64]()},
		{"Err", reflect.TypeFor[error]()},
	}
	resultType := reflect.TypeFor[backup.FileResult]()
	if resultType.NumField() != len(wantFields) {
		t.Fatalf("FileResult has %d fields, want %d", resultType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := resultType.Field(index)
		if field.Name != want.name || field.Type != want.typ {
			t.Fatalf("FileResult field %d = %s %v, want %s %v", index, field.Name, field.Type, want.name, want.typ)
		}
	}

	t.Run("prefix before region", func(t *testing.T) {
		store := config.Store{Root: t.TempDir()}
		assertPreworkflowError(t, store, "backup.s3_uri not set")
	})
	t.Run("empty prefix", func(t *testing.T) {
		store := configuredFileStore(t, t.TempDir())
		if err := store.Set("backup.s3_uri", ""); err != nil {
			t.Fatal(err)
		}
		assertPreworkflowError(t, store, "backup.s3_uri not set")
	})
	t.Run("region after prefix", func(t *testing.T) {
		root := t.TempDir()
		store := config.Store{Root: root}
		if err := store.Set("backup.s3_uri", "s3://bucket/host/"); err != nil {
			t.Fatal(err)
		}
		assertPreworkflowError(t, store, "aws.region not set")
	})
	t.Run("malformed prefix", func(t *testing.T) {
		store := configuredFileStore(t, t.TempDir())
		if err := store.Set("backup.s3_uri", "s3://bucket/good/../escape/"); err != nil {
			t.Fatal(err)
		}
		assertPreworkflowError(t, store, "backup.s3_uri")
	})
	t.Run("configuration read", func(t *testing.T) {
		root := t.TempDir()
		store := config.Store{Root: root}
		if err := os.MkdirAll(filepath.Join(root, "etc/ikigenba"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "etc/ikigenba/config.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertPreworkflowError(t, store, "read backup.s3_uri")
	})
	t.Run("cloud dependency", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		writeFile(t, root, "opt/notes/state/value", "unchanged", 0o600)
		before := fileTreeSnapshot(t, root)
		executions := 0
		results, err := backup.Files(context.Background(), host.Env{
			Root: root,
			Now:  func() time.Time { return time.Unix(1, 0) },
			Execute: func(context.Context, host.Command) (host.Result, error) {
				executions++
				return host.Result{}, errors.New("unexpected host command")
			},
		}, cloud.Env{}, store, "notes")
		if err == nil || !strings.Contains(err.Error(), "cloud access is not configured") || len(results) != 0 {
			t.Fatalf("Files() = %+v, %v", results, err)
		}
		if executions != 0 || !reflect.DeepEqual(fileTreeSnapshot(t, root), before) {
			t.Fatalf("missing cloud dependency effects: executions %d", executions)
		}
	})
}

func TestFilesSelectsAndArchivesServiceTrees(t *testing.T) {
	// R-RPWA-HQD2 R-DCSZ-8N4J R-DE0V-MEV8 R-Z9DH-5EZK
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/alpha/etc/manifest.toml", "app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o640)
	writeFile(t, root, "opt/alpha/etc/env", "TOKEN=value\n", 0o640)
	writeFile(t, root, "opt/alpha/state/data.txt", "ordinary\n", 0o600)
	writeFile(t, root, "opt/alpha/state/app.db", "database", 0o600)
	writeFile(t, root, "opt/alpha/state/app.db-wal", "wal", 0o600)
	writeFile(t, root, "opt/alpha/state/app.db-shm", "shm", 0o600)
	writeFile(t, root, "opt/alpha/state/.app.db-litestream/generation", "metadata", 0o600)
	writeFile(t, root, "opt/alpha/cache/item", "cache", 0o600)
	writeFile(t, root, "opt/alpha/bin/alpha", "binary", 0o700)
	writeFile(t, root, "opt/alpha/share/item", "share", 0o600)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "opt/alpha/state/outside")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "opt/plain/state/app.db", "ordinary without manifest", 0o644)
	writeFile(t, root, "opt/zeta/etc/env", "Z=1\n", 0o600)
	writeFile(t, root, "opt/ignored/cache/item", "ignored", 0o600)
	writeFile(t, root, "etc/systemd/system/generated.service", "unit", 0o644)
	for name, mode := range map[string]fs.FileMode{
		"opt/alpha/etc":               0o711,
		"opt/alpha/state":             0o750,
		"opt/alpha/etc/env":           0o640,
		"opt/alpha/etc/manifest.toml": 0o440,
		"opt/alpha/state/data.txt":    0o604,
	} {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(name)), mode); err != nil {
			t.Fatal(err)
		}
	}
	before := fileTreeSnapshot(t, root)

	executor := &fileExecutor{uid: os.Getuid(), gid: os.Getgid(), user: "ikigenba", group: "ikigenba"}
	client := newFileCloud()
	now := time.Date(2026, time.September, 16, 12, 34, 56, 123400000, time.FixedZone("offset", -5*60*60))
	results, err := backup.Files(context.Background(), host.Env{Root: root, Now: func() time.Time { return now }, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultNames(results); !reflect.DeepEqual(got, []string{"alpha", "plain", "zeta"}) {
		t.Fatalf("result services = %v", got)
	}
	wantSizes := map[string]int64{"alpha": 5648, "plain": 2576, "zeta": 2576}
	for _, result := range results {
		if result.Err != nil || result.Object != "2026-09-16T17:34:56.1234Z.tar.zst" || result.Size != wantSizes[result.Service] {
			t.Fatalf("result = %+v", result)
		}
		uri := "s3://bucket/host/" + result.Service + "/" + result.Object
		if int64(len(client.objects[uri])) != result.Size {
			t.Fatalf("object %q size = %d, result = %d", uri, len(client.objects[uri]), result.Size)
		}
	}

	alpha := readTestArchive(t, client.objects["s3://bucket/host/alpha/2026-09-16T17:34:56.1234Z.tar.zst"])
	wantAlpha := []string{"etc/", "etc/env", "etc/manifest.toml", "state/", "state/data.txt", "state/outside"}
	if got := sortedHeaderNames(alpha); !reflect.DeepEqual(got, wantAlpha) {
		t.Fatalf("alpha members = %v, want %v", got, wantAlpha)
	}
	if got := string(alpha["etc/env"].data); got != "TOKEN=value\n" {
		t.Fatalf("etc/env = %q", got)
	}
	for name, want := range map[string]int64{
		"etc/":              0o711,
		"state/":            0o750,
		"etc/env":           0o640,
		"etc/manifest.toml": 0o440,
		"state/data.txt":    0o604,
	} {
		if got := alpha[name].header.Mode; got != want {
			t.Fatalf("%s mode = %#o, want %#o", name, got, want)
		}
	}
	link := alpha["state/outside"].header
	if link.Typeflag != tar.TypeSymlink || link.Linkname != "/etc/passwd" || len(alpha["state/outside"].data) != 0 {
		t.Fatalf("outside symlink = %#v", link)
	}
	for name, member := range alpha {
		if member.header.Uid != os.Getuid() || member.header.Gid != os.Getgid() || member.header.Uname != "ikigenba" || member.header.Gname != "ikigenba" {
			t.Fatalf("%s ownership = %d:%d %q:%q", name, member.header.Uid, member.header.Gid, member.header.Uname, member.header.Gname)
		}
	}
	plain := readTestArchive(t, client.objects["s3://bucket/host/plain/2026-09-16T17:34:56.1234Z.tar.zst"])
	if got := string(plain["state/app.db"].data); got != "ordinary without manifest" {
		t.Fatalf("manifest-free state/app.db = %q", got)
	}
	if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("Files mutated local filesystem:\nbefore %v\nafter  %v", before, after)
	}
	for _, command := range executor.commands {
		if !strings.HasPrefix(command, "getent passwd ") && !strings.HasPrefix(command, "getent group ") && command != "zstd --quiet --stdout" {
			t.Fatalf("unexpected host command %q", command)
		}
	}
}

func TestFilesExplicitSelectionAndInvalidDiscoveredName(t *testing.T) {
	// R-RPWA-HQD2
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/state/value", "notes", 0o600)
	writeFile(t, root, "opt/other/etc/manifest.toml", "[broken\n", 0o600)
	writeFile(t, root, "opt/host/state/value", "reserved", 0o600)
	writeFile(t, root, "opt/neither/cache/value", "not a service", 0o600)
	executor := &fileExecutor{uid: os.Getuid(), gid: os.Getgid(), unmapped: true}
	client := newFileCloud()
	env := host.Env{Root: root, Now: func() time.Time { return time.Unix(1, 0) }, Execute: executor.execute}

	otherAccess := newFileAccessWatch(t,
		filepath.Join(root, "opt/other"),
		filepath.Join(root, "opt/other/etc/manifest.toml"),
	)
	results, err := backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, "notes")
	otherAccess.assertQuiet(t)
	otherAccess.close()
	if err != nil || len(results) != 1 || results[0].Service != "notes" || results[0].Err != nil {
		t.Fatalf("explicit Files() = %+v, %v", results, err)
	}
	if len(client.puts) != 1 || !strings.Contains(client.puts[0], "/notes/") {
		t.Fatalf("uploads = %v", client.puts)
	}

	for _, name := range []string{".", "..", "nested/name", "nul\x00name", "host", "deploy"} {
		before := len(client.puts)
		serviceAccess := newFileAccessWatch(t, filepath.Join(root, "opt"))
		results, err = backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, name)
		serviceAccess.assertQuiet(t)
		serviceAccess.close()
		if err == nil || len(results) != 0 || len(client.puts) != before {
			t.Fatalf("Files(%q) = %+v, %v, uploads %v", name, results, err, client.puts)
		}
	}
	for _, name := range []string{"missing", "neither"} {
		otherAccess := newFileAccessWatch(t,
			filepath.Join(root, "opt/other"),
			filepath.Join(root, "opt/other/etc/manifest.toml"),
		)
		results, err = backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, name)
		otherAccess.assertQuiet(t)
		otherAccess.close()
		if err == nil || err.Error() != "no service '"+name+"'" || len(results) != 0 {
			t.Fatalf("Files(%q) = %+v, %v", name, results, err)
		}
	}

	client = newFileCloud()
	results, err = backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultNames(results); !reflect.DeepEqual(got, []string{"host", "notes", "other"}) {
		t.Fatalf("all results = %v", got)
	}
	if results[0].Err == nil || results[1].Err != nil || results[2].Err == nil {
		t.Fatalf("all results = %+v", results)
	}
	if len(client.puts) != 1 || !strings.Contains(client.puts[0], "/notes/") {
		t.Fatalf("all-service uploads = %v", client.puts)
	}

	t.Run("empty discovery", func(t *testing.T) {
		emptyRoot := t.TempDir()
		emptyStore := configuredFileStore(t, emptyRoot)
		got, discoverErr := backup.Files(context.Background(), host.Env{
			Root: emptyRoot,
			Now:  func() time.Time { return time.Unix(3, 0) },
		}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			t.Fatal("opened cloud client for empty service set")
			return nil, nil
		}}, emptyStore, "")
		if discoverErr != nil || len(got) != 0 {
			t.Fatalf("empty Files() = %+v, %v", got, discoverErr)
		}
	})

	t.Run("discovery failure", func(t *testing.T) {
		missingRoot := filepath.Join(t.TempDir(), "missing")
		got, discoverErr := backup.Files(context.Background(), host.Env{
			Root: missingRoot,
			Now:  func() time.Time { return time.Unix(3, 0) },
		}, cloud.Env{}, store, "")
		if discoverErr == nil || len(got) != 0 || !strings.Contains(discoverErr.Error(), "discover") {
			t.Fatalf("discovery Files() = %+v, %v", got, discoverErr)
		}
	})
}

func TestFilesServiceFailuresContinueWithoutPartialObjects(t *testing.T) {
	// R-I3CZ-C0Q2 R-GWME-QK2L
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/alpha/state/value", "alpha", 0o600)
	writeFile(t, root, "opt/beta/etc/manifest.toml", "[broken\n", 0o600)
	if err := os.MkdirAll(filepath.Join(root, "opt/delta/etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "opt/delta/etc/manifest.toml")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "opt/gamma/state/unreadable", "gamma", 0o000)
	writeFile(t, root, "opt/omega/state/value", "omega", 0o600)
	executor := &fileExecutor{uid: os.Getuid(), gid: os.Getgid(), unmapped: true}
	client := newFileCloud()
	client.fail = func(uri string, _ []byte) error {
		if strings.Contains(uri, "/alpha/") {
			return errors.New("upload unavailable")
		}
		return nil
	}
	env := host.Env{Root: root, Now: func() time.Time { return time.Unix(2, 0) }, Execute: executor.execute}
	results, err := backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultNames(results); !reflect.DeepEqual(got, []string{"alpha", "beta", "delta", "gamma", "omega"}) {
		t.Fatalf("result services = %v", got)
	}
	for index := range 4 {
		if results[index].Err == nil {
			t.Fatalf("result %d unexpectedly succeeded: %+v", index, results[index])
		}
	}
	if results[4].Err != nil {
		t.Fatalf("later service failed: %v", results[4].Err)
	}
	for _, uri := range client.puts {
		if strings.Contains(uri, "/beta/") || strings.Contains(uri, "/delta/") || strings.Contains(uri, "/gamma/") {
			t.Fatalf("failed local archive was uploaded: %q", uri)
		}
	}

	t.Run("compression failure", func(t *testing.T) {
		cleanRoot := t.TempDir()
		cleanStore := configuredFileStore(t, cleanRoot)
		writeFile(t, cleanRoot, "opt/alpha/state/value", "alpha", 0o600)
		writeFile(t, cleanRoot, "opt/beta/state/value", "beta", 0o600)
		failingExecutor := &fileExecutor{
			unmapped: true,
			compressionResponses: []host.Result{{
				Stderr:   []byte("compression failed\n"),
				ExitCode: 1,
			}},
		}
		compressionClient := newFileCloud()
		got, runErr := backup.Files(context.Background(), fileHostEnv(cleanRoot, failingExecutor.execute), cloud.Env{Open: compressionClient.open}, cleanStore, "")
		var commandErr *host.CommandError
		if runErr != nil || len(got) != 2 || !errors.As(got[0].Err, &commandErr) || got[1].Err != nil {
			t.Fatalf("compression Files() = %+v, %v", got, runErr)
		}
		if len(compressionClient.puts) != 1 || !strings.Contains(compressionClient.puts[0], "/beta/") {
			t.Fatalf("compression uploads = %v", compressionClient.puts)
		}
	})

	t.Run("interruption after successful compression", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cleanRoot := t.TempDir()
		cleanStore := configuredFileStore(t, cleanRoot)
		for _, name := range []string{"alpha", "beta"} {
			writeFile(t, cleanRoot, "opt/"+name+"/state/value", name, 0o600)
		}
		executor := &fileExecutor{uid: os.Getuid(), gid: os.Getgid(), unmapped: true}
		execute := func(ctx context.Context, command host.Command) (host.Result, error) {
			result, executeErr := executor.execute(ctx, command)
			if command.Name == "zstd" && executeErr == nil && result.ExitCode == 0 {
				cancel()
			}
			return result, executeErr
		}
		interrupting := newFileCloud()
		got, runErr := backup.Files(ctx, fileHostEnv(cleanRoot, execute), cloud.Env{Open: interrupting.open}, cleanStore, "")
		if !errors.Is(runErr, context.Canceled) || len(got) != 1 || !errors.Is(got[0].Err, context.Canceled) {
			t.Fatalf("compression interruption Files() = %+v, %v", got, runErr)
		}
		if len(interrupting.puts) != 0 {
			t.Fatalf("compression interruption uploads = %v", interrupting.puts)
		}
		if got := strings.Count(strings.Join(executor.commands, "\n"), "zstd --quiet --stdout"); got != 1 {
			t.Fatalf("compression commands = %v", executor.commands)
		}
	})

	t.Run("interruption during attempted upload", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		interrupting := newFileCloud()
		interrupting.fail = func(uri string, _ []byte) error {
			if strings.Contains(uri, "/beta/") {
				cancel()
				return context.Canceled
			}
			return nil
		}
		cleanRoot := t.TempDir()
		cleanStore := configuredFileStore(t, cleanRoot)
		for _, name := range []string{"alpha", "beta", "gamma"} {
			writeFile(t, cleanRoot, "opt/"+name+"/state/value", name, 0o600)
		}
		interruptEnv := env
		interruptEnv.Root = cleanRoot
		got, runErr := backup.Files(ctx, interruptEnv, cloud.Env{Open: interrupting.open}, cleanStore, "")
		if !errors.Is(runErr, context.Canceled) || len(got) != 2 || got[0].Err != nil || !errors.Is(got[1].Err, context.Canceled) {
			t.Fatalf("interrupted Files() = %+v, %v", got, runErr)
		}
		if strings.Contains(strings.Join(interrupting.puts, "\n"), "/gamma/") {
			t.Fatalf("unattempted gamma upload in %v", interrupting.puts)
		}
	})

	t.Run("interruption during successful upload", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		interrupting := newFileCloud()
		interrupting.fail = func(_ string, _ []byte) error {
			cancel()
			return nil
		}
		cleanRoot := t.TempDir()
		cleanStore := configuredFileStore(t, cleanRoot)
		writeFile(t, cleanRoot, "opt/alpha/state/value", "alpha", 0o600)
		writeFile(t, cleanRoot, "opt/beta/state/value", "beta", 0o600)
		interruptEnv := env
		interruptEnv.Root = cleanRoot
		got, runErr := backup.Files(ctx, interruptEnv, cloud.Env{Open: interrupting.open}, cleanStore, "")
		if !errors.Is(runErr, context.Canceled) || len(got) != 1 || !errors.Is(got[0].Err, context.Canceled) || got[0].Object != "" || got[0].Size != 0 {
			t.Fatalf("successful-upload interruption Files() = %+v, %v", got, runErr)
		}
		if len(interrupting.puts) != 1 || !strings.Contains(interrupting.puts[0], "/alpha/") || len(interrupting.objects) != 1 {
			t.Fatalf("successful-upload interruption uploads = %v, objects = %v", interrupting.puts, interrupting.objects)
		}
	})
}

func TestFilesTimestampSetsAndCreateOnlyCollision(t *testing.T) {
	// R-ZMEX-3VL4 R-AMEV-4J2G
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/alpha/state/value", "alpha", 0o600)
	writeFile(t, root, "opt/zeta/state/value", "zeta", 0o600)
	executor := &fileExecutor{uid: os.Getuid(), gid: os.Getgid(), unmapped: true}
	client := newFileCloud()
	times := []time.Time{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), time.Date(2026, 1, 2, 3, 4, 6, 7, time.UTC), time.Date(2026, 1, 2, 3, 4, 6, 7, time.UTC)}
	nowCalls := 0
	env := host.Env{Root: root, Execute: executor.execute, Now: func() time.Time {
		value := times[nowCalls]
		nowCalls++
		return value
	}}

	first, err := backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, "")
	if err != nil || len(first) != 2 {
		t.Fatalf("first Files() = %+v, %v", first, err)
	}
	for _, result := range first {
		if result.Object != "2026-01-02T03:04:05Z.tar.zst" {
			t.Fatalf("whole-second object = %q", result.Object)
		}
	}
	second, err := backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, "")
	if err != nil || len(second) != 2 {
		t.Fatalf("second Files() = %+v, %v", second, err)
	}
	for _, result := range second {
		if result.Object != "2026-01-02T03:04:06.000000007Z.tar.zst" {
			t.Fatalf("later object = %q", result.Object)
		}
	}
	before := cloneObjects(client.objects)
	collisions, err := backup.Files(context.Background(), env, cloud.Env{Open: client.open}, store, "")
	if err != nil || len(collisions) != 2 || nowCalls != 3 {
		t.Fatalf("collision Files() = %+v, %v, Now calls %d", collisions, err, nowCalls)
	}
	for _, result := range collisions {
		if !errors.Is(result.Err, cloud.ErrAlreadyExists) || result.Object != "" || result.Size != 0 {
			t.Fatalf("collision result = %+v", result)
		}
	}
	if !reflect.DeepEqual(client.objects, before) {
		t.Fatal("collision replaced an existing object")
	}
}

func TestFilesIdentityLookupFailureAndUnmappedOwnership(t *testing.T) {
	// R-Z9DH-5EZK
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/state/value", "notes", 0o600)

	t.Run("execution failure", func(t *testing.T) {
		executor := &fileExecutor{identityErr: errors.New("getent unavailable")}
		client := newFileCloud()
		results, err := backup.Files(context.Background(), fileHostEnv(root, executor.execute), cloud.Env{Open: client.open}, store, "notes")
		var commandErr *host.CommandError
		if err != nil || len(results) != 1 || !errors.As(results[0].Err, &commandErr) || len(client.puts) != 0 {
			t.Fatalf("Files() = %+v, %v, uploads %v", results, err, client.puts)
		}
	})

	t.Run("unmapped numeric identity", func(t *testing.T) {
		executor := &fileExecutor{unmapped: true}
		client := newFileCloud()
		results, err := backup.Files(context.Background(), fileHostEnv(root, executor.execute), cloud.Env{Open: client.open}, store, "notes")
		if err != nil || len(results) != 1 || results[0].Err != nil {
			t.Fatalf("Files() = %+v, %v", results, err)
		}
		members := readTestArchive(t, client.objects[client.puts[0]])
		for name, member := range members {
			if member.header.Uid != os.Getuid() || member.header.Gid != os.Getgid() || member.header.Uname != "" || member.header.Gname != "" {
				t.Fatalf("%s unmapped ownership = %d:%d %q:%q", name, member.header.Uid, member.header.Gid, member.header.Uname, member.header.Gname)
			}
		}
	})
}

func assertPreworkflowError(t *testing.T, store config.Store, want string) {
	t.Helper()
	results, err := backup.Files(context.Background(), host.Env{
		Root: filepath.Join(t.TempDir(), "missing-root"),
		Now:  func() time.Time { t.Fatal("sampled time before validating configuration"); return time.Time{} },
		Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("executed a host command before validating configuration")
			return host.Result{}, nil
		},
	}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
		t.Fatal("opened cloud storage before validating configuration")
		return nil, nil
	}}, store, "notes")
	if err == nil || !strings.Contains(err.Error(), want) || len(results) != 0 {
		t.Fatalf("Files() = %+v, %v, want error containing %q", results, err, want)
	}
}

func configuredFileStore(t *testing.T, root string) config.Store {
	t.Helper()
	store := config.Store{Root: root}
	for key, value := range map[string]string{"backup.s3_uri": "s3://bucket/host/", "aws.region": "us-east-2"} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func writeFile(t *testing.T, root, name, contents string, mode fs.FileMode) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func fileHostEnv(root string, execute func(context.Context, host.Command) (host.Result, error)) host.Env {
	return host.Env{Root: root, Execute: execute, Now: func() time.Time { return time.Unix(1, 0) }}
}

func resultNames(results []backup.FileResult) []string {
	names := make([]string, len(results))
	for index, result := range results {
		names[index] = result.Service
	}
	return names
}

type fileCloud struct {
	objects map[string][]byte
	puts    []string
	fail    func(string, []byte) error
}

type fileAccessWatch struct {
	fd int
}

func newFileAccessWatch(t *testing.T, names ...string) *fileAccessWatch {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	watch := &fileAccessWatch{fd: fd}
	t.Cleanup(watch.close)
	for _, name := range names {
		if _, err := syscall.InotifyAddWatch(fd, name, syscall.IN_ACCESS|syscall.IN_OPEN); err != nil {
			watch.close()
			t.Fatal(err)
		}
	}
	return watch
}

func (watch *fileAccessWatch) assertQuiet(t *testing.T) {
	t.Helper()
	buffer := make([]byte, syscall.SizeofInotifyEvent*4)
	n, err := syscall.Read(watch.fd, buffer)
	if errors.Is(err, syscall.EAGAIN) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("observed %d bytes of unexpected filesystem access events", n)
	}
}

func (watch *fileAccessWatch) close() {
	if watch.fd >= 0 {
		_ = syscall.Close(watch.fd)
		watch.fd = -1
	}
}

func newFileCloud() *fileCloud {
	return &fileCloud{objects: make(map[string][]byte)}
}

func (client *fileCloud) open(_ context.Context, region string) (cloud.Client, error) {
	if region != "us-east-2" {
		return nil, fmt.Errorf("region = %q", region)
	}
	return client, nil
}

func (*fileCloud) GetObject(context.Context, string) (io.ReadCloser, error) {
	panic("unexpected GetObject")
}

func (client *fileCloud) PutObject(_ context.Context, uri string, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	client.puts = append(client.puts, uri)
	if client.fail != nil {
		if err := client.fail(uri, data); err != nil {
			return err
		}
	}
	if _, exists := client.objects[uri]; exists {
		return cloud.ErrAlreadyExists
	}
	client.objects[uri] = append([]byte(nil), data...)
	return nil
}

func (*fileCloud) ListObjects(context.Context, string) ([]cloud.Object, error) {
	panic("unexpected ListObjects")
}

func (*fileCloud) ReadSecrets(context.Context, string) (map[string]string, error) {
	panic("unexpected ReadSecrets")
}

type fileExecutor struct {
	uid                  int
	gid                  int
	user                 string
	group                string
	unmapped             bool
	identityErr          error
	compressionResponses []host.Result
	commands             []string
}

func (executor *fileExecutor) execute(_ context.Context, command host.Command) (host.Result, error) {
	executor.commands = append(executor.commands, strings.Join(append([]string{command.Name}, command.Args...), " "))
	switch command.Name {
	case "getent":
		if executor.identityErr != nil {
			return host.Result{}, executor.identityErr
		}
		if executor.unmapped {
			return host.Result{ExitCode: 2}, nil
		}
		if len(command.Args) != 2 {
			return host.Result{ExitCode: 1}, nil
		}
		switch command.Args[0] {
		case "passwd":
			return host.Result{Stdout: []byte(fmt.Sprintf("%s:x:%d:%d::/nonexistent:/usr/sbin/nologin\n", executor.user, executor.uid, executor.gid))}, nil
		case "group":
			return host.Result{Stdout: []byte(fmt.Sprintf("%s:x:%d:\n", executor.group, executor.gid))}, nil
		}
		return host.Result{ExitCode: 1}, nil
	case "zstd":
		if len(executor.compressionResponses) > 0 {
			response := executor.compressionResponses[0]
			executor.compressionResponses = executor.compressionResponses[1:]
			return response, nil
		}
		data, err := io.ReadAll(command.Stdin)
		if err != nil {
			return host.Result{}, err
		}
		return host.Result{Stdout: rawZstandardFrame(data)}, nil
	default:
		return host.Result{}, fmt.Errorf("unexpected command %q", command.Name)
	}
}

var rawZstandardHeader = []byte{0x28, 0xb5, 0x2f, 0xfd, 0xe0}

func rawZstandardFrame(data []byte) []byte {
	var frame bytes.Buffer
	frame.Write(rawZstandardHeader)
	var encodedSize [8]byte
	binary.LittleEndian.PutUint64(encodedSize[:], decimalUint64(len(data)))
	frame.Write(encodedSize[:])
	for len(data) > 0 {
		size := min(len(data), 128*1024)
		header := decimalUint64(size) << 3
		if size == len(data) {
			header |= 1
		}
		var encodedHeader [8]byte
		binary.LittleEndian.PutUint64(encodedHeader[:], header)
		frame.Write(encodedHeader[:3])
		frame.Write(data[:size])
		data = data[size:]
	}
	return frame.Bytes()
}

func decodeRawZstandardFrame(t *testing.T, frame []byte) []byte {
	t.Helper()
	if len(frame) < 13 || !bytes.Equal(frame[:len(rawZstandardHeader)], rawZstandardHeader) {
		t.Fatalf("invalid zstd frame header: %x", frame)
	}
	wantSize := binary.LittleEndian.Uint64(frame[5:13])
	position := 13
	var decoded bytes.Buffer
	for {
		if position+3 > len(frame) {
			t.Fatal("truncated zstd block header")
		}
		header := uint32(frame[position]) | uint32(frame[position+1])<<8 | uint32(frame[position+2])<<16
		position += 3
		last := header&1 != 0
		if header>>1&3 != 0 {
			t.Fatalf("zstd block is not raw: %#x", header)
		}
		size := int(header >> 3)
		if position+size > len(frame) {
			t.Fatal("truncated zstd raw block")
		}
		decoded.Write(frame[position : position+size])
		position += size
		if last {
			break
		}
	}
	if position != len(frame) || strconv.Itoa(decoded.Len()) != strconv.FormatUint(wantSize, 10) {
		t.Fatalf("zstd frame sizes: consumed %d/%d, decoded %d/%d", position, len(frame), decoded.Len(), wantSize)
	}
	return decoded.Bytes()
}

func decimalUint64(value int) uint64 {
	parsed, err := strconv.ParseUint(strconv.Itoa(value), 10, 64)
	if err != nil {
		panic(err)
	}
	return parsed
}

type archivedMember struct {
	header tar.Header
	data   []byte
}

func readTestArchive(t *testing.T, compressed []byte) map[string]archivedMember {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(decodeRawZstandardFrame(t, compressed)))
	members := make(map[string]archivedMember)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		members[header.Name] = archivedMember{header: *header, data: data}
	}
	return members
}

func sortedHeaderNames(members map[string]archivedMember) []string {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cloneObjects(objects map[string][]byte) map[string][]byte {
	clone := make(map[string][]byte, len(objects))
	for uri, data := range objects {
		clone[uri] = append([]byte(nil), data...)
	}
	return clone
}

func fileTreeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()
	var snapshot []string
	err = filepath.WalkDir(root, func(name string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		line := filepath.ToSlash(relative) + " " + info.Mode().String() + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			line += " -> " + target
		case info.Mode().IsRegular():
			data, err := rootFS.ReadFile(filepath.ToSlash(relative))
			if err != nil {
				if info.Mode().Perm() == 0 {
					line += " unreadable"
					break
				}
				return err
			}
			line += " " + string(data)
		}
		snapshot = append(snapshot, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
