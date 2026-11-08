package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type accountPassCloud struct {
	*restoreCloud
	passes       [][]byte
	accountGets  int
	beforeGet    func(int)
	active       atomic.Bool
	readsOutside atomic.Bool
}

func (c *accountPassCloud) open(ctx context.Context, region string) (cloud.Client, error) {
	_, err := c.restoreCloud.open(ctx, region)
	return c, err
}
func (c *accountPassCloud) GetObject(ctx context.Context, uri string) (io.ReadCloser, error) {
	if uri != restoreAccountsURI {
		return c.restoreCloud.GetObject(ctx, uri)
	}
	c.accountGets++
	if c.beforeGet != nil {
		c.beforeGet(c.accountGets)
	}
	index := min(c.accountGets-1, len(c.passes)-1)
	reader := &accountPassReader{Reader: bytes.NewReader(c.passes[index]), client: c}
	return reader, nil
}

type accountPassReader struct {
	io.Reader
	client *accountPassCloud
}

func (r *accountPassReader) Read(p []byte) (int, error) {
	if !r.client.active.Load() {
		r.client.readsOutside.Store(true)
	}
	return r.Reader.Read(p)
}
func (*accountPassReader) Close() error { return nil }

type restoreReaderOnly struct{ io.Reader }
type restoreWriteObserver struct {
	io.Writer
	written int64
	observe func(int64) error
}

func (w *restoreWriteObserver) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.written += int64(n)
	if err == nil && w.observe != nil {
		err = w.observe(w.written)
	}
	return n, err
}

func accountStreamingEnv(fixture *restoreAccountExecutor, client *accountPassCloud, observe func(int, int64) error) host.Env {
	env := fixture.env()
	base := env.Execute
	pass := 0
	env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		if command.Name != "zstd" || command.Stdout == nil || client.accountGets == 0 {
			return base(ctx, command)
		}
		if !slices.Equal(command.Args, []string{"--quiet", "--decompress", "--stdout"}) {
			return host.Result{}, errors.New("wrong decompression arguments")
		}
		pass++
		client.active.Store(true)
		defer client.active.Store(false)
		writer := &restoreWriteObserver{Writer: command.Stdout, observe: func(written int64) error {
			if observe != nil {
				return observe(pass, written)
			}
			return nil
		}}
		_, err := io.CopyBuffer(writer, restoreReaderOnly{command.Stdin}, make([]byte, 32768))
		return host.Result{}, err
	}
	return env
}

