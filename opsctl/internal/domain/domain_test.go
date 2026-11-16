package domain_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/domain"
)

// R-9J4Y-B40S R-9MSN-GF8V
func TestValidName(t *testing.T) {
	valid := []string{"example.com", "www.example.com", "xn--bcher-kva.de", "localhost", "a", "0.a", "a.1a", "a.1-2", strings.Repeat("a", 63), strings.Repeat("a", 63) + "." + strings.Repeat("a", 63) + "." + strings.Repeat("a", 63) + "." + strings.Repeat("a", 61)}
	invalid := []string{"", "Example.com", "exa_mple.com", "bücher.de", "example.com.", ".example.com", "a..com", "-a.com", "a-.com", "1.2.3.4", "123", "a.123", "a/", "a:b", "a,b", "a\x00b", strings.Repeat("a", 64), strings.Repeat("a", 63) + "." + strings.Repeat("a", 63) + "." + strings.Repeat("a", 63) + "." + strings.Repeat("a", 62)}
	for _, tc := range []struct {
		names []string
		want  bool
	}{{valid, true}, {invalid, false}} {
		for _, name := range tc.names {
			if got := domain.ValidName(name); got != tc.want || domain.ValidName(name) != got {
				t.Errorf("ValidName(%q) = %v, want %v", name, got, tc.want)
			}
		}
	}
}

// R-9FH9-5SSP R-9GP5-JKJE R-9KCU-OVRH R-9O0J-U6ZK R-9P8G-7YQ9 R-QLXF-L2RL
func TestListAbsentEmptyAndSorted(t *testing.T) {
	if domain.Key != "host.domains" {
		t.Fatalf("Key = %q", domain.Key)
	}
	store := config.Store{Root: t.TempDir()}
	list, err := domain.List(store)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("absent List = %#v, %v", list, err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "etc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("List created state: %v", err)
	}
	for _, value := range []string{"", "www.example.com:example.com,example.com"} {
		if err := store.Set(domain.Key, value); err != nil {
			t.Fatal(err)
		}
		list, err := domain.List(store)
		want := []domain.Domain{}
		if value != "" {
			want = []domain.Domain{{Name: "example.com"}, {Name: "www.example.com", Primary: "example.com"}}
		}
		if err != nil || !reflect.DeepEqual(list, want) {
			t.Fatalf("List(%q) = %#v, %v", value, list, err)
		}
	}
}

// R-9HX1-XCA3 R-9QGC-LQGY R-QLXF-L2RL
func TestListMalformedFirstEntryAndReadOnly(t *testing.T) {
	for _, tc := range []struct{ value, bad string }{
		{"example.com,Example.org", "Example.org"}, {"www.example.com:example.com", "www.example.com:example.com"},
		{"a.com,b.com:a.com,c.com:b.com", "c.com:b.com"}, {"a.com,a.com", "a.com"}, {",a.com", ""},
		{"a.com,", ""}, {"a.com,,b.com", ""}, {"a.com:", "a.com:"}, {"a.com:b.com:c.com", "a.com:b.com:c.com"},
		{"a.com, b.com", " b.com"}, {"www.a.com:a.com,a.com", ""}, {"a.com:missing.com,Example.org", "a.com:missing.com"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			store := config.Store{Root: t.TempDir()}
			if err := store.Set(domain.Key, tc.value); err != nil {
				t.Fatal(err)
			}
			before := tree(t, store.Root)
			list, err := domain.List(store)
			if tc.value == "www.a.com:a.com,a.com" {
				if err != nil || len(list) != 2 {
					t.Fatalf("forward target List = %#v, %v", list, err)
				}
			} else if list != nil || !errors.Is(err, domain.ErrMalformed) || err.Error() != fmt.Sprintf("host.domains malformed: %q", tc.bad) {
				t.Fatalf("List = %#v, %v", list, err)
			}
			if !reflect.DeepEqual(tree(t, store.Root), before) {
				t.Fatal("List changed root")
			}
		})
	}
	store := corruptStore(t)
	before := tree(t, store.Root)
	list, err := domain.List(store)
	if list != nil || !errors.Is(err, config.ErrCorrupt) || errors.Is(err, domain.ErrMalformed) {
		t.Fatalf("corrupt List = %#v, %v", list, err)
	}
	if !reflect.DeepEqual(before, tree(t, store.Root)) {
		t.Fatal("corrupt List changed root")
	}
}

