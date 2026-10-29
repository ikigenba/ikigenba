package accounts

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestParseUserKeys(t *testing.T) {
	// R-A5S6-M04U R-A702-ZRVJ R-ABVO-IUUB R-5M09-17QA
	if KeyType != "ssh-ed25519" {
		t.Fatal(KeyType)
	}
	for _, body := range []string{userKeyBody, secondKeyBody, thirdKeyBody} {
		line := KeyType + " " + body + " alice@laptop"
		key, err := ParseKey(line)
		if err != nil || key.Body != body || key.Comment != "alice@laptop" || key.Line() != line {
			t.Fatalf("%+v %v", key, err)
		}
	}
	key, err := ParseKey("  ssh-ed25519\t" + userKeyBody + "  c  d\t e ")
	if err != nil || key.Comment != "c  d\t e" || key.Line() != "ssh-ed25519 "+userKeyBody+" c  d\t e" {
		t.Fatalf("%+v %v", key, err)
	}
	key, err = ParseKey(KeyType + " " + userKeyBody)
	if err != nil || key.Comment != "" {
		t.Fatal(key, err)
	}
	invalid := []string{"", KeyType, "ssh-rsa " + userKeyBody, "restrict " + KeyType + " " + userKeyBody, "-----BEGIN OPENSSH PRIVATE KEY-----", KeyType + " %%%", KeyType + " " + base64.StdEncoding.EncodeToString(make([]byte, 51))}
	for b := 0; b <= 127; b++ {
		if b < 32 && b != 9 || b == 127 {
			invalid = append(invalid, KeyType+" "+userKeyBody+" "+string(rune(b)))
		}
	}
	invalid = append(invalid, KeyType+" "+userKeyBody+" \xff", KeyType+" "+userKeyBody+"=", KeyType+" "+userKeyBody[:len(userKeyBody)-1])
	for _, line := range invalid {
		if _, err := ParseKey(line); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}

func TestUserKeyFiles(t *testing.T) {
	// R-AEBH-AEBP R-AFJD-O62E R-AGRA-1XT3 R-AXTV-EQ6T R-AZ1R-SHXI
	f := newUserFixture(t)
	ctx := context.Background()
	key := f.key()
	path := keyPath(f.env.Root, "alice")
	if err := AddKey(ctx, f.env, "alice", key); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatal(info, err)
	}
	dir, err := os.Stat(rootPath(f.env.Root, "/etc/ssh/authorized_keys"))
	if err != nil || dir.Mode().Perm() != 0755 {
		t.Fatal(dir, err)
	}
	if got := f.read("/etc/ssh/authorized_keys/alice"); got != key.Line()+"\n" {
		t.Fatal(got)
	}
	held := " \t" + KeyType + "\t" + key.Body + " original\n\n" + KeyType + " " + secondKeyBody + " other"
	f.write("/etc/ssh/authorized_keys/alice", held, 0600)
	if err := AddKey(ctx, f.env, "alice", Key{Body: key.Body, Comment: "replacement"}); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if f.read("/etc/ssh/authorized_keys/alice") != held || info.Mode().Perm() != 0600 {
		t.Fatal("matching add rewrote file")
	}
	if err := RemoveKey(ctx, f.env, "alice", Key{Body: thirdKeyBody}); err != nil {
		t.Fatal(err)
	}
	if f.read("/etc/ssh/authorized_keys/alice") != held {
		t.Fatal("absent remove rewrote file")
	}
	if err := AddKey(ctx, f.env, "alice", Key{Body: thirdKeyBody}); err != nil {
		t.Fatal(err)
	}
	want := " \t" + KeyType + "\t" + key.Body + " original\n" + KeyType + " " + secondKeyBody + " other\n" + KeyType + " " + thirdKeyBody + "\n"
	if f.read("/etc/ssh/authorized_keys/alice") != want {
		t.Fatal("held lines not retained in order")
	}
	f.write("/etc/ssh/authorized_keys/alice", key.Line()+"\n"+KeyType+" "+key.Body+" duplicate\n", 0600)
	if err := RemoveKey(ctx, f.env, "alice", key); err != nil {
		t.Fatal(err)
	}
	if got := f.read("/etc/ssh/authorized_keys/alice"); got != "" {
		t.Fatal(got)
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0644 {
		t.Fatal(info.Mode())
	}
	entries, err := os.ReadDir(rootPath(f.env.Root, "/etc/ssh/authorized_keys"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "alice" {
		t.Fatal(entries, err)
	}
	if len(f.commands) != 0 || f.read("/etc/passwd") != userAccountRows || f.read("/etc/group") != userGroups || f.read("/etc/shadow") != userShadow {
		t.Fatal("unrelated mutation")
	}
	if err := RemoveKey(ctx, f.env, "bob", key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyPath(f.env.Root, "bob")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	list, err := ListUsers(ctx, f.env)
	if err != nil || list.Users[1].Keys == nil || len(list.Users[1].Keys) != 0 {
		t.Fatal(list, err)
	}
}

func TestInvalidUserKeysAndWriteFailure(t *testing.T) {
	// R-5KSC-NFZL R-52RP-A5AF
	for _, op := range []func(*userFixture, Key) error{
		func(f *userFixture, k Key) error { return AddUser(context.Background(), f.env, "newuser", "acme", k) },
		func(f *userFixture, k Key) error { return AddKey(context.Background(), f.env, "alice", k) },
		func(f *userFixture, k Key) error { return RemoveKey(context.Background(), f.env, "alice", k) },
	} {
		f := newUserFixture(t)
		for _, key := range []Key{{}, {Body: "bad"}, {Body: userKeyBody, Comment: "bad\ncomment"}} {
			err := op(f, key)
			var refused *RefusedError
			if err == nil || errors.As(err, &refused) || len(f.commands) != 0 {
				t.Fatal(err, f.commands)
			}
		}
	}
	f := newUserFixture(t)
	f.write("/etc/ssh/authorized_keys", "blocks directory", 0600)
	err := AddKey(context.Background(), f.env, "alice", f.key())
	var refused *RefusedError
	if err == nil || errors.As(err, &refused) || len(f.commands) != 0 {
		t.Fatal(err)
	}
	if got := f.read("/etc/ssh/authorized_keys"); got != "blocks directory" {
		t.Fatal(got)
	}
	f = newUserFixture(t)
	if err := os.MkdirAll(keyPath(f.env.Root, "alice"), 0700); err != nil {
		t.Fatal(err)
	}
	err = writeKeys(f.env.Root, "alice", []string{f.key().Line()})
	if err == nil {
		t.Fatal("rename over directory succeeded")
	}
	entries, _ := os.ReadDir(rootPath(f.env.Root, "/etc/ssh/authorized_keys"))
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"alice"}) {
		t.Fatal(names)
	}
	if strings.Contains(err.Error(), "refused") {
		t.Fatal(err)
	}
}

func TestKeyPublicationReplacesWholeFile(t *testing.T) {
	// R-AGRA-1XT3
	f := newUserFixture(t)
	f.write("/etc/ssh/authorized_keys/alice", "prior\n", 0600)
	old, err := os.Open(keyPath(f.env.Root, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close() }()
	before, err := old.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := AddKey(context.Background(), f.env, "alice", f.key()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(keyPath(f.env.Root, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(old)
	if err != nil || string(data) != "prior\n" || os.SameFile(before, after) {
		t.Fatal(string(data), err)
	}
	stat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || strconv.FormatUint(uint64(stat.Uid), 10) != strconv.Itoa(os.Getuid()) {
		t.Fatal("new file owner differs from process")
	}
}
