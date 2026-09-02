package toolkit

import (
	"reflect"
	"slices"
	"testing"
)

// R-CBEG-1LB0: these assignments make interface conformance a compile-time check.
var _ GlobOption = SkipOption{}
var _ GrepOption = SkipOption{}

func TestOptionInterfacesAreSealed(t *testing.T) {
	tests := []struct {
		name       string
		optionType reflect.Type
	}{
		{name: "GlobOption", optionType: reflect.TypeOf((*GlobOption)(nil)).Elem()},
		{name: "GrepOption", optionType: reflect.TypeOf((*GrepOption)(nil)).Elem()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.optionType.Kind(); got != reflect.Interface {
				t.Fatalf("kind = %v, want interface", got)
			}
			if got := test.optionType.NumMethod(); got != 1 {
				t.Fatalf("method count = %d, want 1", got)
			}

			method := test.optionType.Method(0)
			// R-CA6J-NTKB: a nonempty PkgPath marks the method unexported, which
			// prevents types in another package from implementing the interface.
			if method.PkgPath == "" {
				t.Errorf("method %q is exported; option interface is not sealed", method.Name)
			}
		})
	}
}

func TestWithSkipAppliesPatterns(t *testing.T) {
	option := WithSkip("a", "b")
	glob := globConfig{}
	grep := grepConfig{}

	option.applyGlob(&glob)
	option.applyGrep(&grep)

	want := []string{"a", "b"}
	// R-CBEG-1LB0: the shared option preserves and applies every pattern in order.
	if !slices.Equal(glob.skipPatterns, want) {
		t.Errorf("glob skip patterns = %q, want %q", glob.skipPatterns, want)
	}
	if !slices.Equal(grep.skipPatterns, want) {
		t.Errorf("grep skip patterns = %q, want %q", grep.skipPatterns, want)
	}
}
