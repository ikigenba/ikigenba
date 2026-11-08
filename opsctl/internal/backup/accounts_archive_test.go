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
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestAccountsBackupContentsOrderMetadataAndExclusions(t *testing.T) {
	// R-SR96-A6ZT R-TBZG-SALM R-ULCY-864N R-H8QA-FZWB R-T3G6-3WER
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/passwd", "root:x:0:0::/root:/bin/bash\nacme:x:1004:1004::/srv/acme:/usr/sbin/nologin\nalice:x:1001:1001::/home/alice:/bin/bash\nbob:x:1002:1002::/home/bob:/bin/bash\nec2-user:x:2000:2000::/home/ec2-user:/bin/bash\nikigenba:x:3000:3000::/var/lib/ikigenba:/usr/sbin/nologin\n", 0o600)
	writeFile(t, root, "etc/group", "root:x:0:\nshell:x:999:alice,bob\nacme:x:1004:alice,bob\nalice:x:1001:\nbob:x:1002:\n", 0o600)
	writeFile(t, root, "etc/shadow", "root:*:0:0:99999:7:::\nalice:!:0:0:99999:7:::\nbob:!:0:0:99999:7::1:\n", 0o600)
	for name, data := range map[string]string{
		"etc/ssh/ssh_host_z_key": "private-z", "etc/ssh/ssh_host_a_key.pub": "public-a",
		"etc/ssh/authorized_keys/bob": "bob key", "etc/ssh/authorized_keys/alice": "alice key",
		"home/alice/a file\nwith\\escape": "alice content", "home/bob/notes": "bob content", "srv/acme/repo": "business data",
		"home/ec2-user/secret": "excluded ec2", "etc/ssh/authorized_keys/ec2-user": "excluded key", "home/ikigenba/secret": "excluded service", "srv/other/secret": "excluded folder",
	} {
		writeFile(t, root, name, data, 0o640)
	}
	if err := os.Chmod(filepath.Join(root, "srv/acme"), 0o750|fs.ModeSetgid|fs.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "home/bob/notes"), 0o750|fs.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../ec2-user/secret", filepath.Join(root, "home/alice/link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "home/bob/notes"), filepath.Join(root, "home/bob/hard")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "home/bob/pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := fileTreeSnapshot(t, root)
	watch := newFileAccessWatch(t, filepath.Join(root, "home/ec2-user"), filepath.Join(root, "home/ikigenba"), filepath.Join(root, "srv/other"), filepath.Join(root, "etc/ssh/authorized_keys/ec2-user"))
	fixture := &accountsMetadataFixture{t: t, root: root}
	client := newFileCloud()
	result, err := backup.HostBackup(context.Background(), fileHostEnv(root, fixture.execute), cloud.Env{Open: client.open}, store)
	if err != nil || result.Accounts.Err != nil || result.Host.Err != nil {
		t.Fatalf("HostBackup = %+v, %v", result, err)
	}
	if result.Accounts.Service != "accounts" || result.Host.Service != "host" || int64(len(client.objects[client.puts[0]])) != result.Accounts.Size || int64(len(client.objects[client.puts[1]])) != result.Host.Size {
		t.Fatalf("results = %+v, puts %v", result, client.puts)
	}
	members := readTestArchive(t, client.objects[client.puts[0]])
	wantNames := []string{"accounts.json", "etc/ssh/ssh_host_a_key.pub", "etc/ssh/ssh_host_z_key", "etc/ssh/authorized_keys/alice", "etc/ssh/authorized_keys/bob", "home/alice/", "home/alice/a file\nwith\\escape", "home/alice/link", "home/bob/", "home/bob/hard", "home/bob/notes", "srv/acme/", "srv/acme/repo"}
	if got := accountsArchiveNames(t, client.objects[client.puts[0]]); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("archive order = %q, want %q", got, wantNames)
	}
	wantManifest := `{"groups":[{"slug":"acme","gid":1004,"uid":1004,"members":["alice","bob"]}],"users":[{"name":"alice","uid":1001,"gid":1001,"state":"active","groups":["acme"]},{"name":"bob","uid":1002,"gid":1002,"state":"locked","groups":["acme"]}]}`
	if string(members["accounts.json"].data) != wantManifest {
		t.Fatalf("manifest = %s", members["accounts.json"].data)
	}
	for name, entry := range members {
		if name == "accounts.json" {
			continue
		}
		uid, gid := 1234, 5678
		if entry.header.Typeflag == tar.TypeSymlink {
			uid, gid = 9999, 8888
		}
		if entry.header.Uid != uid || entry.header.Gid != gid || entry.header.Uname != "" || entry.header.Gname != "" {
			t.Fatalf("%q owner = %d:%d %q:%q", name, entry.header.Uid, entry.header.Gid, entry.header.Uname, entry.header.Gname)
		}
		if name == "srv/acme/" {
			if entry.header.Mode != 0o3750 || entry.header.PAXRecords["SCHILY.acl.access"] != "user::rwx,user:4321:r-x,group::r-x,mask::r-x,other::---" || entry.header.PAXRecords["SCHILY.acl.default"] != "user::rwx,group::rwx,other::---" {
				t.Fatalf("group mode/ACL = %#v", entry.header)
			}
		} else if len(entry.header.PAXRecords) > 0 {
			for key := range entry.header.PAXRecords {
				if strings.HasPrefix(key, "SCHILY.acl.") {
					t.Fatalf("unexpected ACL on %q: %v", name, entry.header.PAXRecords)
				}
			}
		}
	}
	for _, name := range []string{"home/bob/hard", "home/bob/notes"} {
		if h := members[name].header; h.Typeflag != tar.TypeReg || h.Mode != 0o4750 || string(members[name].data) != "bob content" {
			t.Fatalf("member with hard links = %#v", h)
		}
	}
	if h := members["home/alice/link"].header; h.Typeflag != tar.TypeSymlink || h.Linkname != "../../ec2-user/secret" {
		t.Fatalf("symlink = %#v", h)
	}
	top := []string{"etc/ssh/ssh_host_a_key.pub", "etc/ssh/ssh_host_z_key", "etc/ssh/authorized_keys/alice", "etc/ssh/authorized_keys/bob", "home/alice", "home/bob", "srv/acme"}
	paths := make([]string, len(top))
	for i, name := range top {
		paths[i] = filepath.Join(root, name)
	}
	wantACL := append([]string{"--absolute-names", "--numeric", "--no-effective", "--physical", "--recursive", "--"}, paths...)
	wantFind := append([]string{"-P"}, paths...)
	wantFind = append(wantFind, "-type", "l", "-printf", "%U %G %p\\0")
	if fixture.getfaclCalls != 1 || fixture.findCalls != 1 || !reflect.DeepEqual(fixture.aclArgs, wantACL) || !reflect.DeepEqual(fixture.findArgs, wantFind) {
		t.Fatalf("metadata commands: getfacl %d %q; find %d %q", fixture.getfaclCalls, fixture.aclArgs, fixture.findCalls, fixture.findArgs)
	}
	watch.assertQuiet(t)
	if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("backup changed local files")
	}
}

