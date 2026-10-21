package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

func (f *fixture) directoryCommit(t *testing.T) string {
	t.Helper()
	treeInput := func(input string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, f.gitPath, "mktree")
		c.Env = f.env
		c.Dir = f.repo
		c.Stdin = strings.NewReader(input)
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatal(e, string(b))
		}
		return strings.TrimSpace(string(b))
	}
	entries := ""
	for _, name := range []string{"dist", "other", "root"} {
		path := filepath.Join(f.root, name+"-fixture")
		if e := os.WriteFile(path, []byte(name+" body"), 0600); e != nil {
			t.Fatal(e)
		}
		b := f.command(t, f.repo, "hash-object", "-w", path)
		if name == "root" {
			entries += "100644 blob " + b + "\tindex.html\n"
		} else {
			tree := treeInput("100644 blob " + b + "\tindex.html\n")
			entries += "040000 tree " + tree + "\t" + name + "\n"
		}
	}
	tree := treeInput(entries)
	sha := f.command(t, f.repo, "commit-tree", tree, "-m", "fixture")
	f.command(t, f.repo, "update-ref", "refs/heads/main", sha)
	return sha
}

// R-AE1L-DXIO R-ROWO-XEAP R-ABLS-ME1A R-AIX6-X0HG
func TestPathPublicationControlsServedTreeAndTag(t *testing.T) {
	f := fresh(t)
	f.repository(t)
	sha := f.directoryCommit(t)
	s := f.add(t, "blog", store.Public)
	if r := f.call(t, "publish", `{"name":"blog","path":"dist"}`); r.IsError() {
		t.Fatal(r)
	}
	initial := f.get(t, "GET", "/blog/", "sites", "", nil)
	if initial.Code != 200 || initial.Body.String() != "dist body" {
		t.Fatal(initial.Code, initial.Body.String())
	}
	before, e := f.cfg.Store.Find(context.Background(), "user", "blog")
	if e != nil {
		t.Fatal(e)
	}
	checkOld := func() {
		t.Helper()
		x, e := f.cfg.Store.Find(context.Background(), "user", "blog")
		if e != nil || x.Commit != before.Commit || x.PublishedPath != before.PublishedPath || !x.Published.Equal(before.Published) {
			t.Fatal("publication changed before install", x, e)
		}
		r := f.get(t, "GET", "/blog/", "sites", "", nil)
		if r.Code != initial.Code || r.Header().Get("ETag") != initial.Header().Get("ETag") || r.Body.String() != initial.Body.String() {
			t.Fatal("served state changed before install")
		}
	}
	checkpoints := 0
	f.after = func(time.Duration) <-chan time.Time { checkpoints++; checkOld(); return make(chan time.Time) }
	f.unpacked = func(string, string) { checkpoints++; checkOld() }
	if r := f.call(t, "publish", `{"name":"blog","path":"other"}`); r.IsError() {
		t.Fatal(r)
	}
	if checkpoints < 3 {
		t.Fatal("missing observation checkpoints", checkpoints)
	}
	f.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	f.unpacked = nil
	next := f.get(t, "GET", "/blog/", "sites", "", nil)
	if next.Code != 200 || next.Body.String() != "other body" || next.Header().Get("ETag") == initial.Header().Get("ETag") {
		t.Fatal("same commit path publish", next.Code, next.Header(), next.Body.String())
	}
	x, e := f.cfg.Store.Find(context.Background(), "user", "blog")
	if e != nil || x.Commit != sha || x.Path != "" || x.PublishedPath != "other" {
		t.Fatal(x, e)
	}
	if _, e := os.Stat(f.cfg.Cache.Dir(s.ID, sha, "dist")); !os.IsNotExist(e) {
		t.Fatal("prior directory survived prune", e)
	}
	// Changing the configured path leaves the published directory serving, including on a lazy rebuild.
	if r := f.call(t, "update", `{"name":"blog","path":"dist"}`); r.IsError() {
		t.Fatal(r)
	}
	if e := os.RemoveAll(f.cfg.Cache.Dir(s.ID, sha, "other")); e != nil {
		t.Fatal(e)
	}
	rebuilt := f.get(t, "GET", "/blog/", "sites", "", nil)
	if rebuilt.Code != 200 || rebuilt.Body.String() != "other body" || rebuilt.Header().Get("ETag") != next.Header().Get("ETag") {
		t.Fatal("rebuilt configured path instead of publication", rebuilt.Body.String())
	}
	// Both path refusals preserve the last served directory and its tag.
	for _, path := range []string{"/dist", "absent", "index.html"} {
		r := f.call(t, "publish", fmt.Sprintf(`{"name":"blog","path":%q}`, path))
		if !r.IsError() {
			t.Fatal("path accepted", path)
		}
		b, e := json.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		var answer struct{ Content []struct{ Text string } }
		if e := json.Unmarshal(b, &answer); e != nil {
			t.Fatal(e)
		}
		want := fmt.Sprintf(tools.NoDirectory, path, "main")
		if path == "/dist" {
			want = fmt.Sprintf(tools.InvalidPath, path)
		}
		if len(answer.Content) != 1 || answer.Content[0].Text != want {
			t.Fatalf("refusal %s", b)
		}
		got := f.get(t, "GET", "/blog/", "sites", "", nil)
		if got.Code != next.Code || got.Header().Get("ETag") != next.Header().Get("ETag") || got.Body.String() != next.Body.String() {
			t.Fatal("refusal changed served publication")
		}
		after, e := f.cfg.Store.Find(context.Background(), "user", "blog")
		if e != nil || after.Commit != x.Commit || after.PublishedPath != x.PublishedPath || !after.Published.Equal(x.Published) {
			t.Fatal("refusal changed catalog")
		}
	}
	// The successful request's trail identifies precisely the directory actually served.
	count := 0
	for _, event := range f.events(t) {
		if event.Name == "site.published" {
			count++
			want := "dist"
			if count == 2 {
				want = "other"
			}
			if event.Attrs["path"] != want || event.Attrs["commit"] != sha {
				t.Fatal(event)
			}
		}
	}
	if count != 2 {
		t.Fatal("publication events", count)
	}
}
