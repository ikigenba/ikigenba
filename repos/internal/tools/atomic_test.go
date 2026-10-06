package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

// Inject faults at the final Head invocation, after Rename changed the
// repository config and Size and Head's soundness check succeeded.
func TestAtomicMCPLateRenameDirectoryFaults(t *testing.T) {
	// R-ZCXC-ZLTY R-ZFD5-R5BC
	for _, fault := range []string{"git", "cancel", "root", "repository", "all-permissions"} {
		t.Run(fault, func(t *testing.T) {
			f := newToolsFixture(t)
			var armed atomic.Bool
			var calls atomic.Int64
			var cancelRequest context.CancelFunc
			var witness atomic.Bool
			var restore []func()
			var repoID string
			g, err := git.Find(filepath.Dir(f.GitPath), func() []string {
				env := append([]string(nil), f.Env...)
				if !armed.Load() {
					return env
				}
				n := calls.Add(1)
				if n == 3 {
					if strings.TrimSpace(f.git(t, f.Store.Dir(repoID), "config", "--get", "ikigenba.name")) != "journal" {
						t.Error("config was not renamed at late read")
					} else {
						witness.Store(true)
					}
					if fault == "cancel" {
						cancelRequest()
					}
					paths := []string{}
					switch fault {
					case "root":
						paths = []string{f.Root}
					case "repository":
						paths = []string{f.Store.Dir(repoID)}
					case "all-permissions":
						paths = []string{f.Store.Dir(repoID), f.Root}
					}
					for _, path := range paths {
						info, statErr := os.Stat(path)
						if statErr != nil {
							t.Error(statErr)
							continue
						}
						original := info.Mode().Perm()
						if chmodErr := os.Chmod(path, 0); chmodErr != nil {
							t.Error(chmodErr)
						}
						restore = append(restore, func() {
							info, statErr := os.Stat(path)
							if statErr != nil {
								t.Error(statErr)
							} else if info.Mode().Perm() != 0 {
								t.Errorf("rollback changed fault-time mode %s to %v", path, info.Mode().Perm())
							}
							toolsMust(t, os.Chmod(path, original))
						})
					}
				}
				// Every later git would encounter the same fault: undo must use raw
				// retained bytes, not another validated repository mutation.
				if n >= 3 && fault != "cancel" {
					env = atomicInvalidGit(env)
				}
				return env
			})
			toolsMust(t, err)
			atomicReplaceStore(t, f, g)
			r := f.create(t, f.Caller.UserID, "notes")
			atomicDecorateConfig(t, f, r.ID)
			repoID = r.ID
			before, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			disk := toolsSnapshot(t, f.Root)
			f.Client = atomicCancelServer(t, f, &cancelRequest)
			offset := len(f.events(t))
			armed.Store(true)
			refusal(t, f.call(t, "rename", toolsArguments(r.ID, "journal")), "cannot reach the repositories; try again later")
			toolsEqual(t, witness.Load(), true)
			toolsEqual(t, calls.Load(), int64(3))
			events := f.events(t)[offset:]
			toolsEqual(t, len(events), 1)
			toolsEqual(t, events[0].Name, "tool.called")
			toolsEqual(t, events[0].Attrs["outcome"], "error")
			// Restore parents before children, after the complete refusal, and check
			// cleanup preserved each injected permission change while rolling back.
			for i := len(restore) - 1; i >= 0; i-- {
				restore[i]()
			}
			armed.Store(false)
			got, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, got, before)
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
		})
	}
}

// The context seam lets a server cancel its request while the HTTP client
// stays connected, so MCP observes the actual tool refusal and error event.
func atomicCancelServer(t *testing.T, f *toolsFixture, cancel *context.CancelFunc) *mcp.Client {
	t.Helper()
	srv := mcp.NewServer(mcp.ServerConfig{Name: "repos", Version: "fixture", Telemetry: f.Writer})
	tools.Register(srv, tools.Config{Store: f.Store, Limits: f.Limits, Telemetry: f.Writer})
	handler := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, stop := context.WithCancel(clone.NewContext(r.Context(), f.Base))
		defer stop()
		*cancel = stop
		srv.ServeHTTP(w, r.WithContext(ctx))
	}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client(), Name: "fixture", Version: "fixture"})
}

func atomicInvalidGit(env []string) []string {
	for i, v := range env {
		if strings.HasPrefix(v, "GIT_CONFIG_KEY_0=") {
			env[i] = "GIT_CONFIG_KEY_0=invalid"
		}
	}
	return env
}

func atomicReplaceStore(t *testing.T, f *toolsFixture, g *git.Git) {
	t.Helper()
	f.StoreConfig.Git = g
	s, err := store.Open(toolsContext(t), f.DB, f.StoreConfig)
	toolsMust(t, err)
	f.Store = s
}
func atomicDecorateConfig(t *testing.T, f *toolsFixture, id string) {
	t.Helper()
	root, err := os.OpenRoot(f.Root)
	toolsMust(t, err)
	defer func() { toolsMust(t, root.Close()) }()
	path := filepath.Join(id+".git", "config")
	data, err := root.ReadFile(path)
	toolsMust(t, err)
	data = append(data, []byte("\n# preserve original spacing and mode\n[custom]\n\tvalue = untouched\n")...)
	toolsMust(t, root.WriteFile(path, data, 0600))
	toolsMust(t, root.Chmod(path, 0640))
}
