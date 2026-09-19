package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const d09RestoreHelp = `Usage: devctl restore <space> <app> [--at <timestamp>]

Have opsctl on the space put <app> back from the space's own backups. The
app's etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s unit is stopped for the restore and
started again after it.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
`

func TestCLIDispatchesDeployAndFormatsHostFailure(t *testing.T) {
	// R-O94A-735M R-OLBA-0SKK
	root := d09Root(t)
	name := "gmail-v0.1.0.tar.xz"
	if err := os.WriteFile(filepath.Join(root, name), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := d09Deps(t, root, 0, "")
	result := invokeWithDeps(deps, "deploy", "sbx1", name)
	wantOut := "file: ok (gmail v0.1.0)\nsecrets: ok (2 keys)\nupload: ok (-> ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz)\ninstall: ok (opsctl installed gmail)\n"
	assertResult(t, result, 0, wantOut, "")

	deps = d09Deps(t, root, 1, "install failed\nmore\n")
	result = invokeWithDeps(deps, "deploy", "sbx1", name)
	wantOut = "file: ok (gmail v0.1.0)\nsecrets: ok (2 keys)\nupload: ok (-> ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz)\n"
	wantErr := "devctl: install: ssh ec2-user@18.118.7.42 sudo opsctl install s3://ikigenba.dev/sbx1/deploy/gmail-v0.1.0.tar.xz: exit status 1\n\n> install failed\n> more\n"
	assertResult(t, result, 1, wantOut, wantErr)
}

func TestCLIDispatchesRestoreAndFormatsHostFailure(t *testing.T) {
	// R-OSMO-BF0Q R-OXI9-UHZI
	root := d09Root(t)
	deps := d09Deps(t, root, 1, "restore failed\n")
	result := invokeWithDeps(deps, "restore", "sbx1", "crm")
	wantErr := "devctl: restore: ssh ec2-user@18.118.7.42 sudo opsctl restore crm: exit status 1\n\n> restore failed\n"
	assertResult(t, result, 1, "", wantErr)

	deps = d09Deps(t, root, 0, "")
	result = invokeWithDeps(deps, "restore", "sbx1", "crm", "--at", "2026-09-11T18:00:00Z")
	assertResult(t, result, 0, "restore: ok (opsctl restore crm --at 2026-09-11T18:00:00Z)\n", "")
}

func TestD09CommandBoundaryEarlyResultsOutsideCheckout(t *testing.T) {
	// R-OBK2-YMN0 R-OOYZ-63SN R-ORER-XNA1
	outside := t.TempDir()
	for _, option := range []string{"--help", "-h"} {
		result := invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "restore", option)
		assertResult(t, result, 0, d09RestoreHelp, "")
	}

	result := invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "restore", "sbx1")
	assertResult(t, result, 2, "", "devctl: restore needs <space> and <app>\n\nsee 'devctl restore --help' for usage\n")

	result = invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "deploy", "sbx1", "crm/dist/crm-v0.2.0.tar.xz")
	assertResult(t, result, 2, "", "devctl: no such file 'crm/dist/crm-v0.2.0.tar.xz'\n")

	if err := os.WriteFile(filepath.Join(outside, "notes.tar.xz"), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	result = invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "deploy", "sbx1", "notes.tar.xz")
	assertResult(t, result, 2, "", "devctl: 'notes.tar.xz' is not a file build wrote: name is not <app>-v<semver>.tar.xz\n")
}

func TestD09CommandBoundaryMissingAndStoppedSpaces(t *testing.T) {
	// R-OCRZ-CEDP R-OTUK-P6RF
	root := d09Root(t)
	name := "gmail-v0.1.0.tar.xz"
	if err := os.WriteFile(filepath.Join(root, name), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, operand, diagnostic string
		instances                 []cloud.Instance
	}{
		{name: "missing", operand: "gone", diagnostic: "devctl: no space at 'gone.ikigenba.dev'\n", instances: []cloud.Instance{}},
		{name: "stopped", operand: "sbx2", diagnostic: "devctl: 'sbx2.ikigenba.dev' is stopped\n", instances: []cloud.Instance{{ID: "i-2", Space: "sbx2.ikigenba.dev", State: cloud.StateStopped}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps := d09DepsWithInstances(t, root, test.instances, 0, "")
			assertResult(t, invokeWithDeps(deps, "deploy", test.operand, name), 1, "file: ok (gmail v0.1.0)\n", test.diagnostic)
			assertResult(t, invokeWithDeps(deps, "restore", test.operand, "crm"), 1, "", test.diagnostic)
		})
	}
}

func d09Root(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func d09Deps(t *testing.T, root string, sshStatus int, sshStderr string) seam.Deps {
	return d09DepsWithInstances(t, root, nil, sshStatus, sshStderr)
}

func d09DepsWithInstances(t *testing.T, root string, instances []cloud.Instance, sshStatus int, sshStderr string) seam.Deps {
	t.Helper()
	return seam.Deps{EUID: 1, Dir: root, Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		switch cmd.Path {
		case "tar":
			if cmd.Args[0] == "-t" {
				return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/gmail\n")}, nil
			}
			return seam.Result{Stdout: []byte("app = \"gmail\"\nsecrets = [\"A\", \"B\"]\n")}, nil
		case "git":
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		case "ssh":
			return seam.Result{ExitCode: sshStatus, Stderr: []byte(sshStderr)}, nil
		default:
			t.Fatalf("unexpected command %#v", cmd)
			return seam.Result{}, nil
		}
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{STS: d09STS{}, EC2: d09EC2{instances: instances}, SSM: d09SSM{}, S3: d09S3{}}, nil
	}}
}

type d09STS struct{ cloud.STS }

func (d09STS) CallerAccountID(context.Context) (string, error) { return "123456789012", nil }

type d09EC2 struct {
	cloud.EC2
	instances []cloud.Instance
}

func (f d09EC2) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	if f.instances != nil {
		return f.instances, nil
	}
	return []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "18.118.7.42"}}, nil
}

type d09SSM struct{ cloud.SSM }

func (d09SSM) GetParameter(context.Context, string) (string, error) { return `{"A":"a","B":"b"}`, nil }

type d09S3 struct{ cloud.S3 }

func (d09S3) PutObject(context.Context, string, string, io.Reader, int64) error { return nil }

func d09FailExec(t *testing.T) seam.Runner {
	t.Helper()
	return func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected process access")
		return seam.Result{}, errors.New("unexpected process access")
	}
}

func d09FailCloud(t *testing.T) cloud.Opener {
	t.Helper()
	return func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud access")
		return cloud.Clients{}, errors.New("unexpected cloud access")
	}
}
