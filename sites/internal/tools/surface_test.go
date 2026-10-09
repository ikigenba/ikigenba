package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

// R-LRFI-FPI8 R-LSNE-TH8X R-X6XI-XMN1 R-MAXW-K1DC
func TestAdvertisedSurface(t *testing.T) {
	h := newHarness(t)
	register := tools.Register
	_ = register
	infos, err := h.client.ListTools(context.Background(), identity.Caller{UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"list", "show", "create", "publish", "update", "delete", "apex"}
	if len(infos) != len(names) {
		t.Fatalf("tools %v", infos)
	}
	for i, n := range names {
		x := infos[i]
		if x.Name != n {
			t.Fatalf("order %v", infos)
		}
		effect := mcp.Additive
		if i < 2 {
			effect = mcp.Read
		}
		if n == "delete" {
			effect = mcp.Destructive
		}
		if x.Effect() != effect || x.Annotations.ReadOnlyHint == nil || *x.Annotations.ReadOnlyHint != (effect == mcp.Read) || x.Annotations.DestructiveHint == nil || *x.Annotations.DestructiveHint != (effect == mcp.Destructive) || x.Annotations.OpenWorldHint == nil || *x.Annotations.OpenWorldHint || x.Annotations.IdempotentHint != nil {
			t.Fatalf("annotations %+v", x)
		}
		if (len(x.OutputSchema) == 0) != (n == "apex") {
			t.Fatalf("output %s", x.OutputSchema)
		}
	}
	// R-ZLYM-CZDF
	for _, info := range infos {
		if info.Description == "" {
			t.Fatalf("empty description for %s", info.Name)
		}
	}
	// R-MKP3-M7AW
	if string(infos[0].InputSchema) != "{\"type\":\"object\",\"additionalProperties\":false}" {
		t.Fatalf("list inputSchema: %s", infos[0].InputSchema)
	}
	// R-ZN6I-QR44
	if schemaWithoutCopy(t, infos[1].InputSchema) != "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"\"}},\"required\":[\"name\"],\"additionalProperties\":false}" {
		t.Fatalf("show inputSchema: %s", infos[1].InputSchema)
	}
	if schemaWithoutCopy(t, infos[5].InputSchema) != "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"\"}},\"required\":[\"name\"],\"additionalProperties\":false}" {
		t.Fatalf("delete inputSchema: %s", infos[5].InputSchema)
	}
	// R-ZOEF-4IUT
	if schemaWithoutCopy(t, infos[2].InputSchema) != "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"\"},\"repo\":{\"type\":\"string\",\"description\":\"\"},\"ref\":{\"type\":\"string\",\"description\":\"\"},\"visibility\":{\"type\":\"string\",\"description\":\"\"},\"listed\":{\"type\":\"boolean\",\"description\":\"\"}},\"required\":[\"name\",\"repo\"],\"additionalProperties\":false}" {
		t.Fatalf("create inputSchema: %s", infos[2].InputSchema)
	}
	// R-ZPMB-IALI
	if schemaWithoutCopy(t, infos[3].InputSchema) != "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"\"},\"ref\":{\"type\":\"string\",\"description\":\"\"}},\"required\":[\"name\"],\"additionalProperties\":false}" {
		t.Fatalf("publish inputSchema: %s", infos[3].InputSchema)
	}
	// R-ZQU7-W2C7
	if schemaWithoutCopy(t, infos[4].InputSchema) != "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"\"},\"visibility\":{\"type\":\"string\",\"description\":\"\"},\"listed\":{\"type\":\"boolean\",\"description\":\"\"},\"ref\":{\"type\":\"string\",\"description\":\"\"}},\"required\":[\"name\"],\"additionalProperties\":false}" {
		t.Fatalf("update inputSchema: %s", infos[4].InputSchema)
	}
	// R-ZS24-9U2W
	if schemaWithoutCopy(t, infos[6].InputSchema) != "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"\"},\"clear\":{\"type\":\"boolean\",\"description\":\"\"}},\"additionalProperties\":false}" {
		t.Fatalf("apex inputSchema: %s", infos[6].InputSchema)
	}
	// R-MS0H-WTR2
	if string(infos[1].OutputSchema) != "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"slug\":{\"type\":\"string\"},\"url\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"visibility\":{\"type\":\"string\"},\"listed\":{\"type\":\"boolean\"},\"commit\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"published\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"slug\",\"url\",\"repo\",\"ref\",\"visibility\",\"listed\",\"created\"],\"additionalProperties\":false}" {
		t.Fatalf("show outputSchema: %s", infos[1].OutputSchema)
	}
	if string(infos[2].OutputSchema) != "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"slug\":{\"type\":\"string\"},\"url\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"visibility\":{\"type\":\"string\"},\"listed\":{\"type\":\"boolean\"},\"commit\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"published\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"slug\",\"url\",\"repo\",\"ref\",\"visibility\",\"listed\",\"created\"],\"additionalProperties\":false}" {
		t.Fatalf("create outputSchema: %s", infos[2].OutputSchema)
	}
	if string(infos[3].OutputSchema) != "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"slug\":{\"type\":\"string\"},\"url\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"visibility\":{\"type\":\"string\"},\"listed\":{\"type\":\"boolean\"},\"commit\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"published\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"slug\",\"url\",\"repo\",\"ref\",\"visibility\",\"listed\",\"created\"],\"additionalProperties\":false}" {
		t.Fatalf("publish outputSchema: %s", infos[3].OutputSchema)
	}
	if string(infos[4].OutputSchema) != "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"slug\":{\"type\":\"string\"},\"url\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"},\"ref\":{\"type\":\"string\"},\"visibility\":{\"type\":\"string\"},\"listed\":{\"type\":\"boolean\"},\"commit\":{\"type\":\"string\"},\"created\":{\"type\":\"string\"},\"published\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"slug\",\"url\",\"repo\",\"ref\",\"visibility\",\"listed\",\"created\"],\"additionalProperties\":false}" {
		t.Fatalf("update outputSchema: %s", infos[4].OutputSchema)
	}
	// R-MUGA-OD8G
	if string(infos[0].OutputSchema) != "{\"type\":\"object\",\"properties\":{\"sites\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"slug\":{\"type\":\"string\"},\"url\":{\"type\":\"string\"},\"visibility\":{\"type\":\"string\"},\"listed\":{\"type\":\"boolean\"},\"commit\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"slug\",\"url\",\"visibility\",\"listed\"],\"additionalProperties\":false}}},\"required\":[\"sites\"],\"additionalProperties\":false}" {
		t.Fatalf("list outputSchema: %s", infos[0].OutputSchema)
	}
	// R-MVO7-24Z5
	if string(infos[5].OutputSchema) != "{\"type\":\"object\",\"properties\":{\"deleted\":{\"type\":\"boolean\"},\"id\":{\"type\":\"string\"}},\"required\":[\"deleted\",\"id\"],\"additionalProperties\":false}" {
		t.Fatalf("delete outputSchema: %s", infos[5].OutputSchema)
	}
}

