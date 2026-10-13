package pages_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

func catalogClient(t *testing.T, f *fixture) *mcp.Client {
	t.Helper()
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: &telemetry.Capture{}, Stderr: io.Discard})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "fixture done") })
	server := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: writer})
	tools.Register(server, tools.Config{Store: f.cfg.Store, Runs: f.cfg.Runs, Telemetry: writer})
	endpoint := httptest.NewServer(identity.Require(server))
	t.Cleanup(endpoint.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint.URL})
}

func catalogCall[T any](t *testing.T, client *mcp.Client, name string, args any) T {
	t.Helper()
	input, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), identity.Caller{UserID: "alice", Email: "alice@example.test"}, name, input)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := result.MarshalJSON()
	if err != nil || result.IsError() {
		t.Fatalf("call %s: %s %v", name, wire, err)
	}
	var wrapper struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal(wire, &wrapper); err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(wrapper.Structured, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func nextValue(next *string) string {
	if next == nil {
		return ""
	}
	return *next
}

// R-DEWD-0DU8 R-DJRY-JGT0 R-DM7R-B0AE R-DDOG-MM3J
func TestLandingPaginationParity(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			data := pages.LandingData{Banner: wantBanner("alice")}
			for n := count - 1; n >= 0; n-- {
				p := create(t, f, fmt.Sprintf("prompt-%03d", n), "alice")
				if n < paging.DefaultLimit {
					data.Prompts = append([]pages.PromptRow{{Name: p.Name, URL: "/" + p.Name + "/", Model: p.Model}}, data.Prompts...)
				}
			}
			create(t, f, "prompt-000-private", "bob")
			if count > paging.DefaultLimit {
				data.Next = paging.Token{Tool: "list"}.Encode(data.Prompts[len(data.Prompts)-1].Name)
			}
			client := catalogClient(t, f)
			first := catalogCall[tools.PromptList](t, client, "list", struct{}{})
			if len(first.Prompts) != len(data.Prompts) || nextValue(first.Next) != data.Next {
				t.Fatalf("first page: %+v", first)
			}
			for i, row := range data.Prompts {
				if first.Prompts[i].Name != row.Name {
					t.Fatalf("prompt %d: %+v", i, first.Prompts[i])
				}
			}
			for _, path := range []string{"/", "/?page=&page=bogus&limit=1&other=ignored"} {
				assertWindow(t, f, path, "landing", data)
			}
			if data.Next != "" {
				rest := catalogCall[tools.PromptList](t, client, "list", map[string]string{"page": data.Next})
				if len(rest.Prompts) != 1 || rest.Prompts[0].Name != fmt.Sprintf("prompt-%03d", paging.DefaultLimit) || rest.Next != nil {
					t.Fatalf("remainder: %+v", rest)
				}
				last := rest.Prompts[0]
				data.Page, data.Next = data.Next, ""
				data.Prompts = []pages.PromptRow{{Name: last.Name, URL: "/" + last.Name + "/", Model: last.Model}}
				assertWindow(t, f, "/?page="+url.QueryEscape(data.Page)+"&page=bogus&limit=1", "landing", data)
			}
		})
	}
}

