package sites

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidatePathNormalizesAndRejectsUnsafeRoots(t *testing.T) {
	// R-3TWL-UEHA
	valid := map[string]string{
		"":                      "",
		"/":                     "",
		"public":                "public",
		"/the/content/is/here/": "the/content/is/here",
	}
	for raw, want := range valid {
		got, err := ValidatePath(raw)
		if err != nil || got != want {
			t.Errorf("ValidatePath(%q) = %q, %v; want %q, nil", raw, got, err, want)
		}
	}
	invalid := []string{
		"//", "a//b", "a/./b", "a/../b", ".git", "a/.GIT/b", `a\b`, "a\nb",
		strings.Repeat("a", 256),
	}
	for _, raw := range invalid {
		if got, err := ValidatePath(raw); got != "" || !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ValidatePath(%q) = %q, %v; want ErrInvalidPath", raw, got, err)
		}
	}
}

func TestSubtreeFiltersAndStripsPublishRoot(t *testing.T) {
	entries := []TreeEntry{
		{Path: "project.txt", Data: []byte("outside")},
		{Path: "public/index.html", Data: []byte("index")},
		{Path: "public/assets/app.css", Data: []byte("css")},
		{Path: "publication/nope", Data: []byte("outside")},
	}
	want := []TreeEntry{
		{Path: "index.html", Data: []byte("index")},
		{Path: "assets/app.css", Data: []byte("css")},
	}
	if got := Subtree(entries, "public"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Subtree = %#v, want %#v", got, want)
	}
	if got := Subtree(entries, ""); !reflect.DeepEqual(got, entries) {
		t.Fatalf("root Subtree = %#v, want identity %#v", got, entries)
	}
}
