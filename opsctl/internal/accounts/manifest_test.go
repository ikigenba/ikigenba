package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func restoreManifest() Manifest {
	return Manifest{Groups: []ManifestGroup{{Slug: "acme", GID: 1004, UID: 1004, Members: []string{"alice", "bob"}}, {Slug: "beta", GID: 1005, UID: 1005, Members: []string{"bob"}}}, Users: []ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: StateActive, Groups: []string{"acme"}}, {Name: "bob", UID: 1002, GID: 1002, State: StateLocked, Groups: []string{"acme", "beta"}}}}
}

func TestManifestPublicInterface(t *testing.T) {
	// R-94DG-E2HC R-9815-JDPF
	read := ReadManifest
	check := CheckManifest
	shell := EnsureShellGroup
	group := RestoreGroup
	user := RestoreUser
	f := newUserFixture(t)
	if _, err := read(context.Background(), f.env); err != nil {
		t.Fatal(err)
	}
	f.write("/etc/passwd", "", 0600)
	f.write("/etc/group", "", 0600)
	if err := check(context.Background(), f.env, Manifest{}); err != nil {
		t.Fatal(err)
	}
	if err := shell(context.Background(), f.env); err != nil {
		t.Fatal(err)
	}
	if err := group(context.Background(), f.env, ManifestGroup{Slug: "bad!"}); err == nil {
		t.Fatal("group accepted invalid name")
	}
	if err := user(context.Background(), f.env, ManifestUser{Name: "bad!"}); err == nil {
		t.Fatal("user accepted invalid name")
	}
	data, err := json.Marshal(restoreManifest())
	want := `{"groups":[{"slug":"acme","gid":1004,"uid":1004,"members":["alice","bob"]},{"slug":"beta","gid":1005,"uid":1005,"members":["bob"]}],"users":[{"name":"alice","uid":1001,"gid":1001,"state":"active","groups":["acme"]},{"name":"bob","uid":1002,"gid":1002,"state":"locked","groups":["acme","beta"]}]}`
	if err != nil || string(data) != want {
		t.Fatal(string(data), err)
	}
}

func TestReadManifestInventory(t *testing.T) {
	// R-9AGY-AX6T R-9CWR-2GO7 R-9FCJ-U05L
	f := newUserFixture(t)
	f.write("/etc/passwd", userAccountRows+"empty:x:305:305::/srv/empty:/bin/false\ncarol:x:1003:1003::/home/carol:/bin/bash\n", 0600)
	f.write("/etc/group", strings.Replace(userGroups, "bob,alice,missing", "bob,alice,carol,missing", 1)+"empty:x:305:\nleftover:x:306:\ncarol:x:1003:\n", 0600)
	before := userTree(t, f.env.Root)
	got, err := ReadManifest(context.Background(), f.env)
	want := Manifest{Groups: []ManifestGroup{{Slug: "acme", GID: 301, UID: 301, Members: []string{"alice", "bob"}}, {Slug: "beta", GID: 302, UID: 302, Members: []string{"alice"}}, {Slug: "empty", GID: 305, UID: 305, Members: []string{}}}, Users: []ManifestUser{{Name: "alice", UID: 1001, GID: 1001, State: StateActive, Groups: []string{"acme", "beta"}}, {Name: "bob", UID: 1002, GID: 1002, State: StateLocked, Groups: []string{"acme"}}, {Name: "carol", UID: 1003, GID: 1003, State: StateActive, Groups: []string{}}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, want, err)
	}
	if len(f.commands) != 0 || !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
		t.Fatal("inventory had effects", f.commands)
	}
	f.write("/etc/passwd", "root:x:0:0::/root:/bin/bash\n", 0600)
	f.write("/etc/group", "root:x:0:\nshell:x:400:\n", 0600)
	got, err = ReadManifest(context.Background(), f.env)
	data, marshalErr := json.Marshal(got)
	if err != nil || marshalErr != nil || string(data) != `{"groups":[],"users":[]}` {
		t.Fatal(got, err, marshalErr)
	}
	f.write("/etc/passwd", "acme:x:1004:1004::/srv/acme:/bin/false\nalice:x:1001:1001::/home/alice:/bin/bash\n", 0600)
	f.write("/etc/group", "acme:x:1004:alice\nshell:x:400:alice\n", 0600)
	got, err = ReadManifest(context.Background(), f.env)
	data, marshalErr = json.Marshal(got)
	if err != nil || marshalErr != nil || string(data) != `{"groups":[{"slug":"acme","gid":1004,"uid":1004,"members":["alice"]}],"users":[{"name":"alice","uid":1001,"gid":1001,"state":"active","groups":["acme"]}]}` {
		t.Fatal(string(data), err, marshalErr)
	}
}

