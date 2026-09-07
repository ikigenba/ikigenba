package model_test

import (
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/model"
)

func TestExactPackageSurface(t *testing.T) {
	// R-I3FK-O2QD
	assertFields(t, reflect.TypeFor[model.Config](), []field{
		{"Provider", reflect.TypeFor[string]()},
		{"Model", reflect.TypeFor[string]()},
		{"Wire", reflect.TypeFor[string]()},
		{"Auth", reflect.TypeFor[string]()},
		{"AuthFile", reflect.TypeFor[string]()},
		{"BaseURL", reflect.TypeFor[string]()},
		{"MaxContext", reflect.TypeFor[int64]()},
		{"Settings", reflect.TypeFor[map[string]string]()},
		{"Home", reflect.TypeFor[string]()},
		{"Getenv", reflect.TypeFor[func(string) string]()},
	})
	assertFields(t, reflect.TypeFor[model.Plan](), []field{
		{"Offering", reflect.TypeFor[agentkit.Offering]()},
		{"Model", reflect.TypeFor[string]()},
		{"AuthMode", reflect.TypeFor[agentkit.AuthMode]()},
		{"EnvVar", reflect.TypeFor[string]()},
		{"AuthFile", reflect.TypeFor[string]()},
		{"BaseURL", reflect.TypeFor[string]()},
		{"MaxContext", reflect.TypeFor[int64]()},
	})

	factory := reflect.TypeFor[model.Factory]()
	for i := range factory.NumField() {
		if factory.Field(i).IsExported() {
			t.Errorf("Factory field %q is exported", factory.Field(i).Name)
		}
	}
}

func TestExactFunctionAndMethodSignatures(t *testing.T) {
	// R-I4NH-1UH2
	signatures := []struct {
		name string
		got  reflect.Type
		want reflect.Type
	}{
		{"Resolve", reflect.TypeOf(model.Resolve), reflect.TypeFor[func(model.Config) (model.Plan, error)]()},
		{"Open", reflect.TypeOf(model.Open), reflect.TypeFor[func(model.Config) (*model.Factory, error)]()},
		{"Plan", reflect.TypeOf((*model.Factory).Plan), reflect.TypeFor[func(*model.Factory) model.Plan]()},
		{"New", reflect.TypeOf((*model.Factory).New), reflect.TypeFor[func(*model.Factory, []agentkit.Tool, *agentkit.Log) (*agentkit.Conversation, error)]()},
	}
	for _, signature := range signatures {
		if signature.got != signature.want {
			t.Errorf("%s signature = %s, want %s", signature.name, signature.got, signature.want)
		}
	}

	cfg := catalogConfig(t)
	factory, err := model.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	resolved, err := model.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !reflect.DeepEqual(factory.Plan(), resolved) {
		t.Errorf("Factory.Plan() = %#v, want %#v", factory.Plan(), resolved)
	}
}

type field struct {
	name string
	typ  reflect.Type
}

func assertFields(t *testing.T, typ reflect.Type, want []field) {
	t.Helper()
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ, typ.NumField(), len(want))
	}
	for i, expected := range want {
		got := typ.Field(i)
		if got.Name != expected.name || got.Type != expected.typ {
			t.Errorf("%s field %d = %s %s, want %s %s", typ, i, got.Name, got.Type, expected.name, expected.typ)
		}
	}
}
