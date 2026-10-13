package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

func pagingArgs(name string, page *string, limit *int) string {
	b, _ := json.Marshal(map[string]any{"name": name, "page": page, "limit": limit})
	if name == "" {
		b, _ = json.Marshal(tools.ListArgs{Page: page, Limit: limit})
	}
	return string(b)
}
func pagePtr[T any](v T) *T { return &v }

// R-TU96-D56K R-TXWV-IGEN R-U589-T2UT R-U7O2-KMC7
// R-UA3V-C5TL R-UG7D-90J2 R-UIN6-0K0G R-UL2Y-S3HU
func TestPagingMemberDescriptions(t *testing.T) {
	f := setup(t)
	infos, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name != "list" && info.Name != "runs" {
			continue
		}
		for _, schema := range []struct {
			raw  json.RawMessage
			want map[string]string
		}{{info.InputSchema, map[string]string{"page": paging.PageDescription, "limit": paging.LimitDescription}}, {info.OutputSchema, map[string]string{"next": paging.NextDescription}}} {
			var got struct {
				Properties map[string]struct{ Description string }
			}
			if err := json.Unmarshal(schema.raw, &got); err != nil {
				t.Fatal(err)
			}
			for field, want := range schema.want {
				if got.Properties[field].Description != want {
					t.Fatal(info.Name, field, got)
				}
			}
		}
	}
}

// R-UNIR-JMZ8 R-UPYK-B6GM R-USED-2PY0
func TestPagingRefusalOrder(t *testing.T) {
	f := setup(t)
	p := f.seed(t, "daily")
	listToken, runsToken := paging.Token{Tool: "list"}, paging.Token{Tool: "runs"}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, tool := range []string{"list", "runs"} {
			for _, limit := range []int{0, paging.MaxLimit + 1} {
				_, err := paging.Window(&limit)
				refused(t, f.call(t, tool, pagingArgs(map[string]string{"list": "", "runs": "Daily Digest"}[tool], pagePtr("bogus"), &limit)), err.Error())
			}
		}
		for _, token := range []string{"bogus", listToken.Encode(int64(7)), listToken.Encode("a", "b"), runsToken.Encode("daily")} {
			refused(t, f.call(t, "list", pagingArgs("", &token, nil)), paging.BadPage)
		}
		for _, token := range []string{"bogus", runsToken.Encode("2026-10-05T09:00:07Z"), runsToken.Encode(int64(1759654807), "prr_5a5a5a5a5a5a5a5a"), listToken.Encode("2026-10-05T09:00:07Z", "prr_5a5a5a5a5a5a5a5a")} {
			refused(t, f.call(t, "runs", pagingArgs("nope", &token, nil)), paging.BadPage)
		}
		if failing {
			refused(t, f.call(t, "runs", `{"name":"Daily Digest"}`), fmt.Sprintf(tools.MissingPrompt, "Daily Digest"))
		}
	}
	f.d.SetFailing(false)
	if got := decode[tools.Prompt](t, f.call(t, "show", `{"name":"daily"}`)); !reflect.DeepEqual(got, p) {
		t.Fatal(got, p)
	}
}

// R-UUU5-U9FE R-UX9Y-LSWS R-UZPR-DCE6
func TestPromptPages(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			names := []string{"a-b", "a1", "aa"}
			for i := len(names); i < count; i++ {
				names = append(names, fmt.Sprintf("p%03d", i))
			}
			for _, name := range names {
				f.seed(t, name)
			}
			p := decode[tools.Prompt](t, f.call(t, "show", `{"name":"a-b"}`))
			addPageRun(t, f, p, "prr_1111111111111111", f.now)
			last := addPageRun(t, f, p, "prr_eeeeeeeeeeeeeeee", f.now)
			first := decode[tools.PromptList](t, f.call(t, "list", pagingArgs("", nil, pagePtr(1))))
			if first.Prompts[0].LastRun == nil || first.Prompts[0].LastRun.ID != last.ID {
				t.Fatal(first)
			}

			owner := f.caller
			f.caller.UserID = "other"
			f.seed(t, "foreign")
			f.caller = owner
			for _, limit := range []*int{nil, pagePtr(1), pagePtr(paging.MaxLimit)} {
				var seen []string
				var page *string
				for {
					out := decode[tools.PromptList](t, f.call(t, "list", pagingArgs("", page, limit)))
					n, _ := paging.Window(limit)
					if len(out.Prompts) > n {
						t.Fatal(out)
					}
					for _, p := range out.Prompts {
						seen = append(seen, p.Name)
					}
					if out.Next == nil {
						break
					}
					if *out.Next != (paging.Token{Tool: "list"}).Encode(out.Prompts[len(out.Prompts)-1].Name) {
						t.Fatal(out)
					}
					page = out.Next
				}
				if !reflect.DeepEqual(seen, names) {
					t.Fatal(seen, names)
				}
			}
			for _, args := range []string{`{}`, `{"page":null,"limit":null}`, `{"page":""}`} {
				out := decode[tools.PromptList](t, f.call(t, "list", args))
				if len(out.Prompts) != paging.DefaultLimit || (out.Next != nil) != (count > paging.DefaultLimit) {
					t.Fatal(out)
				}
			}
			token := (paging.Token{Tool: "list"}).Encode("-")
			if got := decode[tools.PromptList](t, f.call(t, "list", pagingArgs("", &token, pagePtr(1)))); len(got.Prompts) != 1 || got.Prompts[0].Name != names[0] {
				t.Fatal(got)
			}
			token = (paging.Token{Tool: "list"}).Encode(string([]byte{255}))
			if got := decode[tools.PromptList](t, f.call(t, "list", pagingArgs("", &token, nil))); len(got.Prompts) != 0 || got.Next != nil {
				t.Fatal(got)
			}
			f.caller.UserID = "empty"
			if string(content(t, f.call(t, "list", `{}`))) != `{"prompts":[]}` {
				t.Fatal("empty list")
			}
		})
	}
}

