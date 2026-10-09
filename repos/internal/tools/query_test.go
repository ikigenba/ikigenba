package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

func queryHead(t *testing.T, f *toolsFixture, r store.Repo, message string) string {
	t.Helper()
	dir := f.Store.Dir(r.ID)
	tree := strings.TrimSpace(f.git(t, dir, "hash-object", "-t", "tree", "--stdin"))
	head := strings.TrimSpace(f.git(t, dir, "commit-tree", tree, "-m", message))
	f.git(t, dir, "update-ref", "refs/heads/main", head)
	return head
}

func queryRepository(t *testing.T, f *toolsFixture, r store.Repo, base, head string) string {
	t.Helper()
	size, err := f.Store.Size(toolsContext(t), r.ID)
	toolsMust(t, err)
	optional := ""
	if head != "" {
		optional = fmt.Sprintf(`,"head":%q`, head)
	}
	credentials, err := json.Marshal(clone.Guidance(base).Text())
	toolsMust(t, err)
	return fmt.Sprintf(`{"id":%q,"name":%q,"default_branch":%q%s,"size_bytes":%d,"available":%t,"created":%q,"clone_url":%q,"credentials":%s}`, r.ID, r.Name, store.DefaultBranch, optional, size, r.Available, r.Created.UTC().Format("2006-01-02T15:04:05Z"), clone.URL(base, r.Name), credentials)
}

func queryListing(t *testing.T, f *toolsFixture, r store.Repo, head string) string {
	t.Helper()
	size, err := f.Store.Size(toolsContext(t), r.ID)
	toolsMust(t, err)
	optional := ""
	if head != "" {
		optional = fmt.Sprintf(`,"head":%q`, head)
	}
	return fmt.Sprintf(`{"id":%q,"name":%q,"size_bytes":%d%s,"available":%t}`, r.ID, r.Name, size, optional, r.Available)
}

func assertOnlyToolCalls(t *testing.T, f *toolsFixture, offset int, outcomes ...string) {
	t.Helper()
	events := f.events(t)[offset:]
	toolsEqual(t, len(events), len(outcomes))
	for i, outcome := range outcomes {
		toolsEqual(t, events[i].Name, "tool.called")
		toolsEqual(t, events[i].Attrs["outcome"], outcome)
	}
}

// R-7Y5T-5AG1 R-81TI-ALO4 R-Q4HR-3JLU R-849B-255I
// R-85H7-FWW7 R-86P3-TOMW R-USWX-FGVD R-8BKP-CRLO
func TestQueryListAndShowReadCurrentOwnerRepositories(t *testing.T) {
	f := newToolsFixture(t)
	zeta := f.create(t, f.Caller.UserID, "zeta")
	alpha := f.create(t, f.Caller.UserID, "alpha")
	f.create(t, "other", "alpha")
	f.create(t, "other", "hidden")
	head := queryHead(t, f, zeta, "first")
	toolsMust(t, os.WriteFile(filepath.Join(f.Store.Dir(alpha.ID), "measured"), []byte("current bytes"), 0600))
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	want := `{"repos":[` + queryListing(t, f, alpha, "") + `,` + queryListing(t, f, zeta, head) + `]}`
	toolsEqual(t, string(successObject(t, f.call(t, "list", ""))), want)
	for _, ref := range []string{alpha.Name, alpha.ID, zeta.Name, zeta.ID} {
		r, h := alpha, ""
		if ref == zeta.Name || ref == zeta.ID {
			r, h = zeta, head
		}
		toolsEqual(t, string(successObject(t, f.call(t, "show", toolsRepoArgument(ref)))), queryRepository(t, f, r, f.Base, h))
	}
	otherCaller := identity.Caller{UserID: "empty", RequestID: "empty-request"}
	result, err := f.Client.CallTool(toolsContext(t), otherCaller, "list", nil)
	toolsMust(t, err)
	toolsEqual(t, string(successObject(t, result)), `{"repos":[]}`)
	base := "http://different.fixture.invalid:9988"
	client := serveTools(t, tools.Config{Store: f.Store, Limits: f.Limits, Telemetry: f.Writer}, base, false)
	result, err = client.CallTool(toolsContext(t), f.Caller, "show", json.RawMessage(toolsRepoArgument(zeta.ID)))
	toolsMust(t, err)
	toolsEqual(t, string(successObject(t, result)), queryRepository(t, f, zeta, base, head))
	after, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, after, before)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	assertOnlyToolCalls(t, f, 0, "ok", "ok", "ok", "ok", "ok", "ok", "ok")
	// The next call reads a newly moved head and a newly enlarged directory.
	head = queryHead(t, f, zeta, "second")
	toolsMust(t, os.WriteFile(filepath.Join(f.Store.Dir(zeta.ID), "later"), []byte("later bytes"), 0600))
	toolsEqual(t, string(successObject(t, f.call(t, "show", toolsRepoArgument(zeta.Name)))), queryRepository(t, f, zeta, f.Base, head))
	toolsEqual(t, string(successObject(t, f.call(t, "list", "{}"))), `{"repos":[`+queryListing(t, f, alpha, "")+`,`+queryListing(t, f, zeta, head)+`]}`)
}

