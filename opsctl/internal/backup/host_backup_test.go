package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var _ func(context.Context, host.Env, cloud.Env, config.Store) (backup.FileResult, error) = backup.HostBackup

func TestHostBackupAPIConfigurationAndPreworkflow(t *testing.T) {
	// R-Y5DO-SCJ9 R-YBH6-P78Q R-9TEE-6UL8 R-ANMR-IAT5
	t.Run("prefix absent before region", func(t *testing.T) {
		assertHostConfigurationError(t, config.Store{Root: t.TempDir()}, "backup.s3_uri not set")
	})
	t.Run("prefix empty", func(t *testing.T) {
		store := config.Store{Root: t.TempDir()}
		if err := store.Set("backup.s3_uri", ""); err != nil {
			t.Fatal(err)
		}
		assertHostConfigurationError(t, store, "backup.s3_uri not set")
	})
	t.Run("region absent after prefix", func(t *testing.T) {
		store := config.Store{Root: t.TempDir()}
		if err := store.Set("backup.s3_uri", "s3://bucket/project/"); err != nil {
			t.Fatal(err)
		}
		assertHostConfigurationError(t, store, "aws.region not set")
	})
	t.Run("region empty", func(t *testing.T) {
		store := configuredHostStore(t, t.TempDir())
		if err := store.Set("aws.region", ""); err != nil {
			t.Fatal(err)
		}
		assertHostConfigurationError(t, store, "aws.region not set")
	})
	t.Run("malformed prefix", func(t *testing.T) {
		store := configuredHostStore(t, t.TempDir())
		if err := store.Set("backup.s3_uri", "s3://bucket/good/../escape/"); err != nil {
			t.Fatal(err)
		}
		assertHostConfigurationError(t, store, "backup.s3_uri")
	})
	t.Run("configuration read", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "etc/ikigenba/config.json", "{", 0o600)
		assertHostConfigurationError(t, config.Store{Root: root}, "read backup.s3_uri")
	})

	root := t.TempDir()
	store := configuredHostStore(t, root)
	t.Run("canceled before attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := backup.HostBackup(ctx, host.Env{
			Root: root,
			Now:  func() time.Time { t.Fatal("sampled time after prior cancellation"); return time.Time{} },
			Execute: func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("executed host command")
				return host.Result{}, nil
			},
		}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			t.Fatal("opened cloud storage after prior cancellation")
			return nil, nil
		}}, store)
		assertZeroHostResult(t, result)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("HostBackup() error = %v", err)
		}
	})
	t.Run("time dependency", func(t *testing.T) {
		result, err := backup.HostBackup(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("executed host command")
			return host.Result{}, nil
		}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			t.Fatal("opened cloud storage without time dependency")
			return nil, nil
		}}, store)
		assertZeroHostResult(t, result)
		if err == nil || !strings.Contains(err.Error(), "host time is not configured") {
			t.Fatalf("HostBackup() error = %v", err)
		}
	})
	t.Run("execution dependency", func(t *testing.T) {
		result, err := backup.HostBackup(context.Background(), host.Env{Root: root, Now: time.Now}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			t.Fatal("opened cloud storage without execution dependency")
			return nil, nil
		}}, store)
		assertZeroHostResult(t, result)
		if err == nil || !strings.Contains(err.Error(), "host execution is not configured") {
			t.Fatalf("HostBackup() error = %v", err)
		}
	})
	t.Run("cloud dependency", func(t *testing.T) {
		writeFile(t, root, "etc/letsencrypt/live/site/cert.pem", "unchanged", 0o600)
		before := fileTreeSnapshot(t, root)
		result, err := backup.HostBackup(context.Background(), host.Env{Root: root, Now: time.Now, Execute: unexpectedHostCommand(t)}, cloud.Env{}, store)
		assertZeroHostResult(t, result)
		if err == nil || !strings.Contains(err.Error(), "cloud access is not configured") {
			t.Fatalf("HostBackup() error = %v", err)
		}
		if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
			t.Fatalf("missing cloud dependency changed filesystem:\nbefore %v\nafter  %v", before, after)
		}
	})
	t.Run("cloud opening", func(t *testing.T) {
		result, err := backup.HostBackup(context.Background(), host.Env{Root: root, Now: time.Now, Execute: unexpectedHostCommand(t)}, cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
			if region != "us-east-2" {
				t.Fatalf("region = %q", region)
			}
			return nil, errors.New("storage unavailable")
		}}, store)
		assertZeroHostResult(t, result)
		if err == nil || !strings.Contains(err.Error(), "open backup storage: storage unavailable") {
			t.Fatalf("HostBackup() error = %v", err)
		}
	})
	t.Run("nil opened client", func(t *testing.T) {
		result, err := backup.HostBackup(context.Background(), host.Env{Root: root, Now: time.Now, Execute: unexpectedHostCommand(t)}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			return nil, nil
		}}, store)
		assertZeroHostResult(t, result)
		if err == nil || !strings.Contains(err.Error(), "cloud client is not configured") {
			t.Fatalf("HostBackup() error = %v", err)
		}
	})
}

