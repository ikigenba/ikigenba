package options_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-repl/internal/options"
)

// R-U659-GK9Z
func TestPairHasExactlyKeyAndValueStringFields(t *testing.T) {
	typeOfPair := reflect.TypeOf(options.Pair{})
	want := []reflect.StructField{
		{Name: "Key", Type: reflect.TypeOf("")},
		{Name: "Value", Type: reflect.TypeOf("")},
	}
	if typeOfPair.NumField() != len(want) {
		t.Fatalf("Pair field count = %d, want %d", typeOfPair.NumField(), len(want))
	}
	for index, wantField := range want {
		got := typeOfPair.Field(index)
		if got.Name != wantField.Name || got.Type != wantField.Type {
			t.Errorf("Pair field %d = %s %s, want %s %s", index, got.Name, got.Type, wantField.Name, wantField.Type)
		}
	}
}

// R-U7D5-UC0O
func TestFlagsHasExactlyConfigRawAndVersionFields(t *testing.T) {
	typeOfFlags := reflect.TypeOf(options.Flags{})
	want := []reflect.StructField{
		{Name: "Config", Type: reflect.TypeOf([]options.Pair(nil))},
		{Name: "Raw", Type: reflect.TypeOf(false)},
		{Name: "Version", Type: reflect.TypeOf(false)},
	}
	if typeOfFlags.NumField() != len(want) {
		t.Fatalf("Flags field count = %d, want %d", typeOfFlags.NumField(), len(want))
	}
	for index, wantField := range want {
		got := typeOfFlags.Field(index)
		if got.Name != wantField.Name || got.Type != wantField.Type {
			t.Errorf("Flags field %d = %s %s, want %s %s", index, got.Name, got.Type, wantField.Name, wantField.Type)
		}
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
