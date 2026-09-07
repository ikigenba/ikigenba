package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

func TestConstants(t *testing.T) {
	// R-NHVQ-SZF1
	if config.Dir != "/etc/ikigenba" {
		t.Errorf("Dir = %q, want /etc/ikigenba", config.Dir)
	}
	if config.FileName != "config.json" {
		t.Errorf("FileName = %q, want config.json", config.FileName)
	}
	if config.LockName != "config.lock" {
		t.Errorf("LockName = %q, want config.lock", config.LockName)
	}
}

func TestErrorValues(t *testing.T) {
	// R-NJ3N-6R5Q
	want := map[error]string{
		config.ErrNotSet:       "key not set",
		config.ErrInvalidKey:   "invalid key",
		config.ErrInvalidValue: "invalid value",
		config.ErrCorrupt:      "config file is corrupt",
	}
	for err, msg := range want {
		if err == nil {
			t.Errorf("error for %q is nil", msg)
			continue
		}
		if err.Error() != msg {
			t.Errorf("error.Error() = %q, want %q", err.Error(), msg)
		}
	}
}

func TestExportedAPI(t *testing.T) {
	// R-NKBJ-KIWF
	st := reflect.TypeOf(config.Store{})
	if st.Kind() != reflect.Struct {
		t.Fatalf("Store is %s, want struct", st.Kind())
	}
	if st.NumField() != 1 {
		t.Fatalf("Store has %d fields, want 1", st.NumField())
	}
	root := st.Field(0)
	if root.Name != "Root" || root.Type.Kind() != reflect.String {
		t.Errorf("Store field = %s %s, want Root string", root.Name, root.Type)
	}

	et := reflect.TypeOf(config.Entry{})
	if et.Kind() != reflect.Struct {
		t.Fatalf("Entry is %s, want struct", et.Kind())
	}
	if et.NumField() != 2 {
		t.Fatalf("Entry has %d fields, want 2", et.NumField())
	}
	key := et.Field(0)
	value := et.Field(1)
	if key.Name != "Key" || key.Type.Kind() != reflect.String {
		t.Errorf("Entry field 0 = %s %s, want Key string", key.Name, key.Type)
	}
	if value.Name != "Value" || value.Type.Kind() != reflect.String {
		t.Errorf("Entry field 1 = %s %s, want Value string", value.Name, value.Type)
	}

	stringType := reflect.TypeOf("")
	boolType := reflect.TypeOf(false)
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	entrySlice := reflect.TypeOf([]config.Entry{})

	vt := reflect.TypeOf(config.ValidKey)
	if vt.Kind() != reflect.Func {
		t.Fatalf("ValidKey is %s, want func", vt.Kind())
	}
	if vt.NumIn() != 1 || vt.In(0) != stringType || vt.NumOut() != 1 || vt.Out(0) != boolType {
		t.Errorf("ValidKey signature = %s, want func(string) bool", vt)
	}

	checkMethod(t, st, "Get", []reflect.Type{stringType}, []reflect.Type{stringType, errorType})
	checkMethod(t, st, "Set", []reflect.Type{stringType, stringType}, []reflect.Type{errorType})
	checkMethod(t, st, "Del", []reflect.Type{stringType}, []reflect.Type{errorType})
	checkMethod(t, st, "List", nil, []reflect.Type{entrySlice, errorType})
}

func TestValidKey(t *testing.T) {
	// R-NLJF-YAN4
	valid := []string{
		"a",
		"0",
		"_",
		".",
		"-",
		"dns.zones",
		"a_b-c.d0",
		"backup.s3_uri",
	}
	for _, key := range valid {
		if !config.ValidKey(key) {
			t.Errorf("ValidKey(%q) = false, want true", key)
		}
	}
	invalid := []string{
		"",
		"A",
		"a b",
		"a/b",
		"a=b",
		"a:b",
		"foo@bar",
		"ä",
		"a\n",
		" dns",
		"dns ",
		"+",
		"a+b",
		"*",
		"A.b",
		"a\r",
		"a\t",
	}
	for _, key := range invalid {
		if config.ValidKey(key) {
			t.Errorf("ValidKey(%q) = true, want false", key)
		}
	}
}

