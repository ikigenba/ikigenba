package options_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/dory/internal/options"
)

type parseSignature func([]string) (options.Flags, error)
type validateSignature func(options.Flags) (options.Options, error)
type usageSignature func() string

var (
	_ parseSignature    = options.ParseFlags
	_ validateSignature = options.Flags.Validate
	_ usageSignature    = options.Usage
)

// R-HIPA-5Z4K
func TestPairPublicShape(t *testing.T) {
	t.Parallel()

	want := []reflect.StructField{
		{Name: "Key", Type: reflect.TypeFor[string]()},
		{Name: "Value", Type: reflect.TypeFor[string]()},
	}
	assertPublicShape(t, reflect.TypeFor[options.Pair](), want)
}

// R-HJX6-JQV9
func TestFlagsPublicShape(t *testing.T) {
	t.Parallel()

	want := []reflect.StructField{
		{Name: "Config", Type: reflect.TypeFor[[]options.Pair]()},
		{Name: "Resume", Type: reflect.TypeFor[string]()},
		{Name: "Version", Type: reflect.TypeFor[bool]()},
	}
	assertPublicShape(t, reflect.TypeFor[options.Flags](), want)
}

// R-HL52-XILY
func TestPublicOperationsAndHelpSentinel(t *testing.T) {
	t.Parallel()

	if options.Usage() == "" {
		t.Fatal("Usage must be non-empty")
	}

	for _, argument := range []string{"-h", "--help"} {
		argument := argument
		t.Run(argument, func(t *testing.T) {
			t.Parallel()
			_, err := options.ParseFlags([]string{argument})
			if !errors.Is(err, options.ErrHelp) {
				t.Fatalf("ParseFlags(%q) error = %v, want ErrHelp", argument, err)
			}
		})
	}
}

// R-HMCZ-BACN
func TestParseFlagsPopulatesFieldsAndDefaultsToZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want options.Flags
	}{
		{name: "zero", want: options.Flags{}},
		{name: "short config", args: []string{"-c", "model=fast"}, want: options.Flags{Config: []options.Pair{{Key: "model", Value: "fast"}}}},
		{name: "long config", args: []string{"--c", "model=fast"}, want: options.Flags{Config: []options.Pair{{Key: "model", Value: "fast"}}}},
		{name: "short resume", args: []string{"-resume", "session"}, want: options.Flags{Resume: "session"}},
		{name: "long resume", args: []string{"--resume", "session"}, want: options.Flags{Resume: "session"}},
		{name: "short version", args: []string{"-V"}, want: options.Flags{Version: true}},
		{name: "long version", args: []string{"--version"}, want: options.Flags{Version: true}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := options.ParseFlags(test.args)
			if err != nil {
				t.Fatalf("ParseFlags(%q) error = %v", test.args, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseFlags(%q) = %#v, want %#v", test.args, got, test.want)
			}
		})
	}
}

// R-HNKV-P23C
func TestParseFlagsPreservesConfigArguments(t *testing.T) {
	t.Parallel()

	got, err := options.ParseFlags([]string{
		"-c", "model=first",
		"--c", "empty=",
		"-c", "model=second=with=equals",
	})
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	want := []options.Pair{
		{Key: "model", Value: "first"},
		{Key: "empty", Value: ""},
		{Key: "model", Value: "second=with=equals"},
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
	}
}

// R-HNKV-P23C
func TestParseFlagsRejectsMalformedConfig(t *testing.T) {
	t.Parallel()

	for _, argument := range []string{"missing-equals", "=empty-key"} {
		argument := argument
		t.Run(argument, func(t *testing.T) {
			t.Parallel()
			_, err := options.ParseFlags([]string{"-c", argument})
			if err == nil {
				t.Fatalf("ParseFlags(-c %q) succeeded, want error", argument)
			}
			if !strings.Contains(err.Error(), argument) {
				t.Fatalf("ParseFlags(-c %q) error = %q, want original argument", argument, err)
			}
		})
	}
}

// R-HL52-XILY
func TestParseFlagsRejectsUnknownFlagAndPositionals(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		text string
	}{
		{name: "unknown flag", args: []string{"-wat"}, text: "wat"},
		{name: "positional argument", args: []string{"prompt.txt"}, text: "prompt.txt"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := options.ParseFlags(test.args)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("ParseFlags(%q) error = %v, want error containing %q", test.args, err, test.text)
			}
		})
	}
}

func assertPublicShape(t *testing.T, got reflect.Type, want []reflect.StructField) {
	t.Helper()
	if got.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", got, got.NumField(), len(want))
	}
	for index, wantField := range want {
		gotField := got.Field(index)
		if gotField.Name != wantField.Name || gotField.Type != wantField.Type || gotField.PkgPath != "" {
			t.Errorf("%s field %d = %s %s (PkgPath %q), want exported %s %s", got, index, gotField.Name, gotField.Type, gotField.PkgPath, wantField.Name, wantField.Type)
		}
	}
}
