package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestStagePreparedCompilesOnceAndRunsBinary(t *testing.T) {
	// R-J7ER-X23Q
	// R-F4E5-34Z0
	prepared := pipelinePrepared(t)
	manifest := []byte("app = \"crm\"\n")
	var commands []seam.Cmd
	deps := seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, cloneCommand(command))
		switch len(commands) {
		case 1:
			if err := os.WriteFile(command.Args[2], []byte("compiled binary"), 0o600); err != nil {
				t.Fatal(err)
			}
			return seam.Result{}, nil
		case 2:
			return seam.Result{Stdout: []byte("v1.2.3\n\n")}, nil
		case 3:
			return seam.Result{Stdout: manifest}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
			return seam.Result{}, nil
		}
	}}

	staged, cleanup, err := stagePrepared(context.Background(), prepared, deps)
	if err != nil {
		t.Fatalf("stagePrepared error = %v", err)
	}
	defer cleanup()

	if len(commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(commands))
	}
	wantGoArgs := []string{"build", "-o", commands[0].Args[2], "./cmd/crm"}
	if commands[0].Path != "go" || !reflect.DeepEqual(commands[0].Args, wantGoArgs) {
		t.Fatalf("compile command = %#v", commands[0])
	}
	if commands[0].Dir != prepared.app.Dir {
		t.Fatalf("compile dir = %q, want %q", commands[0].Dir, prepared.app.Dir)
	}
	wantEnv := []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"}
	if !reflect.DeepEqual(commands[0].Env, wantEnv) {
		t.Fatalf("compile env = %#v, want %#v", commands[0].Env, wantEnv)
	}
	if strings.Contains(strings.Join(commands[0].Args, " "), "ldflags") {
		t.Fatalf("compile arguments inject build-time data: %#v", commands[0].Args)
	}
	if filepath.Dir(filepath.Dir(staged.binary)) != filepath.Join(prepared.app.Dir, "dist") {
		t.Fatalf("staged binary = %q, want under app dist", staged.binary)
	}
	for index, argument := range []string{"--version", "manifest"} {
		command := commands[index+1]
		if command.Path != staged.binary || command.Dir != prepared.app.Dir || !reflect.DeepEqual(command.Args, []string{argument}) {
			t.Fatalf("staged command %d = %#v", index, command)
		}
	}
	if !bytes.Equal(staged.manifest, manifest) {
		t.Fatalf("emitted manifest = %q, want %q", staged.manifest, manifest)
	}
	if _, statErr := os.Stat(staged.binary); statErr != nil {
		t.Fatalf("staged executable missing: %v", statErr)
	}
	cleanup()
	if _, statErr := os.Stat(staged.binary); !os.IsNotExist(statErr) {
		t.Fatalf("staged binary remains after cleanup: %v", statErr)
	}
}

func TestStagePreparedMapsCompilerNonzero(t *testing.T) {
	// R-J7ER-X23Q
	prepared := pipelinePrepared(t)
	var commands []seam.Cmd
	deps := pipelineDeps(t, &commands, seam.Result{ExitCode: 17, Stderr: []byte("compile failed\n")})

	_, _, err := stagePrepared(context.Background(), prepared, deps)
	var processError *ProcessError
	if !errors.As(err, &processError) {
		t.Fatalf("error = %T %v, want *ProcessError", err, err)
	}
	want := ProcessError{Label: "build crm", Status: 17, Stderr: "compile failed\n"}
	if *processError != want {
		t.Fatalf("ProcessError = %#v, want %#v", processError, want)
	}
	if len(commands) != 1 || commands[0].Path != "go" {
		t.Fatalf("commands = %#v, want one go command", commands)
	}
	assertNoPipelineTemps(t, prepared.app.Dir)
}

