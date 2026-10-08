package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestCreateDispatchSuppliesReportedVersionToRelease(t *testing.T) {
	// R-UQNI-QNIP
	h := newCommandHarness(t)
	h.createRoleErr = errors.New("stop after build")
	deps := h.deps()
	exec := deps.Exec
	seen := false
	reported := invokeWithDeps(deps, "--version").stdout
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "tar" && len(cmd.Args) > 0 && cmd.Args[0] == "-cJf" {
			data, err := os.ReadFile(filepath.Join(cmd.Args[3], createSHA, "release.json"))
			if err != nil {
				t.Fatal(err)
			}
			var metadata struct {
				Devctl string `json:"devctl"`
			}
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.Devctl+"\n" != reported {
				t.Fatalf("release devctl %q, version %q", metadata.Devctl, reported)
			}
			seen = true
		}
		return exec(ctx, cmd)
	}
	result := invokeWithDeps(deps, "space", "create", "sbx1", "--acme-email", "alerts@example.test")
	if result.code != 1 || !seen {
		t.Fatalf("result %#v release seen %v", result, seen)
	}
}
