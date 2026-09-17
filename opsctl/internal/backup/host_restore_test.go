package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

var _ func(context.Context, host.Env, cloud.Env, config.Store) (backup.HostRestoreResult, error) = backup.HostRestore

func TestHostRestoreAPI(t *testing.T) {
	// R-Y6LL-649Y R-HPY3-4JKF
	typeOf := reflect.TypeOf(backup.HostRestoreResult{})
	want := []struct {
		name   string
		typeOf reflect.Type
	}{
		{"Object", reflect.TypeOf("")},
		{"Size", reflect.TypeOf(int64(0))},
		{"Files", reflect.TypeOf(int(0))},
		{"SourceReady", reflect.TypeOf(false)},
		{"FilesRestored", reflect.TypeOf(false)},
		{"FailedStep", reflect.TypeOf("")},
	}
	if typeOf.NumField() != len(want) {
		t.Fatalf("HostRestoreResult has %d fields, want %d", typeOf.NumField(), len(want))
	}
	for index, field := range want {
		got := typeOf.Field(index)
		if got.Name != field.name || got.Type != field.typeOf {
			t.Fatalf("HostRestoreResult field %d = %s %v, want %s %v", index, got.Name, got.Type, field.name, field.typeOf)
		}
	}
}

func TestHostRestoreSelectsNewestImmediateArchive(t *testing.T) {
	// R-YGCS-8A7I R-HUTO-NMJ7
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/ikigenba/stale", "stale", 0o600)
	writeFile(t, root, "etc/letsencrypt/live/stale.pem", "stale certificate", 0o600)
	selectedURI := "s3://bucket/project/host/2026-09-16T12:00:00+00:00.tar.zst"
	selectedBody := hostRestoreArchive(t,
		restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o750},
		restoreMember{name: "etc/ikigenba/config.json", data: []byte("restored\n"), mode: 0o640},
	)
	client := &restoreCloud{
		objects: []cloud.Object{
			{URI: "s3://bucket/project/host/not-a-time.tar.zst", Modified: time.Now().Add(100 * time.Hour)},
			{URI: "s3://bucket/project/host/2026-09-17T00:00:00Z.tar.zst/nested", Modified: time.Now().Add(200 * time.Hour)},
			{URI: "s3://other/host/2027-01-01T00:00:00Z.tar.zst", Modified: time.Now().Add(300 * time.Hour)},
			{URI: "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst", Modified: time.Now().Add(400 * time.Hour)},
			{URI: selectedURI, Modified: time.Time{}},
			{URI: "s3://bucket/project/host/2026-09-15T23:59:59Z.tar.zst", Modified: time.Now().Add(500 * time.Hour)},
		},
		bodies: map[string][]byte{selectedURI: selectedBody},
	}
	result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
	if err != nil {
		t.Fatalf("HostRestore() error = %v", err)
	}
	want := backup.HostRestoreResult{
		Object:        "host/2026-09-16T12:00:00+00:00.tar.zst",
		Size:          int64(len(selectedBody)),
		Files:         1,
		SourceReady:   true,
		FilesRestored: true,
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("HostRestore() result = %+v, want %+v", result, want)
	}
	if !reflect.DeepEqual(client.listed, []string{"s3://bucket/project/host/"}) || !reflect.DeepEqual(client.got, []string{selectedURI}) {
		t.Fatalf("cloud reads = list %v, get %v", client.listed, client.got)
	}
	if client.puts != 0 || len(client.readers) != 1 || !client.readers[0].closed {
		t.Fatalf("cloud writes = %d, readers = %#v", client.puts, client.readers)
	}
	if data := readHostRestoreFile(t, root, "etc/ikigenba/config.json"); string(data) != "restored\n" {
		t.Fatalf("restored config = %q", data)
	}
	if _, err := os.Lstat(filepath.Join(root, "etc/letsencrypt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("omitted certificate tree exists: %v", err)
	}
}

