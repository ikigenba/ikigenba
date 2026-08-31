package cli_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
	"github.com/ikigenba/ikigenba/oauth/internal/callback"
	"github.com/ikigenba/ikigenba/oauth/internal/cli"
)

// R-E5L7-OSOC
func TestRunReturnsInProcess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"--example"}, &stdout, &stderr, cli.Deps{})

	if code != 0 {
		t.Fatalf("Run returned exit code %d, want 0 for the phase scaffold", code)
	}
}

// R-E6T4-2KF1
func TestDepsExactFields(t *testing.T) {
	var launcher browser.Launcher
	var entropy io.Reader
	var client *http.Client
	var listen callback.ListenFunc
	_ = cli.Deps{
		Launcher:   launcher,
		Entropy:    entropy,
		HTTPClient: client,
		Listen:     listen,
	}

	type expectedField struct {
		name string
		typ  reflect.Type
	}
	want := []expectedField{
		{"Launcher", reflect.TypeOf((*browser.Launcher)(nil)).Elem()},
		{"Entropy", reflect.TypeOf((*io.Reader)(nil)).Elem()},
		{"HTTPClient", reflect.TypeOf((*http.Client)(nil))},
		{"Listen", reflect.TypeOf((*callback.ListenFunc)(nil)).Elem()},
	}

	typ := reflect.TypeOf(cli.Deps{})
	if typ.NumField() != len(want) {
		t.Fatalf("Deps has %d fields, want exactly %d", typ.NumField(), len(want))
	}
	for i, expected := range want {
		field := typ.Field(i)
		if field.Name != expected.name {
			t.Errorf("Deps field %d is named %q, want %q", i, field.Name, expected.name)
		}
		if field.Type != expected.typ {
			t.Errorf("Deps.%s has type %v, want %v", field.Name, field.Type, expected.typ)
		}
	}
}
