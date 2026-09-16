package apps_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

func TestServiceModelRetainsManifestDatabase(t *testing.T) {
	database := &apps.Database{Engine: "sqlite", Path: "state/app.db"}
	manifest := &apps.Manifest{App: "notes", Database: database}
	service := apps.Service{Name: "notes", Manifest: manifest}

	if service.Manifest.Database != database {
		t.Fatal("service model did not retain its manifest database")
	}
	if err := apps.ValidateName(service.Name); err != nil {
		t.Fatalf("name validation rejected service name: %v", err)
	}
}

// R-XNK8-RUH4
func TestDatabaseHasExactFields(t *testing.T) {
	assertExactFields(t, reflect.TypeFor[apps.Database](), []field{
		{name: "Engine", typ: reflect.TypeFor[string]()},
		{name: "Path", typ: reflect.TypeFor[string]()},
	})
}

// R-XOS5-5M7T
func TestManifestHasExactFields(t *testing.T) {
	assertExactFields(t, reflect.TypeFor[apps.Manifest](), []field{
		{name: "App", typ: reflect.TypeFor[string]()},
		{name: "Port", typ: reflect.TypeFor[int]()},
		{name: "Default", typ: reflect.TypeFor[bool]()},
		{name: "Secrets", typ: reflect.TypeFor[[]string]()},
		{name: "Env", typ: reflect.TypeFor[map[string]string]()},
		{name: "Database", typ: reflect.TypeFor[*apps.Database]()},
	})
}

// R-XQ01-JDYI
func TestServiceHasExactFields(t *testing.T) {
	assertExactFields(t, reflect.TypeFor[apps.Service](), []field{
		{name: "Name", typ: reflect.TypeFor[string]()},
		{name: "Manifest", typ: reflect.TypeFor[*apps.Manifest]()},
		{name: "ManifestError", typ: reflect.TypeFor[error]()},
	})
}

// R-A2TS-K4M0
func TestValidateName(t *testing.T) {
	valid := []string{
		"a",
		"0",
		"App-01",
		strings.Repeat("a", 63),
	}
	for _, name := range valid {
		if err := apps.ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) returned %v", name, err)
		}
	}

	invalid := []string{
		"",
		strings.Repeat("a", 64),
		"-app",
		"app-",
		"app_name",
		"app.name",
		"app/name",
		"café",
		"host",
		"HOST",
		"deploy",
		"backup-host",
		"BACKUP-SERVICES",
		"renew-certificate",
	}
	for _, name := range invalid {
		err := apps.ValidateName(name)
		if err == nil {
			t.Errorf("ValidateName(%q) returned nil", name)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("ValidateName(%q) error %q does not identify the name", name, err)
		}
	}
}

