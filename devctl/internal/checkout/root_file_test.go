package checkout_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestRootFilePublicShapesAndErrors(t *testing.T) {
	t.Parallel()

	// R-Q2QY-ECMR
	if checkout.RootFilePath != "infra/terraform.tfvars.json" {
		t.Fatalf("RootFilePath = %q", checkout.RootFilePath)
	}
	// R-Q3YU-S4DG
	assertFields(t, checkout.RootFile{}, []field{{"Domain", reflect.TypeFor[string]()}, {"Region", reflect.TypeFor[string]()}})
	assertStructTags(t, checkout.RootFile{}, []string{`json:"domain"`, `json:"region"`})
	// R-Q56R-5W45
	assertSignature[func(context.Context, seam.Deps) (checkout.RootFile, error)](checkout.ReadRootFile)
	assertSignature[func(*checkout.Checkout) (checkout.RootFile, error)]((*checkout.Checkout).ReadRootFile)

	// R-Q6EN-JNUU
	assertFields(t, checkout.NoRootFileError{}, []field{{"Checkout", reflect.TypeFor[string]()}})
	assertFields(t, checkout.RootFileError{}, []field{{"Detail", reflect.TypeFor[string]()}})
	if got := (&checkout.NoRootFileError{Checkout: "/work/repo"}).Error(); got != "no infra/terraform.tfvars.json in the checkout" {
		t.Errorf("NoRootFileError.Error() = %q", got)
	}
	if got := (&checkout.RootFileError{Detail: "missing 'region'"}).Error(); got != "infra/terraform.tfvars.json: missing 'region'" {
		t.Errorf("RootFileError.Error() = %q", got)
	}
}

func TestCheckoutReadRootFilePathAndFilesystemFailures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var commands []seam.Cmd
	opened := &checkout.Checkout{
		Root: root,
		Deps: seam.Deps{
			Dir: filepath.Join(root, "nested", "working-directory"),
			Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
				commands = append(commands, cmd)
				return seam.Result{}, nil
			},
		},
	}

	// R-Q8UG-B7C8
	got, err := opened.ReadRootFile()
	var missing *checkout.NoRootFileError
	if got != (checkout.RootFile{}) || !errors.As(err, &missing) || reflect.TypeOf(err) != reflect.TypeOf(missing) || missing.Checkout != root {
		t.Fatalf("ReadRootFile() = %#v, %T %#v, want zero and NoRootFileError for root", got, err, err)
	}
	if want := "no infra/terraform.tfvars.json in the checkout"; err.Error() != want {
		t.Fatalf("ReadRootFile() error = %q, want %q", err, want)
	}
	if len(commands) != 0 {
		t.Fatalf("missing-file ReadRootFile commands = %#v, want none", commands)
	}

	path := filepath.Join(root, checkout.RootFilePath)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	commands = nil
	got, err = opened.ReadRootFile()
	var missingType *checkout.NoRootFileError
	var malformed *checkout.RootFileError
	if got != (checkout.RootFile{}) || err == nil || err.Error() != checkout.RootFilePath+": not a regular file" {
		t.Fatalf("ReadRootFile() = %#v, %v, want contextual filesystem error", got, err)
	}
	if errors.As(err, &missingType) || errors.As(err, &malformed) {
		t.Fatalf("filesystem error has root-file semantic type: %T %v", err, err)
	}
	if len(commands) != 0 {
		t.Fatalf("nonregular-file ReadRootFile commands = %#v, want none", commands)
	}

	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	writeRootDocument(t, root, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	commands = nil
	got, err = opened.ReadRootFile()
	if got != (checkout.RootFile{}) || err == nil || !strings.Contains(err.Error(), checkout.RootFilePath) {
		t.Fatalf("unreadable ReadRootFile() = %#v, %v, want contextual filesystem error", got, err)
	}
	if errors.As(err, &missingType) || errors.As(err, &malformed) {
		t.Fatalf("unreadable-file error has root-file semantic type: %T %v", err, err)
	}
	if len(commands) != 0 {
		t.Fatalf("unreadable-file ReadRootFile commands = %#v, want none", commands)
	}
}

func TestCheckoutReadRootFileRejectsNonObjectDocuments(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty":          "",
		"invalid":        "{",
		"array":          `[]`,
		"string":         `"value"`,
		"null":           `null`,
		"trailing value": `{"domain":"ikigenba.dev","region":"us-east-2"} true`,
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			// R-QA2C-OZ2X
			got, err := readRootDocument(t, document)
			assertRootFileError(t, got, err, "not a JSON object")
		})
	}
}

func TestCheckoutReadRootFileValidatesRequiredStringsInOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
		detail   string
	}{
		{name: "missing region", document: `{"domain":"ikigenba.dev"}`, detail: "missing 'region'"},
		{name: "missing domain first", document: `{}`, detail: "missing 'domain'"},
		{name: "domain not string", document: `{"domain":1,"region":"us-east-2"}`, detail: "'domain' is not a string"},
		{name: "region not string", document: `{"domain":"ikigenba.dev","region":null}`, detail: "'region' is not a string"},
		{name: "empty domain", document: `{"domain":"","region":"us-east-2"}`, detail: "missing 'domain'"},
		{name: "domain failure precedes region", document: `{"domain":false,"region":false}`, detail: "'domain' is not a string"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// R-QBA9-2QTM
			got, err := readRootDocument(t, test.document)
			assertRootFileError(t, got, err, test.detail)
		})
	}
}