func TestHostRestoreRefetchesAccountsAtEachPhase(t *testing.T) {
	// R-P9K0-RVFS R-PARX-5N6H R-F7HL-CKRP R-F8PH-QCIE
	root := t.TempDir()
	store := configuredHostStore(t, root)
	fixture := newRestoreAccountExecutor(t, root)
	writeFile(t, root, "etc/ssh/sshd_config", "config", 0o600)
	manifest := accounts.Manifest{Groups: []accounts.ManifestGroup{{Slug: "acme", UID: 1004, GID: 1004}}, Users: []accounts.ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: accounts.StateActive, Groups: []string{"acme"}}}}
	members := []restoreMember{restoreManifestMember(t, manifest), {name: "etc/ssh/ssh_host_rsa_key", data: []byte("first host key")}, {name: "etc/ssh/authorized_keys/alice", data: []byte("first authorized key")}, {name: "home/alice", typeflag: tar.TypeDir, mode: 0o700}, {name: "home/alice/file", data: []byte("first home")}, {name: "srv/acme", typeflag: tar.TypeDir, mode: 0o750}, {name: "srv/acme/file", data: []byte("first folder")}}
	raw := func() []byte { return decodeRawZstandardFrame(t, hostRestoreArchive(t, members...)) }
	first := raw()
	members[1].data = []byte("later host key")
	members[2].data = []byte("later authorized key")
	members[6].data = []byte("second folder")
	second := raw()
	members[4].data = []byte("third home")
	members[6].data = []byte("third folder")
	third := raw()
	client := &accountPassCloud{restoreCloud: restoreAccountsCloud(t, nil), passes: [][]byte{first, second, third}}
	before := fileTreeSnapshot(t, root)
	client.beforeGet = func(pass int) {
		switch pass {
		case 1:
			if !reflect.DeepEqual(fileTreeSnapshot(t, root), before) {
				t.Error("host changed before accounts validation")
			}
		case 2:
			if string(readHostRestoreFile(t, root, "etc/ikigenba/restored")) != "restored configuration" {
				t.Error("groups download preceded files")
			}
			if _, err := os.Lstat(filepath.Join(root, "srv/acme")); !errors.Is(err, os.ErrNotExist) {
				t.Error("folder changed before groups download")
			}
		case 3:
			if string(readHostRestoreFile(t, root, "srv/acme/file")) != "second folder" {
				t.Error("users download preceded groups")
			}
			for _, name := range []string{"home/alice", "etc/ssh/authorized_keys/alice"} {
				if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s changed before users download", name)
				}
			}
		default:
			t.Errorf("unexpected accounts download %d", pass)
		}
	}
	result, err := backup.HostRestore(context.Background(), accountStreamingEnv(fixture, client, nil), cloud.Env{Open: client.open}, store)
	if err != nil || !result.SSHRestored || !result.GroupsRestored || !result.UsersRestored || result.AccountsSize != int64(len(first)) || result.AccountsObject != "host/"+strings.TrimPrefix(restoreAccountsURI, "s3://bucket/project/host/") {
		t.Fatalf("restore = %+v, %v", result, err)
	}
	if client.accountGets != 3 || client.readsOutside.Load() || !slices.Equal(client.got, []string{restoreConfigurationURI}) {
		t.Fatalf("reads = accounts %d, outside Execute %v, other %v", client.accountGets, client.readsOutside.Load(), client.got)
	}
	for name, want := range map[string]string{"srv/acme/file": "second folder", "home/alice/file": "third home", "etc/ssh/authorized_keys/alice": "first authorized key", "etc/ssh/ssh_host_rsa_key": "first host key"} {
		if string(readHostRestoreFile(t, root, name)) != want {
			t.Errorf("wrong phase supplied %s", name)
		}
	}
	assertNoRestoreTemps(t, root)
}

func TestHostRestoreRejectsAccountsOrderingBeforeMutation(t *testing.T) {
	// R-F51S-L1AB R-F69O-YT10
	goodManifest := restoreTestManifest()
	good := restoreManifestMember(t, goodManifest)
	cases := map[string][]restoreMember{
		"manifest not first":            {{name: "etc/ssh/ssh_host_a", data: []byte("key")}, good},
		"host keys reversed":            {good, {name: "etc/ssh/ssh_host_b"}, {name: "etc/ssh/ssh_host_a"}},
		"authorized keys reversed":      {good, {name: "etc/ssh/authorized_keys/bob"}, {name: "etc/ssh/authorized_keys/alice"}},
		"host key after authorized key": {good, {name: "etc/ssh/authorized_keys/alice"}, {name: "etc/ssh/ssh_host_a"}},
		"home trees reversed":           {good, {name: "home/bob", typeflag: tar.TypeDir}, {name: "home/alice", typeflag: tar.TypeDir}},
		"folder trees reversed":         {good, {name: "srv/beta", typeflag: tar.TypeDir}, {name: "srv/acme", typeflag: tar.TypeDir}},
		"noncontiguous home":            {good, {name: "home/alice", typeflag: tar.TypeDir}, {name: "home/bob", typeflag: tar.TypeDir}, {name: "home/alice/file"}},
		"home after folder":             {good, {name: "srv/acme", typeflag: tar.TypeDir}, {name: "home/alice", typeflag: tar.TypeDir}},
	}
	for _, kind := range []string{"groups", "users"} {
		manifest := restoreTestManifest()
		if kind == "groups" {
			slices.Reverse(manifest.Groups)
		} else {
			slices.Reverse(manifest.Users)
		}
		cases["manifest "+kind+" reversed"] = []restoreMember{restoreManifestMember(t, manifest)}
	}
	for name, members := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Cleanup(func() { assertHostStageGone(t, root) })
			store := configuredHostStore(t, root)
			before := fileTreeSnapshot(t, root)
			client := restoreAccountsCloud(t, hostRestoreArchive(t, members...))
			result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
			if err == nil || result.FailedStep != "accounts" || !result.SourceReady || result.AccountsReady || result.FilesRestored {
				t.Fatalf("restore = %+v, %v", result, err)
			}
			if !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
				t.Error("invalid archive changed host")
			}
		})
	}
}