// R-LV37-L0QB R-LWB3-YSH0 R-LXJ0-CK7P R-LYQW-QBYE R-LZYT-43P3 R-M16P-HVFS R-M2EL-VN6H
func TestArgumentDeclarations(t *testing.T) {
	ref, vis, yes := "preview", "private", true
	cases := []struct {
		v    any
		want string
	}{
		{tools.ListArgs{}, `{}`},
		{tools.ShowArgs{Name: "blog"}, `{"name":"blog"}`},
		{tools.DeleteArgs{Name: "blog"}, `{"name":"blog"}`},
		{tools.CreateArgs{Name: "blog", Repo: "repository", Ref: &ref, Visibility: &vis, Listed: &yes}, `{"name":"blog","repo":"repository","ref":"preview","visibility":"private","listed":true}`},
		{tools.PublishArgs{Name: "blog", Ref: &ref}, `{"name":"blog","ref":"preview"}`},
		{tools.UpdateArgs{Name: "blog", Visibility: &vis, Listed: &yes, Ref: &ref}, `{"name":"blog","visibility":"private","listed":true,"ref":"preview"}`},
		{tools.ApexArgs{Name: &ref, Clear: &yes}, `{"name":"preview","clear":true}`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.v)
		if err != nil || string(b) != c.want {
			t.Fatalf("argument %s %v", b, err)
		}
	}
}

