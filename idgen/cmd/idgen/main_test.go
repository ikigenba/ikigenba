package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// R-SHZS-WTMG
func TestBuiltBinaryWithNoArgumentsPrintsOneDefaultPrefixID(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "idgen")
	build := exec.Command("go", "build", "-o", binary, "./cmd/idgen") // #nosec G204 -- output is a test-owned temporary path
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\noutput:\n%s", err, output)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	run := exec.Command(binary) // #nosec G204 -- the test just built this fixed-path executable
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("run binary: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.Bytes(), stderr.Bytes())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}

	want := regexp.MustCompile(`\AR-[0-9A-Z]{4}-[0-9A-Z]{4}\n\z`)
	if !want.Match(stdout.Bytes()) {
		t.Errorf("stdout = %q, want exactly one newline-terminated default-prefix id", stdout.String())
	}
}
