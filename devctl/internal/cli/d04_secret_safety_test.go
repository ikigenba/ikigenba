package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestSecretValuesNeverReachCLIStreamsOrCommands(t *testing.T) {
	// R-WFYX-IAD9
	const sentinel = "secret-value-sentinel-fab8"

	t.Run("secrets push", func(t *testing.T) {
		root := writeD04SecretApp(t)
		regional := &sentinelSSM{}
		deps := secretSentinelDeps(root, sentinel, regional)
		var commands []seam.Cmd
		deps = recordD04Commands(deps, &commands)

		result := invokeWithDeps(deps,
			"--account", "work", "secrets", "push", testDomain, "crm")
		assertResult(t, result, 0, "crm: ok (1 keys)\n", "")
		assertD04SecretAbsent(t, sentinel, result, commands)
	})

	t.Run("space create", func(t *testing.T) {
		h := newCommandHarness(t)
		h.addAppWithSecrets("crm", "CRM_API_KEY")
		h.createRoleErr = errors.New("stop after secrets")
		deps := h.deps()
		deps.Getenv = func(name string) string {
			if name != "CRM_API_KEY" {
				t.Fatalf("Getenv(%q), want CRM_API_KEY", name)
			}
			return sentinel
		}
		var commands []seam.Cmd
		deps = recordD04Commands(deps, &commands)

		result := invokeWithDeps(deps, "--account", "sandbox", "space", "create",
			testDomain, "--acme-email", "admin@example.com")
		if result.code != 1 || !reflect.DeepEqual(h.mutations, []string{"ssm:put:crm", "iam:create-role"}) {
			t.Fatalf("result = %#v, mutations = %v; want stop after crm write", result, h.mutations)
		}
		assertD04SecretAbsent(t, sentinel, result, commands)
	})
}

func TestMissingManifestSecretHasContextualCLIDiagnostic(t *testing.T) {
	// R-WKUJ-1DC1
	const want = "devctl: crm: no value for 'CRM_API_KEY' in the keyring or the environment\n"

	t.Run("secrets push", func(t *testing.T) {
		root := writeD04SecretApp(t)
		regional := &sentinelSSM{}
		deps := secretSentinelDeps(root, "", regional)
		deps.Exec = func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			switch command.Path {
			case "git":
				return seam.Result{Stdout: []byte(root + "\n")}, nil
			case "secret-tool":
				return seam.Result{ExitCode: 1}, nil
			default:
				t.Fatalf("unexpected command: %#v", command)
				return seam.Result{}, nil
			}
		}

		assertResult(t, invokeWithDeps(deps,
			"--account", "work", "secrets", "push", testDomain, "crm"), 2, "", want)
	})

	t.Run("space create", func(t *testing.T) {
		h := newCommandHarness(t)
		h.addAppWithSecrets("crm", "CRM_API_KEY")
		assertResult(t, invokeWithDeps(h.deps(),
			"--account", "sandbox", "space", "create", testDomain,
			"--acme-email", "admin@example.com"), 2, "", want)
	})
}

func writeD04SecretApp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeD05CLIFile(t, filepath.Join(root, "crm", "cmd", "crm", "main.go"), "package main\n")
	writeD05CLIFile(t, filepath.Join(root, "crm", "etc", "manifest.toml"),
		"app = \"crm\"\nsecrets = [\"CRM_API_KEY\"]\n")
	return root
}

func recordD04Commands(deps seam.Deps, commands *[]seam.Cmd) seam.Deps {
	run := deps.Exec
	deps.Exec = func(ctx context.Context, command seam.Cmd) (seam.Result, error) {
		command.Args = append([]string(nil), command.Args...)
		command.Env = append([]string(nil), command.Env...)
		*commands = append(*commands, command)
		return run(ctx, command)
	}
	return deps
}

func assertD04SecretAbsent(t *testing.T, sentinel string, result runResult, commands []seam.Cmd) {
	t.Helper()
	if strings.Contains(result.stdout, sentinel) || strings.Contains(result.stderr, sentinel) {
		t.Fatalf("secret %q exposed in stdout %q or stderr %q", sentinel, result.stdout, result.stderr)
	}
	for index, command := range commands {
		fields := []struct {
			name  string
			value string
		}{
			{name: "Path", value: command.Path},
			{name: "Args", value: fmt.Sprint(command.Args)},
			{name: "Dir", value: command.Dir},
			{name: "Env", value: fmt.Sprint(command.Env)},
		}
		for _, field := range fields {
			if strings.Contains(field.value, sentinel) {
				t.Fatalf("secret %q exposed in command %d %s: %q", sentinel, index, field.name, field.value)
			}
		}
	}
}
