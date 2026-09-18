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
	assertResult(t, invokeWithDeps(deps, "--account", "work", "secrets", "push", "foo.sbx.ikigenba.dev", "bogus"), 2, "", "devctl: no app 'bogus' in the checkout\n")
	if cloudCalls != 0 {
		t.Fatalf("Deps.Cloud calls = %d, want none", cloudCalls)
	}
}

// R-D386-4WHC
func TestRotatedSecretsRequireDeployWithoutPushDeploying(t *testing.T) {
	const domain = "foo.sbx.ikigenba.dev"
	t.Run("push does no host operation or deployment", func(t *testing.T) {
		root := t.TempDir()
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
			Cloud: func(_ context.Context, _ string, region string) (cloud.Clients, error) {
				if region == "" {
					return cloud.Clients{SSM: &d05DispatchSSM{value: cliPropertiesJSON}}, nil
				}
				return cloud.Clients{SSM: regional, EC2: d05PushBoundaryEC2{domain: domain}, S3: d05ForbiddenS3{t: t}}, nil
			},
		}

		assertResult(t, invokeWithDeps(deps, "--account", "work", "secrets", "push", domain, "crm"), 0, "crm: ok (0 keys)\n", "")
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
			Cloud: func(_ context.Context, _ string, region string) (cloud.Clients, error) {
				if region == "" {
					return cloud.Clients{SSM: &d05DispatchSSM{value: cliPropertiesJSON}}, nil
				}
				return cloud.Clients{
					SSM: &d05DispatchSSM{value: `{}`},
					EC2: d05PushBoundaryEC2{domain: domain},
					S3:  upload,
				}, nil
			},
		}

		for range 2 {
			result := invokeWithDeps(deps, "--account", "work", "deploy", domain, artifactName)
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

func (ec2 d05PushBoundaryEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
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