// R-XSFU-AXFW
func TestParseManifestRejectsMalformedTOMLWithoutPartialResult(t *testing.T) {
	malformed := []string{
		"app = \"notes\"\nport = [3100\n",
		"value = 1-",
		"other = i_n_f",
		"other = n_a_n",
		"[metadata]\n[metadata]\n",
		"answer = 1\nanswer = 2\n",
		"thing = { key = 1, key = 2 }",
		"x = { a = {}, a.b = 1 }",
		"x = { a = { b = 1 }, a.c = 2 }",
		"x = { a = { b = {} , b.c = 1 } }",
		"app = \"notes\"\rport = 3100",
		"# comment\rapp = \"notes\"",
		"values = [1,\r2]",
		"value = \"\"\"line\rbreak\"\"\"",
		"default = true\v",
		"default = true\f",
		"default = true\u00a0",
		"# comment\v",
		"app = \"notes\" # comment\f",
		"values = [1, # comment\x7f\n2]",
		"[a.b]\nx = 1\n[[a]]\ny = 2",
		"[a.b]\nx = 1\n[[a.b]]\ny = 2",
		"[[a.b]]\nx = 1\n[[a]]\ny = 2",
		"[a.b]\n[a]\nb = 2",
		"[a.b.c]\n[a]\nb = {}",
		"schedule = 1979-05-27T07:32:00,1Z",
		"schedule = 1979-05-27T07:32:00,1-07:00",
		"schedule = 1979-05-27T07:32:00,1",
		"schedule = 07:32:00,1",
		"[[tab.arr]]\n[tab]\narr.val1=1",
		"[a.b.c]\nz=9\n[a]\nb.c.t=\"x\"",
		"[a.b.c.d]\nz=9\n[a]\nb.c.d.k.t=\"x\"",
		"[[a.b]]\n[a]\nb.y=2",
		"[a.b.c]\nz=9\n[[totally_unrelated]]\nx=123\n[a]\nb.c.t=\"x\"",
		"schedule = 1985-06-18 17:04:07+12:60",
		"schedule = 2023-10-01T1:32:00Z",
		"schedule = 2023-10-01 1:32:00",
		"schedule = 1:32:00",
		"schedule = 2023-10-01T01:32:00+1:00",
		"schedule = 2023-10-01T01:32:00+01:0",
		"schedule = 2023-10-01T01:32:00+24:00",
	}
	for _, data := range malformed {
		manifest, err := apps.ParseManifest([]byte(data))
		if err == nil {
			t.Errorf("ParseManifest(%q) accepted malformed TOML", data)
		}
		if !reflect.DeepEqual(manifest, apps.Manifest{}) {
			t.Errorf("failed ParseManifest returned partial model: %#v", manifest)
		}
	}

	stringForms := []struct {
		name   string
		prefix string
		suffix string
	}{
		{name: "basic", prefix: `other = "a`, suffix: `b"`},
		{name: "literal", prefix: `other = 'a`, suffix: `b'`},
		{name: "multiline basic", prefix: `other = """a`, suffix: `b"""`},
		{name: "multiline literal", prefix: `other = '''a`, suffix: `b'''`},
	}
	for _, form := range stringForms {
		t.Run(form.name+" rejects raw DEL", func(t *testing.T) {
			data := []byte(form.prefix + "\x7f" + form.suffix)
			manifest, err := apps.ParseManifest(data)
			if err == nil {
				t.Fatalf("ParseManifest(%q) accepted raw DEL in a string", data)
			}
			if !reflect.DeepEqual(manifest, apps.Manifest{}) {
				t.Fatalf("failed ParseManifest returned partial model: %#v", manifest)
			}
		})
	}

	for control := byte(0); control < 0x20; control++ {
		if control == '\t' || control == '\n' || control == '\r' {
			continue
		}
		for _, form := range stringForms {
			data := append([]byte(form.prefix), control)
			data = append(data, form.suffix...)
			if manifest, err := apps.ParseManifest(data); err == nil {
				t.Errorf("ParseManifest(%q) accepted raw control 0x%02x in %s string with %#v", data, control, form.name, manifest)
			}
		}
	}
	for _, form := range stringForms[:2] {
		for _, lineEnding := range []string{"\n", "\r\n"} {
			data := []byte(form.prefix + lineEnding + form.suffix)
			if manifest, err := apps.ParseManifest(data); err == nil {
				t.Errorf("ParseManifest(%q) accepted a line ending in %s string with %#v", data, form.name, manifest)
			}
		}
	}
}