type restoreFileSpan struct {
	name                string
	start, end, treeEnd int64
}
type restoreProgressSnapshot struct {
	written int64
	files   []os.FileInfo
}

func TestHostRestoreWritesAndCompletesTreesWithStream(t *testing.T) {
	// R-PI3B-G9MN R-PJB7-U1DC R-PD7P-X6NV R-PFNI-OQ59
	root := t.TempDir()
	store := configuredHostStore(t, root)
	fixture := newRestoreAccountExecutor(t, root)
	manifest := restoreTestManifest()
	members := []restoreMember{restoreManifestMember(t, manifest)}
	for _, tree := range []string{"home/alice", "home/bob", "srv/acme", "srv/beta"} {
		members = append(members, restoreMember{name: tree, typeflag: tar.TypeDir, mode: 0o750}, restoreMember{name: tree + "/large", data: bytes.Repeat([]byte(tree), 250000), mode: 0o600})
	}
	raw := decodeRawZstandardFrame(t, hostRestoreArchive(t, members...))
	source := bytes.NewReader(raw)
	tarReader := tar.NewReader(source)
	var spans []restoreFileSpan
	for {
		h, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(h.Name, "/large") {
			start := int64(len(raw) - source.Len())
			end := start + h.Size
			spans = append(spans, restoreFileSpan{h.Name, start, end, (end + 511) / 512 * 512})
		}
	}
	// Padding after the last tree must not postpone its completion.
	raw = append(raw, make([]byte, 2*1048576)...)
	client := &accountPassCloud{restoreCloud: restoreAccountsCloud(t, nil), passes: [][]byte{raw}}
	snapshots := map[int][]restoreProgressSnapshot{}
	directory, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	observe := func(pass int, written int64) error {
		if pass == 1 {
			return nil
		}
		snapshot := restoreProgressSnapshot{written: written}
		err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type().IsRegular() {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				snapshot.files = append(snapshot.files, info)
			}
			return nil
		})
		if err != nil {
			return err
		}
		snapshots[pass] = append(snapshots[pass], snapshot)
		for _, span := range spans {
			relevant := pass == 2 && strings.HasPrefix(span.name, "srv/") || pass == 3 && strings.HasPrefix(span.name, "home/")
			if relevant && written > span.treeEnd+1048576 {
				if _, err := os.Stat(filepath.Join(root, span.name)); err != nil {
					return fmt.Errorf("tree %s not installed by byte %d: %w", span.name, written, err)
				}
				account := strings.Split(span.name, "/")[1]
				passwd, err := directory.ReadFile("etc/passwd")
				if err != nil {
					return err
				}
				if !strings.Contains(string(passwd), account+":x:") {
					return fmt.Errorf("account %s not restored by byte %d", account, written)
				}
			}
		}
		return nil
	}
	result, err := backup.HostRestore(context.Background(), accountStreamingEnv(fixture, client, observe), cloud.Env{Open: client.open}, store)
	if err != nil || !result.SSHRestored {
		t.Fatalf("restore = %+v, %v", result, err)
	}
	for _, span := range spans {
		pass := 2
		if strings.HasPrefix(span.name, "home/") {
			pass = 3
		}
		destination, err := os.Stat(filepath.Join(root, span.name))
		if err != nil {
			t.Fatal(err)
		}
		checked := false
		for _, snapshot := range snapshots[pass] {
			required := min(span.end-span.start, snapshot.written-span.start-1048576)
			if required <= 0 {
				continue
			}
			checked = true
			found := false
			for _, file := range snapshot.files {
				if os.SameFile(destination, file) && file.Size() >= required {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s had no same file with %d bytes at output byte %d", span.name, required, snapshot.written)
			}
		}
		if !checked {
			t.Fatalf("no streaming observation for %s", span.name)
		}
	}
	assertNoRestoreTemps(t, root)
}

