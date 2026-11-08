package backup_test

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

const restoreConfigurationURI = "s3://bucket/project/host/2026-09-16T12:00:00+00:00.tar.zst"
const restoreAccountsURI = "s3://bucket/project/host/2026-09-16T12:00:00+00:00.accounts.tar.zst"

func TestHostRestoreAccountsResultAPI(t *testing.T) {
	// R-SUWV-FI7W
	result := backup.HostRestoreResult{AccountsObject: "host/accounts", AccountsSize: 42, AccountsReady: true, Groups: []string{"acme"}, GroupsRestored: true, Users: []string{"alice"}, Locked: []string{"alice"}, TimersInstalled: true, UsersRestored: true, HostKeys: 3, SSHRestored: true}
	var (
		object                                        string
		size                                          int64
		ready, groupsDone, usersDone, timers, sshDone bool
		groups, users, locked                         []string
		keys                                          int
	)
	object, size, ready, groupsDone, usersDone, timers, sshDone, groups, users, locked, keys = result.AccountsObject, result.AccountsSize, result.AccountsReady, result.GroupsRestored, result.UsersRestored, result.TimersInstalled, result.SSHRestored, result.Groups, result.Users, result.Locked, result.HostKeys
	if object != "host/accounts" || size != 42 || !ready || !groupsDone || !usersDone || !timers || !sshDone || keys != 3 || !slices.Equal(groups, []string{"acme"}) || !slices.Equal(users, []string{"alice"}) || !slices.Equal(locked, users) {
		t.Fatalf("result = %+v", result)
	}
}

func TestHostRestoreLegacyHasNoAccountEffects(t *testing.T) {
	// R-TI2Y-P5B3 R-UG85-EQ2J R-TXXN-O5Y4
	root := t.TempDir()
	store := config.Store{Root: root}
	for key, value := range map[string]string{"backup.s3_uri": "s3://bucket/project/", "aws.region": "us-east-2"} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	preserved := map[string]string{"home/ec2-user/file": "machine user", "srv/other/file": "other group", "etc/ssh/authorized_keys/other": "other key", "etc/ssh/ssh_host_other_key": "other host key"}
	for name, data := range preserved {
		writeFile(t, root, name, data, 0o600)
	}
	client := restoreAccountsCloud(t, nil)
	client.objects = []cloud.Object{{URI: restoreConfigurationURI}, {URI: "s3://bucket/project/host/2027-01-01T00:00:00Z.accounts.tar.zst"}, {URI: strings.Replace(restoreAccountsURI, "+00:00", "Z", 1)}}
	env := restoreHostEnv(t, root)
	result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
	if err != nil || !result.AccountsReady || !result.FilesRestored || result.AccountsObject != "" || result.AccountsSize != 0 || result.GroupsRestored || result.UsersRestored || result.SSHRestored {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	if !slices.Equal(client.got, []string{restoreConfigurationURI}) {
		t.Fatalf("downloads = %v", client.got)
	}
	for name, want := range preserved {
		if got := string(readHostRestoreFile(t, root, name)); got != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
	// The absent passwd/group/shadow files would make every accounts function fail.
	for _, name := range []string{"etc/passwd", "etc/group", "etc/shadow"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected %s: %v", name, err)
		}
	}
}

func TestHostRestoreRejectsAccountsBeforeAnyMutation(t *testing.T) {
	// R-F69O-YT10 R-F51S-L1AB R-F8PH-QCIE
	manifest := restoreTestManifest()
	good := restoreManifestMember(t, manifest)
	cases := []struct {
		name    string
		members []restoreMember
		body    []byte
	}{
		{name: "missing manifest"},
		{name: "manifest directory", members: []restoreMember{{name: "accounts.json", typeflag: tar.TypeDir}}},
		{name: "duplicate manifest", members: []restoreMember{good, good}},
		{name: "manifest null", members: []restoreMember{{name: "accounts.json", data: []byte("null")}}},
		{name: "manifest array", members: []restoreMember{{name: "accounts.json", data: []byte("[]")}}},
		{name: "two json values", members: []restoreMember{{name: "accounts.json", data: []byte("{} {}")}}},
		{name: "bad json", members: []restoreMember{{name: "accounts.json", data: []byte("{broken")}}},
		{name: "corrupt tar", body: rawZstandardFrame([]byte("broken tar"))},
		{name: "absolute", members: []restoreMember{good, {name: "/home/alice", typeflag: tar.TypeDir}}},
		{name: "dot", members: []restoreMember{good, {name: "home/./alice", typeflag: tar.TypeDir}}},
		{name: "dotdot", members: []restoreMember{good, {name: "home/alice/../alice", typeflag: tar.TypeDir}}},
		{name: "empty component", members: []restoreMember{good, {name: "home//alice", typeflag: tar.TypeDir}}},
		{name: "unknown user", members: []restoreMember{good, {name: "home/ec2-user", typeflag: tar.TypeDir}}},
		{name: "unknown group", members: []restoreMember{good, {name: "srv/other", typeflag: tar.TypeDir}}},
		{name: "unknown key", members: []restoreMember{good, {name: "etc/ssh/authorized_keys/ec2-user"}}},
		{name: "host key empty suffix", members: []restoreMember{good, {name: "etc/ssh/ssh_host_"}}},
		{name: "host key directory", members: []restoreMember{good, {name: "etc/ssh/ssh_host_rsa_key", typeflag: tar.TypeDir}}},
		{name: "host key nested", members: []restoreMember{good, {name: "etc/ssh/ssh_host_rsa_key/file"}}},
		{name: "home root regular", members: []restoreMember{good, {name: "home/alice"}}},
		{name: "folder root link", members: []restoreMember{good, {name: "srv/acme", typeflag: tar.TypeSymlink, linkname: "elsewhere"}}},
		{name: "missing root", members: []restoreMember{good, {name: "home/alice/file"}}},
		{name: "missing parent", members: []restoreMember{good, {name: "home/alice", typeflag: tar.TypeDir}, {name: "home/alice/missing/file"}}},
		{name: "parent symlink", members: []restoreMember{good, {name: "home/alice", typeflag: tar.TypeDir}, {name: "home/alice/link", typeflag: tar.TypeSymlink, linkname: "/outside"}, {name: "home/alice/link/file"}}},
		{name: "hard link", members: []restoreMember{good, {name: "home/alice", typeflag: tar.TypeDir}, {name: "home/alice/link", typeflag: tar.TypeLink, linkname: "home/alice/file"}}},
		{name: "device", members: []restoreMember{good, {name: "srv/acme", typeflag: tar.TypeDir}, {name: "srv/acme/device", typeflag: tar.TypeChar}}},
		{name: "fifo", members: []restoreMember{good, {name: "srv/acme", typeflag: tar.TypeDir}, {name: "srv/acme/fifo", typeflag: tar.TypeFifo}}},
		{name: "duplicate tree", members: []restoreMember{good, {name: "srv/acme", typeflag: tar.TypeDir}, {name: "srv/acme/", typeflag: tar.TypeDir}}},
	}
	for _, acl := range []string{"", "user::rw", "user::rwx,", "user:x:rwx", "default:user::rwx", "owner::rwx", "user::xwr", "mask::rwa", "other::rwxx"} {
		for _, key := range []string{"SCHILY.acl.access", "SCHILY.acl.default"} {
			cases = append(cases, struct {
				name    string
				members []restoreMember
				body    []byte
			}{name: key + "/" + acl, members: []restoreMember{good, {name: "etc/ssh/ssh_host_rsa_key", pax: map[string]string{key: acl}}}})
		}
	}
	for name, change := range map[string]func(*accounts.Manifest){
		"invalid user name":      func(m *accounts.Manifest) { m.Users[0].Name = "Bad" },
		"invalid group name":     func(m *accounts.Manifest) { m.Groups[0].Slug = "Bad" },
		"duplicate user":         func(m *accounts.Manifest) { m.Users = append(m.Users, m.Users[0]) },
		"duplicate slug":         func(m *accounts.Manifest) { m.Groups = append(m.Groups, m.Groups[0]) },
		"slug and user":          func(m *accounts.Manifest) { m.Users[0].Name = m.Groups[0].Slug },
		"invalid state":          func(m *accounts.Manifest) { m.Users[0].State = "other" },
		"unknown business group": func(m *accounts.Manifest) { m.Users[0].Groups = []string{"other"} },
		"negative uid":           func(m *accounts.Manifest) { m.Users[0].UID = -1 },
		"negative gid":           func(m *accounts.Manifest) { m.Groups[0].GID = -1 },
	} {
		m := restoreTestManifest()
		change(&m)
		cases = append(cases, struct {
			name    string
			members []restoreMember
			body    []byte
		}{name: name, members: []restoreMember{restoreManifestMember(t, m)}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			writeFile(t, root, "etc/letsencrypt/old", "old certificate", 0o600)
			before := fileTreeSnapshot(t, root)
			body := test.body
			if body == nil {
				body = hostRestoreArchive(t, test.members...)
			}
			client := restoreAccountsCloud(t, body)
			result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
			if err == nil || result.FailedStep != "accounts" || !result.SourceReady || result.AccountsReady || result.FilesRestored || result.GroupsRestored || result.UsersRestored || result.SSHRestored {
				t.Fatalf("HostRestore() = %+v, %v", result, err)
			}
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid accounts changed host: before %v after %v", before, after)
			}
			if len(client.readers) != 2 || !client.readers[0].closed || !client.readers[1].closed {
				t.Fatalf("readers = %#v", client.readers)
			}
		})
	}
}