// R-XTNQ-OP6L
func TestParseManifestMapsFieldsAndSuppliesEmptyCollections(t *testing.T) {
	data := []byte(`app = "crm"
port = 3100
default = true
secrets = ["CRM_API_KEY", "CRM_API_SECRET"]

[env]
OUTBOX_RETENTION_DAYS = "7"

[database]
engine = "sqlite"
path = "state/crm.db"
`)
	manifest, err := apps.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}
	want := apps.Manifest{
		App:     "crm",
		Port:    3100,
		Default: true,
		Secrets: []string{"CRM_API_KEY", "CRM_API_SECRET"},
		Env:     map[string]string{"OUTBOX_RETENTION_DAYS": "7"},
		Database: &apps.Database{
			Engine: "sqlite",
			Path:   "state/crm.db",
		},
	}
	if !reflect.DeepEqual(manifest, want) {
		t.Fatalf("ParseManifest result = %#v, want %#v", manifest, want)
	}

	minimal, err := apps.ParseManifest(nil)
	if err != nil {
		t.Fatalf("ParseManifest(empty) returned error: %v", err)
	}
	if minimal.App != "" || minimal.Port != 0 || minimal.Default || minimal.Database != nil {
		t.Fatalf("ParseManifest(empty) did not retain scalar zero values: %#v", minimal)
	}
	if minimal.Secrets == nil || len(minimal.Secrets) != 0 || minimal.Env == nil || len(minimal.Env) != 0 {
		t.Fatalf("ParseManifest(empty) collections are not usable and empty: %#v", minimal)
	}

	equivalent := []byte(`"app" = '''crm'''
port = 0xC1C
default = true
secrets = ["""CRM_API_KEY""", '''CRM_API_SECRET''']
env = { OUTBOX_RETENTION_DAYS = """7""" }
database.engine = '''sqlite'''
database.path = """
state/crm.db"""
`)
	decoded, err := apps.ParseManifest(equivalent)
	if err != nil {
		t.Fatalf("ParseManifest(equivalent forms) returned error: %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("ParseManifest(equivalent forms) = %#v, want %#v", decoded, want)
	}
	crlf := []byte("app = \"crm\"\r\nport = 3100\r\n")
	decoded, err = apps.ParseManifest(crlf)
	if err != nil || decoded.App != "crm" || decoded.Port != 3100 {
		t.Fatalf("ParseManifest(CRLF document) = %#v, %v", decoded, err)
	}
	continued := []byte("app = \"\"\"c\\ \t\n  r\\\r\n\t m\"\"\"")
	decoded, err = apps.ParseManifest(continued)
	if err != nil || decoded.App != "crm" {
		t.Fatalf("ParseManifest(multiline continuation) = %#v, %v", decoded, err)
	}
	for _, data := range []string{
		"# tab\tcomment\napp = \"crm\"",
		"app = \"crm\" # Unicode snowman ☃",
		"secrets = [\n  # Unicode key 鍵\n  \"TOKEN\",\n]",
		"other = \"tab\tand Unicode \u0080☃\"",
		"other = 'tab\tand Unicode \u0080☃'",
		"other = \"\"\"line one\nline two \u0080☃\"\"\"",
		"other = '''line one\r\nline two \u0080☃'''",
		"other = [1, 0x2, 0o3, 0b100]",
		"other = [1.0, 2e0, inf, nan]",
		`other = [[1], ["two"], []]`,
		`other = [{ one = 1 }, { two = "two" }, {}]`,
	} {
		decoded, err = apps.ParseManifest([]byte(data))
		if err != nil {
			t.Errorf("ParseManifest(%q) rejected allowed text: %v", data, err)
		}
	}
	for _, data := range []string{
		`other = [1, "two"]`,
		`other = [true, 1]`,
		`other = [1, 1.0]`,
		`other = [1979-05-27, 07:32:00]`,
		`other = [[1], { value = 1 }]`,
		`other = [{ value = 1 }, [1]]`,
	} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err != nil {
			t.Errorf("ParseManifest(%q) rejected mixed TOML array: %v", data, err)
		}
		if !reflect.DeepEqual(manifest, minimal) {
			t.Errorf("ParseManifest(%q) retained unrelated mixed array: %#v", data, manifest)
		}
	}

	for data, wantValue := range map[string]string{
		"other = \"\"\"abc\"\"\"\"":   "abc\"",
		"other = \"\"\"abc\"\"\"\"\"": "abc\"\"",
	} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err != nil {
			t.Errorf("ParseManifest(%q), whose multiline value ends %q, returned error: %v", data, wantValue, err)
		}
		if !reflect.DeepEqual(manifest, minimal) {
			t.Errorf("ParseManifest(%q) retained unrelated field: %#v", data, manifest)
		}
	}

	for _, data := range []string{
		"schedule = 1979-05-27t07:32:00z",
		"schedule = 1979-05-27t07:32:00.1z",
		"schedule = 1979-05-27T07:32:00.1Z",
		"schedule = 1979-05-27T07:32:00.1-07:00",
		"schedule = 1979-05-27T07:32:00.1",
		"schedule = 07:32:00.1",
		"schedule = 1979-05-27t07:32:00-07:00",
		"schedule = 1979-05-27 07:32:00z",
		"schedule = 1979-05-27t07:32:00",
		"schedule = 1979-05-27T07:32:00+23:59",
		"schedule = 1979-05-27T07:32:00-23:59",
		"schedule = 00:00:00",
		"schedule = 23:59:59.999999999",
	} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err != nil {
			t.Errorf("ParseManifest(%q) rejected valid TOML datetime: %v", data, err)
		}
		if !reflect.DeepEqual(manifest, minimal) {
			t.Errorf("ParseManifest(%q) retained unrelated datetime: %#v", data, manifest)
		}
	}

	for _, data := range []string{
		"[a.b]\nx = 1\n[a]\ny = 2",
		"[a.b.c]\nx = 1\n[a.b]\ny = 2\n[a]\nz = 3",
		"[[a]]\nx = 1\n[a.b]\ny = 2",
		"[a]\nb.c = 1\nb.d = 2",
		"[a.b]\nx = 1\ny = 2",
	} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err != nil {
			t.Errorf("ParseManifest(%q) rejected valid implicit parent definition: %v", data, err)
		}
		if !reflect.DeepEqual(manifest, minimal) {
			t.Errorf("ParseManifest(%q) retained unrelated table: %#v", data, manifest)
		}
	}

	for _, data := range []string{
		"a.b = 1\n\"a\\u0000b\" = 2",
		"a.b.c = 1\n\"a\\u0000b\".c = 2\na.\"b\\u0000c\" = 3\n\"a\\u0000b\\u0000c\" = 4",
		"[a.b]\nx = 1\n[\"a\\u0000b\"]\nx = 2",
		"[a.b.c]\nx = 1\n[\"a\\u0000b\".c]\nx = 2\n[a.\"b\\u0000c\"]\nx = 3\n[\"a\\u0000b\\u0000c\"]\nx = 4",
		"[[a.b]]\nx = 1\n[[a.b]]\nx = 2\n[\"a\\u0000b\"]\nx = 3",
		"[\"a\\u0000b\"]\nx = 1\n[[a.b]]\nx = 2\n[[a.b]]\nx = 3",
	} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err != nil {
			t.Errorf("ParseManifest(%q) confused distinct key paths: %v", data, err)
		}
		if !reflect.DeepEqual(manifest, minimal) {
			t.Errorf("ParseManifest(%q) retained unrelated collision test data: %#v", data, manifest)
		}
	}
}

