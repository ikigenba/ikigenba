package spaceref_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func TestExportedAPIShapes(t *testing.T) {
	t.Parallel()

	// R-QILN-DD9S R-QJTJ-R50H R-QL1G-4WR6
	assertFields(t, spaceref.Space{}, []field{{"Label", "string"}, {"Domain", "string"}})
	assertFields(t, spaceref.App{}, []field{{"Name", "string"}, {"Space", "spaceref.Space"}, {"Hostname", "string"}})
	assertFunctionTypes(t, spaceref.Parse, spaceref.ParseApp, spaceref.ValidLabel)
}

func TestErrorTypes(t *testing.T) {
	t.Parallel()

	// R-QM9C-IOHV R-QNH8-WG8K
	tests := []struct {
		name string
		err  interface {
			error
			ExitCode() int
		}
		fields []field
		want   string
	}{
		{
			name: "not a space", err: &spaceref.NotASpaceError{Operand: "crm.sbx1", Root: "ikigenba.dev"},
			fields: []field{{"Operand", "string"}, {"Root", "string"}},
			want:   "'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'",
		},
		{
			name: "invalid label", err: &spaceref.InvalidLabelError{Operand: "Crm"},
			fields: []field{{"Operand", "string"}}, want: "'Crm' is not a valid label",
		},
		{
			name: "not an app", err: &spaceref.NotAnAppError{Operand: "sbx1"},
			fields: []field{{"Operand", "string"}}, want: "'sbx1' is not an app on a space: <app>.<space>",
		},
		{
			name: "unusable app", err: &spaceref.UnusableAppError{Name: "host"},
			fields: []field{{"Name", "string"}}, want: "'host' is not a usable app name",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertFields(t, reflect.ValueOf(tc.err).Elem().Interface(), tc.fields)
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if got := tc.err.ExitCode(); got != 2 {
				t.Errorf("ExitCode() = %d, want 2", got)
			}
		})
	}
}

func TestValidLabel(t *testing.T) {
	t.Parallel()

	// R-QOP5-A7Z9
	valid := []string{"sbx1", "a", "a-b", "host", strings.Repeat("a", 63)}
	for _, label := range valid {
		if !spaceref.ValidLabel(label) {
			t.Errorf("ValidLabel(%q) = false, want true", label)
		}
	}
	invalid := []string{"", "Foo_1", "Crm", "-a", "a-", "a.b", "sbx1 ", strings.Repeat("a", 64)}
	for _, label := range invalid {
		if spaceref.ValidLabel(label) {
			t.Errorf("ValidLabel(%q) = true, want false", label)
		}
	}

	// Exhaust every one- and two-byte value, including non-ASCII bytes, to check
	// that appref's accepted alphabet is a subset of the label alphabet.
	for length := 1; length <= 2; length++ {
		limit := 1 << (8 * length)
		for value := 0; value < limit; value++ {
			bytes := make([]byte, length)
			for i := range bytes {
				bytes[i] = byte((value >> (8 * i)) & 0xff)
			}
			name := string(bytes)
			if appref.ValidName(name) && !spaceref.ValidLabel(name) {
				t.Fatalf("appref.ValidName(%q) is true but spaceref.ValidLabel is false", name)
			}
		}
	}
	for _, name := range []string{"crm-api", "a--9", strings.Repeat("a", 63)} {
		if !appref.ValidName(name) || !spaceref.ValidLabel(name) {
			t.Errorf("expected %q to be valid under both name grammars", name)
		}
	}
}

func TestParseRefusals(t *testing.T) {
	t.Parallel()

	// R-QR4Y-1RGN
	root := "ikigenba.dev"
	for _, operand := range []string{
		"crm.sbx1", "crm.sbx1.ikigenba.dev", "foo.example.com", "ikigenba.dev", "sbx1.ikigenba.dev.ikigenba.dev",
	} {
		space, err := spaceref.Parse(operand, root)
		var target *spaceref.NotASpaceError
		if space != (spaceref.Space{}) || !errors.As(err, &target) || target.Operand != operand || target.Root != root {
			t.Errorf("Parse(%q, %q) = (%#v, %v), want zero Space and matching NotASpaceError", operand, root, space, err)
		}
	}
	for _, operand := range []string{"Foo_1", "Foo_1.ikigenba.dev"} {
		space, err := spaceref.Parse(operand, root)
		var target *spaceref.InvalidLabelError
		if space != (spaceref.Space{}) || !errors.As(err, &target) || target.Operand != operand {
			t.Errorf("Parse(%q, %q) = (%#v, %v), want zero Space and matching InvalidLabelError", operand, root, space, err)
		}
	}
}

