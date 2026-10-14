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

// R-V47O-LOYZ R-VCQZ-A35U R-VF6S-1MN8 R-VHMK-T64M R-V2ZS-7X8A
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
				data.Prev = paging.Token{Tool: "list-before"}.Encode(last.Name)
				data.Prompts = []pages.PromptRow{{Name: last.Name, URL: "/" + last.Name + "/", Model: last.Model}}
				assertWindow(t, f, "/?page="+url.QueryEscape(data.Page)+"&page=bogus&limit=1", "landing", data)
			}
		})
	}
}

// R-V7VD-R072 R-VDYV-NUWJ R-VGEO-FEDX R-VIUH-6XVB R-V2ZS-7X8A
func TestPromptPaginationParity(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			p := create(t, f, "fixture", "alice")
			other := create(t, f, "other", "alice")
			add(t, f, other, 1000, store.StatusQueued, store.Ending{})
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
				data.Prev = paging.Token{Tool: "runs-before"}.Encode(ordered[paging.DefaultLimit].Started.UTC().Format(time.RFC3339), ordered[paging.DefaultLimit].ID)
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

// R-V2ZS-7X8A R-VRDR-VC26 R-VSLO-93SV R-D2MT-S56G R-D52M-JONU R-D6AI-XGEJ R-VYP6-5YIC
func TestInvalidPagePrecedesCatalog(t *testing.T) {
	f := setup(t)
	create(t, f, "fixture", "alice")
	create(t, f, "private", "bob")
	list := paging.Token{Tool: "list"}.Encode("fixture")
	runs := paging.Token{Tool: "runs"}.Encode(instant.UTC().Format(time.RFC3339), "prr_0000000000000001")
	paths := []string{"/?page=bogus", "/?page=LAST", "/?page=last/", "/?page=" + url.QueryEscape(paging.Token{Tool: "runs-before"}.Encode("time", "id")), "/?page=" + url.QueryEscape(runs), "/?page=" + url.QueryEscape(list+"x")}
	for _, name := range []string{"fixture", "private", "nope", "about", "tools", "", "%66ixture"} {
		for _, token := range []string{"bogus", "LAST", "last/", list, paging.Token{Tool: "list-before"}.Encode("fixture"), runs + "x"} {
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

// R-V5FK-ZGPO R-V6NH-D8GD R-VK2D-KPM0 R-VMI6-C93E R-VOXZ-3SKS
func TestCatalogBackwardAndLastWindows(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			var rows []pages.PromptRow
			for i := 0; i < count; i++ {
				p := create(t, f, fmt.Sprintf("prompt-%03d", i), "alice")
				rows = append(rows, pages.PromptRow{Name: p.Name, URL: "/" + p.Name + "/", Model: p.Model})
			}
			create(t, f, "prompt-001-private", "bob")
			check := func(value string, low, high int, backward, last bool) {
				t.Helper()
				data := pages.LandingData{Banner: wantBanner("alice"), Page: value, Prompts: rows[low:high]}
				if value != "" && low < high {
					data.Prev = paging.Token{Tool: "list-before"}.Encode(rows[low].Name)
				}
				if low < high && !last && (backward || high < count) {
					data.Next = paging.Token{Tool: "list"}.Encode(rows[high-1].Name)
				}
				assertWindow(t, f, "/?page="+url.QueryEscape(value)+"&page=bogus&limit=1&other=ignored", "landing", data)
			}
			if count <= paging.DefaultLimit {
				assertUnderflow(t, f, "/?page=last&other=ignored", "/")
				assertUnderflow(t, f, "/?page="+url.QueryEscape(paging.Token{Tool: "list-before"}.Encode("")), "/")
				return
			}
			// Walk forward by tool tokens, then back through the same window boundaries.
			var starts []int
			value := ""
			for low := 0; low < count; low += paging.DefaultLimit {
				high := min(low+paging.DefaultLimit, count)
				starts = append(starts, low)
				check(value, low, high, false, false)
				value = paging.Token{Tool: "list"}.Encode(rows[high-1].Name)
			}
			for j := len(starts) - 2; j >= 0; j-- {
				key := rows[starts[j+1]].Name
				value = paging.Token{Tool: "list-before"}.Encode(key)
				if j == 0 {
					assertUnderflow(t, f, "/?page="+url.QueryEscape(value), "/")
				} else {
					check(value, starts[j], starts[j]+paging.DefaultLimit, true, false)
				}
			}
			check("last", count-paging.DefaultLimit, count, true, true)
			// An unoccupied position has the same preceding window as its successor.
			key := rows[count-1].Name
			check(paging.Token{Tool: "list-before"}.Encode(key), count-1-paging.DefaultLimit, count-1, true, false)
			check(paging.Token{Tool: "list-before"}.Encode(rows[count-2].Name+"-gap"), count-1-paging.DefaultLimit, count-1, true, false)
			check(paging.Token{Tool: "list-before"}.Encode(""), count-paging.DefaultLimit, count, true, false)
			assertUnderflow(t, f, "/?page="+url.QueryEscape(paging.Token{Tool: "list-before"}.Encode(rows[paging.DefaultLimit].Name)), "/")
			check(paging.Token{Tool: "list"}.Encode("zzz"), count, count, false, false)
		})
	}
}

func assertUnderflow(t *testing.T, f *fixture, path, location string) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		f.calls = nil
		r := request(method, path, "alice")
		r.Header.Set("If-None-Match", "fixture-header")
		r.Body = io.NopCloser(strings.NewReader("fixture-body"))
		w := serve(f.cfg, r)
		if _, cookie := w.Header()["Set-Cookie"]; w.Code != 303 || !reflect.DeepEqual(w.Header().Values("Location"), []string{location}) || cookie || len(f.calls) != 0 || method == "HEAD" && w.Body.Len() != 0 {
			t.Fatalf("underflow %s %s: %d %v, calls %v", method, path, w.Code, w.Header(), f.calls)
		}
	}
}

