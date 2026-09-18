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

func TestCLIDispatchesDeployArgumentsAndStdout(t *testing.T) {
	// R-Z4J0-Z7NA R-TI90-6NW4
	const profile = "SelectedProfile"
	const domain = "deploy.example.test"
	const artifactName = "crm-v1.2.3.tar.xz"
	artifact := []byte("injected artifact bytes")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, artifactName), artifact, 0o600); err != nil {
		t.Fatal(err)
	}

	var profiles []string
	var commands []seam.Cmd
	upload := &d09S3{}
	deps := seam.Deps{
		EUID: 1,
		Dir:  dir,
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command)
			switch command.Path {
			case "tar":
				if command.Args[0] == "-t" {
					return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/crm\n")}, nil
				}
				return seam.Result{Stdout: []byte("app = \"crm\"\n")}, nil
			case "ssh":
				return seam.Result{}, nil
			default:
				t.Fatalf("unexpected command %#v", command)
				return seam.Result{}, errors.New("unreachable")
			}
		},
		Cloud: func(_ context.Context, gotProfile, region string) (cloud.Clients, error) {
			profiles = append(profiles, gotProfile)
			switch region {
			case "":
				return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
			case "us-test-1":
				return cloud.Clients{
					EC2: &d09EC2{instances: []cloud.Instance{{
						ID:      "i-deploy",
						Space:   domain,
						State:   cloud.StateRunning,
						Address: "192.0.2.45",
					}}},
					SSM: &cliSSM{value: `{}`},
					S3:  upload,
				}, nil
			default:
				t.Fatalf("unexpected region %q", region)
				return cloud.Clients{}, errors.New("unreachable")
			}
		},
	}

	result := invokeWithDeps(deps, "--account", profile, "deploy", domain, artifactName)
	wantStdout := "file: ok (crm v1.2.3)\n" +
		"secrets: ok (0 keys)\n" +
		"upload: ok (-> backups/" + domain + "/deploy/" + artifactName + ")\n" +
		"install: ok (opsctl installed crm)\n"
	assertResult(t, result, 0, wantStdout, "")
	if want := []string{profile, profile}; !reflect.DeepEqual(profiles, want) {
		t.Fatalf("Cloud profiles = %#v, want %#v", profiles, want)
	}
	wantCommands := []seam.Cmd{
		{Path: "tar", Args: []string{"-t", "-J", "-f", artifactName}, Dir: dir},
		{Path: "tar", Args: []string{"-x", "-J", "-O", "-f", artifactName, "etc/manifest.toml"}, Dir: dir},
		{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@192.0.2.45", "'sudo' 'opsctl' 'install' 's3://backups/" + domain + "/deploy/" + artifactName + "'"}, Dir: dir},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	if upload.bucket != "backups" || upload.key != domain+"/deploy/"+artifactName ||
		upload.size != int64(len(artifact)) || !bytes.Equal(upload.body, artifact) {
		t.Fatalf("upload = bucket %q key %q size %d body %q", upload.bucket, upload.key, upload.size, upload.body)
	}
}

type d09S3 struct {
	cloud.S3
	bucket string
	key    string
	body   []byte
	size   int64
}

func (fake *d09S3) PutObject(_ context.Context, bucket, key string, body io.Reader, size int64) error {
	contents, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	fake.bucket, fake.key, fake.size = bucket, key, size
	fake.body = contents
	return nil
}

type d09EC2 struct {
	cloud.EC2
	instances []cloud.Instance
}

func (fake *d09EC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return fake.instances, nil
}

func TestCLIDispatchesRestoreWithProfileDepsAndStdout(t *testing.T) {
	// R-FSS4-QJSW
	const profile = "SelectedProfile"
	const domain = "restore.example.test"
	var profiles []string
	var commands []seam.Cmd
	deps := seam.Deps{
		EUID: 1,
		Dir:  t.TempDir(),
		Cloud: func(_ context.Context, gotProfile, region string) (cloud.Clients, error) {
			profiles = append(profiles, gotProfile)
			switch region {
			case "":
				return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
			case "us-test-1":
				return cloud.Clients{EC2: &d09EC2{instances: []cloud.Instance{{
					ID:      "i-restore",
					Space:   domain,
					State:   cloud.StateRunning,
					Address: "192.0.2.44",
				}}}}, nil
			default:
				t.Fatalf("unexpected region %q", region)
				return cloud.Clients{}, errors.New("unreachable")
			}
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command)
			return seam.Result{}, nil
		},
	}

	result := invokeWithDeps(deps, "--account", profile, "restore", domain, "crm")
	assertResult(t, result, 0, "restore: ok (opsctl restore crm)\n", "")
	if !reflect.DeepEqual(profiles, []string{profile, profile}) {
		t.Fatalf("Cloud profiles = %#v, want selected profile twice", profiles)
	}
	if len(commands) != 1 || commands[0].Path != "ssh" {
		t.Fatalf("commands = %#v, want one ssh command", commands)
	}
}
