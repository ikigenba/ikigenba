package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestReleaseHostFailuresThroughCLI(t *testing.T) {
	// R-WU9A-GFHR R-WWP3-7YZ5 R-WXWZ-LQPU
	const sha = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"
	const remote = "/tmp/tmp.Ab12Cd34Ef"
	const temporary = "/opt/ikigenba/releases/.unpack.Ab12Cd34Ef"
	for _, failure := range []string{"copy", "tar", "activate"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "infra"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "infra", "terraform.tfvars.json"), []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			var commands []string
			deps := seam.Deps{Dir: root, EUID: 1000, Getenv: func(string) string { return "" }, Now: func() time.Time { return time.Unix(0, 0) }, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				return cloud.Clients{STS: d09STS{}, EC2: d09EC2{}}, nil
			}}
			deps.Exec = func(_ context.Context, c seam.Cmd) (seam.Result, error) {
				switch c.Path {
				case "git":
					if c.Args[0] == "rev-parse" {
						if c.Args[1] == "--show-toplevel" {
							return seam.Result{Stdout: []byte(root + "\n")}, nil
						}
						return seam.Result{Stdout: []byte(sha + "\n")}, nil
					}
					if c.Args[0] == "worktree" {
						if c.Args[1] == "add" {
							return seam.Result{}, os.MkdirAll(c.Args[3], 0o700)
						}
						return seam.Result{}, os.RemoveAll(c.Args[3])
					}
				case "go":
					return seam.Result{}, os.WriteFile(c.Args[3], []byte("binary"), 0o600)
				case "tar":
					return seam.Result{}, os.WriteFile(c.Args[1], []byte("archive"), 0o600)
				case "scp":
					commands = append(commands, "scp")
					if failure == "copy" {
						return seam.Result{ExitCode: 1, Stderr: []byte("No space left on device")}, nil
					}
					return seam.Result{}, nil
				case "ssh":
					logical := c.Args[len(c.Args)-1]
					commands = append(commands, logical)
					if logical == "'mktemp'" {
						return seam.Result{Stdout: []byte(remote + "\n")}, nil
					}
					if strings.Contains(logical, "'test'") {
						return seam.Result{ExitCode: 1}, nil
					}
					if strings.Contains(logical, "'mktemp' '-d'") {
						return seam.Result{Stdout: []byte(temporary + "\n")}, nil
					}
					if strings.Contains(logical, "'tar'") && failure == "tar" {
						return seam.Result{ExitCode: 2, Stderr: []byte("tar: " + sha + "/auth/bin/auth: Cannot write: No space left on device\ntar: Exiting with failure status due to previous errors")}, nil
					}
					return seam.Result{}, nil
				}
				t.Fatalf("unexpected command %#v", c)
				return seam.Result{}, nil
			}
			deps.Stream = func(_ context.Context, c seam.Cmd, w io.Writer) (seam.Result, error) {
				commands = append(commands, c.Args[len(c.Args)-1])
				_, _ = io.WriteString(w, "streamed activation\n")
				return seam.Result{ExitCode: 1, Stderr: []byte("opsctl: activate failed\n\n> ikigenba-events.service: Main process exited, code=exited, status=1/FAILURE")}, nil
			}
			result := invokeWithDeps(deps, "deploy", "sbx1", "r1")
			prefix := "build: ok (r1, dist/" + sha + ".tar.xz)\nsecrets: ok (0 apps, 0 keys)\n"
			archive := filepath.Join(root, "dist", sha+".tar.xz")
			wantErr := "devctl: copy: scp " + archive + " ec2-user@18.118.7.42:" + remote + ": exit status 1\n\n> No space left on device\n"
			if failure != "copy" {
				prefix += "copy: ok (" + sha + ".tar.xz -> 18.118.7.42)\n"
			}
			if failure == "tar" {
				wantErr = "devctl: unpack: ssh ec2-user@18.118.7.42 sudo tar -x -J --no-same-owner -f " + remote + " -C " + temporary + ": exit status 2\n\n> tar: " + sha + "/auth/bin/auth: Cannot write: No space left on device\n> tar: Exiting with failure status due to previous errors\n"
				if commands[len(commands)-2] != "'sudo' 'rm' '-rf' '"+temporary+"'" || commands[len(commands)-1] != "'rm' '-f' '"+remote+"'" {
					t.Fatal(commands)
				}
				for _, cmd := range commands {
					if strings.Contains(cmd, "'mv'") {
						t.Fatal(cmd)
					}
				}
			}
			if failure == "activate" {
				prefix += "unpack: ok (" + release.Folder(sha) + ")\nstreamed activation\n"
				wantErr = "devctl: activate: ssh ec2-user@18.118.7.42 sudo " + release.Opsctl(sha) + " activate " + sha + " r1: exit status 1\n\n> opsctl: activate failed\n> \n> > ikigenba-events.service: Main process exited, code=exited, status=1/FAILURE\n"
			}
			assertResult(t, result, 1, prefix, wantErr)
		})
	}
}
