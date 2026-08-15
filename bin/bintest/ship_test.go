package bintest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type shipFixture struct {
	repo      string
	tmp       string
	app       string
	ship      string
	fakeBuild string
}

func newShipFixture(t *testing.T) shipFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	tmp := filepath.Join(root, "tmp")
	app := "example"
	for _, dir := range []string{
		filepath.Join(repo, app, "cmd", app),
		filepath.Join(repo, app, "etc"),
		tmp,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("make fixture directory %s: %v", dir, err)
		}
	}
	files := map[string]string{
		"go.work":                        "go 1.26\n\nuse ./example\n",
		app + "/go.mod":                  "module example\n\ngo 1.26\n",
		app + "/.lint-tier":              "cheap\n",
		app + "/VERSION":                 "v1.2.3\n",
		app + "/cmd/" + app + "/main.go": "package main\n\nimport \"fmt\"\n\nfunc main(){fmt.Println(\"red\")}\n",
		app + "/etc/deploy.env":          "ACCOUNT=fixture\nSSH_USER=fixture\nSSH_KEY=/nonexistent\nHOST=fixture.invalid\n",
		app + "/etc/nginx.conf":          "# fixture\n",
		app + "/etc/manifest.env":        "PORT=1234\n",
	}
	for name, content := range files {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture file %s: %v", name, err)
		}
	}
	fakeBuild := filepath.Join(root, "fake-go")
	fakeBuildScript := `#!/usr/bin/env bash
set -euo pipefail
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    out="$2"
    shift 2
    continue
  fi
  shift
done
[ -n "$out" ]
printf '#!/usr/bin/env bash\n' > "$out"
chmod +x "$out"
`
	if err := os.WriteFile(fakeBuild, []byte(fakeBuildScript), 0o755); err != nil {
		t.Fatalf("write fake build command: %v", err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Ship Test")
	runGit(t, repo, "config", "user.email", "ship@example.invalid")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "red fixture")
	return shipFixture{
		repo:      repo,
		tmp:       tmp,
		app:       app,
		ship:      filepath.Join(repoRoot(t), "bin", "ship"),
		fakeBuild: fakeBuild,
	}
}

func (f shipFixture) run(t *testing.T) (string, error) {
	t.Helper()
	env := []string{
		"SHIP_REPO_ROOT=" + f.repo,
		"SHIP_GO=" + f.fakeBuild,
		"TMPDIR=" + f.tmp,
	}
	return runScript(t, env, f.ship, f.app, "--dry-run")
}

func (f shipFixture) bundles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(f.tmp, "suite-artifact-*", "*.tar.gz"))
	if err != nil {
		t.Fatalf("find staged bundles: %v", err)
	}
	return matches
}

// R-X5WI-N72P
func TestShipRefusesRedCloneBeforeStagingAndSucceedsOnceClean(t *testing.T) {
	f := newShipFixture(t)
	out, err := f.run(t)
	if err == nil {
		t.Fatalf("ship accepted red source:\n%s", out)
	}
	if got := f.bundles(t); len(got) != 0 {
		t.Fatalf("ship staged bundles for red source: %v", got)
	}
	if strings.Contains(out, ">> build ") || strings.Contains(out, "not checking the box") {
		t.Fatalf("ship passed its early lint refusal:\n%s", out)
	}

	mainGo := filepath.Join(f.repo, f.app, "cmd", f.app, "main.go")
	clean := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"clean\")\n}\n"
	if err := os.WriteFile(mainGo, []byte(clean), 0o644); err != nil {
		t.Fatalf("write clean source: %v", err)
	}
	runGit(t, f.repo, "add", ".")
	runGit(t, f.repo, "commit", "-m", "clean fixture")
	out, err = f.run(t)
	if err != nil {
		t.Fatalf("ship rejected clean source: %v\n%s", err, out)
	}
	if !strings.Contains(out, "dry-run complete") {
		t.Fatalf("ship did not reach clean success path:\n%s", out)
	}
	if got := f.bundles(t); len(got) != 1 {
		t.Fatalf("clean ship staged %d bundles, want 1: %v", len(got), got)
	}
}