func TestReadManifestInvalidInputs(t *testing.T) {
	// R-9HSC-LJMZ
	for _, kind := range []string{"shadow-missing", "shadow-directory", "shadow-malformed", "uid-negative", "uid-plus", "uid-overflow", "gid-invalid", "group-gid-invalid", "duplicate-uid", "invalid-name"} {
		t.Run(kind, func(t *testing.T) {
			f := newUserFixture(t)
			switch kind {
			case "shadow-missing":
				if err := os.Remove(rootPath(f.env.Root, "/etc/shadow")); err != nil {
					t.Fatal(err)
				}
			case "shadow-directory":
				if err := os.Remove(rootPath(f.env.Root, "/etc/shadow")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(rootPath(f.env.Root, "/etc/shadow"), 0700); err != nil {
					t.Fatal(err)
				}
			case "shadow-malformed":
				f.write("/etc/shadow", "alice:bad\n", 0600)
			case "uid-negative":
				f.write("/etc/passwd", strings.Replace(userAccountRows, "alice:x:1001:", "alice:x:-1:", 1), 0600)
			case "uid-plus":
				f.write("/etc/passwd", strings.Replace(userAccountRows, "alice:x:1001:", "alice:x:+1:", 1), 0600)
			case "uid-overflow":
				f.write("/etc/passwd", strings.Replace(userAccountRows, "alice:x:1001:", "alice:x:9999999999999999999999999999999:", 1), 0600)
			case "gid-invalid":
				f.write("/etc/passwd", strings.Replace(userAccountRows, "alice:x:1001:1001:", "alice:x:1001:nan:", 1), 0600)
			case "group-gid-invalid":
				f.write("/etc/group", strings.Replace(userGroups, "acme:x:301:", "acme:x:bad:", 1), 0600)
			case "duplicate-uid":
				f.write("/etc/passwd", strings.Replace(userAccountRows, "bob:x:1002:", "bob:x:1001:", 1), 0600)
			case "invalid-name":
				f.write("/etc/passwd", strings.ReplaceAll(userAccountRows, "alice", "Alice"), 0600)
				f.write("/etc/group", strings.ReplaceAll(userGroups, "alice", "Alice"), 0600)
			}
			before := userTree(t, f.env.Root)
			_, err := ReadManifest(context.Background(), f.env)
			var refused *RefusedError
			if err == nil || errors.As(err, &refused) || len(f.commands) != 0 || !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
				t.Fatal(err, f.commands)
			}
		})
	}
}

func TestManifestWellFormedValidationBeforeHostRead(t *testing.T) {
	// R-9K85-D34D
	for _, kind := range []string{"group-name", "member-name", "user-name", "user-group-name", "duplicate-name", "duplicate-uid", "duplicate-gid", "negative-group-uid", "negative-group-gid", "negative-user-uid", "negative-user-gid", "state", "duplicate-user-group", "unknown-user-group"} {
		t.Run(kind, func(t *testing.T) {
			m := restoreManifest()
			switch kind {
			case "group-name":
				m.Groups[0].Slug = "bad!"
			case "member-name":
				m.Groups[0].Members = []string{"bad!"}
			case "user-name":
				m.Users[0].Name = "bad!"
			case "user-group-name":
				m.Users[0].Groups = []string{"bad!"}
			case "duplicate-name":
				m.Users[0].Name = m.Groups[0].Slug
			case "duplicate-uid":
				m.Users[0].UID = m.Groups[0].UID
			case "duplicate-gid":
				m.Users[0].GID = m.Groups[0].GID
			case "negative-group-uid":
				m.Groups[0].UID = -1
			case "negative-group-gid":
				m.Groups[0].GID = -1
			case "negative-user-uid":
				m.Users[0].UID = -1
			case "negative-user-gid":
				m.Users[0].GID = -1
			case "state":
				m.Users[0].State = "other"
			case "duplicate-user-group":
				m.Users[0].Groups = []string{"acme", "acme"}
			case "unknown-user-group":
				m.Users[0].Groups = []string{"absent"}
			}
			f := newUserFixture(t)
			// No host account files: invalid manifests must fail on their own terms.
			env := f.env
			env.Root = t.TempDir()
			err := CheckManifest(context.Background(), env, m)
			var refused *RefusedError
			var pathErr *os.PathError
			if err == nil || errors.As(err, &refused) || errors.As(err, &pathErr) || len(f.commands) != 0 {
				t.Fatal(err, f.commands)
			}
		})
	}
	f := newUserFixture(t)
	f.write("/etc/passwd", "", 0600)
	f.write("/etc/group", "", 0600)
	m := Manifest{Groups: []ManifestGroup{{Slug: "zero", UID: 0, GID: 0, Members: []string{"absent", "absent"}}}}
	if err := CheckManifest(context.Background(), f.env, m); err != nil {
		t.Fatal("valid zero ids and repeated member accepted by name rules", err)
	}
}