func TestSetRejectsInvalidKeyAndValue(t *testing.T) {
	// R-NMRC-C2DT
	s := newStore(t)
	if err := s.Set("ok.key", "ok"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(storeFile(s))
	if err != nil {
		t.Fatal(err)
	}

	err = s.Set("INVALID", "x")
	if !errors.Is(err, config.ErrInvalidKey) {
		t.Errorf("uppercase key: err = %v, want wrapping ErrInvalidKey", err)
	}
	err = s.Set("ok.key", "line\nfeed")
	if !errors.Is(err, config.ErrInvalidValue) {
		t.Errorf("value with \\n: err = %v, want wrapping ErrInvalidValue", err)
	}
	err = s.Set("ok.key", "carriage\rreturn")
	if !errors.Is(err, config.ErrInvalidValue) {
		t.Errorf("value with \\r: err = %v, want wrapping ErrInvalidValue", err)
	}
	after, err := os.ReadFile(storeFile(s))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("file changed after rejected Set: %q -> %q", before, after)
	}

	empty := config.Store{Root: t.TempDir()}
	if err := empty.Set("Bad", "x"); !errors.Is(err, config.ErrInvalidKey) {
		t.Errorf("uppercase key on missing file: err = %v, want wrapping ErrInvalidKey", err)
	}
	if err := empty.Set("ok.key", "a\nb"); !errors.Is(err, config.ErrInvalidValue) {
		t.Errorf("newline on missing file: err = %v, want wrapping ErrInvalidValue", err)
	}
	if _, err := os.Stat(storeDir(empty)); !os.IsNotExist(err) {
		t.Errorf("rejected Set created the store directory: %v", err)
	}
}