// R-XUVN-2GXA
func TestParseManifestValidatesRecognizedFieldsAndIgnoresOthers(t *testing.T) {
	valid := []byte(`title = """unrelated
title"""
summary = '''literal
summary'''
count = 3.5
schedule = 1979-05-27T07:32:00Z
details = { nested.value = 1, nested.other = 2 }
[[widgets]]
name = "first"
[widgets.meta]
x = 1
[[widgets]]
name = "second"
[widgets.meta]
x = 2
[metadata]
enabled = true
app = "host"
port = 70000
default = "yes"
secrets = [1]
env = { MODE = 2 }
database = { engine = "postgres", path = "/tmp/wrong" }
[env]
MODE = "production"
`)
	manifest, err := apps.ParseManifest(valid)
	if err != nil {
		t.Fatalf("ParseManifest rejected capability-only manifest: %v", err)
	}
	if manifest.App != "" || manifest.Port != 0 || manifest.Default || len(manifest.Secrets) != 0 || manifest.Database != nil || !reflect.DeepEqual(manifest.Env, map[string]string{"MODE": "production"}) {
		t.Fatalf("unexpected model from unrelated fields: %#v", manifest)
	}
	quotedDot, err := apps.ParseManifest([]byte("[env]\n\"foo.bar\" = \"x\""))
	if err != nil || !reflect.DeepEqual(quotedDot.Env, map[string]string{"foo.bar": "x"}) {
		t.Fatalf("ParseManifest(direct quoted env key) = %#v, %v", quotedDot, err)
	}
	for _, port := range []int{1, 65535} {
		manifest, err := apps.ParseManifest([]byte(fmt.Sprintf("port = %d", port)))
		if err != nil || manifest.Port != port {
			t.Errorf("ParseManifest accepted-port %d = %#v, %v", port, manifest, err)
		}
	}
	for _, root := range []string{"app", "port", "default", "secrets"} {
		for _, format := range []string{"[%s]", "[[%s]]", "%s.child = \"x\"", "%s.child.value = 1"} {
			data := fmt.Sprintf(format, root)
			manifest, err := apps.ParseManifest([]byte(data))
			if err == nil {
				t.Errorf("ParseManifest(%q) accepted recognized scalar root as a table", data)
			}
			if !reflect.DeepEqual(manifest, apps.Manifest{}) {
				t.Errorf("failed ParseManifest(%q) returned partial model: %#v", data, manifest)
			}
		}
	}
	for _, data := range []string{"[[env]]", "[[database]]"} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err == nil {
			t.Errorf("ParseManifest(%q) accepted recognized table root as an array table", data)
		}
		if !reflect.DeepEqual(manifest, apps.Manifest{}) {
			t.Errorf("failed ParseManifest(%q) returned partial model: %#v", data, manifest)
		}
	}
	dotted, err := apps.ParseManifest([]byte("env.MODE = \"production\"\ndatabase.engine = \"sqlite\"\ndatabase.path = \"state/app.db\""))
	if err != nil || !reflect.DeepEqual(dotted.Env, map[string]string{"MODE": "production"}) || !reflect.DeepEqual(dotted.Database, &apps.Database{Engine: "sqlite", Path: "state/app.db"}) {
		t.Errorf("ParseManifest(valid dotted table fields) = %#v, %v", dotted, err)
	}

	tests := []struct {
		name string
		data string
	}{
		{name: "app type", data: "app = 3"},
		{name: "app value", data: "app = \"host\""},
		{name: "port type", data: "port = \"3100\""},
		{name: "port low", data: "port = 0"},
		{name: "port high", data: "port = 65536"},
		{name: "default", data: "default = \"true\""},
		{name: "secrets", data: "secrets = [\"TOKEN\", 2]"},
		{name: "env", data: "[env]\nWORKERS = 2"},
		{name: "env child table", data: "[env.child]\nVALUE = \"x\""},
		{name: "env empty child table", data: "[env.child]"},
		{name: "env dotted key", data: "[env]\nfoo.bar = \"x\""},
		{name: "env dotted root key", data: "env.foo.bar = \"x\""},
		{name: "env dotted inline value", data: "env = { foo.bar = \"x\" }"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest, err := apps.ParseManifest([]byte(test.data))
			if err == nil {
				t.Fatalf("ParseManifest(%q) succeeded with %#v", test.data, manifest)
			}
			if !reflect.DeepEqual(manifest, apps.Manifest{}) {
				t.Fatalf("failed ParseManifest returned partial model: %#v", manifest)
			}
		})
	}
}