func TestHostRestoreReplayFailurePreservesUnfinishedTree(t *testing.T) {
	// R-F9XE-4493 R-F7HL-CKRP R-F8PH-QCIE
	for _, step := range []string{"groups", "users"} {
		for _, failure := range []string{"changed manifest", "invalid following member", "truncated following header", "missing tar end", "truncated member"} {
			t.Run(step+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				store := configuredHostStore(t, root)
				fixture := newRestoreAccountExecutor(t, root)
				manifest := restoreTestManifest()
				prefix, names := "srv/", []string{"acme", "beta"}
				pass := 2
				if step == "users" {
					prefix, names, pass = "home/", []string{"alice", "bob"}, 3
				}
				for _, name := range names {
					writeFile(t, root, prefix+name+"/old", "old "+name, 0o600)
				}
				members := []restoreMember{restoreManifestMember(t, manifest)}
				for _, name := range names {
					members = append(members, restoreMember{name: prefix + name, typeflag: tar.TypeDir, mode: 0o750}, restoreMember{name: prefix + name + "/file", data: []byte("new " + name), mode: 0o600})
				}
				good := decodeRawZstandardFrame(t, hostRestoreArchive(t, members...))
				var bad []byte
				switch failure {
				case "changed manifest":
					members[0].data = append([]byte(" "), members[0].data...)
					bad = decodeRawZstandardFrame(t, hostRestoreArchive(t, members...))
				case "invalid following member", "truncated following header":
					members = append(members, restoreMember{name: "/outside", data: []byte("no")})
					bad = decodeRawZstandardFrame(t, hostRestoreArchive(t, members...))
					if failure == "truncated following header" {
						bad = bad[:len(good)-1024+256]
					}
				case "missing tar end":
					bad = good[:len(good)-1024]
				case "truncated member":
					bad = good[:len(good)-1024-510]
				}
				passes := [][]byte{good, good, good}
				passes[pass-1] = bad
				client := &accountPassCloud{restoreCloud: restoreAccountsCloud(t, nil), passes: passes}
				result, err := backup.HostRestore(context.Background(), accountStreamingEnv(fixture, client, nil), cloud.Env{Open: client.open}, store)
				if err == nil || result.FailedStep != step || !result.FilesRestored || !result.AccountsReady || result.UsersRestored || result.SSHRestored {
					t.Fatalf("restore = %+v, %v", result, err)
				}
				completed := result.Groups
				if step == "users" {
					if !result.GroupsRestored {
						t.Fatal("earlier groups lost")
					}
					completed = result.Users
				} else if result.GroupsRestored {
					t.Fatal("failed groups claimed success")
				}
				if failure == "changed manifest" {
					if len(completed) != 0 {
						t.Fatalf("entries attempted with changed manifest: %v", completed)
					}
					if string(readHostRestoreFile(t, root, prefix+names[0]+"/old")) != "old "+names[0] {
						t.Fatal("first tree changed")
					}
				} else {
					if !slices.Equal(completed, names[:1]) {
						t.Fatalf("completed entries = %v, want %v", completed, names[:1])
					}
					if string(readHostRestoreFile(t, root, prefix+names[0]+"/file")) != "new "+names[0] {
						t.Fatal("completed tree lost")
					}
				}
				if string(readHostRestoreFile(t, root, prefix+names[1]+"/old")) != "old "+names[1] {
					t.Fatal("unfinished destination changed")
				}
				if _, statErr := os.Lstat(filepath.Join(root, prefix+names[1]+"/file")); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("partial tree installed: %v", statErr)
				}
				if client.accountGets != pass {
					t.Fatalf("later downloads after failure: %d", client.accountGets)
				}
				assertNoRestoreTemps(t, root)
			})
		}
	}
}
