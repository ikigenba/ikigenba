package cli_test

import (
	"bufio"
	"os"
	"reflect"
	"strings"
	"testing"
)

// R-VSTJ-F5OX
func TestModuleContract(t *testing.T) {
	contents, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read project go.mod: %v", err)
	}

	modulePath, goVersion, directRequirements := parseGoMod(t, string(contents))

	const expectedModulePath = "github.com/ikigenba/ikigenba/dory"
	if modulePath != expectedModulePath {
		t.Errorf("module path = %q, want %q", modulePath, expectedModulePath)
	}
	if goVersion == "" {
		t.Error("go.mod has no Go version directive")
	}

	expectedDirectRequirements := map[string]bool{
		"github.com/google/uuid":                true,
		"github.com/ikigenba/ikigenba/agentkit": true,
		"github.com/ikigenba/ikigenba/toolkit":  true,
		"modernc.org/sqlite":                    true,
	}
	if !reflect.DeepEqual(directRequirements, expectedDirectRequirements) {
		t.Errorf("direct requirements = %v, want exactly %v", directRequirements, expectedDirectRequirements)
	}
}

func parseGoMod(t *testing.T, contents string) (string, string, map[string]bool) {
	t.Helper()

	var modulePath string
	var goVersion string
	directRequirements := make(map[string]bool)
	inRequireBlock := false

	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(line, "//") {
			continue
		}

		switch {
		case fields[0] == "module" && len(fields) >= 2:
			modulePath = fields[1]
		case fields[0] == "go" && len(fields) >= 2:
			goVersion = fields[1]
		case fields[0] == "require" && len(fields) == 2 && fields[1] == "(":
			inRequireBlock = true
		case inRequireBlock && fields[0] == ")":
			inRequireBlock = false
		case fields[0] == "require" && len(fields) >= 3:
			addDirectRequirement(directRequirements, fields[1], line)
		case inRequireBlock && len(fields) >= 2:
			addDirectRequirement(directRequirements, fields[0], line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan project go.mod: %v", err)
	}

	return modulePath, goVersion, directRequirements
}

func addDirectRequirement(requirements map[string]bool, modulePath, line string) {
	if !strings.Contains(line, "// indirect") {
		requirements[modulePath] = true
	}
}
