package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	hostgit "github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

func mutationObject(t *testing.T, r mcp.Result) tools.Site {
	t.Helper()
	if r.IsError() {
		t.Fatalf("refused: %s", refusal(t, r))
	}
	var s tools.Site
	if err := json.Unmarshal(resultObject(t, r), &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func expectedSiteWire(s store.Site) string {
	wire := fmt.Sprintf(`{"id":%q,"name":%q,"slug":%q,"url":%q,"repo":%q,"ref":%q,"visibility":%q,"listed":%t`, s.ID, s.Name, s.Slug, "https://sites.example.invalid/"+s.Slug+"/", s.Repo, s.Ref, s.Visibility, s.Listed)
	if s.Commit != "" {
		wire += fmt.Sprintf(`,"commit":%q`, s.Commit)
	}
	wire += fmt.Sprintf(`,"created":%q`, s.Created.UTC().Format(time.RFC3339))
	if s.Commit != "" {
		wire += fmt.Sprintf(`,"published":%q`, s.Published.UTC().Format(time.RFC3339))
	}
	return wire + "}"
}
func assertMutationWire(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	if got := string(resultObject(t, r)); got != want {
		t.Fatalf("wire got %s want %s", got, want)
	}
}
func expectRefusal(t *testing.T, h *harness, tool, args, want string) {
	t.Helper()
	if got := refusal(t, h.call(t, "alice", tool, args)); got != want {
		t.Fatalf("%s: got %q want %q", tool, got, want)
	}
}
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	bound, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := bound.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		switch {
		case d.Type()&os.ModeSymlink != 0:
			v, e := bound.Readlink(rel)
			if e != nil {
				return e
			}
			out[rel] = "link:" + v
		case d.IsDir():
			out[rel] = "dir"
		default:
			v, e := bound.ReadFile(rel)
			if e != nil {
				return e
			}
			out[rel] = "file:" + string(v)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func makeTreeMarker(t *testing.T, h *harness, s store.Site) {
	t.Helper()
	p := filepath.Join(h.cacheRoot, s.ID, "marker")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
}

// R-0F87-JH63 R-0494-3JHU R-05H0-HB8J
func TestCreateRulesAndTakenNames(t *testing.T) {
	h := newHarness(t)
	repo := "rep_0123456789abcdef"
	h.repo(t, repo, "alice")
	own := h.add(t, "alice", "owned", false)
	h.add(t, "bob", "taken", true)
	cases := []struct{ args, want string }{
		{`{"name":"Bad","repo":"bad","ref":"..bad","visibility":"secret"}`, fmt.Sprintf(tools.InvalidName, "Bad")},
		{`{"name":"api","repo":"bad","ref":"..bad","visibility":"secret"}`, fmt.Sprintf(tools.InvalidName, "api")},
		{`{"name":"taken","repo":"bad","ref":"..bad","visibility":"secret"}`, fmt.Sprintf(tools.NameTaken, "taken")},
		{`{"name":"owned","repo":"bad"}`, fmt.Sprintf(tools.NameTaken, "owned")},
		{fmt.Sprintf(`{"name":%q,"repo":"bad"}`, own.Slug), fmt.Sprintf(tools.NameTaken, own.Slug)},
		{`{"name":"fresh","repo":"bad","ref":"..bad","visibility":"secret"}`, fmt.Sprintf(tools.NoRepository, "bad")},
		{`{"name":"fresh","repo":"rep_aaaaaaaaaaaaaaaa","ref":"..bad"}`, fmt.Sprintf(tools.NoRepository, "rep_aaaaaaaaaaaaaaaa")},
		{fmt.Sprintf(`{"name":"fresh","repo":%q,"ref":"..bad","visibility":"secret"}`, repo), fmt.Sprintf(tools.InvalidRef, "..bad")},
		{fmt.Sprintf(`{"name":"fresh","repo":%q,"visibility":"Public"}`, repo), tools.BadVisibility},
	}
	for _, c := range cases {
		expectRefusal(t, h, "create", c.args, c.want)
	}
	for i, owner := range []string{"bob", ""} {
		id := fmt.Sprintf("rep_%016x", i+1)
		dir := h.repo(t, id, "alice")
		if owner == "" {
			h.git(t, dir, "config", "--unset", "ikigenba.owner")
		} else {
			h.git(t, dir, "config", "ikigenba.owner", owner)
		}
		expectRefusal(t, h, "create", fmt.Sprintf(`{"name":"fresh","repo":%q}`, id), fmt.Sprintf(tools.NoRepository, id))
	}
}

// R-0494-3JHU
func TestConcurrentCreateHasOneWinner(t *testing.T) {
	h := newHarness(t)
	repo := "rep_0123456789abcdef"
	h.repo(t, repo, "alice")
	const n = 8
	results := make(chan mcp.Result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			<-start
			results <- h.call(t, "alice", "create", fmt.Sprintf(`{"name":"race","repo":%q}`, repo))
		})
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for r := range results {
		if !r.IsError() {
			wins++
		} else if got := refusal(t, r); got != fmt.Sprintf(tools.NameTaken, "race") {
			t.Fatal(got)
		}
	}
	if wins != 1 {
		t.Fatalf("winners %d", wins)
	}
}

// R-0GG3-X8WS R-NRDL-0697
func TestCreateDefaultsAndOverrides(t *testing.T) {
	for _, overrides := range []bool{false, true} {
		t.Run(fmt.Sprint(overrides), func(t *testing.T) {
			h := newHarness(t)
			repo := "rep_0123456789abcdef"
			h.repo(t, repo, "alice")
			other := h.add(t, "bob", "other", true)
			makeTreeMarker(t, h, other)
			if _, err := h.cfg.Store.SetApex(context.Background(), other.ID); err != nil {
				t.Fatal(err)
			}
			before := treeSnapshot(t, h.cacheRoot)
			args := fmt.Sprintf(`{"name":"fresh","repo":%q}`, repo)
			if overrides {
				args = fmt.Sprintf(`{"name":"fresh","repo":%q,"ref":"future","visibility":"private","listed":false}`, repo)
			}
			r := h.call(t, "alice", "create", args)
			o := mutationObject(t, r)
			s, err := h.cfg.Store.Find(context.Background(), "alice", "fresh")
			if err != nil {
				t.Fatal(err)
			}
			assertMutationWire(t, r, expectedSiteWire(s))
			gotOther, err := h.cfg.Store.Find(context.Background(), "bob", "other")
			if err != nil || !reflect.DeepEqual(other, gotOther) {
				t.Fatal("create changed other record")
			}
			wantRef, wantVisibility, wantListed := "main", "public", true
			if overrides {
				wantRef, wantVisibility, wantListed = "future", "private", false
			}
			if s.Owner != "alice" || s.Repo != repo || s.Ref != wantRef || s.Visibility != wantVisibility || s.Listed != wantListed || s.Commit != "" || o.ID != s.ID || o.Ref != s.Ref || o.Visibility != s.Visibility || o.Listed != s.Listed || o.Commit != nil || o.Published != nil {
				t.Fatalf("unexpected site %+v object %+v", s, o)
			}
			sites, err := h.cfg.Store.Visible(context.Background(), "alice")
			if err != nil || len(sites) != 2 {
				t.Fatalf("catalog %+v %v", sites, err)
			}
			apex, ok, err := h.cfg.Store.Apex(context.Background())
			if err != nil || !ok || !reflect.DeepEqual(apex, other) {
				t.Fatalf("apex changed %+v", apex)
			}
			if !reflect.DeepEqual(before, treeSnapshot(t, h.cacheRoot)) {
				t.Fatal("create touched cache")
			}
			if _, err := os.Stat(filepath.Join(h.cacheRoot, s.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("site tree exists: %v", err)
			}
		})
	}
}

// R-0CSE-RXOP
func TestUpdateRuleOrder(t *testing.T) {
	h := newHarness(t)
	s := h.add(t, "alice", "blog", true)
	h.add(t, "bob", "other", true)
	if _, err := h.cfg.Store.SetApex(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ args, want string }{
		{`{"name":"api","visibility":"secret","ref":"..bad"}`, fmt.Sprintf(tools.MissingSite, "api")},
		{`{"name":"other","visibility":"secret","ref":"..bad"}`, fmt.Sprintf(tools.MissingSite, "other")},
		{`{"name":"blog"}`, tools.EmptyUpdate},
		{`{"name":"blog","visibility":"secret","ref":"..bad"}`, tools.BadVisibility},
		{`{"name":"blog","visibility":"private","ref":"..bad"}`, fmt.Sprintf(tools.InvalidRef, "..bad")},
		{`{"name":"blog","visibility":"private"}`, tools.ApexNotPublic},
	} {
		expectRefusal(t, h, "update", c.args, c.want)
	}
}

// R-0LBP-GBVK R-O2CO-G3XG
func TestUpdatePreservesFieldsWithoutRepository(t *testing.T) {
	h := newHarness(t)
	s := h.add(t, "alice", "blog", false)
	var err error
	s, err = h.cfg.Store.Publish(context.Background(), s.ID, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	other := h.add(t, "bob", "other", true)
	makeTreeMarker(t, h, s)
	before := treeSnapshot(t, h.cacheRoot)
	if err := os.RemoveAll(h.reposRoot); err != nil {
		t.Fatal(err)
	}
	r := h.call(t, "alice", "update", `{"name":"blog","visibility":"private","listed":true,"ref":"future"}`)
	o := mutationObject(t, r)
	got, err := h.cfg.Store.Find(context.Background(), "alice", "blog")
	if err != nil {
		t.Fatal(err)
	}
	assertMutationWire(t, r, expectedSiteWire(got))
	want := s
	want.Visibility = "private"
	want.Listed = true
	want.Ref = "future"
	if !reflect.DeepEqual(want, got) || o.ID != got.ID || o.Slug != s.Slug || o.Ref != "future" || o.Visibility != "private" || !o.Listed {
		t.Fatalf("changed wrong fields %+v %+v", got, o)
	}
	againResult := h.call(t, "alice", "update", `{"name":"blog","visibility":"private","listed":true,"ref":"future"}`)
	assertMutationWire(t, againResult, expectedSiteWire(got))
	again := mutationObject(t, againResult)
	if !reflect.DeepEqual(o, again) {
		t.Fatal("no-op changed object")
	}
	gotOther, err := h.cfg.Store.Find(context.Background(), "bob", "other")
	if err != nil || !reflect.DeepEqual(other, gotOther) {
		t.Fatal("changed other site")
	}
	if !reflect.DeepEqual(before, treeSnapshot(t, h.cacheRoot)) {
		t.Fatal("update changed cache")
	}
}

// R-O3KK-TVO5 R-QXMR-ZIN9
func TestDeleteRemovesOnlySiteAndClearsApex(t *testing.T) {
	for _, isApex := range []bool{false, true} {
		t.Run(fmt.Sprint(isApex), func(t *testing.T) {
			h := newHarness(t)
			s := h.add(t, "alice", "blog", true)
			other := h.add(t, "bob", "other", true)
			makeTreeMarker(t, h, s)
			makeTreeMarker(t, h, other)
			apex := other
			if isApex {
				apex = s
			}
			if _, err := h.cfg.Store.SetApex(context.Background(), apex.ID); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(h.cacheRoot, "unrelated"), []byte("root bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			outsideBefore := treeSnapshot(t, h.cacheRoot)
			for path := range outsideBefore {
				if path == s.ID || strings.HasPrefix(path, s.ID+string(filepath.Separator)) {
					delete(outsideBefore, path)
				}
			}
			otherBefore := treeSnapshot(t, filepath.Join(h.cacheRoot, other.ID))
			expectRefusal(t, h, "delete", `{"name":"other"}`, fmt.Sprintf(tools.MissingSite, "other"))
			r := h.call(t, "alice", "delete", `{"name":"blog"}`)
			if string(resultObject(t, r)) != fmt.Sprintf(`{"deleted":true,"id":%q}`, s.ID) {
				t.Fatalf("delete %s", resultObject(t, r))
			}
			if _, err := h.cfg.Store.Find(context.Background(), "alice", "blog"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(h.cacheRoot, s.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cache remains %v", err)
			}
			outsideAfter := treeSnapshot(t, h.cacheRoot)
			if !reflect.DeepEqual(outsideBefore, outsideAfter) {
				t.Fatalf("delete changed outside tree before %+v after %+v", outsideBefore, outsideAfter)
			}
			gotOther, err := h.cfg.Store.Find(context.Background(), "bob", "other")
			if err != nil || !reflect.DeepEqual(other, gotOther) || !reflect.DeepEqual(otherBefore, treeSnapshot(t, filepath.Join(h.cacheRoot, other.ID))) {
				t.Fatal("other changed")
			}
			got, ok, err := h.cfg.Store.Apex(context.Background())
			if err != nil || ok == isApex || !isApex && !reflect.DeepEqual(got, other) {
				t.Fatal("apex wrong")
			}
			expectRefusal(t, h, "delete", `{"name":"blog"}`, fmt.Sprintf(tools.MissingSite, "blog"))
		})
	}
}

// R-0E0B-5PFE R-0NRI-7VCY R-O8G6-CYMX R-WE97-TFX1 R-O9O2-QQDM
func TestApexReadSetClearAndRules(t *testing.T) {
	h := newHarness(t)
	s := h.add(t, "alice", "blog", true)
	other := h.add(t, "bob", "other", true)
	private := h.add(t, "alice", "private", true)
	v := "private"
	if _, _, err := h.cfg.Store.Update(context.Background(), private.ID, store.Change{Visibility: &v}); err != nil {
		t.Fatal(err)
	}
	makeTreeMarker(t, h, s)
	makeTreeMarker(t, h, other)
	before := treeSnapshot(t, h.cacheRoot)
	for _, c := range []struct{ args, want string }{
		{`{"name":"missing","clear":true}`, tools.NameOrClear},
		{`{"name":"other"}`, fmt.Sprintf(tools.MissingSite, "other")},
		{`{"name":"private"}`, tools.ApexNotPublic},
	} {
		expectRefusal(t, h, "apex", c.args, c.want)
	}
	if got := string(resultObject(t, h.call(t, "bob", "apex", `{}`))); got != `{"apex":null}` {
		t.Fatal(got)
	}
	resultObject(t, h.call(t, "bob", "apex", `{"name":"other"}`))
	for i, args := range []string{`{"name":"blog"}`, `{"name":"blog","clear":false}`} {
		if i == 1 {
			var err error
			s, err = h.cfg.Store.Publish(context.Background(), s.ID, strings.Repeat("b", 40))
			if err != nil {
				t.Fatal(err)
			}
		}
		r := h.call(t, "alice", "apex", args)
		assertMutationWire(t, r, `{"apex":`+expectedSiteWire(s)+`}`)
		var o struct {
			Apex *tools.Site `json:"apex"`
		}
		if err := json.Unmarshal(resultObject(t, r), &o); err != nil || o.Apex == nil || o.Apex.ID != s.ID {
			t.Fatalf("apex %+v %v", o, err)
		}
		apex, ok, err := h.cfg.Store.Apex(context.Background())
		if err != nil || !ok || !reflect.DeepEqual(apex, s) {
			t.Fatal("set failed")
		}
	}
	r := h.call(t, "bob", "apex", `{"clear":false}`)
	assertMutationWire(t, r, `{"apex":`+expectedSiteWire(s)+`}`)
	var o struct {
		Apex *tools.Site `json:"apex"`
	}
	if err := json.Unmarshal(resultObject(t, r), &o); err != nil || o.Apex == nil || o.Apex.ID != s.ID {
		t.Fatal("read did not expose apex")
	}
	for range 2 {
		if got := string(resultObject(t, h.call(t, "bob", "apex", `{"clear":true}`))); got != `{"apex":null}` {
			t.Fatal(got)
		}
		_, ok, err := h.cfg.Store.Apex(context.Background())
		if err != nil || ok {
			t.Fatal("clear failed")
		}
	}
	if !reflect.DeepEqual(before, treeSnapshot(t, h.cacheRoot)) {
		t.Fatal("apex changed cache")
	}
	for _, original := range []store.Site{s, other} {
		got, err := h.cfg.Store.Find(context.Background(), original.Owner, original.Name)
		if err != nil || !reflect.DeepEqual(original, got) {
			t.Fatal("apex changed site record")
		}
	}
}

// R-05H0-HB8J
func TestCreateRepositoryDeadlineAndGitFailure(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		h := newHarness(t)
		repo := "rep_0123456789abcdef"
		h.repo(t, repo, "alice")
		h.after = func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- h.now; return ch }
		expectRefusal(t, h, "create", fmt.Sprintf(`{"name":"fresh","repo":%q}`, repo), fmt.Sprintf(tools.TimedOut, h.cfg.Limits.Settings().OperationSeconds))
	})
	t.Run("git stderr", func(t *testing.T) {
		h := newHarness(t)
		repo := "rep_0123456789abcdef"
		dir := h.repo(t, repo, "alice")
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[broken\n"), 0600); err != nil {
			t.Fatal(err)
		}
		r := h.call(t, "alice", "create", fmt.Sprintf(`{"name":"fresh","repo":%q}`, repo))
		got := refusal(t, r)
		g, err := hostgit.Find(filepath.Dir(h.gitPath), func() []string { return h.env })
		if err != nil {
			t.Fatal(err)
		}
		_, err = g.Output(context.Background(), "", "config", "--file", filepath.Join(dir, "config"), "--get", "ikigenba.owner")
		var ge *hostgit.Error
		if !errors.As(err, &ge) {
			t.Fatalf("expected git failure, got %v", err)
		}
		stderr := strings.TrimSuffix(ge.Stderr, "\n")
		want := tools.GitFailed
		if stderr != "" {
			want += "\n\n> " + strings.ReplaceAll(stderr, "\n", "\n> ")
		}
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
}

// R-O2CO-G3XG
func TestUpdateAnswerIndependentOfRepositoryDirectory(t *testing.T) {
	var objects []tools.Site
	for _, present := range []bool{true, false} {
		h := newHarness(t)
		h.add(t, "alice", "blog", true)
		if present {
			h.repo(t, "rep_0123456789abcdef", "alice")
		}
		before := treeSnapshot(t, h.cacheRoot)
		objects = append(objects, mutationObject(t, h.call(t, "alice", "update", `{"name":"blog","ref":"future"}`)))
		if !reflect.DeepEqual(before, treeSnapshot(t, h.cacheRoot)) {
			t.Fatal("update changed trees")
		}
	}
	if !reflect.DeepEqual(objects[0], objects[1]) {
		t.Fatalf("directory changes answer %+v", objects)
	}
}
