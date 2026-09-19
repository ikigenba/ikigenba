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

func TestDeploySeparatesCheckoutAndWorkingDirectoryPaths(t *testing.T) {
	// R-QG5U-LTSE
	checkoutRoot := t.TempDir()
	workingDir := filepath.Join(checkoutRoot, "nested", "work")
	if err := os.MkdirAll(filepath.Join(checkoutRoot, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workingDir, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(checkoutRoot, "infra", "terraform.tfvars.json"),
		[]byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workingDir, "infra", "terraform.tfvars.json"),
		[]byte(`{"domain":"wrong.example"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	const operand = "gmail-v0.1.0.tar.xz"
	if err := os.WriteFile(filepath.Join(workingDir, operand), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []seam.Cmd
	deps := seam.Deps{
		EUID: 1,
		Dir:  workingDir,
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command)
			switch command.Path {
			case "tar":
				if command.Args[0] == "-t" {
					return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/gmail\n")}, nil
				}
				return seam.Result{Stdout: []byte("app = \"gmail\"\nsecrets = [\"A\", \"B\"]\n")}, nil
			case "git":
				return seam.Result{Stdout: []byte(checkoutRoot + "\n")}, nil
			case "ssh":
				return seam.Result{}, nil
			default:
				t.Fatalf("unexpected command %#v", command)
				return seam.Result{}, nil
			}
		},
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			if profile != "ikigenba.dev" || region != "us-east-2" {
				t.Fatalf("cloud identity = (%q, %q), want checkout root file values", profile, region)
			}
			return cloud.Clients{
				STS: d09STS{},
				EC2: d09EC2{},
				SSM: d09SSM{},
				S3:  d09S3{},
			}, nil
		},
	}

	result := invokeWithDeps(deps, "deploy", "sbx1", operand)
	wantOut := "file: ok (gmail v0.1.0)\nsecrets: ok (2 keys)\nupload: ok (-> ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz)\ninstall: ok (opsctl installed gmail)\n"
	assertResult(t, result, 0, wantOut, "")

	wantPrefix := []seam.Cmd{
		{Path: "tar", Args: []string{"-t", "-J", "-f", operand}, Dir: workingDir},
		{Path: "tar", Args: []string{"-x", "-J", "-O", "-f", operand, "etc/manifest.toml"}, Dir: workingDir},
		{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: workingDir},
	}
	if len(commands) != 4 || !reflect.DeepEqual(commands[:3], wantPrefix) {
		t.Fatalf("commands = %#v, want prefix %#v followed by ssh", commands, wantPrefix)
	}
	if commands[3].Path != "ssh" || commands[3].Dir != workingDir {
		t.Fatalf("host command = %#v, want ssh from working directory", commands[3])
	}
}
