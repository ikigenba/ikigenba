package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spacemove"
)

func TestMoveBackupUsesReleaseToolForBothArchitectures(t *testing.T) {
	// R-FCQ2-I9Y8 R-FGDR-NL6B R-34G8-Y3BV
	for _, tc := range []struct{ machine, arch string }{{"x86_64", "amd64"}, {"aarch64", "arm64"}} {
		t.Run(tc.machine, func(t *testing.T) {
			f := newMoveFake(t)
			f.machine = tc.machine
			got := f.run("staging")
			want := strings.ReplaceAll(expectedMove(false, false, false, ""), "host backup, amd64", "host backup, "+tc.arch)
			assertResult(t, got, 0, want, "")
			goCount := 0
			for _, c := range f.allCommands {
				if c.Path != "go" {
					continue
				}
				goCount++
				if len(c.Args) != 5 || c.Args[0] != "build" || c.Args[1] != "-buildvcs=false" || c.Args[2] != "-o" || c.Args[4] != "./cmd/"+filepath.Base(c.Dir) {
					t.Fatal(c)
				}
			}
			if goCount != len(createApps)+3 {
				t.Fatal("unexpected go commands", goCount)
			}
			if len(f.toolCommands) != 3 {
				t.Fatal(f.toolCommands)
			}
			local := filepath.Join(filepath.Dir(f.knownRestored), "opsctl")
			wantTool := []seam.Cmd{
				{Path: "git", Dir: f.h.checkoutRoot, Args: []string{"worktree", "add", "--detach", f.backupWorktree, f.sha}},
				{Path: "go", Dir: filepath.Join(f.backupWorktree, "opsctl"), Args: []string{"build", "-buildvcs=false", "-o", local, "./cmd/opsctl"}, Env: []string{"GOOS=linux", "GOARCH=" + tc.arch, "CGO_ENABLED=0", "GOWORK=off"}},
				{Path: "git", Dir: f.h.checkoutRoot, Args: []string{"worktree", "remove", "--force", f.backupWorktree}},
			}
			actualTool := append([]seam.Cmd(nil), f.toolCommands...)
			actualTool[1].Env = slices.Clone(actualTool[1].Env)
			slices.Sort(actualTool[1].Env)
			slices.Sort(wantTool[1].Env)
			if !reflect.DeepEqual(actualTool, wantTool) {
				t.Fatalf("tool commands %#v want %#v", f.toolCommands, wantTool)
			}
			var sequence []string
			recording := false
			for _, c := range f.allCommands {
				r := c.Args[len(c.Args)-1]
				if c.Path == "ssh" && r == "'uname' '-m'" {
					recording = true
				}
				if recording {
					sequence = append(sequence, c.Path)
				}
				if c.Path == "ssh" && r == "'rm' '-f' '"+f.remoteTemp+"'" {
					break
				}
			}
			if !reflect.DeepEqual(sequence, []string{"ssh", "git", "go", "git", "ssh", "scp", "ssh", "ssh", "ssh"}) {
				t.Fatal("backup order", sequence)
			}
			var remote []string
			for _, c := range f.backupCommands {
				if c.Path == "scp" {
					if !reflect.DeepEqual(c.Args[len(c.Args)-2:], []string{local, "ec2-user@" + moveOldIP + ":" + f.remoteTemp}) {
						t.Fatal(c)
					}
					remote = append(remote, "copy")
				} else {
					remote = append(remote, c.Args[len(c.Args)-1])
				}
			}
			wantRemote := []string{"'sudo' 'opsctl' 'retire'", "'uname' '-m'", "'mktemp'", "copy", "'chmod' '0700' '/tmp/tmp.Ab12Cd34Ef'", "'sudo' '/tmp/tmp.Ab12Cd34Ef' 'host' 'backup'", "'rm' '-f' '/tmp/tmp.Ab12Cd34Ef'"}
			if !reflect.DeepEqual(remote, wantRemote) {
				t.Fatal(remote)
			}
			entries, err := os.ReadDir(filepath.Join(f.h.checkoutRoot, "dist"))
			if err != nil || len(entries) != 1 || entries[0].Name() != f.sha+".tar.xz" {
				t.Fatal("tool left files in dist", entries, err)
			}
			f.clean()
		})
	}
}