func TestHostRestoreManifestCollisionLeavesConfiguration(t *testing.T) {
	// R-TRU5-RB8N
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/passwd", "carol:x:1002:3002::/home/carol:/bin/bash\n", 0o600)
	writeFile(t, root, "etc/letsencrypt/old", "old certificate", 0o600)
	before := fileTreeSnapshot(t, root)
	client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, restoreTestManifest())))
	result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
	var refused *accounts.RefusedError
	if !errors.As(err, &refused) || refused.Reason != "uid 1002 of bob is carol's on this host" || !reflect.DeepEqual(err, refused) || result.FailedStep != "accounts" || result.AccountsReady || result.FilesRestored {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	if !reflect.DeepEqual(fileTreeSnapshot(t, root), before) {
		t.Fatal("collision changed host")
	}
}

func restoreTestManifest() accounts.Manifest {
	return accounts.Manifest{Groups: []accounts.ManifestGroup{{Slug: "acme", UID: 1004, GID: 1004, Members: []string{"alice", "bob"}}, {Slug: "beta", UID: 1005, GID: 1005, Members: []string{"bob"}}}, Users: []accounts.ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: accounts.StateActive, Groups: []string{"acme"}}, {Name: "bob", UID: 1002, GID: 1002, State: accounts.StateLocked, Groups: []string{"acme", "beta"}}}}
}

func restoreManifestMember(t *testing.T, manifest accounts.Manifest) restoreMember {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return restoreMember{name: "accounts.json", data: data, mode: 0o600}
}

func restoreAccountsCloud(t *testing.T, body []byte) *restoreCloud {
	t.Helper()
	configBody := hostRestoreArchive(t, restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o700}, restoreMember{name: "etc/ikigenba/restored", data: []byte("restored configuration"), mode: 0o640})
	return &restoreCloud{objects: []cloud.Object{{URI: restoreAccountsURI}, {URI: restoreConfigurationURI}}, bodies: map[string][]byte{restoreConfigurationURI: configBody, restoreAccountsURI: body}}
}

