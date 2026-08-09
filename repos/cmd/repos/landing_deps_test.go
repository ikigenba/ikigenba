package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestProductionDependencyGraphExcludesBrowserTestEngines(t *testing.T) {
	// R-SZF6-NY1F
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go must be on PATH: %v", err)
	}
	command := exec.Command(goPath, "list", "-deps", "./cmd/repos")
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./cmd/repos: %v: %s", err, output)
	}
	dependencies := strings.Fields(string(output))
	for _, forbidden := range []string{"github.com/chromedp/chromedp", "github.com/dop251/goja"} {
		for _, dependency := range dependencies {
			if dependency == forbidden {
				t.Errorf("production dependency graph contains test-only package %s", forbidden)
			}
		}
	}
}
