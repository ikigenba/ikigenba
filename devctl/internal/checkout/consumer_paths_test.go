package checkout_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestOpenConsumersUseCheckoutRoot(t *testing.T) {
	// R-RFH8-MW7Z R-RGP5-0NYO
	root := t.TempDir()
	dir := filepath.Join(root, "nested", "work")
	files := map[string]string{
		"infra/terraform.tfvars.json":             `{"domain":"example.test","region":"eu-west-1"}`,
		"crm/etc/manifest.toml":                   "app = \"crm\"\n",
		"crm/cmd/crm/main.go":                     "package main\n",
		"nested/work/infra/terraform.tfvars.json": `{"domain":"wrong.test","region":"wrong"}`,
	}
	for path, data := range files {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var commands []seam.Cmd
	deps := seam.Deps{Dir: dir, Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		commands = append(commands, c)
		if len(commands) == 1 {
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		}
		return seam.Result{Stdout: []byte("4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a\n")}, nil
	}}
	opened, err := checkout.Open(t.Context(), deps)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := opened.ReadRootFile()
	if err != nil || cfg != (checkout.RootFile{Domain: "example.test", Region: "eu-west-1"}) {
		t.Fatalf("root file %#v error %v", cfg, err)
	}
	app, err := opened.App("crm")
	if err != nil || app.Dir != filepath.Join(root, "crm") {
		t.Fatalf("app %#v error %v", app, err)
	}
	apps, err := opened.Apps()
	if err != nil || !reflect.DeepEqual(apps, []checkout.App{app}) {
		t.Fatalf("apps %#v error %v", apps, err)
	}
	if opened.Path("crm", "etc") != filepath.Join(root, "crm", "etc") {
		t.Fatal("app path did not use root")
	}
	sha, found, err := opened.ResolveCommit(t.Context(), "r1")
	if err != nil || !found {
		t.Fatalf("resolve %q %v %v", sha, found, err)
	}
	worktree := filepath.Join(root, "dist", "tree")
	if err := opened.AddWorktree(t.Context(), worktree, sha); err != nil {
		t.Fatal(err)
	}
	if err := opened.RemoveWorktree(t.Context(), worktree); err != nil {
		t.Fatal(err)
	}
	if commands[0].Dir != dir || len(commands) != 4 {
		t.Fatalf("commands %#v", commands)
	}
	for _, c := range commands[1:] {
		if c.Dir != root {
			t.Fatalf("command dir %q want %q", c.Dir, root)
		}
	}
}
