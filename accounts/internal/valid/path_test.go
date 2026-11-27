package valid_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// R-EUTI-NSM2.
func TestPathErrorIsDistinct(t *testing.T) {
	_ = [1]*error{&valid.ErrPath}
	all := []error{valid.ErrName, valid.ErrSlug, valid.ErrReserved, valid.ErrKey, valid.ErrType, valid.ErrSize, valid.ErrAddress, valid.ErrPrefix, valid.ErrPath}
	for i, err := range all {
		if err == nil {
			t.Fatalf("error at index %d is nil", i)
		}
		for _, other := range all[:i] {
			if errors.Is(err, other) || errors.Is(other, err) {
				t.Errorf("%v and %v match", err, other)
			}
		}
	}
}

// R-EX9B-FC3G, R-F0X0-KNBJ.
func TestPathLimits(t *testing.T) {
	type limit uint16
	if got, want := [...]limit{valid.MaxRestorePath, valid.MaxDatabasePath, valid.MaxRestoresPerHost},
		[...]limit{512, 256, 10}; got != want {
		t.Errorf("restore limits = %v, want %v", got, want)
	}
	if valid.MaxDatabasePath != protocol.MaxDatabasePath || valid.MaxRestoresPerHost != protocol.MaxRestores || valid.MaxRestorePath+128 > protocol.MaxCopyPath {
		t.Error("restore limits disagree with the protocol")
	}
}

// R-EW1F-1KCR, R-JIF0-AQDZ.
func TestRestorePath(t *testing.T) {
	check := [1]func(string) error{valid.RestorePath}[0]
	for _, path := range []string{
		"", "notes.txt", "a/b/c", ".hidden", "..x", "a b/R&D <draft>.txt", "café/menu.pdf",
		strings.Repeat("a", 510), strings.Repeat("&", 85),
	} {
		if err := check(path); err != nil {
			t.Errorf("RestorePath(%q) = %v, want nil", path, err)
		}
	}
	for _, path := range []string{
		"/etc/passwd", "a//b", "a/", "/", ".", "..", "./a", "a/./b", "a/../b", "../a",
		"a\x00", "a\nb", "a\tb", "a\x7f", "caf\xe9/menu.pdf", strings.Repeat("a", 511), strings.Repeat("&", 86),
	} {
		if err := check(path); !isPathSentinel(err) {
			t.Errorf("RestorePath(%q) = %v, want ErrPath", path, err)
		}
	}
}

// R-EW1F-1KCR, R-EZP4-6VKU.
func TestDatabasePath(t *testing.T) {
	check := [1]func(string) error{valid.DatabasePath}[0]
	for _, path := range []string{
		"data", "data/orders.db", "data/a/b.sqlite", "data/x&y.db", "data/" + strings.Repeat("a", 249), "data/" + strings.Repeat("&", 41) + "abc",
	} {
		if err := check(path); err != nil {
			t.Errorf("DatabasePath(%q) = %v, want nil", path, err)
		}
	}
	for _, path := range []string{
		"", "orders.db", "Data/orders.db", "./data/x.db", "/data/x.db", "data//x.db", "data/../etc/shadow", "data/x.db\n",
		"data/" + strings.Repeat("a", 250), "data/" + strings.Repeat("&", 41) + "abcd",
	} {
		if err := check(path); !isPathSentinel(err) {
			t.Errorf("DatabasePath(%q) = %v, want ErrPath", path, err)
		}
	}
}

func isPathSentinel(err error) bool {
	// Restore path validation promises the sentinel itself.
	return [1]error{err} == [1]error{valid.ErrPath}
}
