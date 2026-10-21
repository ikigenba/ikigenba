package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

// R-9S3E-I266 R-9TBA-VTWV
func TestDescriptionConstants(t *testing.T) {
	h := newHarness(t)
	infos, err := h.client.ListTools(context.Background(), identity.Caller{UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	descriptions := []string{tools.ListDescription, tools.ShowDescription, tools.CreateDescription, tools.PublishDescription, tools.UpdateDescription}
	seen := map[string]bool{}
	for i, d := range descriptions {
		if d == "" || seen[d] || infos[i].Description != d {
			t.Fatalf("description %d", i)
		}
		seen[d] = true
	}
}

func commitDirectories(t *testing.T, h *harness, p string) string {
	t.Helper()
	entries := ""
	for _, d := range []string{"dist", "other"} {
		b := gitInput(t, h, p, d+" body", "hash-object", "-w", "--stdin")
		tree := gitInput(t, h, p, "100644 blob "+b+"\tindex.html\n", "mktree")
		entries += "040000 tree " + tree + "\t" + d + "\n"
	}
	b := gitInput(t, h, p, "root body", "hash-object", "-w", "--stdin")
	entries += "100644 blob " + b + "\tindex.html\n"
	tree := gitInput(t, h, p, entries, "mktree")
	sha := gitInput(t, h, p, "fixture\n", "commit-tree", tree)
	h.git(t, p, "update-ref", "refs/heads/main", sha)
	return sha
}

// R-DRB0-SEJJ R-A0MP-6GD1 R-A6Q7-3B2I R-ABLS-ME1A R-AIX6-X0HG R-UQEB-6IQ8
func TestConfiguredPathAndPublishedPathRemainDistinct(t *testing.T) {
	h := newHarness(t)
	repo := "rep_0123456789abcdef"
	p := h.repo(t, repo, "alice")
	sha := commitDirectories(t, h, p)
	for _, configured := range []string{"dist", ""} {
		name := "configured"
		args := fmt.Sprintf(`{"name":%q,"repo":%q,"path":%q}`, name, repo, configured)
		if configured == "" {
			name = "whole"
			args = fmt.Sprintf(`{"name":%q,"repo":%q}`, name, repo)
		}
		check := func(raw json.RawMessage) {
			t.Helper()
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			path, present := fields["path"]
			if present != (configured != "") || present && path != configured {
				t.Fatalf("configured path: %s", raw)
			}
			if _, ok := fields["published_path"]; ok {
				t.Fatalf("internal path leaked: %s", raw)
			}
		}
		check(resultObject(t, h.call(t, "alice", "create", args)))
		s, err := h.cfg.Store.Find(context.Background(), "alice", name)
		if err != nil || s.Path != configured || s.PublishedPath != "" || s.Commit != "" {
			t.Fatalf("create %+v %v", s, err)
		}
		check(resultObject(t, h.call(t, "alice", "show", fmt.Sprintf(`{"name":%q}`, name))))
		check(resultObject(t, h.call(t, "alice", "update", fmt.Sprintf(`{"name":%q,"listed":true}`, name))))
		oneoff := "other"
		if configured == "" {
			oneoff = "dist"
		}
		for i, selected := range []string{configured, oneoff, configured} {
			publish := fmt.Sprintf(`{"name":%q}`, name)
			if i == 1 {
				publish = fmt.Sprintf(`{"name":%q,"path":%q}`, name, selected)
			}
			h.now = h.now.Add(time.Second)
			r, events := eventCall(t, h, fmt.Sprintf("path-publish-%s-%d", name, i), "publish", publish)
			raw := resultObject(t, r)
			check(raw)
			x, err := h.cfg.Store.Find(context.Background(), "alice", name)
			if err != nil || x.Path != configured || x.PublishedPath != selected || x.Commit != sha || !x.Published.Equal(h.now.UTC().Truncate(time.Second)) {
				t.Fatalf("published %+v %v", x, err)
			}
			body, err := os.ReadFile(filepath.Join(h.cfg.Cache.Dir(x.ID, sha, selected), "index.html"))
			want := selected + " body"
			if selected == "" {
				want = "root body"
			}
			if err != nil || string(body) != want {
				t.Fatalf("tree %q %v", body, err)
			}
			entries, err := os.ReadDir(filepath.Join(h.cacheRoot, x.ID))
			if err != nil || len(entries) != 1 {
				t.Fatalf("prune %v %v", entries, err)
			}
			checkSiteEvents(t, events, []string{"site.published"}, []telemetry.Attrs{{"site": x.ID, "commit": sha, "ref": "main", "path": selected}})
		}
		for _, apexArgs := range []string{fmt.Sprintf(`{"name":%q}`, name), `{}`} {
			raw := resultObject(t, h.call(t, "alice", "apex", apexArgs))
			var wrapper struct{ Apex json.RawMessage }
			if err := json.Unmarshal(raw, &wrapper); err != nil {
				t.Fatal(err)
			}
			check(wrapper.Apex)
		}
	}
	raw := resultObject(t, h.call(t, "alice", "list", `{}`))
	var listing struct{ Sites []json.RawMessage }
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Sites) != 2 {
		t.Fatal(string(raw))
	}
	for _, raw := range listing.Sites {
		var o map[string]any
		_ = json.Unmarshal(raw, &o)
		path, present := o["path"]
		if (o["name"] == "configured" && (!present || path != "dist")) || (o["name"] == "whole" && present) {
			t.Fatal(string(raw))
		}
	}
}

// R-A5IA-PJBT R-A7Y3-H2T7 R-AGHE-5H02 R-DSIX-66A8 R-A4AE-BRL4
func TestPathRefusalOrderAndCatalogFailure(t *testing.T) {
	h := newHarness(t)
	repo := "rep_0123456789abcdef"
	p := h.repo(t, repo, "alice")
	commitDirectories(t, h, p)
	h.add(t, "alice", "blog", true)
	for _, path := range []string{"", "/dist", "./dist", "site/../dist"} {
		expectRefusal(t, h, "create", fmt.Sprintf(`{"name":"fresh","repo":%q,"path":%q,"visibility":"secret"}`, repo, path), fmt.Sprintf(tools.InvalidPath, path))
		expectRefusal(t, h, "publish", fmt.Sprintf(`{"name":"blog","path":%q,"ref":"absent"}`, path), fmt.Sprintf(tools.InvalidPath, path))
		expectRefusal(t, h, "publish", fmt.Sprintf(`{"name":"missing","path":%q}`, path), fmt.Sprintf(tools.MissingSite, "missing"))
		if path != "" {
			expectRefusal(t, h, "update", fmt.Sprintf(`{"name":"blog","path":%q}`, path), fmt.Sprintf(tools.InvalidPath, path))
		}
	}
	for _, path := range []string{"absent", "index.html"} {
		expectRefusal(t, h, "publish", fmt.Sprintf(`{"name":"blog","path":%q}`, path), fmt.Sprintf(tools.NoDirectory, path, "main"))
	}
	expectRefusal(t, h, "update", `{"name":"blog","visibility":"secret","ref":"..bad","path":"/bad"}`, tools.BadVisibility)
	expectRefusal(t, h, "update", `{"name":"blog","ref":"..bad","path":"/bad"}`, fmt.Sprintf(tools.InvalidRef, "..bad"))
	expectRefusal(t, h, "create", fmt.Sprintf(`{"name":"fresh","repo":%q,"ref":"..bad","path":"/bad"}`, repo), fmt.Sprintf(tools.InvalidRef, "..bad"))
	h.db.SetFailing(true)
	for _, tool := range []string{"create", "publish", "update"} {
		args := fmt.Sprintf(`{"name":"blog","repo":%q,"path":"../site"}`, repo)
		if tool != "create" {
			args = `{"name":"blog","path":"../site"}`
		}
		expectRefusal(t, h, tool, args, fmt.Sprintf(tools.InvalidPath, "../site"))
	}
	expectRefusal(t, h, "update", `{"name":"blog","path":""}`, store.Unreachable)
}

// R-AHPA-J8QR R-URM7-KAGX
func TestUpdatePathClearsWithoutPublishingAndEmitsChanges(t *testing.T) {
	h := newHarness(t)
	p := h.repo(t, "rep_0123456789abcdef", "alice")
	commitDirectories(t, h, p)
	h.add(t, "alice", "blog", true)
	resultObject(t, h.call(t, "alice", "publish", `{"name":"blog","path":"other"}`))
	before, err := h.cfg.Store.Find(context.Background(), "alice", "blog")
	if err != nil {
		t.Fatal(err)
	}
	for i, path := range []string{"nope", "", ""} {
		r, events := eventCall(t, h, fmt.Sprintf("path-update-%d", i), "update", fmt.Sprintf(`{"name":"blog","path":%q}`, path))
		after, err := h.cfg.Store.Find(context.Background(), "alice", "blog")
		if err != nil {
			t.Fatal(err)
		}
		want := before
		want.Path = path
		if !reflect.DeepEqual(want, after) {
			t.Fatalf("update %+v want %+v", after, want)
		}
		assertMutationWire(t, r, expectedSiteWire(after))
		if i < 2 {
			checkSiteEvents(t, events, []string{"site.updated"}, []telemetry.Attrs{{"site": after.ID, "field": "path", "value": path}})
		} else {
			checkSiteEvents(t, events, nil, nil)
		}
		before = after
	}
}
