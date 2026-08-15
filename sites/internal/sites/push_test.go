package sites

import (
	"context"
	"encoding/json"
	"errors"
	"eventplane/consumer"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type stubPushVersionClient struct {
	entries []TreeEntry
	commit  Commit
	err     error
	exports int
}

func (s *stubPushVersionClient) Create(context.Context, string, Owner) error { return nil }
func (s *stubPushVersionClient) Commit(context.Context, string, string, []FileChange) (Commit, error) {
	return Commit{}, nil
}

func (s *stubPushVersionClient) Export(context.Context, string) ([]TreeEntry, Commit, error) {
	s.exports++
	return s.entries, s.commit, s.err
}
func (s *stubPushVersionClient) Rename(context.Context, string, string, Owner) error { return nil }
func (s *stubPushVersionClient) Delete(context.Context, string, Owner) error         { return nil }

type pushFixture struct {
	ctx     context.Context
	store   *Store
	layout  Layout
	site    Site
	version *stubPushVersionClient
	handler consumer.Handler
}

func newPushFixture(t *testing.T, repoSHA string) pushFixture {
	return newPushFixtureWithPath(t, repoSHA, "")
}

func newPushFixtureWithPath(t *testing.T, repoSHA, path string) pushFixture {
	t.Helper()
	ctx := context.Background()
	store := newTestStore(t)
	root := filepath.Join(t.TempDir(), "sites-root")
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	site, err := store.Create(ctx, "example", "Example", "owner", "owner@example.com", Public, path)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	if err := store.SetRepoSha(ctx, site.Slug, repoSHA); err != nil {
		t.Fatalf("set initial repo sha: %v", err)
	}
	site.RepoSha = repoSHA
	version := &stubPushVersionClient{}
	return pushFixture{
		ctx:     ctx,
		store:   store,
		layout:  layout,
		site:    site,
		version: version,
		handler: NewPushHandler(store, layout, version),
	}
}

func pushEvent(t *testing.T, subject, branch, sha string) consumer.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"branch": branch, "sha": sha})
	if err != nil {
		t.Fatal(err)
	}
	return consumer.Event{Source: "repos", Kind: "push", Subject: subject, Payload: payload}
}

func (f pushFixture) write(t *testing.T, name, content string) {
	t.Helper()
	path := filepath.Join(f.layout.SiteDir(f.site.Visibility, f.site.Slug), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f pushFixture) files(t *testing.T) map[string]string {
	t.Helper()
	root := f.layout.SiteDir(f.site.Visibility, f.site.Slug)
	got := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return got
	}
	if err != nil {
		t.Fatalf("walk site files: %v", err)
	}
	return got
}

func (f pushFixture) repoSHA(t *testing.T) string {
	t.Helper()
	site, err := f.store.Get(f.ctx, f.site.Slug)
	if err != nil {
		t.Fatalf("get site: %v", err)
	}
	return site.RepoSha
}

func pathsNamedUnder(t *testing.T, root, base string) []string {
	t.Helper()
	var matches []string
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == base {
			matches = append(matches, name)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("walk %s looking for %s: %v", root, base, err)
	}
	return matches
}

