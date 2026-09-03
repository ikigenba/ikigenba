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
	// R-3AIY-084U
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
	if config.goVersion == "" {
		t.Error("go directive is missing or empty")
	}
}

func assertRequiredModules(t *testing.T, requires map[string]string) {
	t.Helper()
	wantModules := []string{
		"github.com/ikigenba/ikigenba/agentkit",
		"github.com/boyter/gocodewalker",
		"github.com/bmatcuk/doublestar/v4",
	}
	for _, module := range wantModules {
		if _, ok := requires[module]; !ok {
			t.Errorf("required module %s is missing", module)
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