func TestHostRestoreReplaysAccountTreesInOrder(t *testing.T) {
	// R-PD7P-X6NV R-PFNI-OQ59 R-U0DG-FPFI R-U2T9-78WW R-TXXN-O5Y4 R-UA4N-HVD2 R-F8PH-QCIE
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/ssh/sshd_config", "host config", 0o600)
	kept := map[string]string{"home/ec2-user/notes": "machine", "home/other/file": "other home", "srv/other/file": "other folder", "etc/ssh/authorized_keys/other": "other key", "etc/ssh/ssh_host_untouched_key": "untouched host key"}
	for name, data := range kept {
		writeFile(t, root, name, data, 0o600)
	}
	for _, name := range []string{"srv/acme/stale", "home/alice/stale", "home/bob/stale"} {
		writeFile(t, root, name, "stale", 0o600)
	}
	manifest := restoreTestManifest()
	specialName := "home/alice/back\\slash\nline\rend"
	members := []restoreMember{
		restoreManifestMember(t, manifest),
		{name: "etc/ssh/ssh_host_rsa_key", data: []byte("private"), mode: 0o600, uid: 23, gid: 24},
		{name: "etc/ssh/ssh_host_rsa_key.pub", data: []byte("public"), mode: 0o644, uid: 23, gid: 24},
		{name: "etc/ssh/authorized_keys/alice", data: []byte("alice key"), mode: 0o640, uid: 41, gid: 42},
		{name: "home/alice", typeflag: tar.TypeDir, mode: 0o3750, uid: 41, gid: 42, pax: map[string]string{"SCHILY.acl.access": "user::rwx,user:1002:r-x,group::r-x,mask::r-x,other::---", "SCHILY.acl.default": "user::rwx,group::r-x,other::---"}},
		{name: specialName, data: []byte("escaped"), mode: 0o6754, uid: 41, gid: 42},
		{name: "home/alice/second-link", typeflag: tar.TypeSymlink, linkname: "/outside", uid: 40, gid: 3},
		{name: "home/alice/third-link", typeflag: tar.TypeSymlink, linkname: "back\\slash\nline\rend", uid: 40, gid: 2},
		{name: "home/alice/fourth-link", typeflag: tar.TypeSymlink, linkname: "/outside-too", uid: 40, gid: 3},
		{name: "home/bob", typeflag: tar.TypeDir, mode: 0o700, uid: 51, gid: 52},
		{name: "home/bob/file", data: []byte("bob home"), mode: 0o600, uid: 51, gid: 52},
		{name: "srv/acme", typeflag: tar.TypeDir, mode: 0o2770, uid: 63, gid: 64},
		{name: "srv/acme/file", data: []byte("group file"), mode: 0o660, uid: 63, gid: 64},
		{name: "srv/beta", typeflag: tar.TypeDir, mode: 0o770, uid: 61, gid: 62},
	}
	body := hostRestoreArchive(t, members...)
	client := restoreAccountsCloud(t, body)
	fixture := newRestoreAccountExecutor(t, root)
	fixture.before = func(command host.Command) {
		if command.Name == "groupadd" && command.Args[len(command.Args)-1] == "acme" {
			if got := string(readHostRestoreFile(t, root, "srv/acme/file")); got != "group file" {
				t.Fatalf("group tree before account = %q", got)
			}
			if len(fixture.inputs) != 1 || !strings.Contains(fixture.inputs[0], "# owner: 63\n# group: 64\n# flags: -s-\n") {
				t.Fatalf("group metadata missing before account: %q", fixture.inputs)
			}
		}
		if command.Name == "useradd" && command.Args[len(command.Args)-1] == "alice" {
			if string(readHostRestoreFile(t, root, "etc/ssh/authorized_keys/alice")) != "alice key" || string(readHostRestoreFile(t, root, specialName)) != "escaped" {
				t.Fatal("user's tree/key missing before account")
			}
		}
		if command.Name == "sshd" {
			if string(readHostRestoreFile(t, root, "etc/ssh/ssh_host_rsa_key")) != "private" || string(readHostRestoreFile(t, root, "etc/ikigenba/restored")) != "restored configuration" {
				t.Fatal("keys or config missing before sshd check")
			}
			want := []string{"-t", "-f", filepath.Join(root, "etc/ssh/sshd_config")}
			if !slices.Equal(command.Args, want) {
				t.Fatalf("sshd args = %v", command.Args)
			}
		}
	}
	result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
	if err != nil || !result.AccountsReady || !result.FilesRestored || !result.GroupsRestored || !result.UsersRestored || !result.SSHRestored || result.TimersInstalled || result.HostKeys != 1 || result.FailedStep != "" {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	if !slices.Equal(result.Groups, []string{"acme", "beta"}) || !slices.Equal(result.Users, []string{"alice", "bob"}) || !slices.Equal(result.Locked, []string{"bob"}) {
		t.Fatalf("restored entries = %+v", result)
	}
	if result.AccountsObject != "host/2026-09-16T12:00:00+00:00.accounts.tar.zst" || result.AccountsSize != int64(len(body)) {
		t.Fatalf("accounts object = %+v", result)
	}
	if len(fixture.inputs) != 5 {
		t.Fatalf("setfacl calls = %d, %v", len(fixture.inputs), fixture.calls)
	}
	rootSlash := filepath.ToSlash(root)
	expectedAlice := fmt.Sprintf("# file: %s/etc/ssh/authorized_keys/alice\n# owner: 41\n# group: 42\nuser::rw-\ngroup::r--\nother::---\n\n# file: %s/home/alice\n# owner: 41\n# group: 42\n# flags: -st\nuser::rwx\nuser:1002:r-x\ngroup::r-x\nmask::r-x\nother::---\ndefault:user::rwx\ndefault:group::r-x\ndefault:other::---\n\n# file: %s/home/alice/back\\134slash\\012line\\015end\n# owner: 41\n# group: 42\n# flags: ss-\nuser::rwx\ngroup::r-x\nother::r--\n\n", rootSlash, rootSlash, rootSlash)
	if fixture.inputs[2] != expectedAlice {
		t.Fatalf("alice metadata =\n%q\nwant\n%q", fixture.inputs[2], expectedAlice)
	}
	linkCalls := []string{}
	for _, call := range fixture.calls {
		if strings.HasPrefix(call, "chown --no-dereference") {
			linkCalls = append(linkCalls, call)
		}
	}
	wantLinks := []string{"chown --no-dereference 40:2 -- " + filepath.Join(root, "home/alice/third-link"), "chown --no-dereference 40:3 -- " + filepath.Join(root, "home/alice/second-link") + " " + filepath.Join(root, "home/alice/fourth-link")}
	if !slices.Equal(linkCalls, wantLinks) {
		t.Fatalf("link ownership = %v, want %v", linkCalls, wantLinks)
	}
	for name, want := range kept {
		if got := string(readHostRestoreFile(t, root, name)); got != want {
			t.Fatalf("untouched %s = %q", name, got)
		}
	}
	for _, name := range []string{"srv/acme/stale", "home/alice/stale", "home/bob/stale"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale %s: %v", name, err)
		}
	}
	for name, want := range map[string]os.FileMode{"home/alice": os.ModeSetgid | os.ModeSticky | 0o750, specialName: os.ModeSetuid | os.ModeSetgid | 0o754, "srv/acme": os.ModeSetgid | 0o770, "etc/ssh/authorized_keys/alice": 0o640} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky|0o777) != want {
			t.Fatalf("%s mode = %v, %v", name, info, err)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "home/alice/second-link")); err != nil || target != "/outside" {
		t.Fatalf("restored link = %q, %v", target, err)
	}
	assertNoRestoreTemps(t, root)
	// A rerun replaces stale contents, retains other identities and skips account creation.
	writeFile(t, root, "home/alice/added-after-restore", "remove on rerun", 0o600)
	store = configuredHostStore(t, root)
	fixture.before = nil
	fixture.calls = nil
	fixture.inputs = nil
	result, err = backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
	if err != nil || !result.SSHRestored {
		t.Fatalf("rerun = %+v, %v", result, err)
	}
	for _, call := range fixture.calls {
		if strings.HasPrefix(call, "groupadd") || strings.HasPrefix(call, "useradd") {
			t.Fatalf("rerun recreated account: %s", call)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "home/alice/added-after-restore")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rerun left stale home: %v", err)
	}
}

