package apps_test

import (
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