// R-XW3J-G8NZ
func TestParseManifestValidatesSQLiteDatabaseDeclaration(t *testing.T) {
	valid := []string{
		"[database]\nengine = \"sqlite\"\npath = \"state/app.db\"",
		"[database]\nengine = \"sqlite\"\npath = \"state/nested/app.db\"",
		"database = { engine = \"sqlite\", path = \"state/inline.db\" }",
	}
	for index, data := range valid {
		manifest, err := apps.ParseManifest([]byte(data))
		if err != nil {
			t.Errorf("ParseManifest(%q) returned error: %v", data, err)
			continue
		}
		wantPath := []string{"state/app.db", "state/nested/app.db", "state/inline.db"}[index]
		if manifest.Database == nil || manifest.Database.Engine != "sqlite" || manifest.Database.Path != wantPath {
			t.Errorf("ParseManifest(%q) database = %#v", data, manifest.Database)
		}
	}

	invalid := []string{
		"[database]",
		"[database]\nengine = \"postgres\"\npath = \"state/app.db\"",
		"[database]\nengine = \"sqlite\"",
		"[database]\nengine = \"sqlite\"\npath = \"\"",
		"[database]\nengine = \"sqlite\"\npath = \"/state/app.db\"",
		"[database]\nengine = \"sqlite\"\npath = \"state\"",
		"[database]\nengine = \"sqlite\"\npath = \"state/\"",
		"[database]\nengine = \"sqlite\"\npath = \"state//app.db\"",
		"[database]\nengine = \"sqlite\"\npath = \"state/./app.db\"",
		"[database]\nengine = \"sqlite\"\npath = \"state/../app.db\"",
		"[database]\nengine = \"sqlite\"\npath = \"state/\\u0000\"",
		"[database]\nengine = \"sqlite\"\npath = \"state/app\\u0000.db\"",
		"[database]\nengine = 1\npath = \"state/app.db\"",
		"[database]\nengine = \"sqlite\"\npath = 1",
	}
	for _, data := range invalid {
		if manifest, err := apps.ParseManifest([]byte(data)); err == nil {
			t.Errorf("ParseManifest(%q) succeeded with %#v", data, manifest)
		}
	}
	for _, data := range []string{
		"[database.child]",
		"[database.child]\nengine = \"sqlite\"\npath = \"state/app.db\"",
		"database.child.value = 1",
		"database.child = { value = 1 }",
		"database = { child.value = 1 }",
	} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err == nil || !strings.Contains(err.Error(), "database.engine") {
			t.Errorf("ParseManifest(%q) = %#v, %v; want missing direct database engine", data, manifest, err)
		}
		if !reflect.DeepEqual(manifest, apps.Manifest{}) {
			t.Errorf("failed ParseManifest returned partial model: %#v", manifest)
		}
	}

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0o750); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	databasePath := filepath.Join(stateDir, "app.db")
	contents := []byte("existing database contents")
	if err := os.WriteFile(databasePath, contents, 0o600); err != nil {
		t.Fatalf("create existing database: %v", err)
	}
	before := snapshotDatabaseState(t, databasePath, stateDir)
	t.Chdir(root)
	if _, err := apps.ParseManifest([]byte("[database]\nengine = \"sqlite\"\npath = \"state/app.db\"")); err != nil {
		t.Fatalf("ParseManifest with existing database returned error: %v", err)
	}
	after := snapshotDatabaseState(t, databasePath, stateDir)
	afterContents, err := os.ReadFile("state/app.db")
	if err != nil {
		t.Fatalf("read existing database after parsing: %v", err)
	}
	if !reflect.DeepEqual(afterContents, contents) || !reflect.DeepEqual(after, before) {
		t.Fatalf("ParseManifest changed existing database state")
	}
}

type databaseState struct {
	mode     uint32
	modified int64
	entries  []string
}

func snapshotDatabaseState(t *testing.T, databasePath, stateDir string) databaseState {
	t.Helper()
	info, err := os.Stat(databasePath)
	if err != nil {
		t.Fatalf("stat existing database: %v", err)
	}
	directoryEntries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state directory: %v", err)
	}
	entries := make([]string, len(directoryEntries))
	for index, entry := range directoryEntries {
		entries[index] = entry.Name()
	}
	return databaseState{
		mode:     uint32(info.Mode()),
		modified: info.ModTime().UnixNano(),
		entries:  entries,
	}
}

type field struct {
	name string
	typ  reflect.Type
}

func assertExactFields(t *testing.T, structure reflect.Type, want []field) {
	t.Helper()
	if structure.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", structure.Name(), structure.NumField(), len(want))
	}
	for index, expected := range want {
		actual := structure.Field(index)
		if actual.Name != expected.name || actual.Type != expected.typ {
			t.Errorf("%s field %d = %s %s, want %s %s", structure.Name(), index, actual.Name, actual.Type, expected.name, expected.typ)
		}
	}
}