func addPageRun(t *testing.T, f *fixture, p tools.Prompt, id string, started time.Time) store.Run {
	t.Helper()
	r, err := f.st.AddRun(context.Background(), store.Run{ID: id, Prompt: p.ID, Model: model(), User: f.caller.UserID, Trigger: store.TriggerManual, Status: store.StatusRunning, Started: started})
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.st.FinishRun(context.Background(), r.ID, store.Ending{Status: store.StatusExited, Finished: started.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// R-UUU5-U9FE R-V25K-4VVK R-V4LC-WFCY R-K7HK-UVA9
func TestRunPages(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 1, 30} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			p := f.seed(t, "daily")
			q := f.seed(t, "other")
			var records []store.Run
			for i := 0; i < count; i++ {
				records = append(records, addPageRun(t, f, p, fmt.Sprintf("prr_%016x", (i*7)%count+1), f.now.Add(-time.Duration(i/3)*time.Second)))
			}
			sort.Slice(records, func(i, j int) bool {
				a, b := records[i], records[j]
				if a.Started.Equal(b.Started) {
					return a.ID > b.ID
				}
				return a.Started.After(b.Started)
			})
			for _, limit := range []*int{nil, pagePtr(4), pagePtr(paging.MaxLimit)} {
				var seen []string
				var page *string
				pages := 0
				for {
					out := decode[tools.RunList](t, f.call(t, "runs", pagingArgs(p.Name, page, limit)))
					pages++
					n, _ := paging.Window(limit)
					if len(out.Runs) > n {
						t.Fatal(out)
					}
					for _, r := range out.Runs {
						seen = append(seen, r.ID)
						if r.Finished == nil || r.Calls == nil || *r.Calls != 0 || r.ToolCalls == nil || r.InputTokens == nil || r.CachedTokens == nil || r.OutputTokens == nil || r.ReasoningTokens == nil || r.CostNanos == nil {
							t.Fatal(r)
						}
					}
					if out.Next == nil {
						break
					}
					last := out.Runs[len(out.Runs)-1]
					if *out.Next != (paging.Token{Tool: "runs"}).Encode(last.Started, last.ID) {
						t.Fatal(out)
					}
					page = out.Next
				}
				want := make([]string, len(records))
				for i, r := range records {
					want[i] = r.ID
				}
				if !reflect.DeepEqual(seen, want) {
					t.Fatal(seen, want)
				}
				if count == 30 && limit != nil && *limit == 4 && pages != 8 {
					t.Fatal(pages)
				}
			}
			if string(content(t, f.call(t, "runs", pagingArgs(q.Name, nil, nil)))) != `{"runs":[]}` {
				t.Fatal("empty runs")
			}
			position := records[1]
			r := addPageRun(t, f, q, "prr_ffffffffffffffff", position.Started.Add(-time.Second))
			token := (paging.Token{Tool: "runs"}).Encode(position.Started.UTC().Format(time.RFC3339), position.ID)
			out := decode[tools.RunList](t, f.call(t, "runs", pagingArgs(q.Name, &token, nil)))
			if len(out.Runs) != 1 || out.Runs[0].ID != r.ID || out.Next != nil {
				t.Fatal(out)
			}
			token = (paging.Token{Tool: "runs"}).Encode("2000-01-01T00:00:00Z", "")
			if got := decode[tools.RunList](t, f.call(t, "runs", pagingArgs(q.Name, &token, nil))); len(got.Runs) != 1 {
				t.Fatal(got)
			}
			for _, args := range []string{`{"name":"daily"}`, `{"name":"daily","page":null,"limit":null}`, `{"name":"daily","page":""}`} {
				got := decode[tools.RunList](t, f.call(t, "runs", args))
				if len(got.Runs) != paging.DefaultLimit || (got.Next != nil) != (count > paging.DefaultLimit) {
					t.Fatal(got)
				}
			}
		})
	}
}
