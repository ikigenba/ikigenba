package opsctl

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSystemHasNoOrphanedServedTreeSeams(t *testing.T) {
	// R-3LHT-WHAO
	legacy := []string{
		"ensure" + "WWWPerms",
		"state" + "WWWFragment",
		"Ensure" + "SystemGroup",
		"Add" + "UserToGroup",
		`"` + "web" + `"`,
		`^[[:space:]]*` + "Chmod" + `\\(ctx context\\.Context, path string, mode os\\.FileMode\\) error$`,
	}
	cmd := exec.Command("grep", "-rnE", "--include=*.go", "--exclude=*_test.go", strings.Join(legacy, "|"), ".")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("orphaned served-tree seams remain:\n%s", out)
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("grep orphaned served-tree seams: %v\n%s", err, out)
		}
	}
	if _, err := os.Stat("www.go"); !os.IsNotExist(err) {
		t.Fatalf("www.go exists or could not be checked: %v", err)
	}
}