func TestHostRestoreAccountPhaseFailuresRetainEarlierPhases(t *testing.T) {
	// R-PD7P-X6NV R-PFNI-OQ59 R-F8PH-QCIE R-UL3Q-XT1B
	for _, step := range []string{"groups", "users"} {
		t.Run(step, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			fixture := newRestoreAccountExecutor(t, root)
			fixture.fail = "groupadd --system --gid 1004 acme"
			if step == "users" {
				fixture.fail = "groupadd --gid 1001 alice"
			}
			body := hostRestoreArchive(t, restoreManifestMember(t, restoreTestManifest()))
			client := restoreAccountsCloud(t, body)
			result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) || !reflect.DeepEqual(err, commandErr) || commandErr.Label != "groupadd" || commandErr.Result.ExitCode != 17 || result.FailedStep != step || !result.FilesRestored || result.UsersRestored || result.SSHRestored {
				t.Fatalf("HostRestore() = %+v, %v", result, err)
			}
			if step == "groups" && (result.GroupsRestored || len(result.Groups) != 0) {
				t.Fatalf("failed group marked completed: %+v", result)
			}
			if step == "users" && (!result.GroupsRestored || len(result.Groups) != 2 || len(result.Users) != 0) {
				t.Fatalf("earlier groups lost: %+v", result)
			}
			for _, call := range fixture.calls {
				if strings.Contains(call, "bob") || strings.HasPrefix(call, "sshd") {
					t.Fatalf("later operation after failure: %v", fixture.calls)
				}
			}
			if string(readHostRestoreFile(t, root, "etc/ikigenba/restored")) != "restored configuration" {
				t.Fatal("earlier config replacement lost")
			}
			assertNoRestoreTemps(t, root)
		})
	}
}

func TestHostRestoreSSHFailureRestoresOriginalFiles(t *testing.T) {
	// R-UCKG-9EUG R-UA4N-HVD2 R-UL3Q-XT1B R-TXXN-O5Y4 R-F8PH-QCIE
	for _, failure := range []string{"setfacl", "sshd", "reload", "start error", "put key"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			writeFile(t, root, "etc/ssh/ssh_host_rsa_key", "old key", 0o640)
			writeFile(t, root, "etc/ssh/ssh_host_untouched_key", "other key", 0o600)
			original, err := os.Lstat(filepath.Join(root, "etc/ssh/ssh_host_rsa_key"))
			if err != nil {
				t.Fatal(err)
			}
			members := []restoreMember{restoreManifestMember(t, accounts.Manifest{}), {name: "etc/ssh/ssh_host_new_key", data: []byte("new key"), mode: 0o600}, {name: "etc/ssh/ssh_host_rsa_key", data: []byte("new key"), mode: 0o600}}
			if failure == "put key" {
				writeFile(t, root, "etc/ssh/ssh_host_blocked_key/blocked", "cannot move directory", 0o600)
				members = append(members[:1], append([]restoreMember{{name: "etc/ssh/ssh_host_blocked_key", data: []byte("replacement"), mode: 0o600}}, members[1:]...)...)
			}
			fixture := newRestoreAccountExecutor(t, root)
			sshDirectory, statErr := os.Stat(filepath.Join(root, "etc/ssh"))
			if statErr != nil {
				t.Fatal(statErr)
			}
			switch failure {
			case "setfacl":
				fixture.fail = "setfacl --restore=-"
			case "sshd", "start error":
				fixture.fail = "sshd -t -f " + filepath.Join(root, "etc/ssh/sshd_config")
			case "reload":
				fixture.fail = "systemctl reload sshd.service"
			case "put key":
				fixture.before = func(command host.Command) {
					if command.Name == "getent" {
						if err := os.Chmod(filepath.Join(root, "etc/ssh"), sshDirectory.Mode().Perm()&^0o200); err != nil {
							t.Fatal(err)
						}
					}
				}
				defer func() {
					if err := os.Chmod(filepath.Join(root, "etc/ssh"), sshDirectory.Mode().Perm()); err != nil {
						t.Error(err)
					}
				}()
			}
			if failure == "start error" {
				fixture.startErr = errors.New("executor refused")
			}
			client := restoreAccountsCloud(t, hostRestoreArchive(t, members...))
			result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
			if err == nil || result.FailedStep != "ssh" || result.SSHRestored || !result.UsersRestored || !result.GroupsRestored || !result.FilesRestored {
				t.Fatalf("HostRestore() = %+v, %v", result, err)
			}
			if failure == "put key" {
				if err := os.Chmod(filepath.Join(root, "etc/ssh"), sshDirectory.Mode().Perm()); err != nil {
					t.Fatal(err)
				}
			}
			after, statErr := os.Lstat(filepath.Join(root, "etc/ssh/ssh_host_rsa_key"))
			if statErr != nil || !os.SameFile(original, after) || after.Mode() != original.Mode() {
				t.Fatalf("old key inode/mode lost: %v %v", after, statErr)
			}
			if string(readHostRestoreFile(t, root, "etc/ssh/ssh_host_rsa_key")) != "old key" || string(readHostRestoreFile(t, root, "etc/ssh/ssh_host_untouched_key")) != "other key" {
				t.Fatal("old host key bytes lost")
			}
			if _, err := os.Lstat(filepath.Join(root, "etc/ssh/ssh_host_new_key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("new key remains after failure: %v", err)
			}
			if failure != "put key" {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) || commandErr.Result.ExitCode != 17 || string(commandErr.Result.Stderr) != "fixture failure\n" {
					t.Fatalf("command error = %v", err)
				}
				label := map[string]string{"setfacl": "setfacl", "sshd": "sshd -t", "reload": "systemctl reload", "start error": "sshd -t"}[failure]
				if commandErr.Label != label {
					t.Fatalf("command label = %q, want %q", commandErr.Label, label)
				}
				if failure == "sshd" || failure == "start error" {
					if slices.Contains(fixture.calls, "systemctl reload sshd.service") {
						t.Fatal("reloaded after failed sshd check")
					}
				}
			}
			assertNoRestoreTemps(t, root)
		})
	}
}