func TestSetCreatesDirAndFileModes(t *testing.T) {
	// R-NNZ8-PU4I
	s := newStore(t)
	dir := storeDir(s)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("store directory already exists: %v", err)
	}
	if err := s.Set("dns.zones", "ikigenba.dev"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %04o, want 0700", info.Mode().Perm())
	}
	file := storeFile(s)
	info, err = os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", file)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestSetGetRoundTripAndFormat(t *testing.T) {
	// R-NP75-3LV7
	s := newStore(t)
	if err := s.Set("z", "last"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("a", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("empty", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("a", "replaced"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get("a")
	if err != nil || got != "replaced" {
		t.Errorf("Get(a) = %q, %v, want %q, nil", got, err, "replaced")
	}
	got, err = s.Get("empty")
	if err != nil || got != "" {
		t.Errorf("Get(empty) = %q, %v, want empty string, nil", got, err)
	}
	got, err = s.Get("z")
	if err != nil || got != "last" {
		t.Errorf("Get(z) = %q, %v, want %q, nil", got, err, "last")
	}

	data, err := os.ReadFile(storeFile(s))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": \"replaced\",\n  \"empty\": \"\",\n  \"z\": \"last\"\n}\n"
	if string(data) != want {
		t.Errorf("config.json = %q, want %q", data, want)
	}
}

func TestGetAbsentKey(t *testing.T) {
	// R-NQF1-HDLW
	s := newStore(t)
	_, err := s.Get("missing.key")
	if !errors.Is(err, config.ErrNotSet) {
		t.Errorf("missing file: err = %v, want wrapping ErrNotSet", err)
	}
	if err := s.Set("present.key", "v"); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get("other.key")
	if !errors.Is(err, config.ErrNotSet) {
		t.Errorf("absent key: err = %v, want wrapping ErrNotSet", err)
	}
}

func TestDel(t *testing.T) {
	// R-NRMX-V5CL
	missing := newStore(t)
	if err := missing.Del("any.key"); err != nil {
		t.Errorf("Del on missing file: %v, want nil", err)
	}
	if _, err := os.Stat(storeFile(missing)); !os.IsNotExist(err) {
		t.Errorf("Del on missing file created the store file: %v", err)
	}

	s := newStore(t)
	if err := s.Set("keep", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("drop", "2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Del("drop"); err != nil {
		t.Fatalf("Del existing key: %v", err)
	}
	if _, err := s.Get("drop"); !errors.Is(err, config.ErrNotSet) {
		t.Errorf("Get after Del: %v, want wrapping ErrNotSet", err)
	}
	got, err := s.Get("keep")
	if err != nil || got != "1" {
		t.Errorf("Get(keep) after Del = %q, %v, want 1, nil", got, err)
	}
	if err := s.Del("drop"); err != nil {
		t.Errorf("Del already-absent key: %v, want nil", err)
	}
	got, err = s.Get("keep")
	if err != nil || got != "1" {
		t.Errorf("Get(keep) after second Del = %q, %v, want 1, nil", got, err)
	}
}

func TestList(t *testing.T) {
	// R-NSUU-8X3A
	s := newStore(t)
	entries, err := s.List()
	if err != nil {
		t.Fatalf("List missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("List missing file = %#v, want empty slice", entries)
	}

	if err := s.Set("b", "2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("c", "3"); err != nil {
		t.Fatal(err)
	}
	entries, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []config.Entry{
		{Key: "a", Value: "1"},
		{Key: "b", Value: "2"},
		{Key: "c", Value: "3"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("List = %#v, want %#v", entries, want)
	}
}

func TestCorruptFile(t *testing.T) {
	// R-NU2Q-MOTZ
	cases := []struct {
		name string
		data string
	}{
		{"array", `[]`},
		{"number", `1`},
		{"string", `"x"`},
		{"bool", `true`},
		{"null", `null`},
		{"null-value", `{"a":null}`},
		{"bool-value", `{"a":true}`},
		{"array-value", `{"a":[]}`},
		{"non-string", `{"a": 1}`},
		{"nested", `{"a": {}}`},
		{"mixed-null", `{"a":"x","b":null}`},
		{"malformed", `{`},
		{"empty", ``},
		{"trailing", "{}\n{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			if err := os.MkdirAll(storeDir(s), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(storeFile(s), []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := s.Get("a")
			if !errors.Is(err, config.ErrCorrupt) {
				t.Errorf("Get: err = %v, want wrapping ErrCorrupt", err)
			}
			if err := s.Set("a", "b"); !errors.Is(err, config.ErrCorrupt) {
				t.Errorf("Set: err = %v, want wrapping ErrCorrupt", err)
			}
			if err := s.Del("a"); !errors.Is(err, config.ErrCorrupt) {
				t.Errorf("Del: err = %v, want wrapping ErrCorrupt", err)
			}
			if _, err := s.List(); !errors.Is(err, config.ErrCorrupt) {
				t.Errorf("List: err = %v, want wrapping ErrCorrupt", err)
			}

			after, err := os.ReadFile(storeFile(s))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != tc.data {
				t.Errorf("file changed: %q -> %q", tc.data, after)
			}
		})
	}
}

func newStore(t *testing.T) config.Store {
	t.Helper()
	return config.Store{Root: t.TempDir()}
}

func storeDir(s config.Store) string {
	return filepath.Join(s.Root, filepath.FromSlash(strings.TrimPrefix(config.Dir, "/")))
}

func storeFile(s config.Store) string {
	return filepath.Join(storeDir(s), config.FileName)
}

func checkMethod(t *testing.T, recv reflect.Type, name string, in, out []reflect.Type) {
	t.Helper()
	m, ok := recv.MethodByName(name)
	if !ok {
		t.Fatalf("missing method %s", name)
	}
	mt := m.Type
	if mt.NumIn() != 1+len(in) {
		t.Errorf("%s has %d parameters, want %d", name, mt.NumIn()-1, len(in))
		return
	}
	for i, want := range in {
		if got := mt.In(i + 1); got != want {
			t.Errorf("%s parameter %d is %s, want %s", name, i, got, want)
		}
	}
	if mt.NumOut() != len(out) {
		t.Errorf("%s has %d results, want %d", name, mt.NumOut(), len(out))
		return
	}
	for i, want := range out {
		if got := mt.Out(i); got != want {
			t.Errorf("%s result %d is %s, want %s", name, i, got, want)
		}
	}
}