func TestHostBackupArchiveContentsAndBoundaries(t *testing.T) {
	// R-YCP3-2YZF R-YDWZ-GQQ4 R-9TEE-6UL8 R-YYIY-T743
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/ikigenba/nested/settings", "host settings\n", 0o640)
	writeFile(t, root, "etc/letsencrypt/live/cert.pem", "certificate\n", 0o600)
	writeFile(t, root, "opt/app/state/value", "service data", 0o600)
	writeFile(t, root, "etc/nginx/nginx.conf", "nginx data", 0o600)
	writeFile(t, root, "etc/systemd/system/ikigenba-app.service", "unit data", 0o600)
	if err := os.Symlink("../../outside-certificate", filepath.Join(root, "etc/letsencrypt/live/current")); err != nil {
		t.Fatal(err)
	}
	before := fileTreeSnapshot(t, root)
	forbidden := newFileAccessWatch(t,
		filepath.Join(root, "opt"),
		filepath.Join(root, "etc/nginx"),
		filepath.Join(root, "etc/systemd/system"),
	)
	executor := &fileExecutor{unmapped: true}
	client := newFileCloud()
	nowCalls := 0
	now := time.Date(2026, time.September, 16, 12, 34, 56, 123400000, time.FixedZone("offset", -5*60*60))
	result, err := backup.HostBackup(context.Background(), host.Env{
		Root: root,
		Now: func() time.Time {
			nowCalls++
			return now
		},
		Execute: executor.execute,
	}, cloud.Env{Open: client.open}, store)
	if err != nil || result.Err != nil {
		t.Fatalf("HostBackup() = %+v, %v", result, err)
	}
	if nowCalls != 1 || result.Service != "host" || result.Object != "2026-09-16T17:34:56.1234Z.tar.zst" || result.Size <= 0 {
		t.Fatalf("HostBackup() = %+v, Now calls %d", result, nowCalls)
	}
	wantURI := "s3://bucket/project/host/" + result.Object
	if !reflect.DeepEqual(client.puts, []string{wantURI}) || int64(len(client.objects[wantURI])) != result.Size {
		t.Fatalf("uploads = %v, object bytes = %d, result = %+v", client.puts, len(client.objects[wantURI]), result)
	}
	members := readTestArchive(t, client.objects[wantURI])
	for name, want := range map[string]string{
		"etc/ikigenba/nested/settings":  "host settings\n",
		"etc/letsencrypt/live/cert.pem": "certificate\n",
	} {
		member, ok := members[name]
		if !ok || string(member.data) != want {
			t.Fatalf("archive member %q = %#v", name, member)
		}
	}
	for _, name := range []string{"etc/ikigenba/", "etc/ikigenba/nested/", "etc/letsencrypt/", "etc/letsencrypt/live/"} {
		if _, ok := members[name]; !ok {
			t.Fatalf("archive lacks directory %q: %v", name, sortedHeaderNames(members))
		}
	}
	if got := members["etc/ikigenba/nested/"].header.Mode; got != 0o750 {
		t.Fatalf("nested directory mode = %#o", got)
	}
	if got := members["etc/ikigenba/nested/settings"].header.Mode; got != 0o640 {
		t.Fatalf("settings mode = %#o", got)
	}
	link := members["etc/letsencrypt/live/current"].header
	if link.Typeflag != tar.TypeSymlink || link.Linkname != "../../outside-certificate" {
		t.Fatalf("symlink = %#v", link)
	}
	for name := range members {
		if !strings.HasPrefix(name, "etc/ikigenba") && !strings.HasPrefix(name, "etc/letsencrypt") {
			t.Fatalf("out-of-scope archive member %q", name)
		}
	}
	if !reflect.DeepEqual(executor.commands, []string{"zstd --quiet --stdout"}) {
		t.Fatalf("host commands = %v", executor.commands)
	}
	forbidden.assertQuiet(t)
	if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("HostBackup mutated local filesystem:\nbefore %v\nafter  %v", before, after)
	}

	t.Run("missing source tree is absent", func(t *testing.T) {
		missingRoot := t.TempDir()
		missingStore := configuredHostStore(t, missingRoot)
		missingClient := newFileCloud()
		missingResult, missingErr := backup.HostBackup(context.Background(), fileHostEnv(missingRoot, (&fileExecutor{unmapped: true}).execute), cloud.Env{Open: missingClient.open}, missingStore)
		if missingErr != nil || missingResult.Err != nil {
			t.Fatalf("HostBackup() = %+v, %v", missingResult, missingErr)
		}
		missingMembers := readTestArchive(t, missingClient.objects[missingClient.puts[0]])
		for name := range missingMembers {
			if strings.HasPrefix(name, "etc/letsencrypt") {
				t.Fatalf("missing source appeared as %q", name)
			}
		}
	})
}