func TestHostRestoreEmptyAccountsStillChecksSSH(t *testing.T) {
	// R-UA4N-HVD2 R-PD7P-X6NV R-PFNI-OQ59
	root := t.TempDir()
	store := configuredHostStore(t, root)
	fixture := newRestoreAccountExecutor(t, root)
	client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreMember{name: "accounts.json", data: []byte(`{"groups":null,"users":null,"future":true}`)}))
	result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
	if err != nil || !result.GroupsRestored || !result.UsersRestored || !result.SSHRestored || len(result.Groups) != 0 || len(result.Users) != 0 || result.HostKeys != 0 {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	want := []string{"zstd --quiet --decompress --stdout", "zstd --quiet --decompress --stdout", "getent group " + hardening.ShellGroup, "zstd --quiet --decompress --stdout", "zstd --quiet --decompress --stdout", "sshd -t -f " + filepath.Join(root, "etc/ssh/sshd_config"), "systemctl reload sshd.service"}
	if !slices.Equal(fixture.calls, want) {
		t.Fatalf("empty accounts commands = %v, want %v", fixture.calls, want)
	}
}

func TestHostRestoreUsersGetInstalledTimer(t *testing.T) {
	// R-PFNI-OQ59
	root := t.TempDir()
	store := configuredHostStore(t, root)
	fixture := newRestoreAccountExecutor(t, root)
	writeFile(t, root, strings.TrimPrefix(release.UserUnitDir, "/")+"/"+release.TurnsTimer, "timer", 0o644)
	manifest := accounts.Manifest{Users: []accounts.ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: accounts.StateActive}}}
	client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, manifest)))
	result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
	if err != nil || !result.TimersInstalled || !result.UsersRestored {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	for _, call := range []string{"loginctl enable-linger alice", "systemctl start user@1001.service", "systemctl --user --machine=alice@.host enable --now " + release.TurnsTimer} {
		if !slices.Contains(fixture.calls, call) {
			t.Fatalf("timer command %q missing: %v", call, fixture.calls)
		}
	}
}

type restoreAccountExecutor struct {
	t        *testing.T
	root     string
	calls    []string
	inputs   []string
	before   func(host.Command)
	fail     string
	startErr error
}

func newRestoreAccountExecutor(t *testing.T, root string) *restoreAccountExecutor {
	t.Helper()
	writeFile(t, root, "etc/group", hardening.ShellGroup+":x:900:\n", 0o600)
	writeFile(t, root, "etc/passwd", "", 0o600)
	writeFile(t, root, "etc/shadow", "", 0o600)
	return &restoreAccountExecutor{t: t, root: root}
}

func (f *restoreAccountExecutor) env() host.Env { return host.Env{Root: f.root, Execute: f.execute} }

func (f *restoreAccountExecutor) execute(ctx context.Context, command host.Command) (host.Result, error) {
	f.t.Helper()
	call := command.Name + " " + strings.Join(command.Args, " ")
	f.calls = append(f.calls, call)
	if f.before != nil {
		f.before(command)
	}
	if call == f.fail {
		return host.Result{ExitCode: 17, Stdout: []byte("fixture stdout\n"), Stderr: []byte("fixture failure\n")}, f.startErr
	}
	switch command.Name {
	case "zstd":
		return restoreHostEnv(f.t, f.root).Execute(ctx, command)
	case "getent":
		if !slices.Equal(command.Args, []string{"group", hardening.ShellGroup}) {
			f.t.Fatalf("unexpected getent: %v", command.Args)
		}
		return host.Result{Stdout: []byte(hardening.ShellGroup + ":x:900:\n")}, nil
	case "setfacl":
		if slices.Equal(command.Args, []string{"--restore=-"}) {
			input, err := io.ReadAll(command.Stdin)
			if err != nil {
				f.t.Fatal(err)
			}
			f.inputs = append(f.inputs, string(input))
		}
	case "groupadd":
		name := command.Args[len(command.Args)-1]
		gid := restoreCommandArgument(command.Args, "--gid")
		appendRestoreAccountLine(f.t, f.root, "etc/group", fmt.Sprintf("%s:x:%s:\n", name, gid))
	case "useradd":
		name := command.Args[len(command.Args)-1]
		uid := restoreCommandArgument(command.Args, "--uid")
		group := restoreCommandArgument(command.Args, "--gid")
		home := restoreCommandArgument(command.Args, "--home-dir")
		shell := restoreCommandArgument(command.Args, "--shell")
		gid := ""
		groupsData := string(readHostRestoreFile(f.t, f.root, "etc/group"))
		for line := range strings.SplitSeq(groupsData, "\n") {
			fields := strings.Split(line, ":")
			if len(fields) == 4 && fields[0] == group {
				gid = fields[2]
			}
		}
		appendRestoreAccountLine(f.t, f.root, "etc/passwd", fmt.Sprintf("%s:x:%s:%s::%s:%s\n", name, uid, gid, home, shell))
		expire := ""
		if restoreCommandArgument(command.Args, "--expiredate") != "" {
			expire = "1"
		}
		appendRestoreAccountLine(f.t, f.root, "etc/shadow", fmt.Sprintf("%s:!:1:0:99999:7::%s:\n", name, expire))
		supplementary := strings.Split(restoreCommandArgument(command.Args, "--groups"), ",")
		for _, g := range supplementary {
			if g != "" {
				f.changeMembership(name, g, true)
			}
		}
	case "gpasswd":
		f.changeMembership(command.Args[1], command.Args[2], command.Args[0] == "--add")
	case "usermod":
		lines := strings.Split(string(readHostRestoreFile(f.t, f.root, "etc/shadow")), "\n")
		for i, line := range lines {
			fields := strings.Split(line, ":")
			if len(fields) == 9 && fields[0] == command.Args[2] {
				fields[7] = command.Args[1]
				lines[i] = strings.Join(fields, ":")
			}
		}
		writeFile(f.t, f.root, "etc/shadow", strings.Join(lines, "\n"), 0o600)
	case "loginctl":
		if slices.Equal(command.Args, []string{"list-users", "--no-legend"}) {
			return host.Result{Stdout: []byte("1002 bob\n")}, nil
		}
	case "systemctl", "sshd", "chown", "chmod", "runuser":
	case "git":
		return host.Result{Stdout: []byte("/srv/acme\n/srv/beta\n")}, nil
	default:
		f.t.Fatalf("unexpected restore command: %s", call)
	}
	return host.Result{}, nil
}

