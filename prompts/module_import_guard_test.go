package prompts

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestModuleAgentkitDependencyIsPublishedOnly(t *testing.T) {
	const published = "github.com/ikigenba/agentkit"

	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}

	var version string
	for _, req := range mod.Require {
		if req.Mod.Path == "agentkit" {
			t.Fatalf("go.mod requires deprecated local module path %q", req.Mod.Path)
		}
		if req.Mod.Path != published {
			continue
		}
		version = req.Mod.Version
	}
	if version == "" {
		t.Fatalf("go.mod does not require %s", published)
	}

	for _, rep := range mod.Replace {
		if rep.Old.Path == "agentkit" {
			t.Fatalf("go.mod replaces deprecated local module path %q", rep.Old.Path)
		}
		if rep.New.Path == "./agentkit" || rep.New.Path == "../agentkit" {
			t.Fatalf("go.mod points at local agentkit path %q", rep.New.Path)
		}
	}

	sum, err := os.ReadFile("go.sum")
	if err != nil {
		t.Fatal(err)
	}
	sumText := string(sum)
	for _, want := range []string{
		published + " " + version + " ",
		published + " " + version + "/go.mod ",
	} {
		if !strings.Contains(sumText, want) {
			t.Fatalf("go.sum is missing %q", want)
		}
	}
}
