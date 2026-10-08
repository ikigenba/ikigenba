package nginx_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

// R-V1OO-ZD9H R-UKM3-MKVR
func TestReleasedRenderingUsesPackageDirAndIgnoresOldInstalls(t *testing.T) {
	root := t.TempDir()
	const sha = "c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18"
	releaseDir := filepath.Join(root, "opt", "ikigenba", "releases", sha)
	for _, directory := range []string{"crm/etc", "crm/bin"} {
		if err := os.MkdirAll(filepath.Join(releaseDir, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "crm", "etc", "manifest.toml"), []byte("app = \"crm\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "crm", "bin", "crm"), []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/"+sha, filepath.Join(root, "opt", "ikigenba", "current")); err != nil {
		t.Fatal(err)
	}
	var queried []string
	env := host.Env{Root: root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
		if c.Name != "systemctl" || len(c.Args) != 4 || c.Args[0] != "show" {
			t.Fatalf("unexpected %+v", c)
		}
		queried = append(queried, c.Args[3])
		return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
	}}
	first, err := nginx.Render(context.Background(), env, "host.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "include /opt/ikigenba/current/crm/etc/nginx.conf*;") || !strings.Contains(string(first), "server_name         crm.host.example;") {
		t.Fatalf("released configuration %s", first)
	}
	if len(queried) != 1 || queried[0] != "ikigenba-crm.socket" {
		t.Fatalf("queries %v", queried)
	}
	for _, directory := range []string{"opt/old/etc", "opt/old/bin", "var/opt/ikigenba/data-only/state"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "opt/old/etc/manifest.toml"), []byte("app = \"old\"\ndefault = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opt/old/bin/old"), []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := nginx.Render(context.Background(), env, "host.example", "")
	if err != nil || string(first) != string(second) {
		t.Fatalf("old install affected config: %v", err)
	}
}

// R-V1OO-ZD9H
func TestFreshRenderingHasNoServiceBlockOrDisabledQueries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "var/opt/ikigenba/keep/state"), 0700); err != nil {
		t.Fatal(err)
	}
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("fresh host queried a unit")
		return host.Result{}, nil
	}}
	data, err := nginx.Render(context.Background(), env, "host.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "keep.host.example") || strings.Contains(string(data), "include /opt/") {
		t.Fatalf("fresh service block %s", data)
	}
}