func TestMoveArchErrorShape(t *testing.T) {
	// R-2X4U-NGVP
	_ = spacemove.ArchError(struct{ Machine string }{})
	e := &spacemove.ArchError{Machine: "riscv64"}
	if e.Error() != "backup: host architecture 'riscv64' is neither x86_64 nor aarch64" || e.ExitCode() != 1 {
		t.Fatal(e)
	}
}

func TestMoveBackupFailuresStopAndCleanUp(t *testing.T) {
	// R-FF5V-9TFM R-2X4U-NGVP
	for _, failure := range []string{"uname", "riscv64", "empty machine", "machine whitespace", "compile", "add", "remove worktree", "mktemp", "outside tmp", "empty path", "tmp directory", "nested path", "copy", "chmod", "host backup", "rm", "cancelled copy", "cancelled chmod", "cancelled backup"} {
		t.Run(failure, func(t *testing.T) {
			f := newMoveFake(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.ctx = ctx
			switch failure {
			case "riscv64":
				f.machine = "riscv64"
			case "empty machine":
				f.machine = ""
			case "machine whitespace":
				f.machine = "x86_64 "
			case "outside tmp":
				f.remoteTemp = "/var/tmp/tmp.Ab12Cd34Ef"
			case "empty path":
				f.remoteTemp = ""
			case "tmp directory":
				f.remoteTemp = "/tmp/"
			case "nested path":
				f.remoteTemp = "/tmp/a/b"
			}
			deps := f.deps()
			original := deps.Exec
			var calls []seam.Cmd
			triggered, removed := false, false
			deps.Exec = func(callCtx context.Context, c seam.Cmd) (seam.Result, error) {
				calls = append(calls, c)
				r := c.Args[len(c.Args)-1]
				if c.Path == "ssh" && r == "'rm' '-f' '"+f.remoteTemp+"'" {
					removed = true
					if callCtx.Err() != nil {
						t.Fatal("cleanup inherited cancellation")
					}
					// The cleanup failure must lose to an earlier failure.
					return seam.Result{ExitCode: 1}, nil
				}
				isTool := c.Path == "go" && f.backupWorktree != "" && c.Dir == filepath.Join(f.backupWorktree, "opsctl")
				isAdd := c.Path == "git" && len(c.Args) > 3 && c.Args[1] == "add" && slices.Contains(f.events, "retire")
				isRemove := c.Path == "git" && len(c.Args) > 3 && c.Args[1] == "remove" && slices.Contains(f.events, "retire")
				match := failure == "compile" && isTool || failure == "add" && isAdd || failure == "remove worktree" && isRemove
				if c.Path == "ssh" {
					match = match || failure == "uname" && r == "'uname' '-m'" || failure == "mktemp" && r == "'mktemp'" || strings.Contains(failure, "chmod") && strings.HasPrefix(r, "'chmod'") || (failure == "host backup" || failure == "cancelled backup") && strings.HasSuffix(r, "'host' 'backup'")
				}
				match = match || strings.Contains(failure, "copy") && c.Path == "scp" && strings.HasSuffix(r, ":"+f.remoteTemp)
				if match {
					triggered = true
					if strings.HasPrefix(failure, "cancelled") {
						cancel()
					}
					stderr := "first failure"
					if isTool {
						stderr = "cmd/opsctl/main.go:9:2: undefined: backup"
					} else if strings.HasSuffix(r, "'host' 'backup'") {
						stderr = "opsctl: host backup: upload failed"
					}
					return seam.Result{ExitCode: 1, Stderr: []byte(stderr)}, nil
				}
				if triggered {
					if !isRemove {
						t.Fatal("continued after failure", c)
					}
				}
				return original(callCtx, c)
			}
			var out, diagnostic bytes.Buffer
			code := Run(ctx, []string{"space", "move", "staging"}, strings.NewReader(""), &out, &diagnostic, deps)
			if code != 1 || out.String() != moveInitial()+"retire: ok (opsctl retire)\n" {
				t.Fatal(code, out.String(), diagnostic.String())
			}
			for _, event := range f.events {
				if event == "stop" || event == "tag" || event == "launch" || event == "allocate" || strings.HasPrefix(event, "objects ") {
					t.Fatal("effects after failed backup", f.events)
				}
			}
			wantRemoved := failure == "rm" || failure == "copy" || failure == "chmod" || failure == "host backup" || strings.HasPrefix(failure, "cancelled")
			if removed != wantRemoved {
				t.Fatal("cleanup", removed, "want", wantRemoved)
			}
			if removed {
				last := calls[len(calls)-1]
				if last.Path != "ssh" || last.Args[len(last.Args)-1] != "'rm' '-f' '"+f.remoteTemp+"'" {
					t.Fatal("cleanup not last", last)
				}
			}
			switch failure {
			case "host backup":
				want := "devctl: backup: ssh ec2-user@" + moveOldIP + " sudo " + f.remoteTemp + " host backup: exit status 1\n\n> opsctl: host backup: upload failed\n"
				if diagnostic.String() != want {
					t.Fatal(diagnostic.String())
				}
			case "compile":
				if diagnostic.String() != "devctl: build opsctl: exit status 1\n\n> cmd/opsctl/main.go:9:2: undefined: backup\n" {
					t.Fatal(diagnostic.String())
				}
			case "outside tmp", "empty path", "tmp directory", "nested path":
				if diagnostic.String() != "devctl: backup: mktemp gave '"+f.remoteTemp+"', not a file under /tmp\n" {
					t.Fatal(diagnostic.String())
				}
			case "rm":
				if diagnostic.String() != "devctl: backup: ssh ec2-user@"+moveOldIP+" rm -f "+f.remoteTemp+": exit status 1\n" {
					t.Fatal(diagnostic.String())
				}
			case "riscv64", "empty machine", "machine whitespace":
				if diagnostic.String() != "devctl: "+(&spacemove.ArchError{Machine: f.machine}).Error()+"\n" {
					t.Fatal(diagnostic.String())
				}
			}
			f.clean()
		})
	}
}

func TestMoveBackupReturnsTypedFailures(t *testing.T) {
	// R-FF5V-9TFM
	for _, failure := range []string{"architecture", "compile", "host"} {
		t.Run(failure, func(t *testing.T) {
			f := newMoveFake(t)
			deps := f.deps()
			original := deps.Exec
			deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				r := c.Args[len(c.Args)-1]
				if failure == "architecture" && c.Path == "ssh" && r == "'uname' '-m'" {
					return seam.Result{Stdout: []byte("riscv64\n")}, nil
				}
				if failure == "compile" && c.Path == "go" && f.backupWorktree != "" && c.Dir == filepath.Join(f.backupWorktree, "opsctl") || failure == "host" && c.Path == "ssh" && strings.HasSuffix(r, "'host' 'backup'") {
					return seam.Result{ExitCode: 7, Stderr: []byte("failure detail")}, nil
				}
				return original(ctx, c)
			}
			var out bytes.Buffer
			err := spacemove.Run(f.ctx, []string{"staging"}, "test-version", &out, deps)
			var arch *spacemove.ArchError
			var process *build.ProcessError
			var command *host.CommandError
			switch failure {
			case "architecture":
				if !errors.As(err, &arch) || arch.Machine != "riscv64" {
					t.Fatal(err)
				}
			case "compile":
				if !errors.As(err, &process) || process.Label != "build opsctl" || process.Status != 7 || process.Stderr != "failure detail" || reflect.ValueOf(err) != reflect.ValueOf(process) {
					t.Fatal(err)
				}
			case "host":
				if !errors.As(err, &command) || command.Step != "backup" || command.Status != 7 || command.Stderr != "failure detail" || reflect.ValueOf(err) != reflect.ValueOf(command) {
					t.Fatal(err)
				}
			}
			f.clean()
		})
	}
}