func TestAccountsBackupEmptyAndOptionalSources(t *testing.T) {
	// R-TBZG-SALM R-ULCY-864N
	for _, keys := range []bool{false, true} {
		t.Run(fmt.Sprint(keys), func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			if keys {
				writeFile(t, root, "etc/ssh/ssh_host_only_key", "key", 0o600)
			}
			fixture := &accountsMetadataFixture{t: t, root: root}
			client := newFileCloud()
			got, err := backup.HostBackup(context.Background(), fileHostEnv(root, fixture.execute), cloud.Env{Open: client.open}, store)
			if err != nil || got.Accounts.Err != nil || got.Host.Err != nil {
				t.Fatalf("backup = %+v, %v", got, err)
			}
			m := readTestArchive(t, client.objects[client.puts[0]])
			if string(m["accounts.json"].data) != `{"groups":[],"users":[]}` || len(m) != 1+fixture.getfaclCalls {
				t.Fatalf("empty manifest archive = %v", m)
			}
			if fixture.findCalls != 0 || fixture.getfaclCalls != map[bool]int{false: 0, true: 1}[keys] {
				t.Fatalf("metadata commands = %d/%d", fixture.getfaclCalls, fixture.findCalls)
			}
		})
	}
}

func TestAccountsBackupFailureBlocksConfigurationAndPreservesCommandErrors(t *testing.T) {
	// R-T9JO-0R48 R-H8QA-FZWB R-ULCY-864N R-UL3Q-XT1B
	cases := []struct {
		name   string
		tool   string
		output []byte
		exit   int
		err    error
	}{
		{name: "getfacl exit", tool: "getfacl", exit: 7},
		{name: "getfacl start", tool: "getfacl", err: errors.New("cannot start")},
		{name: "getfacl missing metadata", tool: "getfacl", output: []byte("\n")},
		{name: "getfacl malformed", tool: "getfacl", output: []byte("not metadata\n")},
		{name: "getfacl bad owner", tool: "getfacl", output: []byte("# file: /unused\n# owner: invalid\n# group: 3\nuser::rwx\n")},
		{name: "find exit", tool: "find", exit: 9},
		{name: "find start", tool: "find", err: errors.New("cannot start")},
		{name: "find missing", tool: "find"},
		{name: "find malformed", tool: "find", output: []byte("1 nope path\x00")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			writeFile(t, root, "etc/passwd", "alice:x:1001:1001::/home/alice:/bin/bash\n", 0o600)
			writeFile(t, root, "etc/group", "shell:x:999:alice\nalice:x:1001:\n", 0o600)
			writeFile(t, root, "etc/shadow", "alice:!:0:0:99999:7:::\n", 0o600)
			writeFile(t, root, "home/alice/file", "value", 0o600)
			if err := os.Symlink("file", filepath.Join(root, "home/alice/link")); err != nil {
				t.Fatal(err)
			}
			fixture := &accountsMetadataFixture{t: t, root: root}
			client := newFileCloud()
			execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
				if cmd.Name == tc.tool {
					return host.Result{Stdout: tc.output, Stderr: []byte("captured detail"), ExitCode: tc.exit}, tc.err
				}
				return fixture.execute(ctx, cmd)
			}
			got, err := backup.HostBackup(context.Background(), fileHostEnv(root, execute), cloud.Env{Open: client.open}, store)
			if err != nil || got.Accounts.Err == nil || got.Accounts.Service != "accounts" || got.Host.Service != "host" || got.Host.Err == nil || got.Host.Err.Error() != "accounts not written" || len(client.puts) != 0 {
				t.Fatalf("backup = %+v, %v; puts %v", got, err, client.puts)
			}
			if tc.exit != 0 || tc.err != nil {
				var commandErr *host.CommandError
				if !errors.As(got.Accounts.Err, &commandErr) || commandErr.Label != tc.tool || commandErr.Result.ExitCode != tc.exit || string(commandErr.Result.Stderr) != "captured detail" {
					t.Fatalf("command error = %#v", got.Accounts.Err)
				}
			}
		})
	}
}

