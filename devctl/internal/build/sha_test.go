package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestRunBuildNamesArtifactByHeadAndOnlyRunsManifest(t *testing.T) {
	// R-FGM7-H5JS
	// R-FCYI-BUBP
	// R-FLHT-08IK
	// R-FJ20-8P16
	// R-5P7P-6OK6
	// R-5LK0-1DC3
	fixture := newPrerequisiteFixture(t, "")
	appDir := filepath.Join(fixture.root, "crm")
	writeTestFile(t, filepath.Join(appDir, "etc", "extra.conf"), []byte("extra bytes"), 0o600)
	writeTestFile(t, filepath.Join(appDir, "share", "nested", "data"), []byte("share bytes"), 0o600)
	finalPath := filepath.Join(fixture.root, File("crm", prerequisiteHead))
	writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	before := snapshotTree(t, fixture.root)
	var commands []seam.Cmd
	deps := fullBuildDeps(t, fixture, &commands, "")
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"crm"}, "test-version", &stdout, deps); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "crm/dist/crm-"+prerequisiteHead+".tar.xz\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	fixture.assertAllPrerequisiteCommands(t)
	after := snapshotTree(t, fixture.root)
	artifactRelative := filepath.FromSlash(File("crm", prerequisiteHead))
	delete(before, artifactRelative)
	delete(after, artifactRelative)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("paths outside published artifact changed: before %#v after %#v", before, after)
	}
	if len(commands) != 6 {
		t.Fatalf("commands = %#v, want open, clean, head, compile, manifest, archive", commands)
	}
	binary := commands[3].Args[2]
	if commands[4].Path != binary || !reflect.DeepEqual(commands[4].Args, []string{"manifest"}) {
		t.Fatalf("binary command = %#v", commands[4])
	}
	extracted := t.TempDir()
	runTar(t, appDir, "-xJf", finalPath, "-C", extracted)
	assertTestFile(t, filepath.Join(extracted, "bin", "crm"), []byte("compiled executable"), 0o711)
	assertTestFile(t, filepath.Join(extracted, "etc", "manifest.toml"), []byte("app = \"crm\"\n"), 0o644)
	assertTestFile(t, filepath.Join(extracted, "etc", "extra.conf"), []byte("extra bytes"), 0o600)
	assertTestFile(t, filepath.Join(extracted, "share", "nested", "data"), []byte("share bytes"), 0o600)
	assertDistNames(t, appDir, filepath.Base(finalPath))
}

func TestRunFailuresPreserveAllEarlierDistPathsAndProduceNoOutput(t *testing.T) {
	// R-FGM7-H5JS
	// R-FPLY-TD2F
	// R-5LK0-1DC3
	// R-FJ20-8P16
	for _, fail := range []string{"open", "clean", "head", "go", "manifest", "stale", "tar", "publish"} {
		t.Run(fail, func(t *testing.T) {
			fixture := newPrerequisiteFixture(t, "")
			appDir := filepath.Join(fixture.root, "crm")
			finalPath := filepath.Join(fixture.root, File("crm", prerequisiteHead))
			if fail == "publish" {
				if err := os.MkdirAll(finalPath, 0o700); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(finalPath, "keep"), []byte("keep"), 0o600)
			} else {
				writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
			}
			writeTestFile(t, filepath.Join(appDir, "dist", "keep.txt"), []byte("keep"), 0o600)
			before := snapshotTree(t, appDir)
			var commands []seam.Cmd
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"crm"}, "test-version", &stdout, fullBuildDeps(t, fixture, &commands, fail))
			if err == nil {
				t.Fatal("build succeeded unexpectedly")
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if after := snapshotTree(t, appDir); !reflect.DeepEqual(after, before) {
				t.Fatalf("app changed on %s failure\nbefore: %#v\nafter: %#v", fail, before, after)
			}
			if fail == "head" && len(commands) != 3 {
				t.Fatalf("commands = %#v, want preflight only", commands)
			}
		})
	}
}

func fullBuildDeps(t *testing.T, fixture *prerequisiteFixture, commands *[]seam.Cmd, fail string) seam.Deps {
	t.Helper()
	deps := fixture.deps()
	gitExec := deps.Exec
	tarExec := fakeTarDeps(t, nil).Exec
	manifest := []byte("app = \"crm\"\n")
	deps.Exec = func(ctx context.Context, command seam.Cmd) (seam.Result, error) {
		*commands = append(*commands, cloneCommand(command))
		step := command.Path
		if step == "git" {
			switch command.Args[0] {
			case "status":
				step = "clean"
			case "rev-parse":
				if command.Args[1] == "HEAD" {
					step = "head"
				} else {
					step = "open"
				}
			}
		} else if step != "go" && step != "tar" {
			step = "manifest"
		}
		if step == fail {
			return seam.Result{}, errors.New("injected failure")
		}
		switch step {
		case "open", "clean", "head":
			return gitExec(ctx, command)
		case "go":
			writeTestFile(t, command.Args[2], []byte("compiled executable"), 0o600)
			return seam.Result{}, nil
		case "manifest":
			if !reflect.DeepEqual(command.Args, []string{"manifest"}) {
				return seam.Result{ExitCode: 99}, nil
			}
			if fail == "stale" {
				return seam.Result{Stdout: []byte("app = \"crm\"\n# stale\n")}, nil
			}
			return seam.Result{Stdout: manifest}, nil
		case "tar":
			return tarExec(ctx, command)
		default:
			t.Fatalf("unexpected command: %#v", command)
			return seam.Result{}, nil
		}
	}
	return deps
}