func TestHostBackupPreservesSourceRootSymlinksWithoutDereferencing(t *testing.T) {
	// R-YCP3-2YZF
	root := t.TempDir()
	writeFile(t, root, "opt/ikigenba-private/secret", "service secret", 0o600)
	writeFile(t, root, "opt/letsencrypt-private/key.pem", "private key", 0o600)
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"etc/ikigenba":    "../opt/ikigenba-private",
		"etc/letsencrypt": "../opt/letsencrypt-private",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	before := fileTreeSnapshot(t, root)
	forbidden := newFileAccessWatch(t,
		filepath.Join(root, "opt/ikigenba-private"),
		filepath.Join(root, "opt/letsencrypt-private"),
	)
	client := newFileCloud()
	result, err := backup.HostBackup(
		context.Background(),
		fileHostEnv(root, (&fileExecutor{unmapped: true}).execute),
		cloud.Env{Open: client.open},
		configuredHostStore(t, t.TempDir()),
	)
	if err != nil || result.Err != nil {
		t.Fatalf("HostBackup() = %+v, %v", result, err)
	}
	members := readTestArchive(t, client.objects[client.puts[0]])
	if got := sortedHeaderNames(members); !reflect.DeepEqual(got, []string{"etc/ikigenba", "etc/letsencrypt"}) {
		t.Fatalf("archive members = %v", got)
	}
	for name, target := range links {
		header := members[name].header
		if header.Typeflag != tar.TypeSymlink || header.Linkname != target {
			t.Fatalf("archive member %q = %#v", name, header)
		}
	}
	forbidden.assertQuiet(t)
	if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("HostBackup mutated local filesystem:\nbefore %v\nafter  %v", before, after)
	}
}

func TestHostBackupTimestampAndCreateOnlyCollision(t *testing.T) {
	// R-YDWZ-GQQ4 R-9TEE-6UL8
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/letsencrypt/cert.pem", "certificate", 0o600)
	executor := &fileExecutor{unmapped: true}
	client := newFileCloud()
	times := []time.Time{
		time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 6, 7, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 6, 7, time.UTC),
	}
	nowCalls := 0
	env := host.Env{Root: root, Execute: executor.execute, Now: func() time.Time {
		value := times[nowCalls]
		nowCalls++
		return value
	}}
	first, err := backup.HostBackup(context.Background(), env, cloud.Env{Open: client.open}, store)
	if err != nil || first.Err != nil || first.Object != "2026-01-02T03:04:05Z.tar.zst" {
		t.Fatalf("first HostBackup() = %+v, %v", first, err)
	}
	second, err := backup.HostBackup(context.Background(), env, cloud.Env{Open: client.open}, store)
	if err != nil || second.Err != nil || second.Object != "2026-01-02T03:04:06.000000007Z.tar.zst" {
		t.Fatalf("second HostBackup() = %+v, %v", second, err)
	}
	before := cloneObjects(client.objects)
	collision, err := backup.HostBackup(context.Background(), env, cloud.Env{Open: client.open}, store)
	if err != nil || collision.Service != "host" || !errors.Is(collision.Err, cloud.ErrAlreadyExists) || collision.Object != "" || collision.Size != 0 || nowCalls != 3 {
		t.Fatalf("collision HostBackup() = %+v, %v, Now calls %d", collision, err, nowCalls)
	}
	if !reflect.DeepEqual(client.objects, before) {
		t.Fatal("collision replaced an existing host object")
	}
}

