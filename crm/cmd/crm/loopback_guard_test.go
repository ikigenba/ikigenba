package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestNoHardcodedLoopbackServicePortsInGoSource(t *testing.T) {
	// R-X1CA-0373
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate guard source file")
	}
	self, err := filepath.Abs(self)
	if err != nil {
		t.Fatalf("resolve guard source path: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}

	loopbackServicePort := regexp.MustCompile(regexp.QuoteMeta("127.0.0.1:"+"30") + `[0-9][0-9]`)
	found := false
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if absPath == self {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range loopbackServicePort.FindAll(src, -1) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				rel = path
			}
			t.Errorf("%s contains hardcoded loopback service port %q", rel, string(match))
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Go source under %s: %v", root, err)
	}
	if found {
		t.Fatal("hardcoded loopback service ports must come from registry")
	}
}