func TestStagePreparedRejectsReportedVersionMismatch(t *testing.T) {
	// R-EX2Q-SIIU
	prepared := pipelinePrepared(t)
	artifact := filepath.Join(prepared.app.Dir, "dist", "crm-v1.2.3.tar.xz")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("earlier"), 0o600); err != nil {
		t.Fatal(err)
	}
	var commands []seam.Cmd
	deps := pipelineDeps(t, &commands, seam.Result{}, seam.Result{Stdout: []byte("v1.2.4\n")})

	_, _, err := stagePrepared(context.Background(), prepared, deps)
	assertUsageError(t, err, "crm: tagged crm/v1.2.3 but the binary reports v1.2.4")
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want compile and --version only", len(commands))
	}
	root, openErr := os.OpenRoot(prepared.app.Dir)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Errorf("close app root: %v", closeErr)
		}
	}()
	contents, readErr := root.ReadFile("dist/crm-v1.2.3.tar.xz")
	if readErr != nil || string(contents) != "earlier" {
		t.Fatalf("earlier artifact = %q, %v", contents, readErr)
	}
	assertNoPipelineTemps(t, prepared.app.Dir)
}

func TestStagePreparedMapsVersionNonzeroWithoutPublishing(t *testing.T) {
	// R-EYAN-6A9J
	prepared := pipelinePrepared(t)
	assertArtifactUnchanged := seedEarlierArtifact(t, prepared)
	var commands []seam.Cmd
	deps := pipelineDeps(t, &commands,
		seam.Result{},
		seam.Result{ExitCode: 18, Stderr: []byte("version failed\n")},
	)

	_, _, err := stagePrepared(context.Background(), prepared, deps)
	var processError *ProcessError
	if !errors.As(err, &processError) {
		t.Fatalf("error = %T %v, want *ProcessError", err, err)
	}
	want := ProcessError{Label: "crm --version", Status: 18, Stderr: "version failed\n"}
	if *processError != want {
		t.Fatalf("ProcessError = %#v, want %#v", processError, want)
	}
	if len(commands) != 2 || !reflect.DeepEqual(commands[1].Args, []string{"--version"}) {
		t.Fatalf("commands = %#v, want compile then --version", commands)
	}
	assertArtifactUnchanged()
}

func TestStagePreparedMapsManifestNonzero(t *testing.T) {
	// R-F4E5-34Z0
	prepared := pipelinePrepared(t)
	var commands []seam.Cmd
	deps := pipelineDeps(t, &commands,
		seam.Result{},
		seam.Result{Stdout: []byte("v1.2.3\n")},
		seam.Result{ExitCode: 19, Stderr: []byte("manifest failed\n")},
	)

	_, _, err := stagePrepared(context.Background(), prepared, deps)
	var processError *ProcessError
	if !errors.As(err, &processError) {
		t.Fatalf("error = %T %v, want *ProcessError", err, err)
	}
	want := ProcessError{Label: "crm manifest", Status: 19, Stderr: "manifest failed\n"}
	if *processError != want {
		t.Fatalf("ProcessError = %#v, want %#v", processError, want)
	}
	assertNoPipelineTemps(t, prepared.app.Dir)
}

