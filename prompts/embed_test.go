package prompts_test

import (
	"bytes"
	"embed"
	"io/fs"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/prompts"
)

// R-LNW1-JWPJ R-LP3X-XOG8 R-LQBU-BG6X R-LRJQ-P7XM R-LSRN-2ZOB R-LTZJ-GRF0
func TestEmbeddedResources(t *testing.T) {
	etc := prompts.Etc()
	checkEmbeddedType(etc)
	cases := []struct {
		first fs.FS
		again func() fs.FS
		dir   string
		names []string
	}{{prompts.Assets(), prompts.Assets, ".", []string{"about.html", "landing.html", "notfound.html", "prompt.html", "run.html", "unavailable.html"}}, {etc, func() fs.FS { return prompts.Etc() }, "etc", []string{"manifest.toml", "nginx.conf"}}, {prompts.Migrations(), prompts.Migrations, ".", []string{"0001_catalog.sql"}}}
	roots, e := fs.ReadDir(etc, ".")
	if e != nil || len(roots) != 1 || roots[0].Name() != "etc" || !roots[0].IsDir() {
		t.Fatalf("etc root: %v %v", roots, e)
	}
	t.Chdir(t.TempDir())
	for _, c := range cases {
		entries, err := fs.ReadDir(c.first, c.dir)
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				t.Fatal(entry.Name())
			}
			names = append(names, entry.Name())
			path := entry.Name()
			if c.dir != "." {
				path = c.dir + "/" + path
			}
			a, e := fs.ReadFile(c.first, path)
			if e != nil {
				t.Fatal(e)
			}
			b, e := fs.ReadFile(c.again(), path)
			if e != nil || !bytes.Equal(a, b) {
				t.Fatalf("embedded contents changed %s %v", path, e)
			}
		}
		if !slices.Equal(names, c.names) {
			t.Fatalf("files %v want %v", names, c.names)
		}
	}
}

func checkEmbeddedType(_ embed.FS) {}
