package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

func domainClient(t *testing.T, h *harness, base string) *mcp.Client {
	t.Helper()
	ts := httptest.NewServer(telemetry.Middleware(h.cfg.Telemetry, identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.server.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), base)))
	}))))
	t.Cleanup(ts.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: ts.URL, HTTPClient: ts.Client()})
}

// R-DLS4-6WHQ R-DPFT-C7PT
func TestDomainFormAndSpaceRulesWithFailingCatalog(t *testing.T) {
	h := newHarness(t)
	h.add(t, "alice", "blog", true)
	for _, base := range []string{"https://sites.sbx.ikigenba.dev", "http://sites.wip.localhost:7400", "http://localhost:7400"} {
		client := domainClient(t, h, base)
		for _, failing := range []bool{false, true} {
			h.db.SetFailing(failing)
			for _, domain := range []string{"Example.org", "example.org.", "ex_ample.org", "-a.org", "192.168.0.1", "sbx.ikigenba.dev", "sites.sbx.ikigenba.dev", "blog.sbx.ikigenba.dev", "ikigenba.dev", "www.ikigenba.dev", "xsbx.ikigenba.dev", "other.ikigenba.dev", "example.org", "wip.localhost", "blog.wip.localhost", "localhost", ""} {
				want := ""
				switch {
				case domain != "" && !store.ValidDomain(domain):
					want = fmt.Sprintf(tools.InvalidDomain, domain)
				case base == "https://sites.sbx.ikigenba.dev" && (domain == "sbx.ikigenba.dev" || domain == "sites.sbx.ikigenba.dev" || domain == "blog.sbx.ikigenba.dev" || domain == "ikigenba.dev") || base == "http://sites.wip.localhost:7400" && (domain == "wip.localhost" || domain == "blog.wip.localhost" || domain == "localhost"):
					want = fmt.Sprintf(tools.SpaceDomain, domain)
				case failing:
					want = store.Unreachable
				}
				r, err := client.CallTool(context.Background(), identity.Caller{UserID: "alice", RequestID: "domain-rule"}, "update", json.RawMessage(fmt.Sprintf(`{"name":"blog","domain":%q}`, domain)))
				if err != nil {
					t.Fatal(err)
				}
				if want == "" {
					resultObject(t, r)
				} else if got := refusal(t, r); got != want {
					t.Fatalf("base %s failing %t domain %q: %q want %q", base, failing, domain, got, want)
				}
			}
		}
		h.db.SetFailing(false)
	}
}

