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
	value      string
	parameters []cloud.Parameter
	gets       int
	puts       int
}

func (ssm *d05DispatchSSM) GetParameter(context.Context, string) (string, error) {
	ssm.gets++
	return ssm.value, nil
}

func (ssm *d05DispatchSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return ssm.parameters, nil
}

func (ssm *d05DispatchSSM) PutSecureParameter(context.Context, string, string) error {
	ssm.puts++
	return nil
}

type d05DispatchSTS struct{ cloud.STS }

func (*d05DispatchSTS) CallerAccountID(context.Context) (string, error) {
	return "123456789012", nil
}

type d05DispatchEC2 struct {
	cloud.EC2
	instances []cloud.Instance
	roots     []string
}

func (ec2 *d05DispatchEC2) ListSpaceInstances(_ context.Context, root string) ([]cloud.Instance, error) {
	ec2.roots = append(ec2.roots, root)
	return ec2.instances, nil
}

func TestCLIDispatchesSecretsWithRootDepsAndStdout(t *testing.T) {
	// R-0C1D-1VCE
	root := t.TempDir()
	writeD05CLIRootFile(t, root)
	regional := &d05DispatchSSM{value: `{}`}
	var opens []d05CloudOpen
	deps := seam.Deps{
		EUID: 1,
		Dir:  root,
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "git" {
				t.Fatalf("secrets list command = %#v, want checkout discovery", command)
			}
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			opens = append(opens, d05CloudOpen{profile: profile, region: region})
			return cloud.Clients{STS: &d05DispatchSTS{}, SSM: regional}, nil
		},
	}
	assertResult(t, invokeWithDeps(deps, "secrets", "list", "sbx1", "crm"), 0, "crm -\n", "")
	if want := []d05CloudOpen{{profile: "ikigenba.dev", region: "us-east-2"}}; !reflect.DeepEqual(opens, want) {
		t.Fatalf("cloud opens = %#v, want %#v", opens, want)
	}
}

type d05CloudOpen struct {
	profile string
	region  string
}

func TestSecretsPushResolvesCheckoutAppBeforeCloud(t *testing.T) {
	// R-0N0G-HT0N
	root := t.TempDir()
	writeD05CLIFile(t, filepath.Join(root, "crm", "cmd", "crm", "main.go"), "package main\n")
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
	assertResult(t, invokeWithDeps(deps, "secrets", "push", "sbx1", "bogus"), 2, "", "devctl: no app 'bogus' in the checkout\n")
	if cloudCalls != 0 {
		t.Fatalf("Deps.Cloud calls = %d, want none", cloudCalls)
	}
}

func TestSecretsCLIParsesSpaceAndConnectsFromRootFile(t *testing.T) {
	// R-267W-YMZ0
	root := t.TempDir()
	writeD05CLIRootFile(t, root)
	writeD05CLIFile(t, filepath.Join(root, "crm", "cmd", "crm", "main.go"), "package main\n")
	writeD05CLIFile(t, filepath.Join(root, "crm", "etc", "manifest.toml"), "app = \"crm\"\n")

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "push label", args: []string{"secrets", "push", "sbx1", "crm"}, want: "crm: ok (0 keys)\n"},
		{name: "list domain", args: []string{"secrets", "list", "sbx1.ikigenba.dev", "crm"}, want: "crm -\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var opens []d05CloudOpen
			e2 := &d05DispatchEC2{instances: []cloud.Instance{{Space: "sbx1.ikigenba.dev", State: cloud.StateRunning}}}
			deps := d05CLIDeps(root, func(_ context.Context, profile, region string) (cloud.Clients, error) {
				opens = append(opens, d05CloudOpen{profile: profile, region: region})
				return cloud.Clients{STS: &d05DispatchSTS{}, EC2: e2, SSM: &d05DispatchSSM{value: `{}`}}, nil
			})
			assertResult(t, invokeWithDeps(deps, test.args...), 0, test.want, "")
			if want := []d05CloudOpen{{profile: "ikigenba.dev", region: "us-east-2"}}; !reflect.DeepEqual(opens, want) {
				t.Fatalf("cloud opens = %#v, want %#v", opens, want)
			}
		})
	}

	for _, subcommand := range []string{"push", "list"} {
		cloudCalls := 0
		deps := d05CLIDeps(root, func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, nil
		})
		assertResult(t, invokeWithDeps(deps, "secrets", subcommand, "crm.sbx1"), 2, "",
			"devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'\n")
		if cloudCalls != 0 {
			t.Fatalf("%s invalid operand opened cloud %d times", subcommand, cloudCalls)
		}
	}
}

func TestSecretsPushMissingSpaceStopsBeforeValuesAndWrites(t *testing.T) {
	// R-0O8C-VKRC
	root := t.TempDir()
	writeD05CLIRootFile(t, root)
	writeD05CLIFile(t, filepath.Join(root, "crm", "cmd", "crm", "main.go"), "package main\n")
	writeD05CLIFile(t, filepath.Join(root, "crm", "etc", "manifest.toml"), "app = \"crm\"\nsecrets = [\"TOKEN\"]\n")
	ssm := &d05DispatchSSM{}
	ec2 := &d05DispatchEC2{}
	valueLookups := 0
	deps := d05CLIDeps(root, func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{STS: &d05DispatchSTS{}, EC2: ec2, SSM: ssm}, nil
	})
	deps.Getenv = func(string) string {
		valueLookups++
		return "must-not-be-read"
	}
	assertResult(t, invokeWithDeps(deps, "secrets", "push", "gone", "crm"), 1, "", "devctl: no space at 'gone.ikigenba.dev'\n")
	if !reflect.DeepEqual(ec2.roots, []string{"ikigenba.dev"}) || valueLookups != 0 || ssm.puts != 0 {
		t.Fatalf("before missing-space refusal: roots=%q value lookups=%d writes=%d", ec2.roots, valueLookups, ssm.puts)
	}
}

func TestSecretsMalformedObjectIsSingleLineOperationFailure(t *testing.T) {
	// R-0UBU-SFGT
	root := t.TempDir()
	writeD05CLIRootFile(t, root)
	ssm := &d05DispatchSSM{value: `{"TOKEN":1}`}
	deps := d05CLIDeps(root, func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{STS: &d05DispatchSTS{}, SSM: ssm}, nil
	})
	assertResult(t, invokeWithDeps(deps, "secrets", "list", "sbx1", "crm"), 1, "",
		"devctl: /sbx1.ikigenba.dev/crm: not a JSON object of strings\n")
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

func writeD05CLIRootFile(t *testing.T, root string) {
	t.Helper()
	writeD05CLIFile(t, filepath.Join(root, "infra", "terraform.tfvars.json"),
		`{"domain":"ikigenba.dev","region":"us-east-2"}`)
}

func d05CLIDeps(root string, open cloud.Opener) seam.Deps {
	return seam.Deps{
		EUID:  1,
		Dir:   root,
		Cloud: open,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
	}
}