// R-M3MI-9EX6 R-M4UE-N6NV R-M62B-0YEK R-M7A7-EQ59
func TestResultDeclarations(t *testing.T) {
	commit, published := "sha", "date"
	s := tools.Site{ID: "id", Name: "name", Slug: "slug", URL: "url", Repo: "repo", Ref: "ref", Visibility: "public", Listed: true, Commit: &commit, Created: "created", Published: &published}
	b, _ := json.Marshal(s)
	want := `{"id":"id","name":"name","slug":"slug","url":"url","repo":"repo","ref":"ref","visibility":"public","listed":true,"commit":"sha","created":"created","published":"date"}`
	if string(b) != want {
		t.Fatalf("site %s", b)
	}
	var decoded tools.Site
	if err := json.Unmarshal(b, &decoded); err != nil || !reflect.DeepEqual(decoded, s) {
		t.Fatal(decoded, err)
	}
	l := tools.SiteList{Sites: []tools.ListedSite{{ID: s.ID, Name: s.Name, Slug: s.Slug, URL: s.URL, Visibility: s.Visibility, Listed: s.Listed, Commit: s.Commit}}}
	b, _ = json.Marshal(l)
	if string(b) != `{"sites":[{"id":"id","name":"name","slug":"slug","url":"url","visibility":"public","listed":true,"commit":"sha"}]}` {
		t.Fatalf("list %s", b)
	}
	var dl tools.SiteList
	if err := json.Unmarshal(b, &dl); err != nil || !reflect.DeepEqual(dl, l) {
		t.Fatal(dl, err)
	}
	b, _ = json.Marshal(tools.Deleted{Deleted: true, ID: "id"})
	if string(b) != `{"deleted":true,"id":"id"}` {
		t.Fatalf("deleted %s", b)
	}
}

// R-NIUA-BS2C R-NK26-PJT1 R-N0JS-L7XX R-ZTA0-NLTL R-FC07-11HE R-QSR6-GFOH R-OK16-82FU
func TestReadCalls(t *testing.T) {
	h := newHarness(t)
	z := h.add(t, "alice", "zulu", false)
	a := h.add(t, "alice", "alpha", true)
	h.add(t, "bob", "other", true)
	sha := strings.Repeat("a", 40)
	h.now = h.now.Add(10 * 1000000000)
	p, err := h.cfg.Store.Publish(context.Background(), a.ID, sha)
	if err != nil {
		t.Fatal(err)
	}
	raw := resultObject(t, h.call(t, "alice", "list", `{}`))
	var listing tools.SiteList
	if err = json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Sites) != 2 || listing.Sites[0].ID != a.ID || listing.Sites[1].ID != z.ID || listing.Sites[1].Commit != nil {
		t.Fatalf("listing %s", raw)
	}
	wantList := `{"sites":[{"id":"` + p.ID + `","name":"alpha","slug":"` + p.Slug + `","url":"https://sites.example.invalid/` + p.Slug + `/","visibility":"public","listed":true,"commit":"` + sha + `"},{"id":"` + z.ID + `","name":"zulu","slug":"` + z.Slug + `","url":"https://sites.example.invalid/` + z.Slug + `/","visibility":"public","listed":false}]}`
	if string(raw) != wantList {
		t.Fatalf("listing %s want %s", raw, wantList)
	}
	if string(resultObject(t, h.call(t, "nobody", "list", `{}`))) != `{"sites":[]}` {
		t.Fatal("empty list")
	}
	for _, s := range []store.Site{p, z} {
		raw = resultObject(t, h.call(t, "alice", "show", `{"name":"`+s.Name+`"}`))
		want := `{"id":"` + s.ID + `","name":"` + s.Name + `","slug":"` + s.Slug + `","url":"https://sites.example.invalid/` + s.Slug + `/","repo":"` + s.Repo + `","ref":"main","visibility":"public","listed":` + map[bool]string{true: "true", false: "false"}[s.Listed]
		if s.Commit != "" {
			want += `,"commit":"` + s.Commit + `"`
		}
		want += `,"created":"` + s.Created.UTC().Format("2006-01-02T15:04:05Z") + `"`
		if s.Commit != "" {
			want += `,"published":"` + s.Published.UTC().Format("2006-01-02T15:04:05Z") + `"`
		}
		want += `}`
		if string(raw) != want {
			t.Fatalf("object %s want %s", raw, want)
		}
	}
	for _, name := range []string{"other", z.Slug, a.ID, "Bad Name", "missing"} {
		if got := refusal(t, h.call(t, "alice", "show", `{"name":"`+name+`"}`)); got != fmt.Sprintf(tools.MissingSite, name) {
			t.Fatal(got)
		}
	}
}