func TestMainPushReconcilesWholeTreeAndRecordsSHA(t *testing.T) {
	// R-F3GA-3K1N
	f := newPushFixture(t, "old-sha")
	f.write(t, "index.html", "old bytes")
	f.write(t, "stale.html", "remove me")
	f.version.entries = []TreeEntry{{Path: "index.html", Data: []byte("new bytes")}, {Path: "about.html", Data: []byte("about")}}
	f.version.commit = Commit{Sha: "new-sha"}

	err := f.handler(f.ctx, pushEvent(t, "/sites/example", "main", "new-sha"))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := map[string]string{"index.html": "new bytes", "about.html": "about"}
	if got := f.files(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
	if got := f.repoSHA(t); got != "new-sha" {
		t.Fatalf("repo sha = %q, want new-sha", got)
	}
}

func TestMainPushMaterializesOnlyMappedPublishRoot(t *testing.T) {
	// R-42FW-ISO5
	f := newPushFixtureWithPath(t, "old-sha", "public")
	f.write(t, "index.html", "old bytes")
	f.write(t, "stale.html", "remove me")
	f.version.entries = []TreeEntry{
		{Path: "project.txt", Data: []byte("outside")},
		{Path: "public/index.html", Data: []byte("new bytes")},
		{Path: "public/new.html", Data: []byte("new page")},
	}
	f.version.commit = Commit{Sha: "new-sha"}

	if err := f.handler(f.ctx, pushEvent(t, "/sites/example", "main", "new-sha")); err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := map[string]string{"index.html": "new bytes", "new.html": "new page"}
	if got := f.files(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped files = %#v, want %#v", got, want)
	}
	if got := f.repoSHA(t); got != "new-sha" {
		t.Fatalf("repo sha = %q, want new-sha", got)
	}
	if matches := pathsNamedUnder(t, f.layout.Root, "project.txt"); len(matches) != 0 {
		t.Fatalf("outside-root project.txt materialized: %v", matches)
	}

	refused := newPushFixtureWithPath(t, "old-sha", "public")
	refused.write(t, "index.html", "old bytes")
	refused.version.entries = append(f.version.entries, TreeEntry{Path: "public/.git/config", Data: []byte("secret")})
	refused.version.commit = Commit{Sha: "bad-sha"}
	err := refused.handler(refused.ctx, pushEvent(t, "/sites/example", "main", "bad-sha"))
	if err == nil || errors.Is(err, consumer.ErrSkip) {
		t.Fatalf("mapped .git handler error = %v, want non-skip error", err)
	}
	if got := refused.files(t); !reflect.DeepEqual(got, map[string]string{"index.html": "old bytes"}) {
		t.Fatalf("refused export changed files: %#v", got)
	}
	if got := refused.repoSHA(t); got != "old-sha" {
		t.Fatalf("refused export sha = %q, want old-sha", got)
	}
	if matches := pathsNamedUnder(t, refused.layout.Root, ".git"); len(matches) != 0 {
		t.Fatalf("mapped .git created under sites root: %v", matches)
	}
}

func TestNonMainPushDoesNothing(t *testing.T) {
	// R-F4O6-HBSC
	f := newPushFixture(t, "old-sha")
	f.write(t, "index.html", "old bytes")
	f.version.entries = []TreeEntry{{Path: "index.html", Data: []byte("new bytes")}}
	f.version.commit = Commit{Sha: "new-sha"}

	err := f.handler(f.ctx, pushEvent(t, "/sites/example", "feature/x", "new-sha"))
	if err != nil {
		t.Fatalf("handler = %v, want nil", err)
	}
	if f.version.exports != 0 {
		t.Fatalf("export calls = %d, want 0", f.version.exports)
	}
	if got := f.files(t); !reflect.DeepEqual(got, map[string]string{"index.html": "old bytes"}) {
		t.Fatalf("files changed: %#v", got)
	}
	if got := f.repoSHA(t); got != "old-sha" {
		t.Fatalf("repo sha = %q, want old-sha", got)
	}
}

func TestPushEchoSuppressesOnlyMatchingSHA(t *testing.T) {
	// R-OEVK-U243
	f := newPushFixture(t, "same-sha")
	f.write(t, "index.html", "old bytes")
	f.version.entries = []TreeEntry{{Path: "index.html", Data: []byte("new bytes")}}
	f.version.commit = Commit{Sha: "different-sha"}

	if err := f.handler(f.ctx, pushEvent(t, "/sites/example", "main", "same-sha")); err != nil {
		t.Fatalf("echo handler = %v, want nil", err)
	}
	if f.version.exports != 0 || f.repoSHA(t) != "same-sha" || f.files(t)["index.html"] != "old bytes" {
		t.Fatalf("echo mutated state: exports=%d sha=%q files=%#v", f.version.exports, f.repoSHA(t), f.files(t))
	}
	if err := f.handler(f.ctx, pushEvent(t, "/sites/example", "main", "different-sha")); err != nil {
		t.Fatalf("different-sha handler: %v", err)
	}
	if f.version.exports != 1 || f.repoSHA(t) != "different-sha" || f.files(t)["index.html"] != "new bytes" {
		t.Fatalf("different sha was not materialized: exports=%d sha=%q files=%#v", f.version.exports, f.repoSHA(t), f.files(t))
	}
}

func TestUnknownPushSlugReturnsSkip(t *testing.T) {
	// R-F5W2-V3J1
	f := newPushFixture(t, "old-sha")
	err := f.handler(f.ctx, pushEvent(t, "/sites/missing", "main", "new-sha"))
	if !errors.Is(err, consumer.ErrSkip) {
		t.Fatalf("handler error = %v, want consumer.ErrSkip", err)
	}
	if f.version.exports != 0 || len(f.files(t)) != 0 || f.repoSHA(t) != "old-sha" {
		t.Fatalf("unknown slug mutated state: exports=%d files=%#v sha=%q", f.version.exports, f.files(t), f.repoSHA(t))
	}
}

func TestPushExportFailureStallsWithoutMutation(t *testing.T) {
	// R-F73Z-8V9Q
	f := newPushFixture(t, "old-sha")
	f.write(t, "index.html", "old bytes")
	f.version.err = errors.New("repos unavailable")

	err := f.handler(f.ctx, pushEvent(t, "/sites/example", "main", "new-sha"))
	if err == nil || errors.Is(err, consumer.ErrSkip) {
		t.Fatalf("handler error = %v, want non-skip stall", err)
	}
	if f.version.exports != 1 || f.repoSHA(t) != "old-sha" || !reflect.DeepEqual(f.files(t), map[string]string{"index.html": "old bytes"}) {
		t.Fatalf("failed export mutated state: exports=%d sha=%q files=%#v", f.version.exports, f.repoSHA(t), f.files(t))
	}
}

func TestPushRejectsGitMetadataBeforeWritingAnything(t *testing.T) {
	// R-F8BV-MN0F
	for _, bad := range []string{".git/config", "nested/.git/HEAD"} {
		t.Run(bad, func(t *testing.T) {
			f := newPushFixture(t, "old-sha")
			f.version.entries = []TreeEntry{{Path: "index.html", Data: []byte("new bytes")}, {Path: bad, Data: []byte("secret")}}
			f.version.commit = Commit{Sha: "new-sha"}

			err := f.handler(f.ctx, pushEvent(t, "/sites/example", "main", "new-sha"))
			if err == nil || errors.Is(err, consumer.ErrSkip) {
				t.Fatalf("handler error = %v, want non-skip stall", err)
			}
			if f.repoSHA(t) != "old-sha" || len(f.files(t)) != 0 {
				t.Fatalf("bad export partially applied: sha=%q files=%#v", f.repoSHA(t), f.files(t))
			}
			if matches := pathsNamedUnder(t, f.layout.Root, ".git"); len(matches) != 0 {
				t.Fatalf(".git created under sites root: %v", matches)
			}
		})
	}
}

func TestPushRejectsEscapingPathsBeforeWritingAnything(t *testing.T) {
	// R-F9JS-0ER4
	for _, bad := range []string{"../../escape.html", "/etc/escape.html"} {
		t.Run(bad, func(t *testing.T) {
			f := newPushFixture(t, "old-sha")
			f.version.entries = []TreeEntry{{Path: "index.html", Data: []byte("new bytes")}, {Path: bad, Data: []byte("escaped")}}
			f.version.commit = Commit{Sha: "new-sha"}

			err := f.handler(f.ctx, pushEvent(t, "/sites/example", "main", "new-sha"))
			if err == nil || errors.Is(err, consumer.ErrSkip) {
				t.Fatalf("handler error = %v, want non-skip stall", err)
			}
			if f.repoSHA(t) != "old-sha" || len(f.files(t)) != 0 {
				t.Fatalf("bad export partially applied: sha=%q files=%#v", f.repoSHA(t), f.files(t))
			}
			if matches := pathsNamedUnder(t, filepath.Dir(f.layout.Root), "escape.html"); len(matches) != 0 {
				t.Fatalf("relative escape exists outside site: %v", matches)
			}
			if _, err := os.Stat("/etc/escape.html"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("absolute escape exists: %v", err)
			}
		})
	}
}