func TestHostBackupAttemptFailuresAndInterruptions(t *testing.T) {
	// R-9TEE-6UL8 R-GWME-QK2L
	storeRoot := t.TempDir()
	store := configuredHostStore(t, storeRoot)
	t.Run("archive read", func(t *testing.T) {
		client := newFileCloud()
		result, err := backup.HostBackup(context.Background(), fileHostEnv(filepath.Join(t.TempDir(), "missing"), (&fileExecutor{unmapped: true}).execute), cloud.Env{Open: client.open}, store)
		if err != nil || result.Service != "host" || result.Err == nil || len(client.puts) != 0 {
			t.Fatalf("HostBackup() = %+v, %v, uploads %v", result, err, client.puts)
		}
	})
	t.Run("compression", func(t *testing.T) {
		executor := &fileExecutor{unmapped: true, compressionResponses: []host.Result{{Stderr: []byte("compression failed\n"), ExitCode: 1}}}
		client := newFileCloud()
		result, err := backup.HostBackup(context.Background(), fileHostEnv(storeRoot, executor.execute), cloud.Env{Open: client.open}, store)
		var commandErr *host.CommandError
		if err != nil || result.Service != "host" || !errors.As(result.Err, &commandErr) || len(client.puts) != 0 {
			t.Fatalf("HostBackup() = %+v, %v, uploads %v", result, err, client.puts)
		}
	})
	t.Run("upload", func(t *testing.T) {
		client := newFileCloud()
		client.fail = func(string, []byte) error { return errors.New("upload unavailable") }
		result, err := backup.HostBackup(context.Background(), fileHostEnv(storeRoot, (&fileExecutor{unmapped: true}).execute), cloud.Env{Open: client.open}, store)
		if err != nil || result.Service != "host" || result.Err == nil || !strings.Contains(result.Err.Error(), "upload unavailable") || len(client.objects) != 0 {
			t.Fatalf("HostBackup() = %+v, %v, objects %v", result, err, client.objects)
		}
	})
	t.Run("compression interruption", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		executor := &fileExecutor{unmapped: true}
		execute := func(ctx context.Context, command host.Command) (host.Result, error) {
			result, executeErr := executor.execute(ctx, command)
			if command.Name == "zstd" {
				cancel()
			}
			return result, executeErr
		}
		client := newFileCloud()
		result, err := backup.HostBackup(ctx, fileHostEnv(storeRoot, execute), cloud.Env{Open: client.open}, store)
		if !errors.Is(err, context.Canceled) || result.Service != "host" || !errors.Is(result.Err, context.Canceled) || len(client.puts) != 0 {
			t.Fatalf("HostBackup() = %+v, %v, uploads %v", result, err, client.puts)
		}
	})
	t.Run("upload interruption", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := newFileCloud()
		client.fail = func(string, []byte) error {
			cancel()
			return context.Canceled
		}
		result, err := backup.HostBackup(ctx, fileHostEnv(storeRoot, (&fileExecutor{unmapped: true}).execute), cloud.Env{Open: client.open}, store)
		if !errors.Is(err, context.Canceled) || result.Service != "host" || !errors.Is(result.Err, context.Canceled) || len(client.objects) != 0 {
			t.Fatalf("HostBackup() = %+v, %v, objects %v", result, err, client.objects)
		}
	})
	t.Run("interruption after upload", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := newFileCloud()
		client.fail = func(string, []byte) error {
			cancel()
			return nil
		}
		result, err := backup.HostBackup(ctx, fileHostEnv(storeRoot, (&fileExecutor{unmapped: true}).execute), cloud.Env{Open: client.open}, store)
		if !errors.Is(err, context.Canceled) || result.Service != "host" || !errors.Is(result.Err, context.Canceled) || len(client.objects) != 1 {
			t.Fatalf("HostBackup() = %+v, %v, objects %v", result, err, client.objects)
		}
	})
}

func assertHostConfigurationError(t *testing.T, store config.Store, want string) {
	t.Helper()
	result, err := backup.HostBackup(context.Background(), host.Env{
		Root: filepath.Join(t.TempDir(), "missing-root"),
		Now: func() time.Time {
			t.Fatal("sampled time before validating configuration")
			return time.Time{}
		},
		Execute: unexpectedHostCommand(t),
	}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
		t.Fatal("opened cloud storage before validating configuration")
		return nil, nil
	}}, store)
	assertZeroHostResult(t, result)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("HostBackup() error = %v, want containing %q", err, want)
	}
}

func configuredHostStore(t *testing.T, root string) config.Store {
	t.Helper()
	store := config.Store{Root: root}
	for key, value := range map[string]string{"backup.s3_uri": "s3://bucket/project/", "aws.region": "us-east-2"} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func assertZeroHostResult(t *testing.T, result backup.FileResult) {
	t.Helper()
	if !reflect.DeepEqual(result, backup.FileResult{}) {
		t.Fatalf("HostBackup() result = %+v, want zero value", result)
	}
}

func unexpectedHostCommand(t *testing.T) func(context.Context, host.Command) (host.Result, error) {
	t.Helper()
	return func(_ context.Context, command host.Command) (host.Result, error) {
		t.Fatalf("unexpected host command: %s", fmt.Sprint(command))
		return host.Result{}, nil
	}
}