// R-9LKR-2NI6 R-47T1-ERTL R-9U41-R1P1 R-9WJU-IL6F
func TestSaveCanonicalRoundtripAndDeletion(t *testing.T) {
	store := config.Store{Root: t.TempDir()}
	if err := domain.Save(store, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "etc/ikigenba/config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty Save created config: %v", err)
	}
	if err := store.Set("another.key", "preserved"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		list  []domain.Domain
		value string
		want  []domain.Domain
	}{
		{[]domain.Domain{{Name: "www.example.com", Primary: "example.com"}, {Name: "example.com"}}, "example.com,www.example.com:example.com", []domain.Domain{{Name: "example.com"}, {Name: "www.example.com", Primary: "example.com"}}},
		{[]domain.Domain{{Name: "ab.com-x"}, {Name: "ab.com", Primary: "p.com"}, {Name: "p.com"}}, "ab.com:p.com,ab.com-x,p.com", []domain.Domain{{Name: "ab.com", Primary: "p.com"}, {Name: "ab.com-x"}, {Name: "p.com"}}},
		{nil, "", []domain.Domain{}}, {[]domain.Domain{}, "", []domain.Domain{}},
	} {
		if err := domain.Save(store, tc.list); err != nil {
			t.Fatal(err)
		}
		value, err := store.Get(domain.Key)
		if tc.value == "" {
			if !errors.Is(err, config.ErrNotSet) {
				t.Fatalf("empty Save Get = %q, %v", value, err)
			}
		} else if err != nil || value != tc.value {
			t.Fatalf("saved %q, %v", value, err)
		}
		list, err := domain.List(store)
		if err != nil || !reflect.DeepEqual(list, tc.want) {
			t.Fatalf("roundtrip = %#v, %v", list, err)
		}
		if value, err := store.Get("another.key"); err != nil || value != "preserved" {
			t.Fatalf("other key = %q, %v", value, err)
		}
	}
	store = corruptStore(t)
	before := configFile(t, store.Root)
	for _, list := range [][]domain.Domain{nil, {{Name: "valid.com"}}} {
		if err := domain.Save(store, list); !errors.Is(err, config.ErrCorrupt) {
			t.Fatalf("corrupt Save = %v", err)
		}
		if before != configFile(t, store.Root) {
			t.Fatal("Save changed corrupt config")
		}
	}
}

// R-UF8X-MU39
func TestSaveRejectsInvalidWithoutConfigChange(t *testing.T) {
	for _, list := range [][]domain.Domain{
		{{Name: "Bad.com"}}, {{Name: "a.com", Primary: "Bad.com"}}, {{Name: "a.com"}, {Name: "a.com"}},
		{{Name: "www.example.com", Primary: "example.com"}}, {{Name: "a.com"}, {Name: "b.com", Primary: "a.com"}, {Name: "c.com", Primary: "b.com"}},
	} {
		store := config.Store{Root: t.TempDir()}
		if err := store.Set("another.key", "original"); err != nil {
			t.Fatal(err)
		}
		before := configFile(t, store.Root)
		err := domain.Save(store, list)
		if !errors.Is(err, domain.ErrMalformed) || !strings.HasPrefix(err.Error(), "host.domains malformed: ") {
			t.Fatalf("Save = %v", err)
		}
		if len(list) == 1 {
			d := list[0]
			entry := d.Name
			if d.Primary != "" {
				entry += ":" + d.Primary
			}
			if err.Error() != fmt.Sprintf("host.domains malformed: %q", entry) {
				t.Fatalf("Save error = %v", err)
			}
		}
		if before != configFile(t, store.Root) {
			t.Fatal("invalid Save changed config")
		}
	}
}

func configFile(t *testing.T, root string) string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rootFS.Close(); err != nil {
			t.Error(err)
		}
	}()
	const path = "etc/ikigenba/config.json"
	info, err := rootFS.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := rootFS.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(info.Mode(), info.ModTime()) + string(data)
}

func corruptStore(t *testing.T) config.Store {
	t.Helper()
	store := config.Store{Root: t.TempDir()}
	if err := store.Set("another.key", "keep"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, "etc/ikigenba/config.json"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	return store
}

func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rootFS.Close(); err != nil {
			t.Error(err)
		}
	}()
	result := map[string]string{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result[path] = fmt.Sprint(info.Mode(), info.ModTime())
		if info.Mode().IsRegular() {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := rootFS.ReadFile(relative)
			if err != nil {
				return err
			}
			result[path] += string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