func (f *restoreAccountExecutor) changeMembership(name, group string, add bool) {
	data := strings.Split(string(readHostRestoreFile(f.t, f.root, "etc/group")), "\n")
	for i, line := range data {
		fields := strings.Split(line, ":")
		if len(fields) != 4 || fields[0] != group {
			continue
		}
		members := []string{}
		if fields[3] != "" {
			members = strings.Split(fields[3], ",")
		}
		members = slices.DeleteFunc(members, func(value string) bool { return value == name })
		if add {
			members = append(members, name)
		}
		fields[3] = strings.Join(members, ",")
		data[i] = strings.Join(fields, ":")
	}
	writeFile(f.t, f.root, "etc/group", strings.Join(data, "\n"), 0o600)
}

func restoreCommandArgument(args []string, option string) string {
	for i, arg := range args {
		if arg == option && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func appendRestoreAccountLine(t *testing.T, root, name, line string) {
	t.Helper()
	data := readHostRestoreFile(t, root, name)
	writeFile(t, root, name, string(data)+line, 0o600)
}

func assertNoRestoreTemps(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".opsctl-") {
			t.Fatalf("temporary entry remains: %s", entry.Name())
		}
	}
}

func TestHostRestoreRetainsPartialAccountProgress(t *testing.T) {
	// R-PD7P-X6NV R-PFNI-OQ59 R-F8PH-QCIE
	for _, step := range []string{"groups", "users"} {
		t.Run(step, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			fixture := newRestoreAccountExecutor(t, root)
			fixture.fail = "groupadd --system --gid 1005 beta"
			if step == "users" {
				fixture.fail = "groupadd --gid 1002 bob"
			}
			client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, restoreTestManifest())))
			result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
			if err == nil || result.FailedStep != step || !result.FilesRestored || result.UsersRestored || result.SSHRestored {
				t.Fatalf("HostRestore() = %+v, %v", result, err)
			}
			if step == "groups" {
				if !slices.Equal(result.Groups, []string{"acme"}) || result.GroupsRestored || len(result.Users) != 0 {
					t.Fatalf("partial groups = %+v", result)
				}
				for _, call := range fixture.calls {
					if strings.Contains(call, "alice") || strings.Contains(call, "bob") {
						t.Fatalf("attempted user after failed group: %v", fixture.calls)
					}
				}
			} else if !result.GroupsRestored || !slices.Equal(result.Groups, []string{"acme", "beta"}) || !slices.Equal(result.Users, []string{"alice"}) || len(result.Locked) != 0 {
				t.Fatalf("partial users = %+v", result)
			}
			if slices.Contains(fixture.calls, "systemctl reload sshd.service") {
				t.Fatal("reloaded sshd after account failure")
			}
			assertNoRestoreTemps(t, root)
		})
	}
}

func TestHostRestoreCreatesOnlyMissingAccountParents(t *testing.T) {
	// R-TXXN-O5Y4 R-U0DG-FPFI R-U2T9-78WW
	root := t.TempDir()
	store := configuredHostStore(t, root)
	fixture := newRestoreAccountExecutor(t, root)
	writeFile(t, root, "etc/ssh/sshd_config", "config", 0o600)
	manifest := accounts.Manifest{Groups: []accounts.ManifestGroup{{Slug: "acme", UID: 1004, GID: 1004}}, Users: []accounts.ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: accounts.StateActive}}}
	client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, manifest),
		restoreMember{name: "etc/ssh/authorized_keys/alice", mode: 0o640, data: []byte("key"), pax: map[string]string{"SCHILY.acl.access": "user::rw-,group::r--,mask:999999999999999999999999999999999999999999:r--,other:7:---"}},
		restoreMember{name: "home/alice", typeflag: tar.TypeDir, mode: 0o750},
		restoreMember{name: "home/alice/file", mode: 0o6750, data: []byte("special")},
		restoreMember{name: "srv/acme", typeflag: tar.TypeDir, mode: 0o2770},
	))
	result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
	if err != nil || !result.SSHRestored {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	for _, name := range []string{"home", "srv", strings.TrimPrefix(hardening.KeysDir, "/")} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("parent %s = %v, %v", name, info, err)
		}
	}
	info, err := os.Lstat(filepath.Join(root, "home/alice/file"))
	if err != nil || info.Mode()&(os.ModeSetuid|os.ModeSetgid|0o777) != os.ModeSetuid|os.ModeSetgid|0o750 {
		t.Fatalf("special member mode = %v, %v", info, err)
	}
	if len(fixture.inputs) != 2 || !strings.Contains(fixture.inputs[1], "mask:999999999999999999999999999999999999999999:r--\nother:7:---\n") {
		t.Fatalf("decimal qualifiers missing: %q", fixture.inputs)
	}
	assertNoRestoreTemps(t, root)
}