// R-V93A-4RXR R-VAB6-IJOG R-VLA9-YHCP R-VNQ2-Q0U3 R-VQ5V-HKBH
func TestRunsBackwardAndLastWindows(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			p := create(t, f, "fixture", "alice")
			other := create(t, f, "other", "alice")
			add(t, f, other, 1000, store.StatusQueued, store.Ending{})
			var ordered []store.Run
			for i := 0; i < count; i++ {
				// Every record shares a second, including all window boundaries.
				r, e := f.cfg.Store.AddRun(context.Background(), store.Run{ID: fmt.Sprintf("prr_%016x", (count-i)*2), Prompt: p.ID, Model: "fixture-run-model", User: "alice", Trigger: store.TriggerManual, Status: store.StatusQueued, Started: instant})
				if e != nil {
					t.Fatal(e)
				}
				ordered = append(ordered, r)
			}
			token := func(tool string, r store.Run) string {
				return paging.Token{Tool: tool}.Encode(r.Started.UTC().Format(time.RFC3339), r.ID)
			}
			check := func(value string, low, high int, backward, last bool) {
				t.Helper()
				data := pages.PromptData{Banner: wantBanner("alice", page.Level{Name: p.Name, URL: "/fixture/"}), Prompt: pages.PromptCard{ID: p.ID, Name: p.Name, Model: p.Model, Text: p.Prompt, System: p.System, Schema: string(p.Schema), Created: p.Created.UTC().Format(pages.CardLayout), CreatedAt: p.Created.UTC().Format(time.RFC3339), RunsKept: count, KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}, Page: value}
				for i, g := range p.Tools {
					data.Prompt.Tools = append(data.Prompt.Tools, pages.ToolGroup{Name: g, First: i == 0, Last: i == len(p.Tools)-1})
				}
				for _, r := range ordered[low:high] {
					data.Runs = append(data.Runs, expectedRow(r, p.Name))
				}
				if value != "" && low < high {
					data.Prev = token("runs-before", ordered[low])
				}
				if low < high && !last && (backward || high < count) {
					data.Next = token("runs", ordered[high-1])
				}
				assertWindow(t, f, "/fixture/?page="+url.QueryEscape(value)+"&page=bogus&limit=1&other=ignored", "prompt", data)
			}
			if count <= paging.DefaultLimit {
				assertUnderflow(t, f, "/fixture/?page=last&other=ignored", "/fixture/")
				assertUnderflow(t, f, "/fixture/?page="+url.QueryEscape(paging.Token{Tool: "runs-before"}.Encode("ignored", "")), "/fixture/")
				return
			}
			var starts []int
			value := ""
			for low := 0; low < count; low += paging.DefaultLimit {
				high := min(low+paging.DefaultLimit, count)
				starts = append(starts, low)
				check(value, low, high, false, false)
				value = token("runs", ordered[high-1])
			}
			for j := len(starts) - 2; j >= 0; j-- {
				value = token("runs-before", ordered[starts[j+1]])
				if j == 0 {
					assertUnderflow(t, f, "/fixture/?page="+url.QueryEscape(value), "/fixture/")
				} else {
					check(value, starts[j], starts[j]+paging.DefaultLimit, true, false)
				}
			}
			check("last", count-paging.DefaultLimit, count, true, true)
			check(token("runs-before", ordered[count-1]), count-1-paging.DefaultLimit, count-1, true, false)
			// Delete a run between the oldest two; its token still selects the preceding window.
			missing := ordered[count-1]
			missing.ID = fmt.Sprintf("prr_%016x", 3)
			if _, err := f.cfg.Store.AddRun(context.Background(), missing); err != nil {
				t.Fatal(err)
			}
			if err := f.cfg.Store.DeleteRun(context.Background(), missing.ID); err != nil {
				t.Fatal(err)
			}
			check(token("runs-before", missing), count-1-paging.DefaultLimit, count-1, true, false)
			check(paging.Token{Tool: "runs-before"}.Encode("ignored", ""), count-paging.DefaultLimit, count, true, false)
			assertUnderflow(t, f, "/fixture/?page="+url.QueryEscape(token("runs-before", ordered[paging.DefaultLimit])), "/fixture/")
			check(paging.Token{Tool: "runs"}.Encode("", "absent"), count, count, false, false)
		})
	}
}
