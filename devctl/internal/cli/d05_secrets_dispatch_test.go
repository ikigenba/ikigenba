package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type d05DispatchSSM struct {
	cloud.SSM
	value string
}

func (ssm *d05DispatchSSM) GetParameter(context.Context, string) (string, error) {
	return ssm.value, nil
}

func TestCLIDispatchesSecretsWithProfileDepsAndStdout(t *testing.T) {
	// R-FXS5-PVIB
	regional := &d05DispatchSSM{value: `{}`}
	var profiles []string
	deps := seam.Deps{
		EUID: 1,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			t.Fatal("secrets list called the process seam")
			return seam.Result{}, nil
		},
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			profiles = append(profiles, profile)
			if region == "" {
				return cloud.Clients{SSM: &d05DispatchSSM{value: cliPropertiesJSON}}, nil
			}
			return cloud.Clients{SSM: regional}, nil
		},
	}
	assertResult(t, invokeWithDeps(deps, "--account", "work-profile", "secrets", "list", "foo.sbx.ikigenba.dev", "crm"), 0, "crm -\n", "")
	if want := []string{"work-profile", "work-profile"}; !reflect.DeepEqual(profiles, want) {
		t.Fatalf("cloud profiles = %q, want %q", profiles, want)
	}
}

func TestSecretsPushResolvesCheckoutAppBeforeCloud(t *testing.T) {
	// R-GB71-XCNY
	root := t.TempDir()
	writeD05CLIFile(t, filepath.Join(root, "crm", "main.go"), "package main\n")
	writeD05CLIFile(t, filepath.Join(root, "crm", "etc", "manifest.toml"), "app = \"crm\"\n")

	cloudCalls := 0
	deps := seam.Deps{
		EUID: 1,
		Dir:  root,
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "git" || !reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}) {
				t.Fatalf("checkout command = %#v", command)
			}
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, nil
		},
	}
	assertResult(t, invokeWithDeps(deps, "--account", "work", "secrets", "push", "foo.sbx.ikigenba.dev", "bogus"), 2, "", "devctl: no app 'bogus' in the checkout\n")
	if cloudCalls != 0 {
		t.Fatalf("Deps.Cloud calls = %d, want none", cloudCalls)
	}
}

func writeD05CLIFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