func TestHostRestoreRejectsInvalidArchivesBeforeChanges(t *testing.T) {
	// R-YHKO-M1Y7 R-HUTO-NMJ7
	tests := []struct {
		name        string
		members     []restoreMember
		corruptTar  bool
		decompress  host.Result
		wantMessage string
	}{
		{name: "corrupt compression", decompress: host.Result{Stderr: []byte("bad frame\n"), ExitCode: 1}, wantMessage: "decompress host archive"},
		{name: "corrupt tar", corruptTar: true, wantMessage: "tar archive"},
		{name: "absolute name", members: []restoreMember{{name: "/etc/ikigenba/file", data: []byte("bad")}}, wantMessage: "unsafe archive entry"},
		{name: "traversal component", members: []restoreMember{{name: "etc/ikigenba/../letsencrypt/file", data: []byte("bad")}}, wantMessage: "unsafe archive entry"},
		{name: "outside destination", members: []restoreMember{{name: "opt/app/file", data: []byte("bad")}}, wantMessage: "outside host restore trees"},
		{name: "special device", members: []restoreMember{{name: "etc/ikigenba/device", typeflag: tar.TypeChar}}, wantMessage: "special device"},
		{name: "write through symlink", members: []restoreMember{
			{name: "etc/ikigenba/link", typeflag: tar.TypeSymlink, linkname: "../../../outside"},
			{name: "etc/ikigenba/link/file", data: []byte("bad")},
		}, wantMessage: "traverses symlink"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			writeFile(t, root, "etc/ikigenba/existing", "configuration", 0o640)
			writeFile(t, root, "etc/letsencrypt/existing", "certificate", 0o600)
			before := fileTreeSnapshot(t, root)
			body := hostRestoreArchive(t, test.members...)
			if test.corruptTar {
				body = rawZstandardFrame([]byte("not a tar archive"))
			}
			uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
			client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
			env := restoreHostEnv(t, root)
			if test.decompress.ExitCode != 0 {
				env.Execute = func(context.Context, host.Command) (host.Result, error) { return test.decompress, nil }
			}
			result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("HostRestore() error = %v, want containing %q", err, test.wantMessage)
			}
			if result.Object != "host/2026-09-16T12:00:00Z.tar.zst" || result.Size != int64(len(body)) || result.SourceReady || result.FilesRestored || result.Files != 0 || result.FailedStep != "source" {
				t.Fatalf("HostRestore() result = %+v", result)
			}
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid archive changed filesystem:\nbefore %v\nafter  %v", before, after)
			}
			if len(client.readers) != 1 || !client.readers[0].closed {
				t.Fatalf("object reader was not closed: %#v", client.readers)
			}
		})
	}
}

func TestHostRestoreReplacesTreesAndPreservesArchiveMetadata(t *testing.T) {
	// R-YISK-ZTOW R-HUTO-NMJ7
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/ikigenba/stale", "remove", 0o600)
	writeFile(t, root, "etc/letsencrypt/stale", "remove", 0o600)
	writeFile(t, root, "opt/app/state/data", "untouched service", 0o600)
	writeFile(t, root, "etc/nginx/nginx.conf", "untouched nginx", 0o600)
	writeFile(t, root, "etc/systemd/system/ikigenba-app.service", "untouched unit", 0o600)
	untouched := map[string]string{
		"opt/app/state/data":                      "untouched service",
		"etc/nginx/nginx.conf":                    "untouched nginx",
		"etc/systemd/system/ikigenba-app.service": "untouched unit",
	}
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o750},
		restoreMember{name: "etc/ikigenba/nested", typeflag: tar.TypeDir, mode: 0o710},
		restoreMember{name: "etc/ikigenba/nested/settings", data: []byte("restored settings\n"), mode: 0o640},
		restoreMember{name: "etc/ikigenba/current", typeflag: tar.TypeSymlink, linkname: "nested/settings"},
		restoreMember{name: "etc/letsencrypt", typeflag: tar.TypeDir, mode: 0o711},
		restoreMember{name: "etc/letsencrypt/archive", typeflag: tar.TypeDir, mode: 0o700},
		restoreMember{name: "etc/letsencrypt/archive/certificate.pem", data: []byte("restored certificate\n"), mode: 0o600},
	)
	uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
	client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
	result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
	if err != nil || !result.SourceReady || !result.FilesRestored || result.Files != 3 || result.FailedStep != "" {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	if data := readHostRestoreFile(t, root, "etc/ikigenba/nested/settings"); string(data) != "restored settings\n" {
		t.Fatalf("restored file = %q", data)
	}
	if info, err := os.Stat(filepath.Join(root, "etc/ikigenba/nested")); err != nil || info.Mode().Perm() != 0o710 {
		t.Fatalf("restored directory mode = %v, %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(root, "etc/ikigenba/nested/settings")); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("restored file mode = %v, %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(root, "etc/ikigenba/current")); err != nil || target != "nested/settings" {
		t.Fatalf("restored symlink = %q, %v", target, err)
	}
	if info, err := os.Lstat(filepath.Join(root, "etc/ikigenba")); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("restored configuration root mode = %v, %v", info, err)
	}
	if info, err := os.Lstat(filepath.Join(root, "etc/letsencrypt")); err != nil || info.Mode().Perm() != 0o711 {
		t.Fatalf("restored certificate root mode = %v, %v", info, err)
	}
	if data := readHostRestoreFile(t, root, "etc/letsencrypt/archive/certificate.pem"); string(data) != "restored certificate\n" {
		t.Fatalf("restored certificate = %q", data)
	}
	if info, err := os.Stat(filepath.Join(root, "etc/letsencrypt/archive/certificate.pem")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("restored certificate mode = %v, %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "etc/ikigenba/stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale configuration remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "etc/letsencrypt/stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale certificate remains: %v", err)
	}
	for name, want := range untouched {
		data := readHostRestoreFile(t, root, name)
		if string(data) != want {
			t.Fatalf("out-of-scope file %q = %q", name, data)
		}
	}
	if client.puts != 0 {
		t.Fatalf("cloud writes = %d", client.puts)
	}
}

