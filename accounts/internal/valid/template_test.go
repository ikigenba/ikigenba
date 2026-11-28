package valid_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// R-UYEN-YZM6.
func TestTemplateErrorsAreDistinct(t *testing.T) {
	_ = [...]*error{&valid.ErrTemplateURL, &valid.ErrTemplateRef}
	errs := [...]error{valid.ErrName, valid.ErrSlug, valid.ErrReserved, valid.ErrKey, valid.ErrType, valid.ErrSize,
		valid.ErrAddress, valid.ErrPrefix, valid.ErrPath, valid.ErrTemplateURL, valid.ErrTemplateRef}
	for i, err := range errs {
		if err == nil {
			t.Fatalf("error at index %d is nil", i)
		}
		for _, other := range errs[:i] {
			if errors.Is(err, other) || errors.Is(other, err) {
				t.Errorf("%v and %v match", err, other)
			}
		}
	}
}

// R-UZMK-CRCV.
func TestTemplateValidatorSignatures(t *testing.T) {
	_ = [2]func(string) error{valid.TemplateURL, valid.TemplateRef}
	type count uint16
	if got, want := [...]count{valid.MaxTemplateURL, valid.MaxTemplateRef}, [...]count{512, 255}; got != want {
		t.Errorf("template limits = %v, want %v", got, want)
	}
}

// R-V0UG-QJ3K.
func TestTemplateURL(t *testing.T) {
	for _, s := range []string{
		"https://github.com/ikigenba/team-template", "https://git.example.com:8443/t.git",
		"https://[2001:db8::1]/template", "https://" + strings.Repeat("a", valid.MaxTemplateURL-8),
	} {
		if err := valid.TemplateURL(s); err != nil {
			t.Errorf("TemplateURL(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{
		"http://github.com/ikigenba/team-template", "HTTPS://github.com/x", "https://user:secret@github.com/x",
		"https://user@github.com/x", "https://github.com/x?ref=main", "https://github.com/x?", "https://github.com/x#main",
		"https:///x", "https://:8443/x", "https://github.com/a b", "https://github.com/café", "https://github.com/%zz", "https://github.com/\x7f",
		"git@github.com:ikigenba/team-template.git", "ssh://github.com/x", "file:///srv/t", "",
		"https://" + strings.Repeat("a", valid.MaxTemplateURL-7),
	} {
		err := valid.TemplateURL(s)
		if [1]error{err} != [1]error{valid.ErrTemplateURL} {
			t.Errorf("TemplateURL(%q) = %v, want ErrTemplateURL", s, err)
		}
	}
}

// R-V22D-4AU9.
func TestTemplateRef(t *testing.T) {
	for _, s := range []string{
		"main", "v2", "release/2026-11", "feature_x.y", "feature/café", "a/b-c", "feature@work",
		strings.Repeat("a", valid.MaxTemplateRef),
	} {
		if err := valid.TemplateRef(s); err != nil {
			t.Errorf("TemplateRef(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{
		"", "-main", "/main", "main/", "main.", "a//b", "a..b", "a@{1}", "@", "HEAD", ".hidden", "a/.b",
		"x.lock", "a/x.lock/b", "a b", "a~1", "a^", "a:b", "a?", "a*", "a[b", "a\\b",
		"a\tb", "a\x7fb", "caf\xe9", strings.Repeat("a", valid.MaxTemplateRef+1),
	} {
		err := valid.TemplateRef(s)
		if [1]error{err} != [1]error{valid.ErrTemplateRef} {
			t.Errorf("TemplateRef(%q) = %v, want ErrTemplateRef", s, err)
		}
	}
}
