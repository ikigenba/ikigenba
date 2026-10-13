package pages_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
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

// R-VECJ-YLAI R-VPBN-EIYR
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
			assertPage(t, serve(f.cfg, request("GET", "/?page=ignored", "alice")), 200, "landing", data)
			if data.Next != "" {
				rest := catalogCall[tools.PromptList](t, client, "list", map[string]string{"page": data.Next})
				if len(rest.Prompts) != 1 || rest.Prompts[0].Name != fmt.Sprintf("prompt-%03d", paging.DefaultLimit) || rest.Next != nil {
					t.Fatalf("remainder: %+v", rest)
				}
			}
		})
	}
}

// R-VGSC-Q4RW R-VJ85-HO9A R-WU9A-VKXG
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
			assertPage(t, serve(f.cfg, request("GET", "/fixture/?page=ignored", "alice")), 200, "prompt", data)
			if data.Next != "" {
				rest := catalogCall[tools.RunList](t, client, "runs", map[string]string{"name": p.Name, "page": data.Next})
				if len(rest.Runs) != 3 || rest.Next != nil {
					t.Fatalf("remainder: %+v", rest)
				}
				for i, run := range ordered[paging.DefaultLimit:] {
					if rest.Runs[i].ID != run.ID {
						t.Fatalf("remainder %d: %+v", i, rest.Runs[i])
					}
				}
			}
		})
	}
}
