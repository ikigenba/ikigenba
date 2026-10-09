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
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

type diskEntry struct {
	Mode       fs.FileMode
	Data, Link string
	Modified   time.Time
}

func diskSnapshot(t *testing.T, root string, times bool) map[string]diskEntry {
	t.Helper()
	out := map[string]diskEntry{}
	confined, err := os.OpenRoot(root)
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = confined.Close() }()
	err = filepath.WalkDir(root, func(p string, _ fs.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		x := diskEntry{Mode: st.Mode()}
		if times {
			x.Modified = st.ModTime()
		}
		if st.Mode().IsRegular() {
			relative, e := filepath.Rel(root, p)
			if e != nil {
				return e
			}
			b, e := confined.ReadFile(relative)
			if e != nil {
				return e
			}
			x.Data = string(b)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			x.Link, e = os.Readlink(p)
			if e != nil {
				return e
			}
		}
		r, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		out[r] = x
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type catalogState struct {
	Records map[string][]store.Site
	Apex    store.Site
	HasApex bool
}

func storeSnapshot(t *testing.T, s *store.Store) catalogState {
	t.Helper()
	out := catalogState{Records: map[string][]store.Site{}}
	for _, owner := range []string{"alice", "bob"} {
		rows, e := s.List(context.Background(), owner)
		if e != nil {
			t.Fatal(e)
		}
		out.Records[owner] = rows
	}
	var e error
	out.Apex, out.HasApex, e = s.Apex(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func catalogSnapshot(t *testing.T, h *harness) catalogState {
	t.Helper()
	return storeSnapshot(t, h.cfg.Store)
}
func seedProtectedCatalog(t *testing.T, h *harness) {
	t.Helper()
	s := h.add(t, "bob", "foreign", false)
	sentinel := filepath.Join(h.cacheRoot, s.ID, "fixture", "kept")
	if e := os.MkdirAll(filepath.Dir(sentinel), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(sentinel, []byte("preserved"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("kept", filepath.Join(filepath.Dir(sentinel), "link")); e != nil {
		t.Fatal(e)
	}
	if _, e := h.cfg.Store.SetApex(context.Background(), s.ID); e != nil {
		t.Fatal(e)
	}
}
func toolKind(tool string) string {
	switch tool {
	case "list", "show":
		return "read"
	case "delete":
		return "destructive"
	default:
		return "additive"
	}
}
func assertToolTrail(t *testing.T, h *harness, start int, tool, outcome string, sites map[string]int) {
	t.Helper()
	n := 0
	seen := map[string]int{}
	for _, e := range h.capture.Events()[start:] {
		switch e.Name {
		case "tool.called":
			n++
			if e.Attrs["tool"] != tool || e.Attrs["kind"] != toolKind(tool) || e.Attrs["outcome"] != outcome {
				t.Fatalf("bad tool event %+v", e)
			}
		case "request.started", "request.finished":
		default:
			if _, allowed := sites[e.Name]; !allowed {
				t.Fatalf("unexpected trail event %+v", e)
			}
			seen[e.Name]++
		}
	}
	if n != 1 || !reflect.DeepEqual(sites, seen) {
		t.Fatalf("tool count %d site counts %v wanted %v", n, seen, sites)
	}
}

func TestCatalogFreeRulesAndFailingCatalog(t *testing.T) {
	// R-ZY5M-6OSD R-01TB-C00G
	h := newHarness(t)
	h.add(t, "alice", "blog", true)
	h.db.SetFailing(true)
	cases := []struct{ tool, args, want string }{
		{"list", `{}`, store.Unreachable}, {"show", `{"name":"bad name"}`, fmt.Sprintf(tools.MissingSite, "bad name")}, {"publish", `{"name":"BAD"}`, fmt.Sprintf(tools.MissingSite, "BAD")}, {"delete", `{"name":"BAD"}`, fmt.Sprintf(tools.MissingSite, "BAD")},
		{"create", `{"name":"BAD","repo":"x","ref":"..bad","visibility":"secret"}`, fmt.Sprintf(tools.InvalidName, "BAD")},
		{"create", `{"name":"api","repo":"x","ref":"..bad","visibility":"secret"}`, fmt.Sprintf(tools.InvalidName, "api")},
		{"create", `{"name":"docs","repo":"x","ref":"..bad","visibility":"secret"}`, fmt.Sprintf(tools.InvalidRef, "..bad")},
		{"create", `{"name":"docs","repo":"x","visibility":"secret"}`, tools.BadVisibility},
		{"update", `{"name":"api","visibility":"secret","ref":"..bad"}`, fmt.Sprintf(tools.MissingSite, "api")},
		{"update", `{"name":"BAD"}`, fmt.Sprintf(tools.MissingSite, "BAD")}, {"update", `{"name":"blog","listed":null}`, tools.EmptyUpdate},
		{"update", `{"name":"blog","visibility":"secret","ref":"..bad"}`, tools.BadVisibility}, {"update", `{"name":"blog","ref":"..bad"}`, fmt.Sprintf(tools.InvalidRef, "..bad")},
		{"apex", `{"name":"BAD","clear":true}`, tools.NameOrClear}, {"apex", `{"name":"BAD"}`, fmt.Sprintf(tools.MissingSite, "BAD")},
		{"show", `{"name":"blog"}`, store.Unreachable}, {"publish", `{"name":"blog"}`, store.Unreachable}, {"update", `{"name":"blog","listed":true}`, store.Unreachable}, {"delete", `{"name":"blog"}`, store.Unreachable}, {"apex", `{}`, store.Unreachable}, {"create", `{"name":"docs","repo":"rep_0102030405060708"}`, store.Unreachable},
	}
	for _, c := range cases {
		if got := refusal(t, h.call(t, "alice", c.tool, c.args)); got != c.want {
			t.Errorf("%s %s: %q want %q", c.tool, c.args, got, c.want)
		}
	}
}

func TestInvalidArgumentsAndUnknownTool(t *testing.T) {
	// R-ZZDI-KGJ2 R-00LE-Y89R
	h := newHarness(t)
	h.add(t, "alice", "blog", true)
	seedProtectedCatalog(t, h)
	before := catalogSnapshot(t, h)
	trees := diskSnapshot(t, h.cacheRoot, false)
	cases := []struct{ tool, args string }{
		{"list", `{"name":"blog"}`}, {"show", `{"site":"blog"}`},
		{"create", `{"name":"docs"}`}, {"create", `{"name":"docs","repo":"rep_0102030405060708","listed":"no","slug":"docs"}`},
		{"publish", `{"ref":"main"}`}, {"update", `{"name":"blog","listed":"no","new_name":"journal"}`},
		{"delete", `{"id":"sit_0102030405060708"}`}, {"apex", `{"name":3,"clear":"yes","site":"blog"}`},
	}
	for _, c := range cases {
		start := len(h.capture.Events())
		r := h.call(t, "alice", c.tool, c.args)
		if !r.IsError() {
			t.Errorf("%s did not refuse invalid arguments", c.tool)
		}
		ev := h.capture.Events()[start:]
		n := 0
		for _, e := range ev {
			if e.Name == "tool.called" {
				n++
				if e.Attrs["outcome"] != "invalid_arguments" || e.Attrs["duration_us"] != int64(0) || e.Attrs["tool"] != c.tool || e.Attrs["kind"] != toolKind(c.tool) {
					t.Errorf("bad event %+v", e)
				}
			} else if e.Name != "request.started" && e.Name != "request.finished" {
				t.Errorf("unexpected event %+v", e)
			}
		}
		if !reflect.DeepEqual(before, catalogSnapshot(t, h)) || !reflect.DeepEqual(trees, diskSnapshot(t, h.cacheRoot, false)) {
			t.Fatal("invalid arguments changed catalog or trees")
		}
		if n != 1 {
			t.Errorf("%s tool events %d", c.tool, n)
		}
	}
	start := len(h.capture.Events())
	_, err := h.client.CallTool(context.Background(), identity.Caller{UserID: "alice", RequestID: "req_00112233445566778899aabbccddeeff"}, "upload", json.RawMessage(`{}`))
	var rpc *mcp.RPCError
	if !errors.As(err, &rpc) || rpc.Code != -32602 {
		t.Fatalf("unknown: %v", err)
	}
	if e := h.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, e := range h.capture.Events()[start:] {
		if e.Name == "tool.called" {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual(before, catalogSnapshot(t, h)) || !reflect.DeepEqual(trees, diskSnapshot(t, h.cacheRoot, false)) {
		t.Fatal("catalog changed")
	}
}

func TestToolClockKindsAndRepositoriesUntouched(t *testing.T) {
	// R-XVCP-61ZC R-9D0Y-WP6T R-NBIW-15M6 R-0ACM-0E7B R-0317-PRR5
	h := newHarness(t)
	repo := h.repo(t, "rep_0123456789abcdef", "alice")
	h.add(t, "alice", "blog", true)
	h.add(t, "bob", "foreign", false)
	before := diskSnapshot(t, h.reposRoot, true)
	cases := []struct{ tool, args, kind string }{{"list", `{}`, "read"}, {"show", `{"name":"blog"}`, "read"}, {"show", `{"name":"missing"}`, "read"}, {"update", `{"name":"blog","ref":"preview"}`, "additive"}, {"update", `{"name":"blog","visibility":"secret"}`, "additive"}, {"apex", `{}`, "additive"}, {"apex", `{"name":"blog"}`, "additive"}, {"apex", `{"clear":true}`, "additive"}, {"publish", `{"name":"foreign"}`, "additive"}, {"publish", `{"name":"missing"}`, "additive"}, {"publish", `{"name":"blog"}`, "additive"}, {"delete", `{"name":"blog"}`, "destructive"}, {"delete", `{"name":"blog"}`, "destructive"}}
	trace := filepath.Join(t.TempDir(), "git-trace.json")
	h.env = append(h.env, "GIT_TRACE2_EVENT="+trace)
	calls := 0
	h.after = func(time.Duration) <-chan time.Time { calls++; return make(chan time.Time) }
	for _, c := range cases {
		if c.tool == "publish" && c.args == `{"name":"blog"}` {
			if err := os.RemoveAll(repo); err != nil {
				t.Fatal(err)
			}
			before = diskSnapshot(t, h.reposRoot, true)
		}
		start := len(h.capture.Events())
		r := h.call(t, "alice", c.tool, c.args)
		outcome := "ok"
		if r.IsError() {
			outcome = "error"
		}
		sites := map[string]int{}
		if !r.IsError() {
			switch c.tool {
			case "update":
				sites["site.updated"] = 1
			case "apex":
				if c.args != `{}` {
					sites["site.apex"] = 1
				}
			case "delete":
				sites["site.deleted"] = 1
			}
		}
		assertToolTrail(t, h, start, c.tool, outcome, sites)
		if r.IsError() && refusal(t, r) == store.Unreachable {
			t.Fatal("false catalog refusal")
		}
		if !reflect.DeepEqual(before, diskSnapshot(t, h.reposRoot, true)) {
			t.Fatalf("%s changed repositories", c.tool)
		}
	}
	if calls != 0 {
		t.Fatalf("nongit tools clock calls %d", calls)
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatalf("nongit tools made trace: %v", err)
	}
}

func TestRefusedMutationsPreserveCatalogAndTrees(t *testing.T) {
	// R-9FGR-O8O7
	h := newHarness(t)
	h.add(t, "alice", "blog", true)
	seedProtectedCatalog(t, h)
	if e := os.WriteFile(filepath.Join(h.cacheRoot, "keep"), []byte("kept"), 0600); e != nil {
		t.Fatal(e)
	}
	before := catalogSnapshot(t, h)
	trees := diskSnapshot(t, h.cacheRoot, false)
	for _, c := range []struct{ tool, args string }{{"create", `{"name":"BAD","repo":"x"}`}, {"publish", `{"name":"blog"}`}, {"update", `{"name":"blog","visibility":"secret"}`}, {"delete", `{"name":"missing"}`}, {"apex", `{"name":"blog","clear":true}`}} {
		refusal(t, h.call(t, "alice", c.tool, c.args))
		if !reflect.DeepEqual(before, catalogSnapshot(t, h)) || !reflect.DeepEqual(trees, diskSnapshot(t, h.cacheRoot, false)) {
			t.Fatalf("%s changed state", c.tool)
		}
	}
}

func TestCreateLeavesRepositoryUnchanged(t *testing.T) {
	// R-NBIW-15M6
	h := newHarness(t)
	h.repo(t, "rep_0123456789abcdef", "alice")
	before := diskSnapshot(t, h.reposRoot, true)
	resultObject(t, h.call(t, "alice", "create", `{"name":"docs","repo":"rep_0123456789abcdef"}`))
	if !reflect.DeepEqual(before, diskSnapshot(t, h.reposRoot, true)) {
		t.Fatal("create changed repository")
	}
}

func TestLaterCreateCatalogFailureHonorsFreeRules(t *testing.T) {
	// R-01TB-C00G R-9FGR-O8O7
	for _, visibility := range []string{"public", "secret"} {
		t.Run(visibility, func(t *testing.T) {
			h := newHarness(t)
			h.repo(t, "rep_0123456789abcdef", "alice")
			catalogBefore := catalogSnapshot(t, h)
			before := diskSnapshot(t, h.cacheRoot, false)
			h.after = func(time.Duration) <-chan time.Time {
				h.db.SetFailing(true)
				return make(chan time.Time)
			}
			want := store.Unreachable
			if visibility == "secret" {
				want = tools.BadVisibility
			}
			if got := refusal(t, h.call(t, "alice", "create", `{"name":"docs","repo":"rep_0123456789abcdef","visibility":"`+visibility+`"}`)); got != want {
				t.Fatal(got)
			}
			if !reflect.DeepEqual(before, diskSnapshot(t, h.cacheRoot, false)) {
				t.Fatal("later catalog failure changed trees")
			}
			if !reflect.DeepEqual(catalogBefore, recoveredCatalogSnapshot(t, h)) {
				t.Fatal("later catalog failure changed catalog")
			}
		})
	}
}

func recoveredCatalogSnapshot(t *testing.T, h *harness) catalogState {
	t.Helper()
	h.db.SetFailing(false)
	defer h.db.SetFailing(true)
	return storeSnapshot(t, h.cfg.Store)
}