func TestParseSuccess(t *testing.T) {
	t.Parallel()

	// R-QSCU-FJ7C
	want := spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}
	for _, operand := range []string{"sbx1", "sbx1.ikigenba.dev"} {
		space, err := spaceref.Parse(operand, "ikigenba.dev")
		if err != nil || space != want {
			t.Errorf("Parse(%q, %q) = (%#v, %v), want (%#v, nil)", operand, "ikigenba.dev", space, err, want)
		}
	}
	space, err := spaceref.Parse("sbx1.IKIGENBA.DEV", "ikigenba.dev")
	var target *spaceref.NotASpaceError
	if space != (spaceref.Space{}) || !errors.As(err, &target) {
		t.Errorf("byte-inexact suffix Parse = (%#v, %v), want zero Space and NotASpaceError", space, err)
	}
}

func TestParseAppRefusals(t *testing.T) {
	t.Parallel()

	// R-QTKQ-TAY1
	root := "ikigenba.dev"
	for _, operand := range []string{"sbx1", "crm.sbx1.example.com", "ikigenba.dev", "crm.ikigenba.dev"} {
		app, err := spaceref.ParseApp(operand, root)
		var target *spaceref.NotAnAppError
		if app != (spaceref.App{}) || !errors.As(err, &target) || target.Operand != operand {
			t.Errorf("ParseApp(%q, %q) = (%#v, %v), want zero App and matching NotAnAppError", operand, root, app, err)
		}
	}
	for _, operand := range []string{"Crm.sbx1", "crm.Sbx1.ikigenba.dev"} {
		app, err := spaceref.ParseApp(operand, root)
		var target *spaceref.InvalidLabelError
		if app != (spaceref.App{}) || !errors.As(err, &target) || target.Operand != operand {
			t.Errorf("ParseApp(%q, %q) = (%#v, %v), want zero App and matching InvalidLabelError", operand, root, app, err)
		}
	}
	app, err := spaceref.ParseApp("host.sbx1", root)
	var unusable *spaceref.UnusableAppError
	if app != (spaceref.App{}) || !errors.As(err, &unusable) || unusable.Name != "host" {
		t.Errorf("ParseApp reserved name = (%#v, %v), want zero App and UnusableAppError for host", app, err)
	}
}

func TestParseAppSuccess(t *testing.T) {
	t.Parallel()

	// R-QUSN-72OQ
	want := spaceref.App{
		Name: "crm", Space: spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"}, Hostname: "crm.sbx1.ikigenba.dev",
	}
	for _, operand := range []string{"crm.sbx1", "crm.sbx1.ikigenba.dev"} {
		app, err := spaceref.ParseApp(operand, "ikigenba.dev")
		if err != nil || app != want {
			t.Errorf("ParseApp(%q, %q) = (%#v, %v), want (%#v, nil)", operand, "ikigenba.dev", app, err, want)
		}
	}
}

func assertFunctionTypes(
	t *testing.T,
	parse func(string, string) (spaceref.Space, error),
	parseApp func(string, string) (spaceref.App, error),
	validLabel func(string) bool,
) {
	t.Helper()
	if parse == nil || parseApp == nil || validLabel == nil {
		t.Fatal("exported spaceref functions are nil")
	}
}

type field struct {
	name     string
	typeName string
}

func assertFields(t *testing.T, value any, want []field) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typeOf, typeOf.NumField(), len(want))
	}
	for i, expected := range want {
		actual := typeOf.Field(i)
		if actual.Name != expected.name || actual.Type.String() != expected.typeName {
			t.Errorf("%s field %d = %s %s, want %s %s", typeOf, i, actual.Name, actual.Type, expected.name, expected.typeName)
		}
	}
}