// R-URP1-1P4O R-81TI-ALO4 R-ZJ0U-WGJF R-8BKP-CRLO
func TestQueryMissingRepositoryAndRuleVocabulary(t *testing.T) {
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	other := f.create(t, "other", "private")
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	calls := 0
	for _, ref := range []string{other.Name, other.ID, "rep_ffffffffffffffff", "rep_zz", "Not Valid", "", "quoted'\nvalue"} {
		for _, name := range []string{"show", "delete"} {
			refusal(t, f.call(t, name, toolsRepoArgument(ref)), "invalid arguments:\nrepo: "+fmt.Sprintf(tools.NoRepository, ref))
			calls++
		}
	}
	refusal(t, f.call(t, "create", `{"name":"Bad Name"}`), "invalid arguments:\nname: "+tools.InvalidName)
	refusal(t, f.call(t, "create", `{"name":"notes"}`), "invalid arguments:\nname: "+fmt.Sprintf(tools.NameTaken, "notes"))
	refusal(t, f.call(t, "rename", `{"repo":"missing","name":"notes"}`), "invalid arguments:\nrepo: "+fmt.Sprintf(tools.NoRepository, "missing")+"\nname: "+fmt.Sprintf(tools.NameTaken, "notes"))
	calls += 3
	after, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, after, before)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	assertOnlyToolCalls(t, f, 0, repeatQueryOutcome(calls, "error")...)
}

func repeatQueryOutcome(count int, outcome string) []string {
	result := make([]string, count)
	for i := range result {
		result[i] = outcome
	}
	return result
}

// R-8ACS-YZUZ R-86P3-TOMW R-85H7-FWW7 R-8BKP-CRLO
func TestQueryUnavailableAndDamagedRepositoriesRemainReadable(t *testing.T) {
	f := newToolsFixture(t)
	unavailable := f.create(t, f.Caller.UserID, "unavailable")
	damaged := f.create(t, f.Caller.UserID, "damaged")
	queryHead(t, f, unavailable, "restored main")
	queryHead(t, f, damaged, "damaged main")
	root, err := os.OpenRoot(f.Store.Dir(unavailable.ID))
	toolsMust(t, err)
	t.Cleanup(func() { toolsMust(t, root.Close()) })
	headBytes, err := root.ReadFile("HEAD")
	toolsMust(t, err)
	toolsMust(t, root.Remove("HEAD"))
	toolsMust(t, f.Store.Verify(toolsContext(t), f.Writer))
	unavailable, err = f.Store.Find(toolsContext(t), f.Caller.UserID, unavailable.ID)
	toolsMust(t, err)
	toolsEqual(t, unavailable.Available, false)
	toolsMust(t, root.WriteFile("HEAD", headBytes, 0600))
	toolsMust(t, os.Remove(filepath.Join(f.Store.Dir(damaged.ID), "HEAD")))
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	offset := len(f.events(t))
	for _, r := range []store.Repo{unavailable, damaged} {
		for _, ref := range []string{r.Name, r.ID} {
			toolsEqual(t, string(successObject(t, f.call(t, "show", toolsRepoArgument(ref)))), queryRepository(t, f, r, f.Base, ""))
		}
	}
	toolsEqual(t, string(successObject(t, f.call(t, "list", "{}"))), `{"repos":[`+queryListing(t, f, damaged, "")+`,`+queryListing(t, f, unavailable, "")+`]}`)
	after, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, after, before)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	assertOnlyToolCalls(t, f, offset, "ok", "ok", "ok", "ok", "ok")
}

func queryContextClient(t *testing.T, f *toolsFixture, decorate func(context.Context) (context.Context, context.CancelFunc)) *mcp.Client {
	t.Helper()
	srv := mcp.NewServer(mcp.ServerConfig{Name: "repos", Version: "fixture", Telemetry: f.Writer})
	tools.Register(srv, tools.Config{Store: f.Store, Limits: f.Limits, Telemetry: f.Writer})
	handler := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := decorate(r.Context())
		defer cancel()
		srv.ServeHTTP(w, r.WithContext(clone.NewContext(ctx, f.Base)))
	}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 15 * time.Second
	return mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: client, Name: "fixture", Version: "fixture"})
}

type queryRequestCancel struct{ cancel context.CancelFunc }

// R-USWX-FGVD R-8BKP-CRLO
func TestQueryRefusesWhenHeadFailsAfterReadingRepository(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	queryHead(t, f, r, "main")
	var active atomic.Pointer[queryRequestCancel]
	var armed atomic.Bool
	var gitCalls atomic.Int64
	g, err := git.Find(filepath.Dir(f.GitPath), func() []string {
		if armed.Load() {
			gitCalls.Add(1)
			if request := active.Load(); request != nil {
				request.cancel()
			}
		}
		return append([]string(nil), f.Env...)
	})
	toolsMust(t, err)
	cfg := f.StoreConfig
	cfg.Git = g
	f.Store, err = store.Open(toolsContext(t), f.DB, cfg)
	toolsMust(t, err)

	f.Client = queryContextClient(t, f, func(ctx context.Context) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(ctx)
		request := &queryRequestCancel{cancel: cancel}
		active.Store(request)
		return ctx, func() { active.CompareAndSwap(request, nil); cancel() }
	})
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	for _, name := range []string{"list", "show"} {
		args := "{}"
		if name == "show" {
			args = toolsRepoArgument(r.ID)
		}
		armed.Store(false)
		successObject(t, f.call(t, name, args))
		armed.Store(true)
		calls := gitCalls.Load()
		refusal(t, f.call(t, name, args), tools.Unreachable)
		if gitCalls.Load() <= calls {
			t.Fatal("call did not reach the real git used by Store.Head")
		}
	}
	armed.Store(false)
	after, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, after, before)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	assertOnlyToolCalls(t, f, 0, "ok", "error", "ok", "error")
}
