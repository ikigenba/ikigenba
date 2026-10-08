package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const d09RestoreHelp = `Usage: devctl restore <space> <app> [--at <timestamp>]

Have opsctl on the space put <app> back from the space's own backups. The
app's etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s socket and service are stopped for the
restore and started again after it, unless <app> is disabled.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
`

func TestCLIDispatchesDeployAndFormatsHostFailure(t *testing.T) {
	// R-VQ0L-U00U
	root := d09Root(t)
	name := "gmail-c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9.tar.xz"
	if err := os.WriteFile(filepath.Join(root, name), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := d09Deps(t, root, 0)
	var ssh []seam.Cmd
	baseExec := deps.Exec
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "ssh" {
			ssh = append(ssh, cmd)
			return seam.Result{Stdout: []byte("installed\n"), Stderr: []byte("notice\n")}, nil
		}
		return baseExec(ctx, cmd)
	}
	result := invokeWithDeps(deps, "deploy", "sbx1", name)
	wantOut := "file: ok (gmail c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9)\nsecrets: ok (2 keys)\nupload: ok (-> ikigenba.dev/sbx1/deploy/gmail-c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9.tar.xz)\ninstall: ok (opsctl installed gmail)\n"
	assertResult(t, result, 0, wantOut, "")
	if len(ssh) != 1 || ssh[0].Path != "ssh" || ssh[0].Args[6] != "ec2-user@18.118.7.42" || ssh[0].Args[7] != "'sudo' 'opsctl' 'install' 's3://ikigenba.dev/sbx1/deploy/gmail-c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9.tar.xz'" {
		t.Fatalf("install command = %#v", ssh)
	}

	deps = d09Deps(t, root, 1)
	baseExec = deps.Exec
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "ssh" {
			return seam.Result{ExitCode: 1, Stdout: []byte("install failed\nmore output\n"), Stderr: []byte("permission denied\nmore error\n")}, nil
		}
		return baseExec(ctx, cmd)
	}
	result = invokeWithDeps(deps, "deploy", "sbx1", name)
	wantOut = "file: ok (gmail c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9)\nsecrets: ok (2 keys)\nupload: ok (-> ikigenba.dev/sbx1/deploy/gmail-c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9.tar.xz)\n"
	wantErr := "devctl: install: ssh ec2-user@18.118.7.42 sudo opsctl install s3://ikigenba.dev/sbx1/deploy/gmail-c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9.tar.xz: exit status 1\n\n> install failed\n> more output\n> permission denied\n> more error\n"
	assertResult(t, result, 1, wantOut, wantErr)
}

func TestCLIDispatchesRestoreAndFormatsHostFailure(t *testing.T) {
	// R-OSMO-BF0Q
	root := d09Root(t)
	deps := d09Deps(t, root, 1)
	baseExec := deps.Exec
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "ssh" {
			return seam.Result{ExitCode: 1, Stdout: []byte("restore failed\nmore output\n"), Stderr: []byte("backup missing\nmore error\n")}, nil
		}
		return baseExec(ctx, cmd)
	}
	result := invokeWithDeps(deps, "restore", "sbx1", "crm")
	wantErr := "devctl: restore: ssh ec2-user@18.118.7.42 sudo opsctl restore crm: exit status 1\n\n> restore failed\n> more output\n> backup missing\n> more error\n"
	assertResult(t, result, 1, "", wantErr)

	deps = d09Deps(t, root, 0)
	result = invokeWithDeps(deps, "restore", "sbx1", "crm", "--at", "2026-09-11T18:00:00Z")
	assertResult(t, result, 0, "restore: ok (opsctl restore crm --at 2026-09-11T18:00:00Z)\n", "")
}

func TestD09CommandBoundaryEarlyResultsOutsideCheckout(t *testing.T) {
	// R-VXC0-4MH0 R-JW73-1HBN R-ORER-XNA1
	outside := t.TempDir()
	for _, option := range []string{"--help", "-h"} {
		result := invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "restore", option)
		assertResult(t, result, 0, d09RestoreHelp, "")
	}
	assertResult(t, invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "restore", "sbx1", "crm", "--help"), 0, d09RestoreHelp, "")

	result := invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "restore", "sbx1")
	assertResult(t, result, 2, "", "devctl: restore needs <space> and <app>\n\nsee 'devctl restore --help' for usage\n")

	result = invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "deploy", "sbx1", "crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz")
	assertResult(t, result, 2, "", "devctl: no such file 'crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz'\n")

	if err := os.WriteFile(filepath.Join(outside, "notes.tar.xz"), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	result = invokeWithDeps(seam.Deps{EUID: 1, Dir: outside, Exec: d09FailExec(t), Cloud: d09FailCloud(t)}, "deploy", "sbx1", "notes.tar.xz")
	assertResult(t, result, 2, "", "devctl: 'notes.tar.xz' is not an app file build wrote: name is not <app>-<sha>.tar.xz\n")
}

