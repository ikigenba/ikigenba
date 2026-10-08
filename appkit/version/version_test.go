package version_test

import (
	"os"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/version"
)

// R-XDPF-GQHC: Importing and using version proves its path and package name.
// R-XEXB-UI81: Callers name both environment variables through exported constants.
func TestEnvironmentConstants(t *testing.T) {
	const commitVariable = version.CommitVariable
	const releaseVariable = version.ReleaseVariable
	if commitVariable != "IKIGENBA_COMMIT" {
		t.Fatalf("CommitVariable = %q", commitVariable)
	}
	if releaseVariable != "IKIGENBA_RELEASE" {
		t.Fatalf("ReleaseVariable = %q", releaseVariable)
	}
}

// R-XG58-89YQ: Display is callable with the declared signature.
// R-XM8Q-54O7: Empty values produce no identity.
func TestDisplaySignature(t *testing.T) {
	t.Setenv(version.CommitVariable, "")
	t.Setenv(version.ReleaseVariable, "")
	displays := []func() string{version.Display}
	if got := displays[0](); got != "" {
		t.Fatalf("Display() = %q, want empty string", got)
	}
}

// R-A063-27PP: Shortening counts runes and preserves exactly one final dirty suffix.
// R-XIL0-ZTG4: A commit with an empty or unset release displays the short commit.
func TestCommitDisplay(t *testing.T) {
	cases := []struct {
		name   string
		commit string
		want   string
	}{
		{name: "long", commit: "abcdef0123456789", want: "abcdef0"},
		{name: "seven", commit: "abcdef0", want: "abcdef0"},
		{name: "short", commit: "abc", want: "abc"},
		{name: "unicode", commit: "甲乙丙丁戊己庚辛", want: "甲乙丙丁戊己庚"},
		{name: "short unicode", commit: "甲乙丙", want: "甲乙丙"},
		{name: "dirty long", commit: "abcdef0123456789-dirty", want: "abcdef0-dirty"},
		{name: "dirty short", commit: "abc-dirty", want: "abc-dirty"},
		{name: "dirty unicode", commit: "甲乙丙丁戊己庚辛-dirty", want: "甲乙丙丁戊己庚-dirty"},
		{name: "dirty empty prefix", commit: "-dirty", want: "-dirty"},
		{name: "one suffix removed", commit: "-dirty-dirty", want: "-dirty-dirty"},
		{name: "internal dirty", commit: "a-dirty-extra", want: "a-dirty"},
		{name: "case preserved", commit: "ABCDEF012345", want: "ABCDEF0"},
		{name: "whitespace preserved", commit: " a b ", want: " a b "},
	}
	for _, releaseUnset := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(tc.name+releaseState(releaseUnset), func(t *testing.T) {
				t.Setenv(version.CommitVariable, tc.commit)
				setEmptyEnv(t, version.ReleaseVariable, releaseUnset)
				if got := version.Display(); got != tc.want {
					t.Fatalf("Display() = %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// R-XJSX-DL6T: A release and commit display the unchanged label and short commit.
func TestLabelledDisplayReadsCurrentEnvironment(t *testing.T) {
	t.Setenv(version.CommitVariable, "abcdef0123456789")
	t.Setenv(version.ReleaseVariable, "label")
	if got := version.Display(); got != "label (abcdef0)" {
		t.Fatalf("Display() = %q", got)
	}
	t.Setenv(version.CommitVariable, "甲乙丙丁戊己庚辛-dirty")
	t.Setenv(version.ReleaseVariable, " label (custom) ")
	if got := version.Display(); got != " label (custom)  (甲乙丙丁戊己庚-dirty)" {
		t.Fatalf("Display() after environment change = %q", got)
	}
}

// R-XL0T-RCXI: A label without a commit is shown unchanged.
func TestReleaseOnly(t *testing.T) {
	for _, commitUnset := range []bool{false, true} {
		t.Run(releaseState(commitUnset), func(t *testing.T) {
			setEmptyEnv(t, version.CommitVariable, commitUnset)
			t.Setenv(version.ReleaseVariable, " label (custom) ")
			if got := version.Display(); got != " label (custom) " {
				t.Fatalf("Display() = %q", got)
			}
		})
	}
}

// R-XM8Q-54O7: Empty and unset values in either variable produce no identity.
func TestNoIdentity(t *testing.T) {
	for _, commitUnset := range []bool{false, true} {
		for _, releaseUnset := range []bool{false, true} {
			t.Run("commit"+releaseState(commitUnset)+" release"+releaseState(releaseUnset), func(t *testing.T) {
				setEmptyEnv(t, version.CommitVariable, commitUnset)
				setEmptyEnv(t, version.ReleaseVariable, releaseUnset)
				if got := version.Display(); got != "" {
					t.Fatalf("Display() = %q, want empty string", got)
				}
			})
		}
	}
}

func setEmptyEnv(t *testing.T, name string, unset bool) {
	t.Helper()
	// Setenv registers restoration before Unsetenv changes the current state.
	t.Setenv(name, "")
	if unset {
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func releaseState(unset bool) string {
	if unset {
		return " unset"
	}
	return " empty"
}