func TestManifestCollisionPrecedence(t *testing.T) {
	// R-9NVU-IECG R-9QBN-9XTU R-9TZC-F91X R-9XN1-KKA0
	cases := []struct{ name, passwd, groups, want string }{
		{"group taken account", "acme:x:17:18::/home/acme:/bin/bash\n", "", "acme on this host is not a business group"},
		{"group taken group", "", "acme:x:17:\n", "acme on this host is not a business group"},
		{"group uid", "acme:x:017:18::/srv/acme:/bin/false\n", "acme:x:18:\n", "acme on this host has uid 017, not 1004"},
		{"group gid", "acme:x:1004:18::/srv/acme:/bin/false\n", "acme:x:018:\n", "acme on this host has gid 018, not 1004"},
		{"free group uid", "z:x:01004:19::/root:/bin/false\na:x:1004:20::/root:/bin/false\n", "x:x:1004:\n", "uid 1004 of acme is z's on this host"},
		{"free group gid", "", "z:x:01004:\na:x:1004:\n", "gid 1004 of acme is z's on this host"},
		{"user taken account", "alice:x:17:18::/home/alice:/bin/bash\n", "", "alice on this host is not a user"},
		{"user taken group", "", "alice:x:1001:\n", "alice on this host is not a user"},
		{"user uid", "alice:x:017:18::/home/alice:/bin/bash\n", "shell:x:400:alice\n", "alice on this host has uid 017, not 1001"},
		{"user gid", "alice:x:1001:018::/home/alice:/bin/bash\n", "shell:x:400:alice\n", "alice on this host has gid 018, not 1001"},
		{"free user uid", "z:x:01001:19::/root:/bin/false\na:x:1001:20::/root:/bin/false\n", "x:x:1001:\n", "uid 1001 of alice is z's on this host"},
		{"free user gid", "", "z:x:01001:\na:x:1001:\n", "gid 1001 of alice is z's on this host"},
		{"fixture user", "carol:x:1002:19::/root:/bin/false\n", "", "uid 1002 of bob is carol's on this host"},
		{"fixture group first", "carol:x:1002:19::/root:/bin/false\n", "carol:x:1004:\n", "gid 1004 of acme is carol's on this host"},
		{"empty", "", "", ""},
		{"matching", "acme:x:01004:1004::/srv/acme:/bin/false\nalice:x:01001:01001::/home/alice:/bin/bash\n", "acme:x:01004:alice\nshell:x:400:alice\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t)
			f.write("/etc/passwd", tc.passwd, 0600)
			f.write("/etc/group", tc.groups, 0600)
			if err := os.Remove(rootPath(f.env.Root, "/etc/shadow")); err != nil {
				t.Fatal(err)
			}
			before := userTree(t, f.env.Root)
			m := restoreManifest()
			m.Groups[0], m.Groups[1] = m.Groups[1], m.Groups[0]
			m.Users[0], m.Users[1] = m.Users[1], m.Users[0]
			err := CheckManifest(context.Background(), f.env, m)
			var refused *RefusedError
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.As(err, &refused) || refused.Reason != tc.want {
				t.Fatal(err, tc.want)
			}
			if len(f.commands) != 0 || !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
				t.Fatal("collision check mutated state", f.commands)
			}
		})
	}
}
