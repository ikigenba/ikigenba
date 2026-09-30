package toolkit_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/toolkit"
)

func TestPackageImportPath(t *testing.T) {
	// R-NP5G-QO43: an external package imports toolkit by its module path and uses it.
	tool, err := toolkit.Read(t.TempDir())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if tool == nil {
		t.Fatal("Read returned a nil tool")
	}
	if got := tool.Name(); got != "Read" {
		t.Errorf("Name() = %q, want %q", got, "Read")
	}
}
