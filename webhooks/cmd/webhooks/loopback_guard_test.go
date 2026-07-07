package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-0EFT-N61V
func TestGoSourceDoesNotHardcodeLoopbackServicePorts(t *testing.T) {
	root := moduleRoot(t)
	prefix := "127.0.0.1:" + "30"
	self := filepath.Clean("cmd/webhooks/loopback_guard_test.go")

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == self {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasLoopbackServicePort(string(body), prefix) {
			t.Fatalf("%s contains a hardcoded loopback service port", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go source under %s: %v", root, err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("could not find module root from %s", dir)
		}
		dir = next
	}
}

func hasLoopbackServicePort(src, prefix string) bool {
	for offset := 0; ; {
		found := strings.Index(src[offset:], prefix)
		if found < 0 {
			return false
		}
		i := offset + found + len(prefix)
		if i+2 <= len(src) && isASCIIDigit(src[i]) && isASCIIDigit(src[i+1]) {
			return true
		}
		offset += found + 1
	}
}

func isASCIIDigit(b byte) bool {
	return '0' <= b && b <= '9'
}