func TestHostRestoreRejectsAccountDestinationAncestorLinks(t *testing.T) {
	// R-TXXN-O5Y4 R-UCKG-9EUG
	for _, tree := range []string{"home", "srv", "key file", "host key"} {
		t.Run(tree, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			fixture := newRestoreAccountExecutor(t, root)
			writeFile(t, root, "opt/private/authorized_keys/alice", "keep private", 0o600)
			writeFile(t, root, "opt/private/ssh_host_rsa_key", "keep host key", 0o600)
			manifest := accounts.Manifest{Groups: []accounts.ManifestGroup{{Slug: "acme", UID: 1004, GID: 1004}}, Users: []accounts.ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: accounts.StateActive}}}
			var member restoreMember
			var link string
			switch tree {
			case "home":
				link = "home"
				member = restoreMember{name: "home/alice", typeflag: tar.TypeDir, mode: 0o700}
			case "srv":
				link = "srv"
				member = restoreMember{name: "srv/acme", typeflag: tar.TypeDir, mode: 0o700}
			case "key file":
				link = "etc/ssh"
				member = restoreMember{name: "etc/ssh/authorized_keys/alice", data: []byte("replace")}
			case "host key":
				link = "etc/ssh"
				member = restoreMember{name: "etc/ssh/ssh_host_rsa_key", data: []byte("replace")}
			}
			target := "opt/private"
			if strings.HasPrefix(link, "etc/") {
				target = "../opt/private"
			}
			if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
				t.Fatal(err)
			}
			before := fileTreeSnapshot(t, filepath.Join(root, "opt"))
			client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, manifest), member))
			result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
			if err == nil || result.SSHRestored {
				t.Fatalf("HostRestore() = %+v, %v", result, err)
			}
			if after := fileTreeSnapshot(t, filepath.Join(root, "opt")); !reflect.DeepEqual(after, before) {
				t.Fatalf("symlink ancestor changed /opt: before %v after %v", before, after)
			}
			for _, input := range fixture.inputs {
				if strings.Contains(input, filepath.Join(root, "opt")) {
					t.Fatalf("metadata command targeted /opt: %q", input)
				}
			}
			assertNoRestoreTemps(t, root)
		})
	}
}

func TestHostRestoreAccountsReadFailuresCloseReaders(t *testing.T) {
	// R-F69O-YT10 R-F8PH-QCIE
	for _, failure := range []string{"get", "read", "close", "compression"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			before := fileTreeSnapshot(t, root)
			base := restoreAccountsCloud(t, nil)
			readFailure := errors.New("accounts reader failure")
			reader := &restoreFailingAccountsReader{body: strings.NewReader(string(hostRestoreArchive(t, restoreManifestMember(t, accounts.Manifest{}))))}
			client := &restoreDownloadAccountsClient{restoreCloud: base, reader: reader}
			if failure == "get" {
				client.getErr = readFailure
			}
			if failure == "read" {
				reader.readErr = readFailure
			}
			if failure == "close" {
				reader.closeErr = readFailure
			}
			env := restoreHostEnv(t, root)
			decompress := env.Execute
			decompressCalls := 0
			env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
				decompressCalls++
				if failure == "compression" && decompressCalls == 2 {
					return host.Result{ExitCode: 4, Stderr: []byte("corrupt compressed accounts")}, nil
				}
				return decompress(ctx, command)
			}
			result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }}, store)
			if err == nil || result.FailedStep != "accounts" || !result.SourceReady || result.AccountsReady || result.FilesRestored || !reader.closed {
				t.Fatalf("HostRestore() = %+v, %v; reader closed %v", result, err, reader.closed)
			}
			if failure != "compression" && !errors.Is(err, readFailure) {
				t.Fatalf("reader cause lost: %v", err)
			}
			if !reflect.DeepEqual(fileTreeSnapshot(t, root), before) {
				t.Fatal("accounts read failure changed host")
			}
		})
	}
}

type restoreDownloadAccountsClient struct {
	*restoreCloud
	reader io.ReadCloser
	getErr error
}

func (c *restoreDownloadAccountsClient) GetObject(ctx context.Context, uri string) (io.ReadCloser, error) {
	if uri == restoreAccountsURI {
		return c.reader, c.getErr
	}
	return c.restoreCloud.GetObject(ctx, uri)
}

type restoreFailingAccountsReader struct {
	body              *strings.Reader
	readErr, closeErr error
	closed            bool
}

func (r *restoreFailingAccountsReader) Read(p []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	return r.body.Read(p)
}
func (r *restoreFailingAccountsReader) Close() error { r.closed = true; return r.closeErr }

func TestHostRestoreLinkOwnershipFailureStopsUser(t *testing.T) {
	// R-UL3Q-XT1B R-PFNI-OQ59 R-U0DG-FPFI
	root := t.TempDir()
	store := configuredHostStore(t, root)
	fixture := newRestoreAccountExecutor(t, root)
	manifest := accounts.Manifest{Users: []accounts.ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: accounts.StateActive}}}
	client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, manifest), restoreMember{name: "home/alice", typeflag: tar.TypeDir, mode: 0o700}, restoreMember{name: "home/alice/link", typeflag: tar.TypeSymlink, linkname: "elsewhere", uid: 12, gid: 34}))
	fixture.fail = "chown --no-dereference 12:34 -- " + filepath.Join(root, "home/alice/link")
	result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Label != "chown" || commandErr.Result.ExitCode != 17 || string(commandErr.Result.Stderr) != "fixture failure\n" || result.FailedStep != "users" || !result.GroupsRestored || result.UsersRestored || result.SSHRestored {
		t.Fatalf("HostRestore() = %+v, %v", result, err)
	}
	for _, call := range fixture.calls {
		if strings.HasPrefix(call, "useradd") || strings.HasPrefix(call, "sshd") {
			t.Fatalf("operation after metadata failure: %v", fixture.calls)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "home/alice/link")); err != nil || target != "elsewhere" {
		t.Fatalf("restored tree lost after metadata failure: %q %v", target, err)
	}
	assertNoRestoreTemps(t, root)
}

