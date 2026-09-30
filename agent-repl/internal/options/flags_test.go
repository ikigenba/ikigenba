package options_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-repl/internal/options"
)

// R-RLBD-UAVE
func TestPairHasKeyAndValueStringFields(t *testing.T) {
	key, value := "provider", "openai"
	pair := options.Pair{Key: key, Value: value}
	if pair.Key != key || pair.Value != value {
		t.Fatalf("Pair = %+v, want Key provider and Value openai", pair)
	}
}

// R-RMJA-82M3
func TestFlagsHasConfigRawAndVersionFields(t *testing.T) {
	config := []options.Pair{{Key: "k", Value: "v"}}
	raw, version := true, true
	flags := options.Flags{Config: config, Raw: raw, Version: version}
	if !reflect.DeepEqual(flags.Config, config) || flags.Raw != raw || flags.Version != version {
		t.Fatalf("Flags = %+v, want the constructed field values", flags)
	}
}

// R-U8L2-83RD
func TestParseFlagsHasRequiredSignature(t *testing.T) {
	got, err := options.ParseFlags(nil)
	if err != nil {
		t.Fatalf("ParseFlags(nil) error = %v", err)
	}
	if !reflect.DeepEqual(got, options.Flags{}) {
		t.Fatalf("ParseFlags(nil) = %#v, want zero Flags", got)
	}
}

// R-UB0U-ZN8R
func TestHelpSpellingsReturnErrHelp(t *testing.T) {
	for _, argument := range []string{"-h", "--help"} {
		t.Run(argument, func(t *testing.T) {
			_, err := options.ParseFlags([]string{argument})
			if !errors.Is(err, options.ErrHelp) {
				t.Fatalf("ParseFlags(%q) error = %v, want ErrHelp", argument, err)
			}
		})
	}
}

// R-UC8R-DEZG
func TestSupportedFlagsPopulateFieldsAndAbsentFlagsReturnZero(t *testing.T) {
	got, err := options.ParseFlags([]string{"-c", "one=1", "--c=two=2", "-raw", "--raw", "-V", "--version"})
	if err != nil {
		t.Fatalf("ParseFlags(supported flags) error = %v", err)
	}
	want := options.Flags{
		Config:  []options.Pair{{Key: "one", Value: "1"}, {Key: "two", Value: "2"}},
		Raw:     true,
		Version: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseFlags(supported flags) = %#v, want %#v", got, want)
	}

	zero, err := options.ParseFlags(nil)
	if err != nil {
		t.Fatalf("ParseFlags(nil) error = %v", err)
	}
	if !reflect.DeepEqual(zero, options.Flags{}) {
		t.Errorf("ParseFlags(nil) = %#v, want zero Flags", zero)
	}
}

// R-UDGN-R6Q5
func TestRepeatedConfigPreservesOrderDuplicatesAndFirstEqualsSplit(t *testing.T) {
	got, err := options.ParseFlags([]string{"-c", "same=first", "-c", "other=a=b", "-c", "same=last"})
	if err != nil {
		t.Fatalf("ParseFlags(repeated config) error = %v", err)
	}
	want := []options.Pair{
		{Key: "same", Value: "first"},
		{Key: "other", Value: "a=b"},
		{Key: "same", Value: "last"},
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Errorf("Config = %#v, want %#v", got.Config, want)
	}
}

// R-UEOK-4YGU
func TestMalformedConfigNamesArgumentAndEmptyValueIsAccepted(t *testing.T) {
	for _, argument := range []string{"missing-equals", "=empty-key"} {
		t.Run(argument, func(t *testing.T) {
			_, err := options.ParseFlags([]string{"-c", argument})
			if err == nil {
				t.Fatalf("ParseFlags(-c %q) returned nil error", argument)
			}
			if !strings.Contains(err.Error(), argument) {
				t.Errorf("error %q does not name argument %q", err, argument)
			}
		})
	}

	got, err := options.ParseFlags([]string{"-c", "empty="})
	if err != nil {
		t.Fatalf("ParseFlags(-c empty=) error = %v", err)
	}
	want := []options.Pair{{Key: "empty", Value: ""}}
	if !reflect.DeepEqual(got.Config, want) {
		t.Errorf("Config = %#v, want %#v", got.Config, want)
	}
}

// R-UFWG-IQ7J
func TestParseFlagsDoesNotRejectSemanticallyInvalidConfig(t *testing.T) {
	got, err := options.ParseFlags([]string{"-c", "provider=not-a-provider"})
	if err != nil {
		t.Fatalf("ParseFlags(semantically invalid config) error = %v", err)
	}
	want := []options.Pair{{Key: "provider", Value: "not-a-provider"}}
	if !reflect.DeepEqual(got.Config, want) {
		t.Errorf("Config = %#v, want %#v", got.Config, want)
	}
}