func TestAccountsManifestFailurePreventsConfigurationArchive(t *testing.T) {
	// R-T9JO-0R48 R-H8QA-FZWB
	root := t.TempDir()
	store := configuredHostStore(t, root)
	if err := os.Remove(filepath.Join(root, "etc/shadow")); err != nil {
		t.Fatal(err)
	}
	client := newFileCloud()
	got, err := backup.HostBackup(context.Background(), fileHostEnv(root, unexpectedHostCommand(t)), cloud.Env{Open: client.open}, store)
	if err != nil || got.Accounts.Err == nil || got.Host.Err == nil || got.Host.Err.Error() != "accounts not written" || len(client.puts) > 0 {
		t.Fatalf("backup = %+v, %v", got, err)
	}
}

type accountsMetadataFixture struct {
	t                       *testing.T
	root                    string
	getfaclCalls, findCalls int
	aclArgs, findArgs       []string
	files                   fileExecutor
}

func (f *accountsMetadataFixture) execute(ctx context.Context, cmd host.Command) (host.Result, error) {
	switch cmd.Name {
	case "getfacl":
		f.getfaclCalls++
		f.aclArgs = append([]string(nil), cmd.Args...)
		var b strings.Builder
		for _, top := range cmd.Args[6:] {
			err := filepath.WalkDir(top, func(name string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Type()&os.ModeSymlink != 0 || entry.Type()&os.ModeNamedPipe != 0 {
					return nil
				}
				fmt.Fprintf(&b, "# file: %s\n# owner: 1234\n# group: 5678\n", escapeFixtureACLPath(name))
				if name == filepath.Join(f.root, "srv/acme") {
					b.WriteString("user::rwx\nuser:4321:r-x\ngroup::r-x\nmask::r-x\nother::---\ndefault:user::rwx\ndefault:group::rwx\ndefault:other::---\n\n")
				} else {
					b.WriteString("user::rw-\ngroup::r--\nother::---\n\n")
				}
				return nil
			})
			if err != nil {
				f.t.Fatal(err)
			}
		}
		return host.Result{Stdout: []byte(b.String())}, nil
	case "find":
		f.findCalls++
		f.findArgs = append([]string(nil), cmd.Args...)
		var b strings.Builder
		for _, top := range cmd.Args[1 : len(cmd.Args)-4] {
			if err := filepath.WalkDir(top, func(name string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Type()&os.ModeSymlink != 0 {
					fmt.Fprintf(&b, "9999 8888 %s\x00", name)
				}
				return nil
			}); err != nil {
				f.t.Fatal(err)
			}
		}
		return host.Result{Stdout: []byte(b.String())}, nil
	default:
		return f.files.execute(ctx, cmd)
	}
}
func escapeFixtureACLPath(value string) string {
	var b strings.Builder
	for _, c := range []byte(value) {
		if c == '\\' || c == '\n' || c == '\t' || c == ' ' {
			fmt.Fprintf(&b, "\\%03o", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
func accountsArchiveNames(t *testing.T, compressed []byte) []string {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(decodeRawZstandardFrame(t, compressed)))
	var names []string
	for {
		h, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
}

func TestRetireAccountsInterruptionRetainsFailedArchive(t *testing.T) {
	// R-V5U1-FWN4 R-H8QA-FZWB
	root := t.TempDir()
	store := configuredHostStore(t, root)
	executor := newRetireExecutor(nil)
	client := newFileCloud()
	ctx, cancel := context.WithCancel(context.Background())
	client.fail = func(uri string, _ []byte) error {
		if strings.HasSuffix(uri, ".accounts.tar.zst") {
			cancel()
			return context.Canceled
		}
		return nil
	}
	got, err := backup.Retire(ctx, host.Env{Root: root, Now: func() time.Time { return time.Unix(4, 0) }, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
	if !errors.Is(err, context.Canceled) || got.FailedStep != "accounts" || got.Accounts.Service != "accounts" || !errors.Is(got.Accounts.Err, context.Canceled) || got.Host.Service != "" || !got.ServicesStopped || !got.LitestreamStopped || len(client.puts) != 1 {
		t.Fatalf("retire = %+v, %v, puts %v", got, err, client.puts)
	}
}

func TestHostBackupConfigurationAttemptFailuresKeepAccounts(t *testing.T) {
	// R-H8QA-FZWB R-T3G6-3WER
	for _, failure := range []string{"read", "compression", "upload", "collision", "interruption"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			var preserved []byte
			client := newFileCloud()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timestamp := "1970-01-01T00:00:07Z"
			configURI := "s3://bucket/project/host/" + timestamp + ".tar.zst"
			if failure == "collision" {
				client.objects[configURI] = []byte("existing configuration")
			}
			if failure == "read" {
				// The account archive is prepared before this target can be inspected.
				if err := os.MkdirAll(filepath.Join(root, "etc/letsencrypt"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			client.fail = func(uri string, data []byte) error {
				if strings.HasSuffix(uri, ".accounts.tar.zst") {
					preserved = append([]byte(nil), data...)
					if failure == "read" {
						originalInfo, err := os.Stat(filepath.Join(root, "etc/letsencrypt"))
						if err != nil {
							t.Fatal(err)
						}
						if err := os.Chmod(filepath.Join(root, "etc/letsencrypt"), 0o000); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if err := os.Chmod(filepath.Join(root, "etc/letsencrypt"), originalInfo.Mode()); err != nil {
								t.Error(err)
							}
						})
					}
					return nil
				}
				if failure == "upload" {
					return errors.New("configuration upload rejected")
				}
				if failure == "interruption" {
					cancel()
					return context.Canceled
				}
				return nil
			}
			fileFixture := &fileExecutor{}
			calls := 0
			execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
				if cmd.Name == "zstd" {
					calls++
					if failure == "compression" && calls == 2 {
						return host.Result{ExitCode: 8, Stderr: []byte("config compression detail")}, nil
					}
				}
				return fileFixture.execute(ctx, cmd)
			}
			got, err := backup.HostBackup(ctx, host.Env{Root: root, Now: func() time.Time { return time.Unix(7, 0) }, Execute: execute}, cloud.Env{Open: client.open}, store)
			if (err != nil) != (failure == "interruption") || got.Accounts.Err != nil || got.Accounts.Object != timestamp+".accounts.tar.zst" || got.Accounts.Size == 0 || got.Host.Service != "host" || got.Host.Err == nil || got.Host.Object != "" || got.Host.Size != 0 {
				t.Fatalf("backup = %+v, %v", got, err)
			}
			if !bytes.Equal(client.objects["s3://bucket/project/host/"+timestamp+".accounts.tar.zst"], preserved) {
				t.Fatal("accounts archive was not retained")
			}
			if failure == "collision" && (!errors.Is(got.Host.Err, cloud.ErrAlreadyExists) || string(client.objects[configURI]) != "existing configuration") {
				t.Fatalf("collision = %+v", got)
			}
			if failure == "interruption" && (!errors.Is(err, context.Canceled) || !errors.Is(got.Host.Err, context.Canceled)) {
				t.Fatalf("interrupted = %+v, %v", got, err)
			}
		})
	}
}

func TestAccountsBackupReadFailureHasHostPathAndSkipsConfiguration(t *testing.T) {
	// R-T9JO-0R48
	root := t.TempDir()
	storeRoot := t.TempDir()
	store := configuredHostStore(t, storeRoot)
	emptyBackupAccounts(t, root)
	writeFile(t, root, "etc/passwd", "alice:x:1001:1001::/home/alice:/bin/bash\n", 0o600)
	writeFile(t, root, "etc/group", "shell:x:999:alice\nalice:x:1001:\n", 0o600)
	writeFile(t, root, "etc/shadow", "alice:!:0:0:99999:7:::\n", 0o600)
	writeFile(t, root, "home/alice/notes", "secret", 0o000)
	writeFile(t, root, "etc/ikigenba/untouched", "config", 0o600)
	writeFile(t, root, "etc/letsencrypt/untouched", "certificate", 0o600)
	watch := newFileAccessWatch(t, filepath.Join(root, "etc/ikigenba"), filepath.Join(root, "etc/letsencrypt"))
	fixture := &accountsMetadataFixture{t: t, root: root}
	client := newFileCloud()
	got, err := backup.HostBackup(context.Background(), fileHostEnv(root, fixture.execute), cloud.Env{Open: client.open}, store)
	if err != nil || got.Accounts.Err == nil || got.Accounts.Err.Error() != "/home/alice/notes: permission denied" || got.Host.Err == nil || got.Host.Err.Error() != "accounts not written" || len(client.puts) > 0 {
		t.Fatalf("backup = %+v, %v", got, err)
	}
	watch.assertQuiet(t)
}

func TestAccountsBackupRefusesSourceAncestorLinks(t *testing.T) {
	// R-T10D-CCXD R-TBZG-SALM
	for _, ancestor := range []string{"etc", "etc/passwd", "etc/group", "etc/shadow", "etc/ssh", "etc/ssh/authorized_keys", "home", "srv"} {
		t.Run(ancestor, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, t.TempDir())
			emptyBackupAccounts(t, root)
			writeFile(t, root, "etc/passwd", "alice:x:1001:1001::/home/alice:/bin/bash\nacme:x:1004:1004::/srv/acme:/usr/sbin/nologin\n", 0o600)
			writeFile(t, root, "etc/group", "shell:x:999:alice\nalice:x:1001:\nacme:x:1004:alice\n", 0o600)
			writeFile(t, root, "etc/shadow", "alice:!:0:0:99999:7:::\n", 0o600)
			target := filepath.Join(root, "opt/private")
			writeFile(t, root, "opt/private/ssh_host_leaked", "forbidden bytes", 0o600)
			writeFile(t, root, "opt/private/alice/secret", "forbidden home", 0o600)
			writeFile(t, root, "opt/private/acme/secret", "forbidden folder", 0o600)
			// Moving the fixture account files aside also makes the /etc link probe
			// realistic without allowing the reader to enter the forbidden tree.
			if ancestor == "etc" {
				if err := os.Rename(filepath.Join(root, "etc"), filepath.Join(target, "etc-saved")); err != nil {
					t.Fatal(err)
				}
			}
			if ancestor == "etc/passwd" || ancestor == "etc/group" || ancestor == "etc/shadow" {
				if err := os.Rename(filepath.Join(root, ancestor), filepath.Join(target, filepath.Base(ancestor))); err != nil {
					t.Fatal(err)
				}
			}
			parent := filepath.Dir(filepath.Join(root, ancestor))
			if err := os.MkdirAll(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			linkTarget := target
			switch ancestor {
			case "etc":
				linkTarget = filepath.Join(target, "etc-saved")
			case "etc/passwd", "etc/group", "etc/shadow":
				linkTarget = filepath.Join(target, filepath.Base(ancestor))
			}
			relativeTarget, err := filepath.Rel(parent, linkTarget)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(relativeTarget, filepath.Join(root, ancestor)); err != nil {
				t.Fatal(err)
			}
			watch := newFileAccessWatch(t, target)
			client := newFileCloud()
			got, err := backup.HostBackup(context.Background(), fileHostEnv(root, unexpectedHostCommand(t)), cloud.Env{Open: client.open}, store)
			if err != nil || got.Accounts.Err == nil || got.Host.Err == nil || got.Host.Err.Error() != "accounts not written" || len(client.puts) > 0 {
				t.Fatalf("backup through %q = %+v, %v", ancestor, got, err)
			}
			watch.assertQuiet(t)
		})
	}
}
