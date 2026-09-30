package toolkit

import (
	"slices"
	"testing"
)

// R-CBEG-1LB0: these assignments make interface conformance a compile-time check.
var _ GlobOption = SkipOption{}
var _ GrepOption = SkipOption{}

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
