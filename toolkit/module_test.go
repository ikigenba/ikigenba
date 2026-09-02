package toolkit

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
)

type moduleConfig struct {
	path      string
	goVersion string
	requires  map[string]string
}

func TestModuleContract(t *testing.T) {
	// R-C1N8-ZFDG
	config, err := readModuleConfig()
	if err != nil {
		t.Fatal(err)
	}
	assertModuleDirectives(t, config)
	assertRequiredModules(t, config.requires)
	assertNoWorkspaceFile(t)
}

func readModuleConfig() (moduleConfig, error) {
	contents, err := os.ReadFile("go.mod")
	if err != nil {
		return moduleConfig{}, fmt.Errorf("read go.mod: %w", err)
	}

	config := moduleConfig{requires: make(map[string]string)}
	inRequireBlock := false

	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "//") {
			continue
		}

		if inRequireBlock {
			if fields[0] == ")" {
				inRequireBlock = false
				continue
			}
			if len(fields) >= 2 {
				config.requires[fields[0]] = fields[1]
			}
			continue
		}

		switch fields[0] {
		case "module":
			if len(fields) == 2 {
				config.path = fields[1]
			}
		case "go":
			if len(fields) == 2 {
				config.goVersion = fields[1]
			}
		case "require":
			if len(fields) == 2 && fields[1] == "(" {
				inRequireBlock = true
			} else if len(fields) >= 3 {
				config.requires[fields[1]] = fields[2]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return moduleConfig{}, fmt.Errorf("scan go.mod: %w", err)
	}
	return config, nil
}

func assertModuleDirectives(t *testing.T, config moduleConfig) {
	t.Helper()
	if want := "github.com/ikigenba/ikigenba/toolkit"; config.path != want {
		t.Errorf("module path = %q, want %q", config.path, want)
	}
	if want := "1.26"; config.goVersion != want {
		t.Errorf("go directive = %q, want %q", config.goVersion, want)
	}
}

func assertRequiredModules(t *testing.T, requires map[string]string) {
	t.Helper()
	wantRequires := map[string]string{
		"github.com/ikigenba/ikigenba/agentkit": "v0.1.0",
		"github.com/boyter/gocodewalker":        "v1.5.1",
		"github.com/bmatcuk/doublestar/v4":      "v4.10.0",
	}
	for module, want := range wantRequires {
		if got := requires[module]; got != want {
			t.Errorf("require %s = %q, want %q", module, got, want)
		}
	}
}

func assertNoWorkspaceFile(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("go.work"); err == nil {
		t.Error("go.work exists in module directory")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect workspace file: %v", err)
	}
}
