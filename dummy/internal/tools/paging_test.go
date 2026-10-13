package tools_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func pageJSON(t *testing.T, rows []widget.Widget, next string) string {
	t.Helper()
	raw := listJSON(t, rows)
	if next == "" {
		return raw
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	return raw[:len(raw)-1] + `,"next":` + string(encoded) + `}`
}

// R-ZOC8-C061 R-ZPK4-PRWQ: ordered page answers and continuation across each edge.
func TestPagedListResults(t *testing.T) {
	for _, count := range []int{0, 1, paging.DefaultLimit, paging.DefaultLimit + 1, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store, _ := toolsDatabaseStore(t)
			var all []widget.Widget
			for i := range count {
				w, errs := toolsStoreCreate(t, store, widget.Draft{Name: fmt.Sprintf("row-%d", i), Count: i, Status: widget.StatusActive})
				if errs.Any() {
					t.Fatal(errs)
				}
				all = append(all, w)
			}
			client := clientOver(t, store)
			token := paging.Token{Tool: "list_widgets"}
			for _, args := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"page":null,"limit":null}`), json.RawMessage(`{"page":""}`)} {
				n := min(count, paging.DefaultLimit)
				next := ""
				if count > n {
					next = token.Encode(all[n-1].Seq)
				}
				assertSuccess(t, call(t, client, "list_widgets", args), pageJSON(t, all[:n], next))
			}
			for _, limit := range []int{1, 2, paging.DefaultLimit, paging.MaxLimit} {
				start, page := 0, ""
				for {
					end := min(start+limit, count)
					next := ""
					if end < count {
						next = token.Encode(all[end-1].Seq)
					}
					args, err := json.Marshal(map[string]any{"page": page, "limit": limit})
					if err != nil {
						t.Fatal(err)
					}
					assertSuccess(t, call(t, client, "list_widgets", args), pageJSON(t, all[start:end], next))
					if next == "" {
						break
					}
					start, page = end, next
				}
			}
			args, err := json.Marshal(map[string]any{"page": token.Encode(int64(1 << 62))})
			if err != nil {
				t.Fatal(err)
			}
			assertSuccess(t, call(t, client, "list_widgets", args), listJSON(t, nil))
			// R-HYD4-UGBQ: all paged reads leave the ordered store unchanged.
			if got := toolsStoreAll(t, store); !reflect.DeepEqual(got, all) {
				t.Fatalf("paged reads changed store: %+v", got)
			}
		})
	}
}

// R-ZKOJ-6OXY R-ZN4B-Y8FC: limit precedes page and both precede storage failures.
func TestPagingRefusalsBeforeStore(t *testing.T) {
	store, handle := toolsDatabaseStore(t)
	client := clientOver(t, store)
	for _, failing := range []bool{false, true} {
		handle.SetFailing(failing)
		for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
			_, want := paging.Window(&limit)
			for _, page := range []any{nil, "", "malformed!", paging.Token{Tool: "other"}.Encode(int64(3))} {
				args, err := json.Marshal(map[string]any{"limit": limit, "page": page})
				if err != nil {
					t.Fatal(err)
				}
				assertError(t, call(t, client, "list_widgets", args), want.Error())
			}
		}
		for _, page := range []string{"malformed!", paging.Token{Tool: "other"}.Encode(int64(3)), paging.Token{Tool: "list_widgets"}.Encode("wrong type"), paging.Token{Tool: "list_widgets"}.Encode(int64(3), int64(4))} {
			for _, limit := range []any{nil, 1, paging.MaxLimit} {
				args, err := json.Marshal(map[string]any{"page": page, "limit": limit})
				if err != nil {
					t.Fatal(err)
				}
				assertError(t, call(t, client, "list_widgets", args), paging.BadPage)
			}
			args, err := json.Marshal(map[string]any{"page": page})
			if err != nil {
				t.Fatal(err)
			}
			assertError(t, call(t, client, "list_widgets", args), paging.BadPage)
		}
	}
	handle.SetFailing(false)
}