func TestCheckoutReadRootFileReturnsValuesAndIgnoresOtherMembers(t *testing.T) {
	t.Parallel()

	// R-QCI5-GIKB
	got, err := readRootDocument(t, `{"domain":" Ikigenba.DEV ","region":" US-East-2 ","instance_type":"t4g.small","tags":{"a":1},"future_limit":1e400,"enabled":true,"optional":null,"zones":["a",2,false]}`)
	if err != nil {
		t.Fatalf("ReadRootFile() error = %v", err)
	}
	want := checkout.RootFile{Domain: " Ikigenba.DEV ", Region: " US-East-2 "}
	if got != want {
		t.Fatalf("ReadRootFile() = %#v, want %#v", got, want)
	}
}

func TestCheckoutReadRootFileAcceptsTrailingWhitespace(t *testing.T) {
	t.Parallel()

	got, err := readRootDocument(t, "{\"domain\":\"ikigenba.dev\",\"region\":\"us-east-2\"}\n \t\r")
	if err != nil {
		t.Fatalf("ReadRootFile() error = %v", err)
	}
	want := checkout.RootFile{Domain: "ikigenba.dev", Region: "us-east-2"}
	if got != want {
		t.Fatalf("ReadRootFile() = %#v, want %#v", got, want)
	}
}

func TestReadRootFileOpensOnceAndReadsFromDiscoveredRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootDocument(t, root, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	workingDir := filepath.Join(root, "nested", "working-directory")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "exact context")
	var commands []seam.Cmd
	deps := seam.Deps{Dir: workingDir, Exec: func(gotCtx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if gotCtx != ctx {
			t.Fatalf("Open context = %v, want exact supplied context", gotCtx)
		}
		commands = append(commands, cmd)
		return seam.Result{Stdout: []byte(root + "\n")}, nil
	}}

	// R-QDQ1-UAB0
	got, err := checkout.ReadRootFile(ctx, deps)
	if err != nil {
		t.Fatalf("ReadRootFile() error = %v", err)
	}
	if got != (checkout.RootFile{Domain: "ikigenba.dev", Region: "us-east-2"}) {
		t.Fatalf("ReadRootFile() = %#v", got)
	}
	wantCommands := []seam.Cmd{{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: workingDir}}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}

	commands = nil
	deps.Exec = func(gotCtx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if gotCtx != ctx {
			t.Fatalf("failed Open context = %v, want exact supplied context", gotCtx)
		}
		commands = append(commands, cmd)
		return seam.Result{ExitCode: 128}, nil
	}
	got, err = checkout.ReadRootFile(ctx, deps)
	var notInCheckout *checkout.NotInCheckoutError
	if got != (checkout.RootFile{}) || !errors.As(err, &notInCheckout) || reflect.TypeOf(err) != reflect.TypeOf(notInCheckout) || notInCheckout.Dir != workingDir {
		t.Fatalf("ReadRootFile() = %#v, %T %#v, want Open error", got, err, err)
	}
	if want := "'" + workingDir + "' is not inside a git checkout"; err.Error() != want {
		t.Fatalf("ReadRootFile() error = %q, want %q", err, want)
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("failed ReadRootFile commands = %#v, want %#v", commands, wantCommands)
	}

	commands = nil
	openFailure := errors.New("open failed")
	deps.Exec = func(gotCtx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if gotCtx != ctx {
			t.Fatalf("runner-error Open context = %v, want exact supplied context", gotCtx)
		}
		commands = append(commands, cmd)
		return seam.Result{}, openFailure
	}
	got, err = checkout.ReadRootFile(ctx, deps)
	if got != (checkout.RootFile{}) {
		t.Fatalf("ReadRootFile() = %#v, want zero", got)
	}
	if want := "git rev-parse --show-toplevel: open failed"; err == nil || err.Error() != want {
		t.Fatalf("ReadRootFile() error = %v, want %q", err, want)
	}
	if unwrapped := errors.Unwrap(err); !errors.Is(unwrapped, openFailure) || errors.Unwrap(unwrapped) != nil {
		t.Fatalf("ReadRootFile() error chain = %v, want Open's wrapper directly around identical runner error", err)
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("runner-error ReadRootFile commands = %#v, want %#v", commands, wantCommands)
	}
}

func readRootDocument(t *testing.T, document string) (checkout.RootFile, error) {
	t.Helper()
	root := t.TempDir()
	writeRootDocument(t, root, document)
	return (&checkout.Checkout{Root: root, Deps: seam.Deps{
		Dir: filepath.Join(root, "not-the-checkout-root"),
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			t.Fatal("ReadRootFile passed a command to Exec")
			return seam.Result{}, nil
		},
	}}).ReadRootFile()
}

func writeRootDocument(t *testing.T, root, document string) {
	t.Helper()
	path := filepath.Join(root, checkout.RootFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertRootFileError(t *testing.T, got checkout.RootFile, err error, detail string) {
	t.Helper()
	if got != (checkout.RootFile{}) {
		t.Errorf("ReadRootFile() = %#v, want zero", got)
	}
	var rootErr *checkout.RootFileError
	if !errors.As(err, &rootErr) || reflect.TypeOf(err) != reflect.TypeOf(rootErr) || rootErr.Detail != detail {
		t.Fatalf("ReadRootFile() error = %T %#v, want RootFileError detail %q", err, err, detail)
	}
	if want := checkout.RootFilePath + ": " + detail; err.Error() != want {
		t.Fatalf("ReadRootFile() error = %q, want %q", err, want)
	}
}
