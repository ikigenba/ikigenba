package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// R-4Z1L-J2YK
func TestGoSourceDoesNotHardcodeLedgerLoopbackPorts(t *testing.T) {
	moduleRoot := filepath.Clean(filepath.Join("..", ".."))
	thisFile := filepath.Join(moduleRoot, "cmd", "ledger", "loopback_guard_test.go")
	loopbackPrefix := "127.0.0.1:" + "3"
	forbidden := regexp.MustCompile(regexp.QuoteMeta(loopbackPrefix) + `[01][0-9]{2}`)

	err := filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Clean(path) == filepath.Clean(thisFile) || filepath.Ext(path) != ".go" {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if match := forbidden.Find(source); match != nil {
			t.Fatalf("%s contains hardcoded loopback ledger port literal %q", path, match)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Go source for hardcoded loopback ports: %v", err)
	}
}
