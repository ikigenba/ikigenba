package agentkit

import (
	"os"
	"strings"
	"testing"
)

func TestModuleContract(t *testing.T) {
	// R-1OGL-CHMW
	contents, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	module := string(contents)
	if !strings.Contains(module, "module github.com/ikigenba/ikigenba/agentkit\n") {
		t.Fatalf("unexpected module declaration:\n%s", module)
	}
	if !strings.Contains(module, "\ngo 1.26\n") {
		t.Fatalf("unexpected Go version declaration:\n%s", module)
	}
	if _, err := os.Stat("go.work"); !os.IsNotExist(err) {
		t.Fatalf("go.work must not exist in the module root: %v", err)
	}
}