func TestHostRestorePhaseFailures(t *testing.T) {
	// R-YGCS-8A7I R-HUTO-NMJ7
	t.Run("cancellation before source selection has no failed step", func(t *testing.T) {
		root := t.TempDir()
		store := configuredHostStore(t, root)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := backup.HostRestore(ctx, restoreHostEnv(t, root), cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			t.Fatal("opened cloud storage after preworkflow cancellation")
			return nil, nil
		}}, store)
		if !errors.Is(err, context.Canceled) || result != (backup.HostRestoreResult{}) {
			t.Fatalf("HostRestore() = %+v, %v, want zero result and context cancellation", result, err)
		}
	})

	t.Run("no matching archive", func(t *testing.T) {
		root := t.TempDir()
		store := configuredHostStore(t, root)
		writeFile(t, root, "etc/ikigenba/existing", "keep", 0o600)
		before := fileTreeSnapshot(t, root)
		client := &restoreCloud{objects: []cloud.Object{{URI: "s3://bucket/project/host/not-an-archive"}}}
		result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
		if err == nil || !strings.Contains(err.Error(), "no host backup found") || result.FailedStep != "source" || result.SourceReady || result.FilesRestored {
			t.Fatalf("HostRestore() = %+v, %v", result, err)
		}
		if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
			t.Fatalf("missing archive changed filesystem:\nbefore %v\nafter  %v", before, after)
		}
	})

	t.Run("replacement failure preserves source phase", func(t *testing.T) {
		store := configuredHostStore(t, t.TempDir())
		body := hostRestoreArchive(t,
			restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o750},
			restoreMember{name: "etc/ikigenba/file", data: []byte("ready"), mode: 0o600},
		)
		uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
		client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
		missingRoot := filepath.Join(t.TempDir(), "missing")
		result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, missingRoot), cloud.Env{Open: client.open}, store)
		if err == nil || result.Object != "host/2026-09-16T12:00:00Z.tar.zst" || result.Size != int64(len(body)) || !result.SourceReady || result.FilesRestored || result.Files != 0 || result.FailedStep != "files" {
			t.Fatalf("HostRestore() = %+v, %v", result, err)
		}
	})

	t.Run("cancellation after selection retains source result", func(t *testing.T) {
		root := t.TempDir()
		store := configuredHostStore(t, root)
		writeFile(t, root, "etc/ikigenba/existing", "keep", 0o600)
		before := fileTreeSnapshot(t, root)
		body := hostRestoreArchive(t,
			restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o750},
			restoreMember{name: "etc/ikigenba/file", data: []byte("do not publish"), mode: 0o600},
		)
		uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
		client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
		ctx, cancel := context.WithCancel(context.Background())
		env := restoreHostEnv(t, root)
		execute := env.Execute
		env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
			result, err := execute(ctx, command)
			cancel()
			return result, err
		}

		result, err := backup.HostRestore(ctx, env, cloud.Env{Open: client.open}, store)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("HostRestore() error = %v, want context cancellation", err)
		}
		want := backup.HostRestoreResult{
			Object:     "host/2026-09-16T12:00:00Z.tar.zst",
			Size:       int64(len(body)),
			FailedStep: "source",
		}
		if !reflect.DeepEqual(result, want) {
			t.Fatalf("HostRestore() result = %+v, want %+v", result, want)
		}
		if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
			t.Fatalf("canceled restore changed filesystem:\nbefore %v\nafter  %v", before, after)
		}
	})

	t.Run("second replacement failure retains first replacement", func(t *testing.T) {
		root := t.TempDir()
		store := configuredHostStore(t, root)
		writeFile(t, root, "etc/ikigenba/stale", "remove", 0o600)
		writeFile(t, root, "etc/letsencrypt/blocked/existing.pem", "existing", 0o600)
		blocked := filepath.Join(root, "etc/letsencrypt/blocked")
		blockedInfo, err := os.Stat(blocked)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(blocked, 0); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Chmod(blocked, blockedInfo.Mode().Perm()); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("restore blocked directory permissions: %v", err)
			}
		}()

		body := hostRestoreArchive(t,
			restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o750},
			restoreMember{name: "etc/ikigenba/restored", data: []byte("published"), mode: 0o600},
			restoreMember{name: "etc/letsencrypt", typeflag: tar.TypeDir, mode: 0o700},
			restoreMember{name: "etc/letsencrypt/archived.pem", data: []byte("not published"), mode: 0o600},
		)
		uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
		client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
		result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
		if err == nil {
			t.Fatal("HostRestore() error = nil, want second replacement failure")
		}
		want := backup.HostRestoreResult{
			Object:      "host/2026-09-16T12:00:00Z.tar.zst",
			Size:        int64(len(body)),
			Files:       1,
			SourceReady: true,
			FailedStep:  "files",
		}
		if !reflect.DeepEqual(result, want) {
			t.Fatalf("HostRestore() result = %+v, want %+v", result, want)
		}
		if data := readHostRestoreFile(t, root, "etc/ikigenba/restored"); string(data) != "published" {
			t.Fatalf("first replacement = %q", data)
		}
		if _, err := os.Lstat(filepath.Join(root, "etc/ikigenba/stale")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale configuration remains after first replacement: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(root, "etc/letsencrypt/archived.pem")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed second replacement published archive: %v", err)
		}
	})

	t.Run("source configuration is captured before selected object access", func(t *testing.T) {
		root := t.TempDir()
		store := configuredHostStore(t, root)
		body := hostRestoreArchive(t,
			restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o750},
			restoreMember{name: "etc/ikigenba/restored", data: []byte("captured source"), mode: 0o600},
		)
		uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
		client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
		client.listHook = func(listedPrefix string) {
			if listedPrefix != "s3://bucket/project/host/" {
				t.Fatalf("ListObjects() prefix = %q, want captured prefix", listedPrefix)
			}
			if err := store.Set("backup.s3_uri", "s3://different/reconfigured/"); err != nil {
				t.Fatal(err)
			}
			if err := store.Set("aws.region", "eu-west-1"); err != nil {
				t.Fatal(err)
			}
		}

		result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
		if err != nil || !result.SourceReady || !result.FilesRestored {
			t.Fatalf("HostRestore() = %+v, %v", result, err)
		}
		if !reflect.DeepEqual(client.opened, []string{"us-east-2"}) ||
			!reflect.DeepEqual(client.listed, []string{"s3://bucket/project/host/"}) ||
			!reflect.DeepEqual(client.got, []string{uri}) {
			t.Fatalf("cloud source = open %v, list %v, get %v", client.opened, client.listed, client.got)
		}
		if data := readHostRestoreFile(t, root, "etc/ikigenba/restored"); string(data) != "captured source" {
			t.Fatalf("restored file = %q", data)
		}
	})

	t.Run("reader returned with download error is closed", func(t *testing.T) {
		root := t.TempDir()
		store := configuredHostStore(t, root)
		uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
		client := &restoreCloud{
			objects: []cloud.Object{{URI: uri}},
			bodies:  map[string][]byte{uri: rawZstandardFrame(nil)},
			getErr:  errors.New("download interrupted"),
		}
		result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
		if err == nil || !strings.Contains(err.Error(), "download interrupted") || result.FailedStep != "source" {
			t.Fatalf("HostRestore() = %+v, %v", result, err)
		}
		if len(client.readers) != 1 || !client.readers[0].closed {
			t.Fatalf("object reader was not closed: %#v", client.readers)
		}
	})

	t.Run("preworkflow dependency failure has no step", func(t *testing.T) {
		root := t.TempDir()
		store := configuredHostStore(t, root)
		result, err := backup.HostRestore(context.Background(), host.Env{Root: root}, cloud.Env{}, store)
		if err == nil || result != (backup.HostRestoreResult{}) {
			t.Fatalf("HostRestore() = %+v, %v", result, err)
		}
	})
}