// R-756Q-V46K R-2Y0K-YG7Z
func TestDomainUpdateRefusalOrderAndTrail(t *testing.T) {
	h := newHarness(t)
	blog := h.add(t, "alice", "blog", true)
	h.add(t, "alice", "private", true)
	other := h.add(t, "bob", "other", true)
	first, second := "blog.example.org", "other.example.org"
	if _, _, err := h.cfg.Store.Update(context.Background(), blog.ID, store.Change{Domain: &first}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.cfg.Store.Update(context.Background(), other.ID, store.Change{Domain: &second}); err != nil {
		t.Fatal(err)
	}
	resultObject(t, h.call(t, "alice", "update", `{"name":"private","visibility":"private"}`))
	before := catalogSnapshot(t, h)
	for _, c := range []struct{ args, want string }{
		{`{"name":"other","domain":"Example.org"}`, fmt.Sprintf(tools.MissingSite, "other")},
		{`{"name":"missing","domain":"other.example.org"}`, fmt.Sprintf(tools.MissingSite, "missing")},
		{`{"name":"blog","visibility":"secret","ref":"..bad","path":"/bad","domain":"Example.org"}`, tools.BadVisibility},
		{`{"name":"blog","ref":"..bad","path":"/bad","domain":"Example.org"}`, fmt.Sprintf(tools.InvalidRef, "..bad")},
		{`{"name":"blog","path":"/bad","domain":"Example.org"}`, fmt.Sprintf(tools.InvalidPath, "/bad")},
		{`{"name":"blog","domain":"Example.org"}`, fmt.Sprintf(tools.InvalidDomain, "Example.org")},
		{`{"name":"blog","domain":"sites.example.invalid"}`, fmt.Sprintf(tools.SpaceDomain, "sites.example.invalid")},
		{`{"name":"blog","visibility":"private"}`, tools.DomainNotPublic},
		{`{"name":"blog","visibility":"private","domain":"other.example.org"}`, tools.DomainNotPublic},
		{`{"name":"private","domain":"other.example.org"}`, tools.DomainNotPublic},
		{`{"name":"blog","domain":"other.example.org"}`, fmt.Sprintf(tools.DomainTaken, second)},
	} {
		start := len(h.capture.Events())
		expectRefusal(t, h, "update", c.args, c.want)
		assertToolTrail(t, h, start, "update", "error", map[string]int{})
		if !reflect.DeepEqual(before, catalogSnapshot(t, h)) {
			t.Fatal("refusal changed catalog")
		}
	}
	if _, err := h.cfg.Store.SetApex(context.Background(), blog.ID); err != nil {
		t.Fatal(err)
	}
	expectRefusal(t, h, "update", `{"name":"blog","visibility":"private"}`, tools.ApexNotPublic)
}

// R-77MJ-MNNY R-6XVC-KHQE R-78UG-0FEN
func TestDomainUpdatesKeepOtherFieldsAndAppearInAllResults(t *testing.T) {
	h := newHarness(t)
	p := h.repo(t, "rep_0123456789abcdef", "alice")
	commitDirectories(t, h, p)
	h.add(t, "alice", "blog", true)
	other := h.add(t, "bob", "other", true)
	resultObject(t, h.call(t, "alice", "publish", `{"name":"blog","path":"other"}`))
	s, err := h.cfg.Store.Find(context.Background(), "alice", "blog")
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"blog.example.org", "blog.example.org", ""} {
		r := h.call(t, "alice", "update", fmt.Sprintf(`{"name":"blog","domain":%q}`, domain))
		x, err := h.cfg.Store.Find(context.Background(), "alice", "blog")
		want := s
		want.Domain = domain
		if err != nil || !reflect.DeepEqual(x, want) {
			t.Fatalf("updated %+v want %+v: %v", x, want, err)
		}
		gotOther, otherErr := h.cfg.Store.Find(context.Background(), "bob", "other")
		if otherErr != nil || !reflect.DeepEqual(gotOther, other) {
			t.Fatal("update changed another site", gotOther, otherErr)
		}
		assertMutationWire(t, r, expectedSiteWire(want))
		assertMutationWire(t, h.call(t, "alice", "show", `{"name":"blog"}`), expectedSiteWire(want))
		for _, args := range []string{`{"name":"blog"}`, `{}`} {
			assertMutationWire(t, h.call(t, "alice", "apex", args), `{"apex":`+expectedSiteWire(want)+`}`)
		}
		rawListing := resultObject(t, h.call(t, "alice", "list", `{}`))
		assertMutationWire(t, h.call(t, "bob", "apex", `{}`), `{"apex":`+expectedSiteWire(want)+`}`)
		wantListing := fmt.Sprintf(`{"sites":[{"id":%q,"name":%q,"slug":%q,"url":%q,"visibility":%q,"listed":true`, want.ID, want.Name, want.Slug, "https://sites.example.invalid/"+want.Slug+"/", want.Visibility)
		if domain != "" {
			wantListing += fmt.Sprintf(`,"domain":%q`, domain)
		}
		wantListing += fmt.Sprintf(`,"commit":%q}]}`, want.Commit)
		if string(rawListing) != wantListing {
			t.Fatalf("listing %s want %s", rawListing, wantListing)
		}
		listing := decodeListing(t, rawListing)
		var listed struct{ Sites []map[string]json.RawMessage }
		if err := json.Unmarshal(rawListing, &listed); err != nil {
			t.Fatal(err)
		}
		_, present := listed.Sites[0]["domain"]
		if present != (domain != "") {
			t.Fatalf("domain presence: %s", rawListing)
		}
		if _, present := listed.Sites[0]["published_path"]; present {
			t.Fatalf("internal path leaked: %s", rawListing)
		}
		if len(listing.Sites) != 1 || pathValue(listing.Sites[0].Domain) != domain {
			t.Fatal(listing)
		}
		by, e := h.cfg.Store.ByDomain(context.Background(), "blog.example.org")
		if domain == "" {
			if !errors.Is(e, store.ErrNotFound) {
				t.Fatal(by, e)
			}
		} else if e != nil || !reflect.DeepEqual(by, want) {
			t.Fatal(by, e)
		}
	}
	resultObject(t, h.call(t, "alice", "apex", `{"clear":true}`))
	resultObject(t, h.call(t, "alice", "update", `{"name":"blog","domain":"blog.example.org"}`))
	resultObject(t, h.call(t, "alice", "update", `{"name":"blog","visibility":"private","domain":""}`))
	resultObject(t, h.call(t, "alice", "update", `{"name":"blog","visibility":"public","domain":"blog.example.org"}`))
	assertMutationWire(t, h.call(t, "alice", "publish", `{"name":"blog"}`), expectedPublishedDomain(t, h, "blog"))
	resultObject(t, h.call(t, "alice", "delete", `{"name":"blog"}`))
	r := h.call(t, "bob", "update", `{"name":"other","domain":"blog.example.org"}`)
	x := mutationObject(t, r)
	if x.ID != other.ID || pathValue(x.Domain) != "blog.example.org" {
		t.Fatal(x)
	}
}

func expectedPublishedDomain(t *testing.T, h *harness, name string) string {
	t.Helper()
	x, err := h.cfg.Store.Find(context.Background(), "alice", name)
	if err != nil || x.Domain != "blog.example.org" {
		t.Fatal(x, err)
	}
	return expectedSiteWire(x)
}

// R-76EN-8VX9
func TestConcurrentDomainAssignmentAndPrivateTakenRefusal(t *testing.T) {
	h := newHarness(t)
	const n = 8
	for i := range n {
		h.add(t, fmt.Sprintf("user-%d", i%2), fmt.Sprintf("site-%d", i), true)
	}
	results := make(chan mcp.Result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			results <- h.call(t, fmt.Sprintf("user-%d", i%2), "update", fmt.Sprintf(`{"name":"site-%d","domain":"shared.example.org"}`, i))
		})
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for r := range results {
		if r.IsError() {
			if got := refusal(t, r); got != fmt.Sprintf(tools.DomainTaken, "shared.example.org") {
				t.Fatal(got)
			}
		} else {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("winners %d", wins)
	}
	count := 0
	for _, user := range []string{"user-0", "user-1"} {
		sites, err := h.cfg.Store.List(context.Background(), user, "", n)
		if err != nil {
			t.Fatal(err)
		}
		for _, site := range sites {
			if site.Domain == "shared.example.org" {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("sites holding domain: %d", count)
	}
	held, err := h.cfg.Store.ByDomain(context.Background(), "shared.example.org")
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if fmt.Sprintf("site-%d", i) == held.Name {
			continue
		}
		got := refusal(t, h.call(t, fmt.Sprintf("user-%d", i%2), "update", fmt.Sprintf(`{"name":"site-%d","domain":"shared.example.org"}`, i)))
		if got != fmt.Sprintf(tools.DomainTaken, "shared.example.org") {
			t.Fatal(got)
		}
	}
}