func TestHostAccountsBackupRestoreRoundTrip(t *testing.T) {
	// R-F69O-YT10 R-U0DG-FPFI R-U2T9-78WW R-PD7P-X6NV R-PFNI-OQ59 R-UA4N-HVD2
	source := t.TempDir()
	store := configuredHostStore(t, source)
	writeFile(t, source, "etc/passwd", "acme:x:1004:1004::/srv/acme:/usr/sbin/nologin\nalice:x:1001:1001::/home/alice:/bin/bash\n", 0o600)
	writeFile(t, source, "etc/group", hardening.ShellGroup+":x:900:alice\nacme:x:1004:alice\nalice:x:1001:\n", 0o600)
	writeFile(t, source, "etc/shadow", "alice:!:1:0:99999:7:::\n", 0o600)
	for name, data := range map[string]string{"home/alice/file\nwith\\escape": "home contents", "srv/acme/file": "shared contents", "etc/ssh/authorized_keys/alice": "user key", "etc/ssh/ssh_host_rsa_key": "host key"} {
		writeFile(t, source, name, data, 0o640)
	}
	if err := os.Symlink("file\nwith\\escape", filepath.Join(source, "home/alice/link")); err != nil {
		t.Fatal(err)
	}
	metadata := &accountsMetadataFixture{t: t, root: source}
	uploaded := newFileCloud()
	saved, err := backup.HostBackup(context.Background(), fileHostEnv(source, metadata.execute), cloud.Env{Open: uploaded.open}, store)
	if err != nil || saved.Accounts.Err != nil || saved.Host.Err != nil {
		t.Fatalf("HostBackup() = %+v, %v", saved, err)
	}
	client := &restoreCloud{bodies: uploaded.objects}
	for uri := range uploaded.objects {
		client.objects = append(client.objects, cloud.Object{URI: uri})
	}
	destination := t.TempDir()
	destinationStore := configuredHostStore(t, destination)
	writeFile(t, destination, "etc/ssh/sshd_config", "destination ssh config", 0o600)
	fixture := newRestoreAccountExecutor(t, destination)
	restored, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, destinationStore)
	if err != nil || !restored.SSHRestored || !slices.Equal(restored.Groups, []string{"acme"}) || !slices.Equal(restored.Users, []string{"alice"}) || restored.HostKeys != 1 {
		t.Fatalf("HostRestore() = %+v, %v", restored, err)
	}
	for _, name := range []string{"home/alice/file\nwith\\escape", "srv/acme/file", "etc/ssh/authorized_keys/alice", "etc/ssh/ssh_host_rsa_key"} {
		want := readHostRestoreFile(t, source, name)
		if got := readHostRestoreFile(t, destination, name); !slices.Equal(got, want) {
			t.Fatalf("round trip %q = %q, want %q", name, got, want)
		}
	}
	if target, err := os.Readlink(filepath.Join(destination, "home/alice/link")); err != nil || target != "file\nwith\\escape" {
		t.Fatalf("round trip link = %q %v", target, err)
	}
	restoredManifest, err := accounts.ReadManifest(context.Background(), fixture.env())
	sourceManifest, sourceErr := accounts.ReadManifest(context.Background(), host.Env{Root: source})
	if err != nil || sourceErr != nil || !reflect.DeepEqual(restoredManifest, sourceManifest) {
		t.Fatalf("round trip manifest = %+v %v, source %+v %v", restoredManifest, err, sourceManifest, sourceErr)
	}
	if len(fixture.inputs) != 3 || !strings.Contains(fixture.inputs[0], "# owner: 1234\n# group: 5678\n") || !strings.Contains(fixture.inputs[0], "default:user::rwx\ndefault:group::rwx\ndefault:other::---\n") {
		t.Fatalf("round trip ownership/default ACL = %q", fixture.inputs)
	}
	assertNoRestoreTemps(t, destination)
}

func TestHostRestoreMissingFolderStillGuardsParent(t *testing.T) {
	// R-TXXN-O5Y4 R-PD7P-X6NV
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprint(linked), func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			fixture := newRestoreAccountExecutor(t, root)
			writeFile(t, root, "opt/private/kept", "private", 0o600)
			if linked {
				if err := os.Symlink("opt/private", filepath.Join(root, "srv")); err != nil {
					t.Fatal(err)
				}
			}
			before := fileTreeSnapshot(t, filepath.Join(root, "opt"))
			manifest := accounts.Manifest{Groups: []accounts.ManifestGroup{{Slug: "acme", UID: 1004, GID: 1004}}}
			client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, manifest)))
			result, err := backup.HostRestore(context.Background(), fixture.env(), cloud.Env{Open: client.open}, store)
			if linked {
				if err == nil || result.FailedStep != "groups" || result.GroupsRestored {
					t.Fatalf("HostRestore() = %+v, %v", result, err)
				}
				for _, call := range fixture.calls {
					if strings.HasPrefix(call, "groupadd") || strings.HasPrefix(call, "useradd") {
						t.Fatalf("created account behind symlink: %v", fixture.calls)
					}
				}
			} else {
				if err != nil || !result.SSHRestored {
					t.Fatalf("HostRestore() = %+v, %v", result, err)
				}
				info, err := os.Lstat(filepath.Join(root, "srv"))
				if err != nil || info.Mode().Perm() != 0o755 {
					t.Fatalf("new parent = %v %v", info, err)
				}
			}
			if after := fileTreeSnapshot(t, filepath.Join(root, "opt")); !reflect.DeepEqual(after, before) {
				t.Fatalf("missing folder path changed /opt: before %v after %v", before, after)
			}
			assertNoRestoreTemps(t, root)
		})
	}
}

func TestHostRestoreChecksAccountFileAncestorsBeforeReading(t *testing.T) {
	// R-TXXN-O5Y4 R-TRU5-RB8N
	for _, name := range []string{"passwd", "group", "shadow"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			private := filepath.Join(root, "etc/opt/private")
			writeFile(t, root, "etc/opt/private/"+name, "sensitive account data", 0o600)
			source := filepath.Join(root, "etc", name)
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("opt/private/"+name, source); err != nil {
				t.Fatal(err)
			}
			before := fileTreeSnapshot(t, root)
			watch := newFileAccessWatch(t, private)
			manifest := accounts.Manifest{Users: []accounts.ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: accounts.StateActive}}}
			client := restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, manifest)))
			result, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
			watch.assertQuiet(t)
			if err == nil || result.FailedStep != "accounts" || result.AccountsReady || result.FilesRestored {
				t.Fatalf("HostRestore() = %+v, %v", result, err)
			}
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
				t.Fatal("account source guard changed host")
			}
		})
	}
}