type restoreMember struct {
	name     string
	typeflag byte
	mode     int64
	linkname string
	data     []byte
}

func hostRestoreArchive(t *testing.T, members ...restoreMember) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, member := range members {
		typeflag := member.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		mode := member.mode
		if mode == 0 {
			mode = 0o600
		}
		header := &tar.Header{Name: member.name, Typeflag: typeflag, Mode: mode, Linkname: member.linkname}
		if typeflag == tar.TypeReg {
			header.Size = int64(len(member.data))
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return rawZstandardFrame(archive.Bytes())
}

func restoreHostEnv(t *testing.T, root string) host.Env {
	t.Helper()
	return host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name != "zstd" || !reflect.DeepEqual(command.Args, []string{"--quiet", "--decompress", "--stdout"}) {
			return host.Result{}, fmt.Errorf("unexpected command %q %v", command.Name, command.Args)
		}
		compressed, err := io.ReadAll(command.Stdin)
		if err != nil {
			return host.Result{}, err
		}
		return host.Result{Stdout: decodeRawZstandardFrame(t, compressed)}, nil
	}}
}

func readHostRestoreFile(t *testing.T, rootName, name string) []byte {
	t.Helper()
	root, err := os.OpenRoot(rootName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type trackedRestoreReader struct {
	*bytes.Reader
	closed bool
}

func (reader *trackedRestoreReader) Close() error {
	reader.closed = true
	return nil
}

type restoreCloud struct {
	objects  []cloud.Object
	bodies   map[string][]byte
	opened   []string
	listed   []string
	got      []string
	readers  []*trackedRestoreReader
	puts     int
	getErr   error
	listHook func(string)
}

func (client *restoreCloud) open(_ context.Context, region string) (cloud.Client, error) {
	client.opened = append(client.opened, region)
	if region != "us-east-2" {
		return nil, fmt.Errorf("region = %q", region)
	}
	return client, nil
}

func (client *restoreCloud) GetObject(_ context.Context, uri string) (io.ReadCloser, error) {
	client.got = append(client.got, uri)
	body, ok := client.bodies[uri]
	if !ok {
		return nil, cloud.ErrNotFound
	}
	reader := &trackedRestoreReader{Reader: bytes.NewReader(body)}
	client.readers = append(client.readers, reader)
	return reader, client.getErr
}

func (client *restoreCloud) PutObject(context.Context, string, io.Reader) error {
	client.puts++
	return errors.New("unexpected cloud write")
}

func (client *restoreCloud) ListObjects(_ context.Context, prefix string) ([]cloud.Object, error) {
	client.listed = append(client.listed, prefix)
	if client.listHook != nil {
		client.listHook(prefix)
	}
	return append([]cloud.Object(nil), client.objects...), nil
}

func (*restoreCloud) ReadSecrets(context.Context, string) (map[string]string, error) {
	return nil, errors.New("unexpected secret read")
}
