package apps_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

func TestReadSpaceEnvReturnsStoredValues(t *testing.T) {
	// R-QTM0-T9V0 R-QW1T-KTCE
	store := config.Store{Root: t.TempDir()}
	if got, err := apps.ReadSpaceEnv(store); err != nil || got != (apps.SpaceEnv{}) {
		t.Fatalf("missing store = %#v, %v", got, err)
	}
	for _, tc := range []struct {
		key, value string
		set        func(*apps.SpaceEnv, string)
	}{
		{"aws.region", " region ", func(s *apps.SpaceEnv, v string) { s.Region = v }},
		{"hosts.domain", "never-created.invalid.", func(s *apps.SpaceEnv, v string) { s.HostsDomain = v }},
		{"hosts.zone_id", " \t ", func(s *apps.SpaceEnv, v string) { s.HostsZoneID = v }},
		{"hosts.launch_template", "no such template\\\"", func(s *apps.SpaceEnv, v string) { s.HostsLaunchTemplate = v }},
		{"hosts.instance_profile", "never-created\x00profile", func(s *apps.SpaceEnv, v string) { s.HostsInstanceProfile = v }},
	} {
		if err := store.Set(tc.key, tc.value); err != nil {
			t.Fatal(err)
		}
		var want apps.SpaceEnv
		tc.set(&want, tc.value)
		if got, err := apps.ReadSpaceEnv(store); err != nil || got != want {
			t.Fatalf("%s = %#v, %v, want %#v", tc.key, got, err, want)
		}
		if err := store.Del(tc.key); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"hosts.domain": "", "hosts.zone_id": "unused", "hosts.instance_profile": "no such profile"} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	want := apps.SpaceEnv{HostsZoneID: "unused", HostsInstanceProfile: "no such profile"}
	if got, err := apps.ReadSpaceEnv(store); err != nil || got != want {
		t.Fatalf("partial settings = %#v, %v, want %#v", got, err, want)
	}
}

func TestReadSpaceEnvDoesNotWriteStoreAndWrapsFailures(t *testing.T) {
	// R-QX9P-YL33
	store := config.Store{Root: t.TempDir()}
	fs, err := os.OpenRoot(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	if _, err := apps.ReadSpaceEnv(store); err != nil {
		t.Fatal(err)
	}
	dir := strings.TrimPrefix(config.Dir, "/")
	if _, err := fs.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing store changed: %v", err)
	}
	if err := fs.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, config.FileName)
	for _, tc := range []struct {
		data string
		err  error
	}{
		{` { "hosts.domain" : "example.invalid" } `, nil},
		{`{"hosts.domain": false}`, config.ErrCorrupt},
	} {
		before := []byte(tc.data)
		if err := fs.WriteFile(name, before, 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := fs.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := apps.ReadSpaceEnv(store); !errors.Is(err, tc.err) {
			t.Fatalf("ReadSpaceEnv error = %v, want %v", err, tc.err)
		}
		after, err := fs.ReadFile(name)
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("store bytes changed: %q, %v", after, err)
		}
		final, err := fs.Stat(name)
		if err != nil || !os.SameFile(info, final) || final.Mode() != info.Mode() {
			t.Fatalf("store file changed: %v, %v", final, err)
		}
	}
	if _, err := fs.Lstat(filepath.Join(dir, config.LockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store lock created: %v", err)
	}
	if err := fs.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(name, 0o700); err != nil {
		t.Fatal(err)
	}
	_, storeErr := store.List()
	var pathErr *os.PathError
	if !errors.As(storeErr, &pathErr) {
		t.Fatalf("fixture store error = %v", storeErr)
	}
	if _, err := apps.ReadSpaceEnv(store); !errors.Is(err, pathErr.Err) {
		t.Fatalf("ReadSpaceEnv error = %v, want wrapped %v", err, storeErr)
	}
}
