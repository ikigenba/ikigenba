package checkout_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestFoundationPublicShapes(t *testing.T) {
	t.Parallel()

	// R-VGLU-EXV4
	assertFields(t, checkout.Checkout{}, []field{{"Root", reflect.TypeFor[string]()}, {"Deps", reflect.TypeFor[seam.Deps]()}})
	assertSignature[func(context.Context, seam.Deps) (*checkout.Checkout, error)](checkout.Open)
	// R-VLHF-Y0TW
	assertFields(t, checkout.App{}, []field{{"Name", reflect.TypeFor[string]()}, {"Dir", reflect.TypeFor[string]()}, {"Manifest", reflect.TypeFor[checkout.Manifest]()}})
	// R-CM5K-S43M
	assertFields(t, checkout.Manifest{}, []field{{"App", reflect.TypeFor[string]()}, {"Secrets", reflect.TypeFor[[]string]()}})
	assertStructTags(t, checkout.Manifest{}, []string{`toml:"app"`, `toml:"secrets"`})
	// R-VNX8-PKBA
	assertSignature[func(io.Reader) (checkout.Manifest, error)](checkout.DecodeManifest)
	// R-VQD1-H3SO
	assertFields(t, checkout.NotInCheckoutError{}, []field{{"Dir", reflect.TypeFor[string]()}})
	assertFields(t, checkout.NoAppError{}, []field{{"Name", reflect.TypeFor[string]()}})
	// R-VRKX-UVJD
	assertFields(t, checkout.ManifestError{}, []field{{"App", reflect.TypeFor[string]()}, {"Detail", reflect.TypeFor[string]()}, {"Err", reflect.TypeFor[error]()}})
	// R-VSSU-8NA2
	assertFields(t, checkout.GitError{}, []field{{"Args", reflect.TypeFor[[]string]()}, {"ExitCode", reflect.TypeFor[int]()}, {"Stderr", reflect.TypeFor[string]()}})
	// R-CJPS-0KM8
	if checkout.ManifestFile != "etc/manifest.toml" {
		t.Fatalf("ManifestFile = %q", checkout.ManifestFile)
	}

	// R-VHTQ-SPLT
	assertSignature[func(*checkout.Checkout, ...string) string]((*checkout.Checkout).Path)
	assertSignature[func(*checkout.Checkout) ([]checkout.App, error)]((*checkout.Checkout).Apps)
	assertSignature[func(*checkout.Checkout, string) (checkout.App, error)]((*checkout.Checkout).App)

	// R-CIHV-MSVJ
	assertSignature[func(*checkout.Checkout, context.Context) (string, error)]((*checkout.Checkout).Head)
	assertSignature[func(*checkout.Checkout, context.Context) (bool, error)]((*checkout.Checkout).Clean)
	assertSignature[func(*checkout.Checkout, context.Context) ([]string, error)]((*checkout.Checkout).TagsAtHead)
}

func TestFoundationErrors(t *testing.T) {
	t.Parallel()

	// R-VQD1-H3SO
	if got := (&checkout.NotInCheckoutError{Dir: "/tmp/work"}).Error(); got != "'/tmp/work' is not inside a git checkout" {
		t.Errorf("NotInCheckoutError.Error() = %q", got)
	}
	if got := (&checkout.NoAppError{Name: "crm"}).Error(); got != "no app 'crm' in the checkout" {
		t.Errorf("NoAppError.Error() = %q", got)
	}

	cause := &manifestCause{}
	manifestErr := &checkout.ManifestError{App: "crm", Detail: "cannot decode", Err: cause}
	// R-VRKX-UVJD
	if got := manifestErr.Error(); got != "crm: etc/manifest.toml: cannot decode" {
		t.Errorf("ManifestError.Error() = %q", got)
	}
	if got := manifestErr.Unwrap(); reflect.TypeOf(got) != reflect.TypeOf(cause) || reflect.ValueOf(got).Pointer() != reflect.ValueOf(cause).Pointer() {
		t.Errorf("ManifestError.Unwrap() = %#v, want exact Err %#v", got, cause)
	}
	if !errors.Is(manifestErr, cause) {
		t.Error("ManifestError does not unwrap its cause")
	}

	// R-VSSU-8NA2
	gitErr := &checkout.GitError{Args: []string{"tag", "--points-at", "HEAD"}, ExitCode: 7, Stderr: "failure"}
	if got := gitErr.Error(); got != "git tag --points-at HEAD: exit status 7" {
		t.Errorf("GitError.Error() = %q", got)
	}
}

