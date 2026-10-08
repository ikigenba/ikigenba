package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestRetiredCommandsAreUnknown(t *testing.T) {
	// R-YNCA-5JLD R-RMSM-XIO5
	for _, test := range []struct {
		args          []string
		message, help string
	}{
		{[]string{"remove", "sbx1", "crm"}, "unknown command 'remove'", "devctl --help"},
		{[]string{"space", "init", "sbx1", "--opsctl", "obsolete"}, "unknown subcommand 'init'", "devctl space --help"},
	} {
		calls := 0
		assertResult(t, invokeWithDeps(noExternalDeps(&calls), test.args...), 2, "", "devctl: "+test.message+"\n\nsee '"+test.help+"' for usage\n")
		if calls != 0 {
			t.Fatalf("external calls %d", calls)
		}
	}
}

func TestDeploySyntaxAndHelpThroughCLI(t *testing.T) {
	// R-R9DQ-Q1II R-RBTJ-HKZW R-RD1F-VCQL R-RE9C-94HA
	for _, args := range [][]string{{"deploy", "--help"}, {"deploy", "-h"}, {"deploy", "sbx1", "r1", "--help"}} {
		calls := 0
		deps := noExternalDeps(&calls)
		deps.Dir = t.TempDir()
		assertResult(t, invokeWithDeps(deps, args...), 0, expectedDeployUsage, "")
		if calls != 0 {
			t.Fatalf("external calls %d", calls)
		}
	}
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"deploy"}, "deploy needs <space> and <sha|tag>"},
		{[]string{"deploy", "sbx1"}, "deploy needs <space> and <sha|tag>"},
		{[]string{"deploy", "sbx1", "r1", "extra"}, "deploy takes only <space> and <sha|tag>"},
		{[]string{"deploy", "sbx1", "--bad"}, "unknown option '--bad'"},
	} {
		calls := 0
		assertResult(t, invokeWithDeps(noExternalDeps(&calls), test.args...), 2, "", "devctl: "+test.message+"\n\nsee 'devctl deploy --help' for usage\n")
		if calls != 0 {
			t.Fatalf("external calls %d", calls)
		}
	}
}

func TestDeployArchivePathIsACommitOperand(t *testing.T) {
	// R-9WMJ-QLVW
	f := newDeployRelease(t)
	operand := "dist/" + deploySHA + ".tar.xz"
	f.put(filepath.Join(f.root, operand), "existing archive")
	f.fail = "cloud"
	assertResult(t, invokeWithDeps(f.deps(), "deploy", "sbx1", operand), 2, "", "devctl: '"+operand+"' is not a commit\n")
	if len(f.commands) != 2 || !strings.HasSuffix(f.commands[1].Args[len(f.commands[1].Args)-1], operand+"^{commit}") {
		t.Fatalf("commands %#v", f.commands)
	}
	data, err := os.ReadFile(filepath.Join(f.root, operand))
	if err != nil || string(data) != "existing archive" {
		t.Fatalf("archive %q error %v", data, err)
	}
}

func TestSuiteManifestFailuresHaveNoArchiveOrOutput(t *testing.T) {
	// R-R6XX-YI14 R-R85U-C9RT
	for _, nonzero := range []bool{false, true} {
		f := newCLISuiteFixture(t)
		f.apps = []string{"crm"}
		f.manifestFails = nonzero
		f.manifestNewline = !nonzero
		deps := f.deps()
		exec := deps.Exec
		commands := []seam.Cmd{}
		deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
			commands = append(commands, c)
			return exec(ctx, c)
		}
		var output strings.Builder
		err := build.Run(t.Context(), []string{"r1"}, version, &output, deps)
		if output.Len() != 0 {
			t.Fatalf("output %q", output.String())
		}
		if nonzero {
			var process *build.ProcessError
			if !errors.As(err, &process) || process.Label != "crm manifest" || process.Status != 1 || process.Stderr != "boom" {
				t.Fatalf("error %#v", err)
			}
		} else {
			var stale *build.StaleManifestError
			if !errors.As(err, &stale) || stale.App != "crm" {
				t.Fatalf("error %#v", err)
			}
		}
		for _, c := range commands {
			if c.Path == "tar" {
				t.Fatal("tar called after manifest failure")
			}
		}
		f = newCLISuiteFixture(t)
		f.apps = []string{"crm"}
		f.manifestFails = nonzero
		f.manifestNewline = !nonzero
		if nonzero {
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 1, "", "devctl: crm manifest: exit status 1\n\n> boom\n")
		} else {
			assertResult(t, invokeWithDeps(f.deps(), "build", "r1"), 2, "", "devctl: crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit\n")
		}
		if f.tarCalls != 0 {
			t.Fatalf("tar calls %d", f.tarCalls)
		}
	}
}

func TestSuiteErrorsNeverWriteOutput(t *testing.T) {
	// R-R85U-C9RT
	for _, failure := range []string{"operand", "resolve", "compile", "archive", "cleanup"} {
		f := newCLISuiteFixture(t)
		f.apps = []string{"crm"}
		args := []string{"build", "r1"}
		switch failure {
		case "operand":
			args = []string{"build"}
		case "resolve":
			f.noCommit = true
		case "compile":
			f.failCompile = "crm"
		case "archive":
			f.tarFails = true
		case "cleanup":
			f.removeFails = true
		}
		result := invokeWithDeps(f.deps(), args...)
		if result.code == 0 || result.stdout != "" {
			t.Fatalf("%s: %#v", failure, result)
		}
	}
}
