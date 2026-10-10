package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func releasedServicesRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	const sha = "c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18"
	release := filepath.Join(root, "opt", "ikigenba", "releases", sha)
	if err := os.MkdirAll(release, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/"+sha, filepath.Join(root, "opt", "ikigenba", "current")); err != nil {
		t.Fatal(err)
	}
	// Create the package through its real release path; discovery returns current.
	for _, directory := range []string{"bin", "etc", "share"} {
		if err := os.MkdirAll(filepath.Join(release, "crm", directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, value := range map[string]string{"bin/crm": "binary", "etc/manifest.toml": "app = \"crm\"\ndescription = \"CRM\"\nmcp = true\n", "share/icon.svg": "<svg/>\n"} {
		if err := os.WriteFile(filepath.Join(release, "crm", path), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	renderFixture(t, root, "leftover", "app = \"leftover\"\n", true, nil)
	return root
}

// R-96EY-4QOE R-97MU-IIF3 R-98UQ-WA5S R-9A2N-A1WH R-UGXD-9BQC
// R-9EY8-T4V9 R-9ILX-YG3C R-BZML-VP81
func TestWriteAndApplyLayoutPathsAndEntries(t *testing.T) {
	for _, layout := range []string{"fresh", "per-app", "released"} {
		for _, apply := range []bool{false, true} {
			t.Run(layout+map[bool]string{false: "/write", true: "/apply"}[apply], func(t *testing.T) {
				root := t.TempDir()
				if layout == "per-app" {
					root = perAppRoot(t)
					renderFixture(t, root, "crm", "app = \"crm\"\n", true, nil)
				}
				if layout == "released" {
					root = releasedServicesRoot(t)
				}
				path := apps.ServicesPath
				if layout == "per-app" && !apply {
					path = apps.PerAppServicesPath
				}
				other := apps.PerAppServicesPath
				if path == other {
					other = apps.ServicesPath
				}
				otherFile := filepath.Join(root, other)
				if err := os.MkdirAll(filepath.Dir(otherFile), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(otherFile, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				runDir := filepath.Join(root, "run", "ikigenba")
				if err := os.MkdirAll(runDir, 0700); err != nil {
					t.Fatal(err)
				}
				fs, err := os.OpenRoot(root)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = fs.Close() }()
				if err := fs.Chmod("run/ikigenba", 0700); err != nil {
					t.Fatal(err)
				}
				var commands []host.Command
				env := renderWriteEnv(t, root, map[string]bool{"crm": true}, &commands)
				if apply {
					err = Apply(context.Background(), env, "host.example")
				} else {
					_, err = Write(context.Background(), env, "host.example")
				}
				if err != nil {
					t.Fatal(err)
				}
				data := readRootFile(t, root, strings.TrimPrefix(path, "/"))
				listed := layout == "released" || layout == "per-app" && !apply
				if listed {
					if !strings.Contains(string(data), `"name": "crm"`) || !strings.Contains(string(data), `"enabled": false`) || strings.Contains(string(data), "leftover") {
						t.Fatalf("entries %s", data)
					}
				} else if string(data) != "{\n  \"services\": []\n}\n" {
					t.Fatalf("empty list %s", data)
				}
				if got := readRootFile(t, root, strings.TrimPrefix(other, "/")); string(got) != "untouched" {
					t.Fatalf("other path changed %s", got)
				}
				info, err := os.Lstat(filepath.Join(root, path))
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0640 {
					t.Fatalf("file %v %v", info, err)
				}
				runInfo, err := os.Stat(runDir)
				if err != nil || runInfo.Mode().Perm() != 0700 {
					t.Fatalf("run mode %v %v", runInfo, err)
				}
				chowns := 0
				for _, c := range commands {
					switch c.Name {
					case "systemctl":
						if !listed || c.Args[0] != "show" {
							t.Fatalf("mutating query %+v", c)
						}
					case "id":
					case "chown":
						chowns++
						wantLen := 2
						if path == apps.PerAppServicesPath {
							wantLen = 3
						}
						if len(c.Args) != wantLen || c.Args[0] != "root:ikigenba" || !strings.HasPrefix(c.Args[len(c.Args)-1], filepath.Dir(filepath.Join(root, path))+"/") {
							t.Fatalf("ownership %+v", c)
						}
					default:
						t.Fatalf("unexpected command %+v", c)
					}
				}
				if chowns != 1 {
					t.Fatalf("chown count %d", chowns)
				}
			})
		}
	}
}

// R-98UQ-WA5S R-9CIG-1LDV
func TestApplyWithNoCurrentDoesNotReadPerAppManifests(t *testing.T) {
	root := perAppRoot(t)
	renderFixture(t, root, "bad", "bad =\n", true, nil)
	if err := Apply(context.Background(), writeEnv(t, root, nil), "host.example"); err != nil {
		t.Fatal(err)
	}
	if got := readRootFile(t, root, "run/ikigenba/services.json"); string(got) != "{\n  \"services\": []\n}\n" {
		t.Fatalf("document %s", got)
	}
	if _, err := Write(context.Background(), writeEnv(t, root, nil), "host.example"); err == nil {
		t.Fatal("Write accepted bad per-app manifest")
	}
}
