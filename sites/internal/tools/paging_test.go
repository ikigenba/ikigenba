package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

// R-4TZO-3339
func TestListArgumentFields(t *testing.T) {
	page, limit := "position", 7
	args := tools.ListArgs{Page: &page, Limit: &limit}
	raw, err := json.Marshal(args)
	if err != nil || string(raw) != `{"page":"position","limit":7}` {
		t.Fatalf("args %s: %v", raw, err)
	}
	var decoded tools.ListArgs
	if err = json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(args, decoded) {
		t.Fatal(decoded, err)
	}
}

// R-4XND-8EBC R-9ZES-SOMC R-6AAS-YA2A R-6BIP-C1SZ
func TestListPagingSurface(t *testing.T) {
	h := newHarness(t)
	infos, err := h.client.ListTools(context.Background(), identity.Caller{UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if tools.ListDescription == "" || infos[0].Description != tools.ListDescription {
		t.Fatal("list description differs from constant")
	}
	page, _ := json.Marshal(paging.PageDescription)
	limit, _ := json.Marshal(paging.LimitDescription)
	next, _ := json.Marshal(paging.NextDescription)
	input := `{"type":"object","properties":{"page":{"type":"string","description":` + string(page) + `},"limit":{"type":"integer","description":` + string(limit) + `}},"additionalProperties":false}`
	output := `{"type":"object","properties":{"sites":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"slug":{"type":"string"},"url":{"type":"string"},"path":{"type":"string"},"visibility":{"type":"string"},"listed":{"type":"boolean"},"commit":{"type":"string"}},"required":["id","name","slug","url","visibility","listed"],"additionalProperties":false}},"next":{"type":"string","description":` + string(next) + `}},"required":["sites"],"additionalProperties":false}`
	if string(infos[0].InputSchema) != input {
		t.Fatalf("input %s want %s", infos[0].InputSchema, input)
	}
	if string(infos[0].OutputSchema) != output {
		t.Fatalf("output %s want %s", infos[0].OutputSchema, output)
	}
}

// R-5035-ZXSQ R-51B2-DPJF R-A4AE-BRL4
func TestListRulesBeforeCatalog(t *testing.T) {
	h := newHarness(t)
	h.add(t, "alice", "blog", true)
	h.add(t, "bob", "other", true)
	before := catalogSnapshot(t, h)
	token := paging.Token{Tool: "list"}
	badPages := []string{"mistyped", token.Encode("blog") + "x", paging.Token{Tool: "list_widgets"}.Encode("blog"), token.Encode(int64(3)), token.Encode("a", "b")}
	for _, failing := range []bool{false, true} {
		h.db.SetFailing(failing)
		for _, badPage := range append(append([]string{}, badPages...), "", token.Encode("blog")) {
			for _, limit := range []int{0, -1, paging.MaxLimit + 1} {
				_, err := paging.Window(&limit)
				args, _ := json.Marshal(tools.ListArgs{Page: &badPage, Limit: &limit})
				if got := refusal(t, h.call(t, "alice", "list", string(args))); got != err.Error() {
					t.Fatalf("limit refusal %q want %q", got, err.Error())
				}
			}
		}
		for _, badPage := range badPages {
			args, _ := json.Marshal(map[string]any{"page": badPage, "limit": 1})
			if got := refusal(t, h.call(t, "alice", "list", string(args))); got != paging.BadPage {
				t.Fatal(got)
			}
		}
		for _, args := range []string{`{}`, `{"limit":null}`, `{"page":null}`, `{"page":"","limit":null}`} {
			r := h.call(t, "alice", "list", args)
			if failing {
				if got := refusal(t, r); got != store.Unreachable {
					t.Fatal(got)
				}
			} else if got := decodeListing(t, resultObject(t, r)); len(got.Sites) != 1 || got.Sites[0].Name != "blog" || got.Next != nil {
				t.Fatal(got)
			}
		}
		h.db.SetFailing(false)
		if !reflect.DeepEqual(before, catalogSnapshot(t, h)) {
			t.Fatal("list rule changed catalog")
		}
	}
}

func decodeListing(t *testing.T, raw json.RawMessage) tools.SiteList {
	t.Helper()
	var out tools.SiteList
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// R-6CQL-PTJO R-6DYI-3LAD
func TestListPositionAndRemainder(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"aa", "a1", "a-b"} {
		h.add(t, "alice", name, true)
	}
	h.add(t, "bob", "a0", true)
	token := paging.Token{Tool: "list"}
	cases := []struct {
		user, position string
		limit          int
		names          []string
		next           string
	}{
		{"alice", "", 2, []string{"a-b", "a1"}, "a1"},
		{"alice", "a-b", 2, []string{"a1", "aa"}, ""},
		{"alice", "-", 1, []string{"a-b"}, "a-b"},
		{"alice", string([]byte{0xff}), 2, []string{}, ""},
		{"alice", "a0", 2, []string{"a1", "aa"}, ""},
		{"nobody", "", 2, []string{}, ""},
		{"bob", "a-b", 2, []string{"a0"}, ""},
	}
	for _, c := range cases {
		args := map[string]any{"limit": c.limit}
		if c.position != "" {
			args["page"] = token.Encode(c.position)
		}
		rawArgs, _ := json.Marshal(args)
		raw := resultObject(t, h.call(t, c.user, "list", string(rawArgs)))
		out := decodeListing(t, raw)
		names := make([]string, 0, len(out.Sites))
		for _, s := range out.Sites {
			names = append(names, s.Name)
		}
		if !reflect.DeepEqual(names, c.names) {
			t.Fatalf("position %q names %v want %v", c.position, names, c.names)
		}
		if c.next == "" {
			var members map[string]json.RawMessage
			if err := json.Unmarshal(raw, &members); err != nil {
				t.Fatal(err)
			}
			if _, ok := members["next"]; ok {
				t.Fatalf("last page next: %s", raw)
			}
		} else if out.Next == nil || *out.Next != token.Encode(c.next) {
			t.Fatalf("next %v want %s", out.Next, token.Encode(c.next))
		}
		if len(c.names) == 0 && string(raw) != `{"sites":[]}` {
			t.Fatalf("empty %s", raw)
		}
	}
}

// R-54YR-J0RI R-6CQL-PTJO R-6DYI-3LAD
func TestListTraversal(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 1} {
		for _, limit := range []int{-1, 0, 1, 2, paging.DefaultLimit, paging.MaxLimit} {
			t.Run(fmt.Sprintf("count=%d/limit=%d", count, limit), func(t *testing.T) {
				h := newHarness(t)
				want := make([]string, count)
				for i := count - 1; i >= 0; i-- {
					want[i] = fmt.Sprintf("site-%04d", i)
					h.add(t, "alice", want[i], true)
				}
				h.add(t, "bob", "site-0000-other", true)
				args := map[string]any{}
				var requested *int
				if limit == -1 {
					args["limit"] = nil
				} else if limit != 0 {
					args["limit"] = limit
					requested = &limit
				}
				size, err := paging.Window(requested)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				sizes := []int{}
				for call := 0; call <= count; call++ {
					raw, _ := json.Marshal(args)
					out := decodeListing(t, resultObject(t, h.call(t, "alice", "list", string(raw))))
					sizes = append(sizes, len(out.Sites))
					for _, site := range out.Sites {
						got = append(got, site.Name)
					}
					if out.Next == nil {
						break
					}
					args["page"] = *out.Next
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("traversal %v want %v", got, want)
				}
				expected := []int{}
				for remaining := count; remaining > 0; remaining -= size {
					expected = append(expected, min(size, remaining))
				}
				if !reflect.DeepEqual(sizes, expected) {
					t.Fatalf("page sizes %v want %v", sizes, expected)
				}
			})
		}
	}
}
