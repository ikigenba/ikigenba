package build_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestSuiteBuildsAndRunsHostManifests(t *testing.T) {
	// R-681W-UOJ9 R-6AHP-M80N
	for _, failure := range []string{"", "stale crm", "manifest crm", "manifest build crm", "compile crm", "compile opsctl", "compile turns", "tar", "remove"} {
		t.Run(failure, func(t *testing.T) {
			f := newSuite(t)
			f.apps = []string{"auth", "crm"}
			f.fail = failure
			f.removeFail = failure == "remove"
			f.deps.Getenv = func(key string) string {
				switch key {
				case "GOOS":
					return "plan9"
				case "GOARCH":
					return "riscv64"
				default:
					return ""
				}
			}
			out, err := f.run(context.Background(), "r1")
			if failure == "" {
				if err != nil || out == "" {
					t.Fatalf("stdout %q error %v", out, err)
				}
			} else if err == nil || out != "" {
				t.Fatalf("stdout %q error %v", out, err)
			}
			manifestBuilds := map[string]seam.Cmd{}
			manifestRuns := map[string]int{}
			compileCounts := map[string]int{}
			for _, c := range f.commands {
				if c.Path == "go" {
					name := filepath.Base(c.Dir)
					output := suiteCompilerOutput(c.Args)
					compileCounts[name]++
					if compileCounts[name] == 1 {
						continue
					}
					if _, exists := manifestBuilds[name]; exists {
						t.Fatalf("duplicate manifest build for %s", name)
					}
					manifestBuilds[name] = c
					wantEnv := []string{"CGO_ENABLED=0", "GOARCH=" + runtime.GOARCH, "GOOS=" + runtime.GOOS, "GOWORK=off"}
					env := slices.Clone(c.Env)
					slices.Sort(env)
					wantArgs := []string{"build", "-buildvcs=false", "-o", output, "./cmd/" + name}
					if !slices.Equal(c.Args, wantArgs) || !slices.Equal(env, wantEnv) || c.Dir != filepath.Join(f.worktrees[0], name) {
						t.Fatalf("manifest build %#v, want args %q env %q", c, wantArgs, wantEnv)
					}
					if !filepath.IsAbs(output) || !strings.HasPrefix(output, filepath.Join(f.root, "dist")+string(os.PathSeparator)) || strings.HasPrefix(output, f.worktrees[0]+string(os.PathSeparator)) {
						t.Fatalf("manifest output location %q", output)
					}
					for _, release := range f.releases {
						if output == release {
							t.Fatalf("manifest output equals release output %q", output)
						}
					}
					if _, err := os.Stat(output); !os.IsNotExist(err) {
						t.Fatalf("manifest output remains at %q: %v", output, err)
					}
				} else if filepath.IsAbs(c.Path) {
					name := filepath.Base(c.Dir)
					manifestBuild, exists := manifestBuilds[name]
					if !exists || c.Path != suiteCompilerOutput(manifestBuild.Args) || !slices.Equal(c.Args, []string{"manifest"}) {
						t.Fatalf("unexpected executable run %#v", c)
					}
					manifestRuns[name]++
				}
			}
			for _, name := range f.apps {
				wantBuilds, wantRuns := 1, 1
				if name == "crm" {
					switch failure {
					case "compile crm":
						wantBuilds, wantRuns = 0, 0
					case "manifest build crm":
						wantRuns = 0
					}
				}
				_, built := manifestBuilds[name]
				if built != (wantBuilds == 1) || manifestRuns[name] != wantRuns {
					t.Fatalf("%s manifest built %v, runs %d; want %d builds, %d runs", name, built, manifestRuns[name], wantBuilds, wantRuns)
				}
			}
		})
	}
}
