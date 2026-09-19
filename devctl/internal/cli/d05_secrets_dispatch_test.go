package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	// R-0Z7G-BIFL
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

// R-D386-4WHC
func TestRotatedSecretsRequireDeployWithoutPushDeploying(t *testing.T) {
	const domain = "foo.ikigenba.dev"
	t.Run("push does no host operation or deployment", func(t *testing.T) {
		root := t.TempDir()
		writeD05CLIRootFile(t, root)
		writeD05CLIFile(t, filepath.Join(root, "crm", "cmd", "crm", "main.go"), "package main\n")
		writeD05CLIFile(t, filepath.Join(root, "crm", "etc", "manifest.toml"), "app = \"crm\"\n")

		regional := &d05PushBoundarySSM{}
		var commands []seam.Cmd
		deps := seam.Deps{
			EUID: 1,
			Dir:  root,
			Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				commands = append(commands, command)
				if command.Path != "git" {
					return seam.Result{}, errors.New("unexpected host operation")
				}
				return seam.Result{Stdout: []byte(root + "\n")}, nil
			},
			Cloud: func(_ context.Context, _, _ string) (cloud.Clients, error) {
				return cloud.Clients{STS: &d05DispatchSTS{}, SSM: regional, EC2: d05PushBoundaryEC2{domain: domain}, S3: d05ForbiddenS3{t: t}}, nil
			},
		}

		assertResult(t, invokeWithDeps(deps, "secrets", "push", domain, "crm"), 0, "crm: ok (0 keys)\n", "")
		if regional.puts != 1 {
			t.Fatalf("secret writes = %d, want one", regional.puts)
		}
		if len(commands) != 1 || commands[0].Path != "git" {
			t.Fatalf("process commands = %#v, want checkout discovery only", commands)
		}
	})

	t.Run("same installed version still uploads and installs", func(t *testing.T) {
		const artifactName = "crm-v1.2.3.tar.xz"
		root := t.TempDir()
		writeD05CLIRootFile(t, root)
		artifact := []byte("same artifact bytes")
		if err := os.WriteFile(filepath.Join(root, artifactName), artifact, 0o600); err != nil {
			t.Fatal(err)
		}

		upload := &d05CountingS3{}
		var installs int
		deps := seam.Deps{
			EUID: 1,
			Dir:  root,
			Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				switch command.Path {
				case "git":
					return seam.Result{Stdout: []byte(root + "\n")}, nil
				case "tar":
					if command.Args[0] == "-t" {
						return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/crm\n")}, nil
					}
					return seam.Result{Stdout: []byte("app = \"crm\"\n")}, nil
				case "ssh":
					installs++
					return seam.Result{}, nil
				default:
					t.Fatalf("unexpected command %#v", command)
					return seam.Result{}, errors.New("unreachable")
				}
			},
			Cloud: func(_ context.Context, _, _ string) (cloud.Clients, error) {
				return cloud.Clients{
					STS: &d05DispatchSTS{},
					SSM: &d05DispatchSSM{value: `{}`},
					EC2: d05PushBoundaryEC2{domain: domain},
					S3:  upload,
				}, nil
			},
		}

		for range 2 {
			result := invokeWithDeps(deps, "deploy", domain, artifactName)
			if result.code != 0 {
				t.Fatalf("repeated deploy failed: %#v", result)
			}
		}
		if upload.puts != 2 || installs != 2 {
			t.Fatalf("two deploys produced uploads %d installs %d, want 2 and 2", upload.puts, installs)
		}
	})
}

type d05PushBoundarySSM struct {
	cloud.SSM
	puts int
}

func (ssm *d05PushBoundarySSM) PutSecureParameter(context.Context, string, string) error {
	ssm.puts++
	return nil
}

type d05PushBoundaryEC2 struct {
	cloud.EC2
	domain string
}

func (ec2 d05PushBoundaryEC2) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return []cloud.Instance{{Space: ec2.domain, State: cloud.StateRunning, Address: "192.0.2.50"}}, nil
}

type d05ForbiddenS3 struct {
	cloud.S3
	t *testing.T
}

func (s3 d05ForbiddenS3) PutObject(context.Context, string, string, io.Reader, int64) error {
	s3.t.Fatal("secrets push uploaded a deployment artifact")
	return nil
}

type d05CountingS3 struct {
	cloud.S3
	puts int
}

func (s3 *d05CountingS3) PutObject(_ context.Context, _, _ string, body io.Reader, size int64) error {
	contents, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(contents)) != size || !bytes.Equal(contents, []byte("same artifact bytes")) {
		return errors.New("unexpected uploaded artifact")
	}
	s3.puts++
	return nil
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