func TestOpen(t *testing.T) {
	t.Parallel()

	wantDeps := seam.Deps{Dir: "/work/subdir"}
	var commands []seam.Cmd
	wantDeps.Exec = func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		commands = append(commands, cmd)
		return seam.Result{Stdout: []byte("/work/repo\n\n")}, nil
	}

	// R-VGLU-EXV4 R-VV8N-06RG
	got, err := checkout.Open(context.Background(), wantDeps)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got.Root != "/work/repo" {
		t.Errorf("Open().Root = %q", got.Root)
	}
	if got.Deps.Dir != wantDeps.Dir || reflect.ValueOf(got.Deps.Exec).Pointer() != reflect.ValueOf(wantDeps.Exec).Pointer() {
		t.Error("Open().Deps differs from supplied deps")
	}
	wantCommands := []seam.Cmd{{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: wantDeps.Dir}}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Errorf("commands = %#v, want %#v", commands, wantCommands)
	}
}

func TestOpenFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result seam.Result
		err    error
	}{
		{name: "nonzero", result: seam.Result{Stdout: []byte("/work/repo\n"), ExitCode: 128}},
		{name: "empty", result: seam.Result{Stdout: []byte("\r\n")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := seam.Deps{Dir: "/work", Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				return test.result, test.err
			}}
			// R-VWGJ-DYI5
			got, err := checkout.Open(context.Background(), deps)
			if got != nil {
				t.Errorf("Open() = %#v, want nil", got)
			}
			var notInCheckout *checkout.NotInCheckoutError
			if !errors.As(err, &notInCheckout) || notInCheckout.Dir != deps.Dir {
				t.Errorf("Open() error = %#v", err)
			}
		})
	}

	runnerErr := errors.New("cannot start")
	deps := seam.Deps{Dir: "/work", Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		return seam.Result{}, runnerErr
	}}
	// R-VWGJ-DYI5
	got, err := checkout.Open(context.Background(), deps)
	if got != nil || !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "git rev-parse --show-toplevel") {
		t.Errorf("Open() = %#v, %v", got, err)
	}
}

func TestPath(t *testing.T) {
	t.Parallel()

	opened := &checkout.Checkout{Root: filepath.Join("root", "checkout")}
	// R-VYWC-5HZJ
	if got := opened.Path(); got != opened.Root {
		t.Errorf("Path() = %q, want %q", got, opened.Root)
	}
	want := filepath.Join(opened.Root, "crm", "etc", "manifest.toml")
	if got := opened.Path("crm", ".", "etc", "tmp", "..", "manifest.toml"); got != want {
		t.Errorf("Path(elements) = %q, want %q", got, want)
	}
}

func TestDecodeManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		doc     string
		want    checkout.Manifest
		wantErr bool
	}{
		{name: "minimal", doc: "app = 'crm'", want: checkout.Manifest{App: "crm", Secrets: []string{}}},
		{name: "secrets", doc: "app = 'crm'\nsecrets = ['CRM_KEY', 'MAP_KEY']", want: checkout.Manifest{App: "crm", Secrets: []string{"CRM_KEY", "MAP_KEY"}}},
		{name: "opaque fields", doc: "app = 'crm'\nport = 8080\ndefault = true\n[env]\nMODE = 'prod'\n[database]\nengine = 'postgres'", want: checkout.Manifest{App: "crm", Secrets: []string{}}},
		{name: "invalid toml", doc: "app = [", wantErr: true},
		{name: "absent app", doc: "secrets = []", wantErr: true},
		{name: "empty app", doc: "app = ''", wantErr: true},
		{name: "non-string app", doc: "app = 2", wantErr: true},
		{name: "non-array secrets", doc: "app = 'crm'\nsecrets = 'KEY'", wantErr: true},
		{name: "non-string secret", doc: "app = 'crm'\nsecrets = [1]", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// R-VNX8-PKBA R-CNDH-5VUB R-COLD-JNL0
			got, err := checkout.DecodeManifest(strings.NewReader(test.doc))
			if test.wantErr {
				if err == nil {
					t.Fatalf("DecodeManifest() = %#v, nil", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeManifest() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("DecodeManifest() = %#v, want %#v", got, test.want)
			}
		})
	}
}

type field struct {
	name   string
	typeOf reflect.Type
}

type manifestCause struct{}

func (*manifestCause) Error() string { return "bad document" }

func assertSignature[T any](T) {}

func assertFields(t *testing.T, value any, want []field) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typeOf, typeOf.NumField(), len(want))
	}
	for index, wantField := range want {
		got := typeOf.Field(index)
		if got.Name != wantField.name || got.Type != wantField.typeOf {
			t.Errorf("%s field %d = %s %s, want %s %s", typeOf, index, got.Name, got.Type, wantField.name, wantField.typeOf)
		}
	}
}

func assertStructTags(t *testing.T, value any, want []string) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d tags", typeOf, typeOf.NumField(), len(want))
	}
	for index, wantTag := range want {
		if got := string(typeOf.Field(index).Tag); got != wantTag {
			t.Errorf("%s field %d tag = %q, want %q", typeOf, index, got, wantTag)
		}
	}
}