func TestD09CommandBoundaryMissingAndStoppedSpaces(t *testing.T) {
	// R-5WJ3-HB0C R-2B3I-HPXS
	root := d09Root(t)
	name := "gmail-c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9.tar.xz"
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
			deps := d09DepsWithInstances(t, root, test.instances, 0)
			assertResult(t, invokeWithDeps(deps, "deploy", test.operand, name), 1, "file: ok (gmail c3d5e7f9a1b2c4d6e8f0a2b4c6d8e0f1a3b5c7d9)\n", test.diagnostic)
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

func d09Deps(t *testing.T, root string, sshStatus int) seam.Deps {
	return d09DepsWithInstances(t, root, nil, sshStatus)
}

func d09DepsWithInstances(t *testing.T, root string, instances []cloud.Instance, sshStatus int) seam.Deps {
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
			return seam.Result{ExitCode: sshStatus}, nil
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

const deploySHA = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"

type deployReleaseFixture struct {
	t            *testing.T
	root         string
	commands     []seam.Cmd
	manifests    map[string]string
	held         map[string]string
	secretCalls  []string
	state        cloud.InstanceState
	missingSpace bool
	fail         string
	metadata     string
}

func newDeployRelease(t *testing.T) *deployReleaseFixture {
	f := &deployReleaseFixture{t: t, root: d09Root(t), manifests: map[string]string{}, held: map[string]string{"auth": `{"GOOGLE_CLIENT_ID":"id","GOOGLE_CLIENT_SECRET":"secret","EXTRA":"unused"}`}, state: cloud.StateRunning}
	for _, app := range []string{"auth", "dashboard", "events", "mcp", "repos", "scripts", "sites", "telemetry"} {
		f.manifests[app] = "app = \"" + app + "\"\n"
	}
	f.manifests["auth"] += "secrets = [\"GOOGLE_CLIENT_SECRET\",\"GOOGLE_CLIENT_ID\",\"GOOGLE_CLIENT_ID\"]\n"
	return f
}
func (f *deployReleaseFixture) put(path, text string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *deployReleaseFixture) deps() seam.Deps {
	return seam.Deps{EUID: 1, Dir: f.root, Getenv: func(string) string { return "" }, Now: func() time.Time { return time.Unix(0, 0) }, Exec: f.exec, Stream: func(_ context.Context, c seam.Cmd, w io.Writer) (seam.Result, error) {
		f.commands = append(f.commands, c)
		if f.fail == "activate" {
			return seam.Result{ExitCode: 7, Stderr: []byte("activation failed\n")}, nil
		}
		_, err := io.WriteString(w, "activated\nprogress as produced\n")
		return seam.Result{}, err
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		if f.fail == "cloud" {
			f.t.Fatal("cloud called after resolve failed")
		}
		instances := []cloud.Instance{{ID: "i-1", Space: "sbx1.ikigenba.dev", State: f.state, Address: "18.118.7.42"}}
		if f.missingSpace {
			instances = []cloud.Instance{}
		}
		return cloud.Clients{STS: d09STS{}, EC2: d09EC2{instances: instances}, SSM: deployReleaseSSM{f: f}, S3: deployNoS3{t: f.t}}, nil
	}}
}
func (f *deployReleaseFixture) exec(_ context.Context, c seam.Cmd) (seam.Result, error) {
	f.commands = append(f.commands, c)
	switch c.Path {
	case "git":
		if c.Args[0] == "rev-parse" {
			if c.Args[1] == "--show-toplevel" {
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			if f.fail == "cloud" {
				if strings.Contains(c.Args[len(c.Args)-1], "main") && !strings.Contains(c.Args[len(c.Args)-1], "refs/tags/main") {
					return seam.Result{Stdout: []byte(deploySHA + "\n")}, nil
				}
				return seam.Result{ExitCode: 1}, nil
			}
			return seam.Result{Stdout: []byte(deploySHA + "\n")}, nil
		}
		if c.Args[0] == "worktree" && c.Args[1] == "add" {
			for app, manifest := range f.manifests {
				f.put(filepath.Join(c.Args[3], app, "etc/manifest.toml"), manifest)
				f.put(filepath.Join(c.Args[3], app, "cmd", app, "main.go"), "package main\n")
			}
			return seam.Result{}, nil
		}
		if c.Args[0] == "worktree" && c.Args[1] == "remove" {
			return seam.Result{}, os.RemoveAll(c.Args[3])
		}
	case "go":
		if f.fail == "build" && filepath.Base(c.Dir) == "dashboard" {
			return seam.Result{ExitCode: 1, Stderr: []byte("# github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\ncmd/dashboard/main.go:41:2: undefined: render\n")}, nil
		}
		f.put(c.Args[3], "binary")
		return seam.Result{}, nil
	case "tar":
		data, err := os.ReadFile(filepath.Join(c.Args[3], deploySHA, "release.json"))
		if err != nil {
			f.t.Fatal(err)
		}
		f.metadata = string(data)
		f.put(c.Args[1], "archive")
		return seam.Result{}, nil
	case "scp":
		if f.fail == "copy" {
			return seam.Result{ExitCode: 9, Stderr: []byte("copy failed\n")}, nil
		}
		return seam.Result{}, nil
	case "ssh":
		remote := c.Args[len(c.Args)-1]
		if strings.Contains(remote, "'test' '-e'") {
			return seam.Result{ExitCode: 1}, nil
		}
		if remote == "'mktemp'" {
			return seam.Result{Stdout: []byte("/tmp/release\n")}, nil
		}
		if strings.Contains(remote, "'mktemp' '-d'") {
			return seam.Result{Stdout: []byte("/opt/ikigenba/releases/.unpack.fake\n")}, nil
		}
		return seam.Result{}, nil
	default:
		app := filepath.Base(c.Path)
		if manifest, ok := f.manifests[app]; ok && reflect.DeepEqual(c.Args, []string{"manifest"}) {
			return seam.Result{Stdout: []byte(manifest)}, nil
		}
	}
	f.t.Fatalf("unexpected process %#v", c)
	return seam.Result{}, nil
}

type deployReleaseSSM struct {
	cloud.SSM
	f *deployReleaseFixture
}

func (s deployReleaseSSM) GetParameter(_ context.Context, path string) (string, error) {
	s.f.secretCalls = append(s.f.secretCalls, path)
	return s.f.held[filepath.Base(path)], nil
}

type deployNoS3 struct {
	cloud.S3
	t *testing.T
}

func (s deployNoS3) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	s.t.Fatal("S3 ListObjects")
	return nil, nil
}
func (s deployNoS3) PutObject(context.Context, string, string, io.Reader, int64) error {
	s.t.Fatal("S3 PutObject")
	return nil
}
func (s deployNoS3) DeleteObjects(context.Context, string, []string) error {
	s.t.Fatal("S3 DeleteObjects")
	return nil
}
func (s deployNoS3) CopyObject(context.Context, string, string, string) error {
	s.t.Fatal("S3 CopyObject")
	return nil
}

func TestDeployReleaseBuildSecretsCopyAndActivate(t *testing.T) {
	// R-VQ0L-U00U R-VR8I-7RRJ R-W9IZ-YBVY R-WAQW-C3MN R-WBYS-PVDC R-WD6P-3N41
	for _, operand := range []string{"r1", "auth/v0.18.2", "4b22285"} {
		t.Run(operand, func(t *testing.T) {
			f := newDeployRelease(t)
			version := invokeWithDeps(f.deps(), "--version").stdout
			result := invokeWithDeps(f.deps(), "deploy", "sbx1", operand)
			label := ""
			if operand != "4b22285" {
				label = operand + ", "
			}
			want := "build: ok (" + label + "dist/" + deploySHA + ".tar.xz)\nsecrets: ok (8 apps, 2 keys)\ncopy: ok (" + deploySHA + ".tar.xz -> 18.118.7.42)\nunpack: ok (/opt/ikigenba/releases/" + deploySHA + ")\nactivated\nprogress as produced\n"
			assertResult(t, result, 0, want, "")
			if !reflect.DeepEqual(f.secretCalls, []string{"/sbx1.ikigenba.dev/auth"}) {
				t.Fatalf("secret calls %#v", f.secretCalls)
			}
			var metadata struct {
				Devctl string `json:"devctl"`
			}
			if err := json.Unmarshal([]byte(f.metadata), &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.Devctl != strings.TrimSuffix(version, "\n") {
				t.Fatalf("version %q want %q", metadata.Devctl, version)
			}
			last := f.commands[len(f.commands)-1]
			wantRemote := "'sudo' '/opt/ikigenba/releases/" + deploySHA + "/opsctl/bin/opsctl' 'activate' '" + deploySHA + "'"
			if label != "" {
				wantRemote += " '" + operand + "'"
			}
			if last.Args[len(last.Args)-1] != wantRemote {
				t.Fatalf("activation %#v", last)
			}
		})
	}
}

func TestDeployReleaseRefAndSpaceFailuresBuildNothing(t *testing.T) {
	// R-W8B3-KK59
	for _, operand := range []string{"r9", "main"} {
		f := newDeployRelease(t)
		f.fail = "cloud"
		assertResult(t, invokeWithDeps(f.deps(), "deploy", "sbx1", operand), 2, "", "devctl: '"+operand+"' is not a commit\n")
		if len(f.commands) != 2 {
			t.Fatalf("commands %#v", f.commands)
		}
	}
	for _, kind := range []string{"missing", "stopped"} {
		f := newDeployRelease(t)
		message := "devctl: no space at 'gone.ikigenba.dev'\n"
		operand := "gone"
		f.missingSpace = true
		if kind == "stopped" {
			f.missingSpace = false
			f.state = cloud.StateStopped
			operand = "sbx1"
			message = "devctl: 'sbx1.ikigenba.dev' is stopped\n"
		}
		assertResult(t, invokeWithDeps(f.deps(), "deploy", operand, "r1"), 1, "", message)
		if len(f.commands) != 2 || len(f.secretCalls) != 0 {
			t.Fatalf("commands %#v secrets %#v", f.commands, f.secretCalls)
		}
	}
}

func TestDeployReleaseBuildAndMissingSecretFailures(t *testing.T) {
	// R-W9IZ-YBVY R-WAQW-C3MN
	f := newDeployRelease(t)
	f.fail = "build"
	assertResult(t, invokeWithDeps(f.deps(), "deploy", "sbx1", "r1"), 1, "", "devctl: build dashboard: exit status 1\n\n> # github.com/ikigenba/ikigenba/dashboard/cmd/dashboard\n> cmd/dashboard/main.go:41:2: undefined: render\n")
	if len(f.secretCalls) != 0 {
		t.Fatalf("secret calls %#v", f.secretCalls)
	}
	f = newDeployRelease(t)
	f.held["auth"] = `{"GOOGLE_CLIENT_ID":"id"}`
	f.manifests["dashboard"] += "secrets = [\"SECOND\"]\n"
	assertResult(t, invokeWithDeps(f.deps(), "deploy", "sbx1", "r1"), 2, "build: ok (r1, dist/"+deploySHA+".tar.xz)\n", "devctl: auth: secrets missing GOOGLE_CLIENT_SECRET\n\nrun 'devctl secrets push sbx1 auth'\n")
	if !reflect.DeepEqual(f.secretCalls, []string{"/sbx1.ikigenba.dev/auth"}) {
		t.Fatalf("calls %#v", f.secretCalls)
	}
	for _, c := range f.commands {
		if c.Path == "ssh" || c.Path == "scp" {
			t.Fatalf("host process %#v", c)
		}
	}
}

func TestDeployReleaseCopyAndActivateFailuresStop(t *testing.T) {
	// R-WBYS-PVDC
	for _, failure := range []string{"copy", "activate"} {
		t.Run(failure, func(t *testing.T) {
			f := newDeployRelease(t)
			f.fail = failure
			result := invokeWithDeps(f.deps(), "deploy", "sbx1", "r1")
			diagnostic := failure + " failed"
			if failure == "activate" {
				diagnostic = "activation failed"
			}
			if result.code != 1 || !strings.Contains(result.stderr, diagnostic) {
				t.Fatalf("result %#v", result)
			}
			prefix := "build: ok (r1, dist/" + deploySHA + ".tar.xz)\nsecrets: ok (8 apps, 2 keys)\n"
			if failure == "copy" {
				if result.stdout != prefix {
					t.Fatalf("output %q", result.stdout)
				}
				for _, c := range f.commands {
					if strings.Contains(strings.Join(c.Args, " "), "activate") {
						t.Fatalf("activated after copy failure %#v", c)
					}
				}
			} else if result.stdout != prefix+"copy: ok ("+deploySHA+".tar.xz -> 18.118.7.42)\nunpack: ok (/opt/ikigenba/releases/"+deploySHA+")\n" {
				t.Fatalf("output %q", result.stdout)
			}
		})
	}
}

func TestDeployReleaseEmptySecretsMakesNoSSMCalls(t *testing.T) {
	// R-WAQW-C3MN
	f := newDeployRelease(t)
	f.manifests["auth"] = "app = \"auth\"\n"
	result := invokeWithDeps(f.deps(), "deploy", "sbx1", "r1")
	if result.code != 0 || result.stderr != "" || !strings.Contains(result.stdout, "secrets: ok (8 apps, 0 keys)\n") || len(f.secretCalls) != 0 {
		t.Fatalf("result %#v secret calls %#v", result, f.secretCalls)
	}
}
