package cli_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/cli"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

type braveListener struct {
	net.Listener
	beforeAccept func()
}

func (l braveListener) Accept() (net.Conn, error) {
	l.beforeAccept()
	return l.Listener.Accept()
}

// R-EWQH-AQW6 R-5ZU6-V3HN
func TestServeSnapshotsBraveKeyWithoutRefusingStart(t *testing.T) {
	for _, tc := range []struct {
		name, initial string
		present       bool
		wantRequests  int64
	}{
		{"present", "snapshot-brave-fixture", true, 2},
		{"absent", "ignored-absent-value", false, 0},
		{"empty", "", true, 0},
		{"whitespace", " \t\n", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newServeFixture(t)
			sink := integrationSink(f)
			requests := &atomic.Int64{}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				integrationAnswer(w, "supplied web answer")
			}))
			defer provider.Close()
			f.env[agent.KeyVariable(agentkit.HostAnthropic)] = "provider-fixture-key"
			f.p.BaseURL = provider.URL
			lookups := &atomic.Int64{}
			inherited := &atomic.Bool{}
			originalInherit := f.p.Inherit
			f.p.Inherit = func(fd uintptr) (net.Listener, error) {
				ln, err := originalInherit(fd)
				inherited.Store(err == nil)
				return braveListener{Listener: ln, beforeAccept: func() {
					if lookups.Load() != 1 {
						t.Error("accept preceded Brave snapshot")
					}
				}}, err
			}
			changed := &atomic.Bool{}
			originalLookup := f.p.LookupEnv
			f.p.LookupEnv = func(key string) (string, bool) {
				if key != "BRAVE_API_KEY" {
					return originalLookup(key)
				}
				lookups.Add(1)
				if !inherited.Load() {
					t.Error("Brave lookup preceded inherited listener")
				}
				if changed.Load() {
					if tc.present && tc.initial != "" {
						return "", false
					}
					return "later-brave-fixture", true
				}
				return tc.initial, tc.present
			}
			f.start(t)
			if lookups.Load() != 1 || f.err.String() != "" {
				t.Fatal("startup", lookups.Load(), f.err.String())
			}
			changed.Store(true)
			if list := decodeServe[tools.PromptList](t, f.call(t, "list", map[string]any{})); len(list.Prompts) != 0 {
				t.Fatal(list)
			}
			p := integrationPrompt(t, f, "web-snapshot", []string{"web"})
			for range 2 {
				started := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
				if started.Status != store.StatusRunning && started.Status != store.StatusQueued {
					t.Fatal(started)
				}
				awaitServeEvent(t, sink, "run.finished", started.ID)
				result := decodeServe[tools.RunResult](t, f.call(t, "result", map[string]any{"run": started.ID}))
				if result.Status != store.StatusExited || result.ExitCode == nil {
					t.Fatal(result)
				}
				if tc.wantRequests > 0 && *result.ExitCode != 0 || tc.wantRequests == 0 && *result.ExitCode != agent.ExitUnusable {
					t.Fatal(result)
				}
			}
			if code := f.stop(t); code != cli.ExitSuccess || f.err.String() != "" || requests.Load() != tc.wantRequests || lookups.Load() != 1 {
				t.Fatal(code, f.err.String(), requests.Load(), lookups.Load())
			}
		})
	}
}
