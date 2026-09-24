package apps_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

// R-U8W8-NGQ6
func TestTimeoutsContract(t *testing.T) {
	assertExactFields(t, reflect.TypeFor[apps.Timeouts](), []field{
		{name: "DrainSeconds", typ: reflect.TypeFor[int64]()},
		{name: "StopSeconds", typ: reflect.TypeFor[int64]()},
	})
	got, err := apps.ReadTimeouts(config.Store{Root: t.TempDir()})
	if err != nil || got != (apps.Timeouts{DrainSeconds: 5, StopSeconds: 10}) {
		t.Fatalf("ReadTimeouts(empty) = %#v, %v", got, err)
	}
}

// R-UA45-18GV
func TestReadTimeoutsUsesSpaceWideDefaultsAndNoManifestOverride(t *testing.T) {
	store := config.Store{Root: t.TempDir()}
	if err := store.Set("apps.drain_seconds", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("apps.stop_seconds", ""); err != nil {
		t.Fatal(err)
	}
	got, err := apps.ReadTimeouts(store)
	if err != nil || got != (apps.Timeouts{DrainSeconds: 5, StopSeconds: 10}) {
		t.Fatalf("ReadTimeouts(empty values) = %#v, %v", got, err)
	}
	manifest, err := apps.ParseManifest([]byte("drain_seconds = 99\nstop_seconds = 100\n"))
	if err != nil || !reflect.DeepEqual(manifest, apps.Manifest{Secrets: []string{}, Env: map[string]string{}}) {
		t.Fatalf("manifest settings = %#v, %v", manifest, err)
	}
	if err := store.Set("apps.drain_seconds", "7"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("apps.stop_seconds", "12"); err != nil {
		t.Fatal(err)
	}
	got, err = apps.ReadTimeouts(store)
	if err != nil || got != (apps.Timeouts{DrainSeconds: 7, StopSeconds: 12}) {
		t.Fatalf("ReadTimeouts(configured) = %#v, %v", got, err)
	}
}

// R-UBC1-F07K
func TestReadTimeoutsValidatesExactRangeOrderAndErrors(t *testing.T) {
	for _, test := range []struct {
		drain, stop, want string
		values            apps.Timeouts
	}{
		{drain: "0001", stop: "09223372036", values: apps.Timeouts{DrainSeconds: 1, StopSeconds: 9223372036}},
		{drain: strings.Repeat("0", 70) + "2", stop: "3", values: apps.Timeouts{DrainSeconds: 2, StopSeconds: 3}},
		{drain: "0", stop: "0", want: "apps.drain_seconds is not a positive whole number of seconds: '0'"},
		{drain: "-1", stop: "x", want: "apps.drain_seconds is not a positive whole number of seconds: '-1'"},
		{drain: "1", stop: "1.5", want: "apps.stop_seconds is not a positive whole number of seconds: '1.5'"},
		{drain: "1", stop: "9223372037", want: "apps.stop_seconds is not a positive whole number of seconds: '9223372037'"},
		{drain: "0005", stop: "05", want: "apps.stop_seconds (5) is not greater than apps.drain_seconds (5)"},
	} {
		store := config.Store{Root: t.TempDir()}
		if err := store.Set("apps.drain_seconds", test.drain); err != nil {
			t.Fatal(err)
		}
		if err := store.Set("apps.stop_seconds", test.stop); err != nil {
			t.Fatal(err)
		}
		got, err := apps.ReadTimeouts(store)
		if test.want == "" {
			if err != nil || got != test.values {
				t.Errorf("ReadTimeouts(%q, %q) = %#v, %v", test.drain, test.stop, got, err)
			}
		} else if err == nil || err.Error() != test.want {
			t.Errorf("ReadTimeouts(%q, %q) error = %v, want %q", test.drain, test.stop, err, test.want)
		}
	}
}

// R-UCJX-SRY9
func TestReadTimeoutsDoesNotWriteAndWrapsStoreFailure(t *testing.T) {
	root := t.TempDir()
	store := config.Store{Root: root}
	if _, err := apps.ReadTimeouts(store); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "ikigenba")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadTimeouts created config directory: %v", err)
	}
	path := filepath.Join(root, "etc", "ikigenba", config.FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.ReadTimeouts(store); !errors.Is(err, config.ErrCorrupt) {
		t.Fatalf("ReadTimeouts(corrupt) = %v, want ErrCorrupt", err)
	}
	assertFile(t, path, "{")
}