// R-DHC5-RXBM R-DKZU-X8JP R-DNFN-OS13 R-DDOG-MM3J
func TestPromptPaginationParity(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			p := create(t, f, "fixture", "alice")
			other := create(t, f, "other", "alice")
			add(t, f, other, 100, store.StatusQueued, store.Ending{})
			var ordered []store.Run
			for i := 0; i < count; i++ {
				// Adjacent records share a second; their IDs deliberately oppose start order.
				run := store.Run{ID: fmt.Sprintf("prr_%016x", count-i), Prompt: p.ID, Model: "fixture-run-model", User: "alice", Trigger: store.TriggerManual, Status: store.StatusQueued, Started: instant.Add(time.Duration(i/2) * time.Second)}
				saved, err := f.cfg.Store.AddRun(context.Background(), run)
				if err != nil {
					t.Fatal(err)
				}
				if i%2 == 0 {
					ordered = append([]store.Run{saved}, ordered...)
				} else {
					ordered = append(ordered[:1], append([]store.Run{saved}, ordered[1:]...)...)
				}
			}
			data := pages.PromptData{Banner: wantBanner("alice", page.Level{Name: p.Name, URL: "/" + p.Name + "/"}), Prompt: pages.PromptCard{ID: p.ID, Name: p.Name, Model: p.Model, Text: p.Prompt, System: p.System, Schema: string(p.Schema), Created: p.Created.UTC().Format(pages.CardLayout), CreatedAt: p.Created.UTC().Format(time.RFC3339), RunsKept: count, KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}}
			for i, group := range p.Tools {
				data.Prompt.Tools = append(data.Prompt.Tools, pages.ToolGroup{Name: group, First: i == 0, Last: i == len(p.Tools)-1})
			}
			for _, run := range ordered[:paging.DefaultLimit] {
				data.Runs = append(data.Runs, expectedRow(run, p.Name))
			}
			if count > paging.DefaultLimit {
				last := ordered[paging.DefaultLimit-1]
				data.Next = paging.Token{Tool: "runs"}.Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
			}
			client := catalogClient(t, f)
			first := catalogCall[tools.RunList](t, client, "runs", map[string]string{"name": p.Name})
			if len(first.Runs) != len(data.Runs) || nextValue(first.Next) != data.Next {
				t.Fatalf("first page: %+v", first)
			}
			for i, row := range data.Runs {
				if first.Runs[i].ID != row.ID {
					t.Fatalf("run %d: %+v", i, first.Runs[i])
				}
			}
			for _, path := range []string{"/fixture/", "/fixture/?page=&page=bogus&limit=1&other=ignored"} {
				assertWindow(t, f, path, "prompt", data)
			}
			if data.Next != "" {
				rest := catalogCall[tools.RunList](t, client, "runs", map[string]string{"name": p.Name, "page": data.Next})
				if len(rest.Runs) != 3 || rest.Next != nil {
					t.Fatalf("remainder: %+v", rest)
				}
				data.Page, data.Next = data.Next, ""
				data.Runs = nil
				for i, run := range ordered[paging.DefaultLimit:] {
					if rest.Runs[i].ID != run.ID {
						t.Fatalf("remainder %d: %+v", i, rest.Runs[i])
					}
					data.Runs = append(data.Runs, expectedRow(run, p.Name))
				}
				assertWindow(t, f, "/fixture/?page="+url.QueryEscape(data.Page)+"&page=bogus&limit=1", "prompt", data)
			}
		})
	}
}

func assertWindow(t *testing.T, f *fixture, path, template string, data any) {
	t.Helper()
	get := serve(f.cfg, request("GET", path, "alice"))
	assertPage(t, get, 200, template, data)
	head := serve(f.cfg, request("HEAD", path, "alice"))
	if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
		t.Fatalf("HEAD %s differs: %d %v", path, head.Code, head.Header())
	}
}

// R-DDOG-MM3J R-DONK-2JRS R-DPVG-GBIH R-D2MT-S56G R-D52M-JONU R-D6AI-XGEJ R-D8QB-OZVX
func TestInvalidPagePrecedesCatalog(t *testing.T) {
	f := setup(t)
	create(t, f, "fixture", "alice")
	create(t, f, "private", "bob")
	list := paging.Token{Tool: "list"}.Encode("fixture")
	runs := paging.Token{Tool: "runs"}.Encode(instant.UTC().Format(time.RFC3339), "prr_0000000000000001")
	paths := []string{"/?page=bogus", "/?page=" + url.QueryEscape(runs), "/?page=" + url.QueryEscape(list+"x")}
	for _, name := range []string{"fixture", "private", "nope", "about", "tools", "", "%66ixture"} {
		for _, token := range []string{"bogus", list, runs + "x"} {
			paths = append(paths, "/"+name+"/?page="+url.QueryEscape(token)+"&page=&limit=1&other=ignored")
		}
	}
	for _, failing := range []bool{false, true} {
		f.db.SetFailing(failing)
		for _, cancelled := range []bool{false, true} {
			for _, path := range paths {
				var get *httptest.ResponseRecorder
				for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
					r := request(method, path, "alice")
					r.Body = io.NopCloser(strings.NewReader("fixture-body"))
					r.Header.Set("If-None-Match", "fixture-header")
					if cancelled {
						ctx, cancel := context.WithCancel(r.Context())
						cancel()
						r = r.WithContext(ctx)
					}
					f.calls = nil
					w := serve(f.cfg, r)
					switch method {
					case "GET":
						assertPage(t, w, 404, "notfound", pages.NoticeData{Banner: bannerValue(page.User{})})
						get = w
					case "HEAD":
						if w.Code != get.Code || !reflect.DeepEqual(w.Header(), get.Header()) || w.Body.Len() != 0 {
							t.Fatalf("invalid HEAD %s differs", path)
						}
					default:
						if w.Code != 405 || w.Body.Len() != 0 || !reflect.DeepEqual(w.Header(), http.Header{"Allow": []string{"GET, HEAD"}}) || len(f.calls) != 0 {
							t.Fatalf("method %s %s: %d %v", method, path, w.Code, w.Header())
						}
					}
					if method == "GET" || method == "HEAD" {
						if !reflect.DeepEqual(f.calls, []page.User{{}}) || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
							t.Fatalf("invalid page %s: banner %v headers %v", path, f.calls, w.Header())
						}
					}
				}
			}
		}
	}
}