func TestCommandStartErrorsNamePathAndAreNotProcessErrors(t *testing.T) {
	// R-70T2-JC34
	tests := []struct {
		name   string
		failAt int
	}{
		{name: "compiler", failAt: 1},
		{name: "version", failAt: 2},
		{name: "manifest", failAt: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared := pipelinePrepared(t)
			startErr := errors.New("could not start")
			var commands []seam.Cmd
			deps := seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
				commands = append(commands, cloneCommand(command))
				if len(commands) == test.failAt {
					return seam.Result{}, startErr
				}
				if len(commands) == 2 {
					return seam.Result{Stdout: []byte("v1.2.3\n")}, nil
				}
				return seam.Result{}, nil
			}}

			_, _, err := stagePrepared(context.Background(), prepared, deps)
			var processError *ProcessError
			if errors.As(err, &processError) {
				t.Fatalf("error = %#v, unexpectedly matches *ProcessError", err)
			}
			failedPath := commands[len(commands)-1].Path
			if !strings.Contains(err.Error(), failedPath) || !errors.Is(err, startErr) {
				t.Fatalf("error = %v, want path %q wrapping start error", err, failedPath)
			}
			assertNoPipelineTemps(t, prepared.app.Dir)
		})
	}

	// The archive stage uses the same result mapper for tar.
	t.Run("tar", func(t *testing.T) {
		path := "tar"
		startErr := errors.New("could not start")
		_, err := executeResult(context.Background(), seam.Deps{
			Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				return seam.Result{}, startErr
			},
		}, "ignored label", seam.Cmd{Path: path, Dir: "/tmp"})
		var processError *ProcessError
		if errors.As(err, &processError) {
			t.Fatalf("error = %#v, unexpectedly matches *ProcessError", err)
		}
		if !strings.Contains(err.Error(), path) || !errors.Is(err, startErr) {
			t.Fatalf("error = %v, want path %q wrapping start error", err, path)
		}
	})
}

func TestStagePreparedVersionStartErrorDoesNotPublish(t *testing.T) {
	// R-EYAN-6A9J
	prepared := pipelinePrepared(t)
	assertArtifactUnchanged := seedEarlierArtifact(t, prepared)
	startErr := errors.New("could not start")
	var commands []seam.Cmd
	deps := seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, cloneCommand(command))
		if len(commands) == 2 {
			return seam.Result{}, startErr
		}
		return seam.Result{}, nil
	}}

	_, _, err := stagePrepared(context.Background(), prepared, deps)
	var processError *ProcessError
	if errors.As(err, &processError) {
		t.Fatalf("error = %#v, unexpectedly matches *ProcessError", err)
	}
	if len(commands) != 2 || !reflect.DeepEqual(commands[1].Args, []string{"--version"}) {
		t.Fatalf("commands = %#v, want compile then --version", commands)
	}
	if !strings.Contains(err.Error(), commands[1].Path) || !errors.Is(err, startErr) {
		t.Fatalf("error = %v, want path %q wrapping start error", err, commands[1].Path)
	}
	assertArtifactUnchanged()
}

func pipelinePrepared(t *testing.T) preparedBuild {
	t.Helper()
	appDir := filepath.Join(t.TempDir(), "crm")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return preparedBuild{app: checkout.App{Name: "crm", Dir: appDir}, version: "v1.2.3"}
}

func pipelineDeps(t *testing.T, commands *[]seam.Cmd, results ...seam.Result) seam.Deps {
	t.Helper()
	call := 0
	return seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		*commands = append(*commands, cloneCommand(command))
		if call >= len(results) {
			t.Fatalf("unexpected command: %#v", command)
		}
		result := results[call]
		call++
		return result, nil
	}}
}

func cloneCommand(command seam.Cmd) seam.Cmd {
	command.Args = append([]string(nil), command.Args...)
	command.Env = append([]string(nil), command.Env...)
	return command
}

func seedEarlierArtifact(t *testing.T, prepared preparedBuild) func() {
	t.Helper()
	dist := filepath.Join(prepared.app.Dir, "dist")
	if err := os.MkdirAll(dist, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dist, prepared.app.Name+"-"+prepared.version+".tar.xz")
	want := []byte("earlier artifact")
	if err := os.WriteFile(artifact, want, 0o600); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		root, err := os.OpenRoot(dist)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if closeErr := root.Close(); closeErr != nil {
				t.Errorf("close dist root: %v", closeErr)
			}
		}()
		entries, err := os.ReadDir(dist)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != filepath.Base(artifact) {
			t.Fatalf("dist entries = %#v, want only earlier artifact", entries)
		}
		got, err := root.ReadFile(filepath.Base(artifact))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("earlier artifact = %q, want %q", got, want)
		}
	}
}

func assertNoPipelineTemps(t *testing.T, appDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(appDir, "dist", ".devctl-build-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary paths remain: %#v", matches)
	}
}