// R-9J4G-TJWA
func TestReadCallsLeaveStateAndListingIndependent(t *testing.T) {
	h := newHarness(t)
	alice := h.add(t, "alice", "blog", true)
	bob := h.add(t, "bob", "other", false)
	if _, err := h.cfg.Store.SetApex(context.Background(), bob.ID); err != nil {
		t.Fatal(err)
	}
	for _, site := range []store.Site{alice, bob} {
		dir := filepath.Join(h.cacheRoot, site.ID, "fixture")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte(site.Name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := func() []store.Site {
		t.Helper()
		var all []store.Site
		for _, owner := range []string{"alice", "bob"} {
			sites, err := h.cfg.Store.List(context.Background(), owner)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, sites...)
		}
		return all
	}
	before := state()
	apex, ok, err := h.cfg.Store.Apex(context.Background())
	if err != nil || !ok {
		t.Fatal("apex fixture", err)
	}
	trees := diskSnapshot(t, h.cacheRoot, true)
	cases := []struct{ user, tool, args string }{{"alice", "list", `{}`}, {"alice", "show", `{"name":"blog"}`}, {"alice", "apex", `{}`}, {"bob", "apex", `{"clear":false}`}, {"alice", "apex", `{"name":null,"clear":null}`}}
	answers := make([]string, len(cases))
	for _, exists := range []bool{false, true} {
		if exists {
			h.repo(t, "rep_0123456789abcdef", "bob")
		}
		for i, c := range cases {
			got := string(resultObject(t, h.call(t, c.user, c.tool, c.args)))
			if !exists {
				answers[i] = got
			} else if got != answers[i] {
				t.Fatalf("repository changed %s answer %s want %s", c.tool, got, answers[i])
			}
			if !reflect.DeepEqual(before, state()) {
				t.Fatal("read changed catalog")
			}
			actual, has, e := h.cfg.Store.Apex(context.Background())
			if e != nil || has != ok || !reflect.DeepEqual(apex, actual) {
				t.Fatal("read changed apex", e)
			}
			if !reflect.DeepEqual(trees, diskSnapshot(t, h.cacheRoot, true)) {
				t.Fatal("read changed trees")
			}
		}
	}
}

// R-X85F-BEDQ
func TestToolsListingAcrossCatalogAndRepositoryStates(t *testing.T) {
	var expected []mcp.ToolInfo
	for _, failing := range []bool{false, true} {
		for _, exists := range []bool{false, true} {
			t.Run(fmt.Sprintf("failing=%t/repos=%t", failing, exists), func(t *testing.T) {
				h := newHarness(t)
				if exists {
					h.repo(t, "rep_0123456789abcdef", "alice")
				}
				if failing {
					h.db.SetFailing(true)
				}
				for _, called := range []bool{false, true} {
					if called {
						h.call(t, "alice", "show", `{"name":"Bad Name"}`)
					}
					start := len(h.capture.Events())
					got, err := h.client.ListTools(context.Background(), identity.Caller{UserID: "alice"})
					if err != nil {
						t.Fatal(err)
					}
					if expected == nil {
						expected = got
					} else if !reflect.DeepEqual(expected, got) {
						t.Fatal("listing changed")
					}
					if err = h.cfg.Telemetry.Flush(context.Background()); err != nil {
						t.Fatal(err)
					}
					for _, e := range h.capture.Events()[start:] {
						if e.Name == "tool.called" {
							t.Fatal("listing recorded call")
						}
					}
				}
			})
		}
	}
}

// R-ZUHX-1DKA
func TestGitFailureTextAtToolBoundary(t *testing.T) {
	h := newHarness(t)
	p := h.repo(t, "rep_0123456789abcdef", "alice")
	if err := os.WriteFile(p+"/config", []byte("[broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := git.Find(filepath.Dir(h.gitPath), func() []string { return h.env })
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Output(context.Background(), "", "config", "--file", p+"/config", "--get", "ikigenba.owner")
	var ge *git.Error
	if !errors.As(err, &ge) {
		t.Fatalf("not git error: %v", err)
	}
	text := strings.TrimSuffix(ge.Stderr, "\n")
	if text == "" {
		t.Fatal("fixture needs stderr")
	}
	want := tools.GitFailed + "\n\n> " + strings.ReplaceAll(text, "\n", "\n> ")
	got := refusal(t, h.call(t, "alice", "create", `{"name":"docs","repo":"rep_0123456789abcdef"}`))
	if got != want {
		t.Fatalf("failure %q want %q", got, want)
	}
	// A write failure has no git stderr to quote.
	h = newHarness(t)
	p = h.repo(t, "rep_0123456789abcdef", "alice")
	commitFile(t, h, p, "body")
	h.add(t, "alice", "blog", true)
	parent, err := os.OpenRoot(filepath.Dir(h.cacheRoot))
	if err != nil {
		t.Fatal(err)
	}
	if err = parent.Chmod(filepath.Base(h.cacheRoot), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Chmod(filepath.Base(h.cacheRoot), 0700); _ = parent.Close() })
	if got = refusal(t, h.call(t, "alice", "publish", `{"name":"blog"}`)); got != tools.GitFailed {
		t.Fatal(got)
	}
}

// R-ZVPT-F5AZ
func TestLimitRefusalNumbersAtToolBoundary(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			h := newHarness(t)
			p := h.repo(t, "rep_0123456789abcdef", "alice")
			commitFile(t, h, p, "four")
			h.add(t, "alice", "blog", true)
			s := settings.Defaults()
			s.SiteMaxBytes = 3
			s.OperationSeconds = 23
			configureCache(t, h, s, nil)
			want := fmt.Sprintf(tools.TooLarge, 3)
			if timeout {
				h.after = func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- h.now; return ch }
				want = fmt.Sprintf(tools.TimedOut, 23)
			}
			if got := refusal(t, h.call(t, "alice", "publish", `{"name":"blog"}`)); got != want {
				t.Fatalf("refusal %q want %q", got, want)
			}
		})
	}
}

// schemaWithoutCopy retains byte order and all schema structure while checking copy by type and presence.
func schemaWithoutCopy(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	pattern := regexp.MustCompile(`"description":("(?:[^"\\]|\\.)*")`)
	n := 0
	result := pattern.ReplaceAllStringFunc(string(raw), func(s string) string {
		var d string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(s, `"description":`)), &d); err != nil || d == "" {
			t.Fatalf("invalid property description: %s", s)
		}
		n++
		return `"description":""`
	})
	if n == 0 {
		t.Fatal("no property descriptions")
	}
	return result
}
