package tools_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

// These MCP tests inject their faults at the actual final Head git invocation,
// after a separate catalog connection and the repository config witness a
// committed rename. The original late-Head regression remains unchanged.
func TestAtomicMCPCommittedRenameLateFaults(t *testing.T) {
	// R-14B4-62WX R-15J0-JUNM
	for _, fault := range []string{"git", "cancel", "source-directory", "source-file", "root", "repository", "all-permissions"} {
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
					// At this exact boundary Rename has returned and Size and Head's
					// soundness check have succeeded. Observe committed state by use.
					observer, openErr := store.Open(toolsContext(t), f.StoreConfig)
					if openErr != nil {
						t.Errorf("postcommit observer: %v", openErr)
					} else {
						row, findErr := observer.Find(toolsContext(t), f.Caller.UserID, "journal")
						if findErr != nil || row.Name != "journal" {
							t.Errorf("rename was not committed at late read: %#v, %v", row, findErr)
						} else {
							witness.Store(true)
						}
						if closeErr := observer.Close(); closeErr != nil {
							t.Error(closeErr)
						}
					}
					if strings.TrimSpace(f.git(t, f.Store.Dir(repoID), "config", "--get", "ikigenba.name")) != "journal" {
						t.Error("config was not renamed at late read")
					}
					if fault == "cancel" {
						cancelRequest()
					}
					paths := []string{}
					switch fault {
					case "source-directory":
						paths = []string{f.DBParent}
					case "source-file":
						paths = []string{f.StoreConfig.Source}
					case "root":
						paths = []string{f.Root}
					case "repository":
						paths = []string{f.Store.Dir(repoID)}
					case "all-permissions":
						paths = []string{f.Store.Dir(repoID), f.Root, f.StoreConfig.Source, f.DBParent}
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
			atomicReplaceStore(t, f, g, false)
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
			atomicReopenCatalog(t, f, before)
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

// Helpers stay in this exclusive test file; no other partition's helper or
// test file needs a concurrent change.
func atomicReplaceStore(t *testing.T, f *toolsFixture, g *git.Git, memory bool) {
	t.Helper()
	toolsMust(t, f.Store.Close())
	f.StoreConfig.Git = g
	if memory {
		f.StoreConfig.Source = ":memory:"
	}
	s, err := store.Open(toolsContext(t), f.StoreConfig)
	toolsMust(t, err)
	f.Store = s
	t.Cleanup(func() { toolsMust(t, s.Close()) })
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
func atomicReopenCatalog(t *testing.T, f *toolsFixture, want []store.Repo) {
	t.Helper()
	toolsMust(t, f.Store.Close())
	s, err := store.Open(toolsContext(t), f.StoreConfig)
	toolsMust(t, err)
	defer func() { toolsMust(t, s.Close()) }()
	got, err := s.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, got, want)
}

// These untagged tests prove the public coordination mechanism. They are not
// represented as MCP proofs: cancellation is injected after the committed
// method returns, then the same receiver performs its prescribed answer read.
func TestAtomicCoordinateLateCancellationAndPermissions(t *testing.T) {
	for _, memory := range []bool{false, true} {
		for _, op := range []string{"create", "rename"} {
			for _, fault := range []string{"cancel", "source-directory", "source-file", "root", "repository", "all-permissions"} {
				if memory && strings.HasPrefix(fault, "source-") {
					continue
				}
				t.Run(atomicCaseName(memory, op, fault), func(t *testing.T) {
					f := newToolsFixture(t)
					atomicReplaceStore(t, f, f.Git, memory)
					r := f.create(t, f.Caller.UserID, "notes")
					atomicDecorateConfig(t, f, r.ID)
					before, err := f.Store.All(toolsContext(t))
					toolsMust(t, err)
					disk := toolsSnapshot(t, f.Root)
					ctx, cancel := context.WithCancel(toolsContext(t))
					defer cancel()
					var restore []func()
					var committed store.Repo
					injected := errors.New("late test fault")
					err = f.Store.Coordinate(ctx, func(scoped context.Context) error {
						var mutationErr error
						if op == "create" {
							committed, mutationErr = f.Store.Create(scoped, f.Caller.UserID, "journal")
						} else {
							committed, mutationErr = f.Store.Rename(scoped, r.ID, "journal")
						}
						if mutationErr != nil {
							return mutationErr
						}
						// A nil mutation return is committed on this receiver, not staged.
						found, findErr := f.Store.Find(scoped, f.Caller.UserID, "journal")
						if findErr != nil {
							return findErr
						}
						toolsEqual(t, found, committed)
						toolsEqual(t, strings.TrimSpace(f.git(t, f.Store.Dir(committed.ID), "config", "--get", "ikigenba.name")), "journal")
						if fault == "cancel" {
							cancel()
							_, readErr := f.Store.Size(scoped, committed.ID)
							if readErr == nil {
								t.Error("late canceled answer read succeeded")
							}
							return readErr
						}
						paths := []string{}
						switch fault {
						case "source-directory":
							paths = []string{f.DBParent}
						case "source-file":
							paths = []string{f.StoreConfig.Source}
						case "root":
							paths = []string{f.Root}
						case "repository":
							paths = []string{f.Store.Dir(committed.ID)}
						case "all-permissions":
							paths = []string{f.Store.Dir(committed.ID), f.Root}
							if !memory {
								paths = append(paths, f.StoreConfig.Source, f.DBParent)
							}
						}
						for _, path := range paths {
							info, statErr := os.Stat(path)
							if statErr != nil {
								return statErr
							}
							original := info.Mode().Perm()
							if chmodErr := os.Chmod(path, 0); chmodErr != nil {
								return chmodErr
							}
							restore = append(restore, func() {
								if op == "create" && path == f.Store.Dir(committed.ID) {
									if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
										return
									}
								}
								toolsMust(t, os.Chmod(path, original))
							})
						}
						return injected
					})
					if err == nil {
						t.Fatal("late fault was accepted")
					}
					if fault == "cancel" && !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
					if fault != "cancel" && !errors.Is(err, injected) {
						t.Fatal(err)
					}
					toolsEqual(t, committed.Name, "journal")
					for i := len(restore) - 1; i >= 0; i-- {
						restore[i]()
					}
					got, err := f.Store.All(toolsContext(t))
					toolsMust(t, err)
					toolsEqual(t, got, before)
					toolsEqual(t, toolsSnapshot(t, f.Root), disk)
					if !memory {
						atomicReopenCatalog(t, f, before)
					}
				})
			}
		}
	}
}
func atomicCaseName(memory bool, op, fault string) string {
	catalog := "file"
	if memory {
		catalog = "memory"
	}
	return catalog + "/" + op + "/" + fault
}

func TestAtomicCoordinateDeleteRestoresTree(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(atomicCaseName(memory, "delete", "late-error"), func(t *testing.T) {
			f := newToolsFixture(t)
			atomicReplaceStore(t, f, f.Git, memory)
			r := f.create(t, f.Caller.UserID, "notes")
			atomicDecorateConfig(t, f, r.ID)
			root, err := os.OpenRoot(f.Root)
			toolsMust(t, err)
			toolsMust(t, root.WriteFile(filepath.Join(r.ID+".git", "fixture"), []byte("retained bytes"), 0440))
			toolsMust(t, root.Symlink("fixture", filepath.Join(r.ID+".git", "fixture-link")))
			toolsMust(t, root.Close())
			before, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			disk := toolsSnapshot(t, f.Root)
			injected := errors.New("answer failed after deletion")
			err = f.Store.Coordinate(toolsContext(t), func(ctx context.Context) error {
				if err := f.Store.Delete(ctx, r.ID); err != nil {
					return err
				}
				_, findErr := f.Store.Find(ctx, f.Caller.UserID, r.ID)
				if !errors.Is(findErr, store.ErrNotFound) {
					t.Fatalf("delete was not committed: %v", findErr)
				}
				_, statErr := os.Stat(f.Store.Dir(r.ID))
				if !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("tree still present after committed Delete: %v", statErr)
				}
				return injected
			})
			if !errors.Is(err, injected) {
				t.Fatal(err)
			}
			got, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, got, before)
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			if !memory {
				atomicReopenCatalog(t, f, before)
			}
		})
	}
}

// doneProbe establishes that a competing operation reached its cancellable
// gate wait, without sleeping or depending on goroutine scheduling delays.
type atomicDoneProbe struct {
	context.Context
	seen chan struct{}
	once sync.Once
}

func (p *atomicDoneProbe) Done() <-chan struct{} {
	p.once.Do(func() { close(p.seen) })
	return p.Context.Done()
}
func atomicWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-toolsContext(t).Done():
		t.Fatal("operation deadline")
		var zero T
		return zero
	}
}

func TestAtomicCoordinateSerializesMutationsAndExpiredContexts(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	var expired context.Context
	toolsMust(t, f.Store.Coordinate(toolsContext(t), func(ctx context.Context) error { expired = ctx; return nil }))
	for _, useExpired := range []bool{false, true} {
		entered := make(chan struct{})
		release := make(chan struct{})
		scopeDone := make(chan error, 1)
		fault := errors.New("rollback held scope")
		go func() {
			scopeDone <- f.Store.Coordinate(toolsContext(t), func(ctx context.Context) error {
				_, err := f.Store.Rename(ctx, r.ID, "temporary")
				if err != nil {
					return err
				}
				close(entered)
				<-release
				return fault
			})
		}()
		atomicWait(t, entered)
		base := toolsContext(t)
		if useExpired {
			base = expired
		}
		ctx, cancel := context.WithCancel(base)
		probe := &atomicDoneProbe{Context: ctx, seen: make(chan struct{})}
		done := make(chan error, 1)
		go func() { _, err := f.Store.Rename(probe, r.ID, "competitor"); done <- err }()
		atomicWait(t, probe.seen)
		select {
		case err := <-done:
			t.Fatalf("mutation bypassed active coordination: %v", err)
		default:
		}
		cancel()
		if err := atomicWait(t, done); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		row, err := f.Store.Find(toolsContext(t), f.Caller.UserID, r.ID)
		toolsMust(t, err)
		toolsEqual(t, row.Name, "temporary")
		close(release)
		if err := atomicWait(t, scopeDone); !errors.Is(err, fault) {
			t.Fatal(err)
		}
		row, err = f.Store.Find(toolsContext(t), f.Caller.UserID, r.ID)
		toolsMust(t, err)
		toolsEqual(t, row.Name, "notes")
	}
}

func TestAtomicCoordinatePreservesContextAndPreflightRefusals(t *testing.T) {
	f := newToolsFixture(t)
	type marker struct{}
	original := identity.NewContext(clone.NewContext(context.WithValue(toolsContext(t), marker{}, "preserved"), f.Base), f.Caller)
	toolsMust(t, f.Store.Coordinate(original, func(ctx context.Context) error {
		toolsEqual(t, ctx.Value(marker{}), "preserved")
		caller, ok := identity.FromContext(ctx)
		toolsEqual(t, ok, true)
		toolsEqual(t, caller, f.Caller)
		base, ok := clone.FromContext(ctx)
		toolsEqual(t, ok, true)
		toolsEqual(t, base, f.Base)
		return nil
	}))
	r := f.create(t, f.Caller.UserID, "notes")
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	eventOffset := len(f.events(t))
	ctx, cancel := context.WithCancel(original)
	cancel()
	for _, method := range []string{"Create", "Find"} {
		calls := 0
		var result store.Repo
		var methodErr error
		err = f.Store.Coordinate(ctx, func(callbackCtx context.Context) error {
			toolsEqual(t, callbackCtx.Value(marker{}), "preserved")
			toolsEqual(t, callbackCtx.Err(), context.Canceled)
			switch method {
			case "Create":
				result, methodErr = f.Store.Create(callbackCtx, f.Caller.UserID, "new")
			case "Find":
				result, methodErr = f.Store.Find(callbackCtx, f.Caller.UserID, r.ID)
			}
			calls++
			return methodErr
		})
		toolsEqual(t, calls, 1)
		toolsEqual(t, result, store.Repo{})
		if !errors.Is(methodErr, context.Canceled) || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: actual method error %v, coordination error %v", method, methodErr, err)
		}
		if errors.Is(methodErr, store.ErrNotFound) || errors.Is(methodErr, store.ErrNameTaken) {
			t.Fatalf("%s context failure became a catalog verdict: %v", method, methodErr)
		}
		got, readErr := f.Store.All(toolsContext(t))
		toolsMust(t, readErr)
		toolsEqual(t, got, before)
		toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	}
	toolsEqual(t, len(f.events(t)), eventOffset)
	atomicReopenCatalog(t, f, before)
}

func TestAtomicMCPUnwritableSnapshotRefusals(t *testing.T) {
	// R-14B4-62WX R-15J0-JUNM
	for _, op := range []string{"create", "rename"} {
		t.Run(op, func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			before, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			disk := toolsSnapshot(t, f.Root)
			info, err := os.Stat(f.StoreConfig.Source)
			toolsMust(t, err)
			mode := info.Mode().Perm()
			toolsMust(t, os.Chmod(f.StoreConfig.Source, 0400))
			t.Cleanup(func() { toolsMust(t, os.Chmod(f.StoreConfig.Source, mode)) })
			args := `{"name":"new"}`
			if op == "rename" {
				args = toolsArguments(r.ID, "new")
			}
			offset := len(f.events(t))
			refusal(t, f.call(t, op, args), "cannot reach the repositories; try again later")
			events := f.events(t)[offset:]
			toolsEqual(t, len(events), 1)
			toolsEqual(t, events[0].Name, "tool.called")
			toolsEqual(t, events[0].Attrs["outcome"], "error")
			got, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, got, before)
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			toolsMust(t, os.Chmod(f.StoreConfig.Source, mode))
			atomicReopenCatalog(t, f, before)
		})
	}
}

func TestAtomicCoordinateQueuesSuccessfulCompetitorAndVerify(t *testing.T) {
	for _, operation := range []string{"create", "verify"} {
		t.Run(operation, func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			entered := make(chan struct{})
			release := make(chan struct{})
			scopeDone := make(chan error, 1)
			fault := errors.New("late coordinated failure")
			go func() {
				scopeDone <- f.Store.Coordinate(toolsContext(t), func(ctx context.Context) error {
					_, err := f.Store.Rename(ctx, r.ID, "temporary")
					if err != nil {
						return err
					}
					close(entered)
					<-release
					return fault
				})
			}()
			atomicWait(t, entered)
			probe := &atomicDoneProbe{Context: toolsContext(t), seen: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				if operation == "create" {
					_, err := f.Store.Create(probe, f.Caller.UserID, "competitor")
					done <- err
				} else {
					done <- f.Store.Verify(probe, f.Writer)
				}
			}()
			atomicWait(t, probe.seen)
			select {
			case err := <-done:
				t.Fatalf("operation bypassed coordination: %v", err)
			default:
			}
			close(release)
			if err := atomicWait(t, scopeDone); !errors.Is(err, fault) {
				t.Fatal(err)
			}
			toolsMust(t, atomicWait(t, done))
			row, err := f.Store.Find(toolsContext(t), f.Caller.UserID, r.ID)
			toolsMust(t, err)
			toolsEqual(t, row.Name, "notes")
			if operation == "create" {
				created, err := f.Store.Find(toolsContext(t), f.Caller.UserID, "competitor")
				toolsMust(t, err)
				toolsEqual(t, created.Name, "competitor")
			}
		})
	}
}

func TestAtomicCoordinateCloseWaitsForCallbackAndCleanup(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	entered := make(chan struct{})
	release := make(chan struct{})
	scopeDone := make(chan error, 1)
	fault := errors.New("late failure before close")
	var callbackActive atomic.Bool
	go func() {
		scopeDone <- f.Store.Coordinate(toolsContext(t), func(ctx context.Context) error {
			_, err := f.Store.Rename(ctx, r.ID, "temporary")
			if err != nil {
				return err
			}
			callbackActive.Store(true)
			close(entered)
			<-release
			callbackActive.Store(false)
			return fault
		})
	}()
	atomicWait(t, entered)
	attempting := make(chan struct{})
	closed := make(chan error, 1)
	go func() {
		close(attempting)
		err := f.Store.Close()
		if callbackActive.Load() {
			t.Error("Close returned during the active callback")
		}
		closed <- err
	}()
	atomicWait(t, attempting)
	select {
	case err := <-closed:
		t.Fatalf("Close bypassed active coordination: %v", err)
	default:
	}
	close(release)
	if err := atomicWait(t, scopeDone); !errors.Is(err, fault) {
		t.Fatal(err)
	}
	toolsMust(t, atomicWait(t, closed))
	// Close must wait for the undo as well as the callback; reopening sees the
	// original committed row and raw tree, not the temporary renamed state.
	atomicReopenCatalog(t, f, before)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
}

func TestAtomicMCPPreCanceledServerContextWitness(t *testing.T) {
	f := newToolsFixture(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "repos", Version: "fixture", Telemetry: f.Writer})
	tools.Register(srv, tools.Config{Store: f.Store, Limits: f.Limits, Telemetry: f.Writer})
	h := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(clone.NewContext(r.Context(), f.Base))
		cancel()
		srv.ServeHTTP(w, r.WithContext(ctx))
	}))
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	f.Client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client(), Name: "fixture", Version: "fixture"})
	disk := toolsSnapshot(t, f.Root)
	for _, call := range []struct{ name, args, text, outcome string }{
		{"create", `{"name":"new"}`, "cannot reach the repositories; try again later", "error"},
		{"rename", `{"repo":"missing","name":"new"}`, "cannot reach the repositories; try again later", "error"},
		{"create", `{"name":"Not A Name"}`, "invalid arguments:\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit", "error"},
		{"rename", `{"repo":"missing","name":"Not A Name"}`, "invalid arguments:\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit", "error"},
		{"create", `{"name":"Not A Name","bogus":1}`, "invalid arguments:\nbogus: unknown field", "invalid_arguments"},
		{"rename", `{"name":5,"bogus":true}`, "invalid arguments:\nrepo: missing required field\nname: expected string, got number\nbogus: unknown field", "invalid_arguments"},
	} {
		offset := len(f.events(t))
		refusal(t, f.call(t, call.name, call.args), call.text)
		events := f.events(t)[offset:]
		toolsEqual(t, len(events), 1)
		toolsEqual(t, events[0].Name, "tool.called")
		toolsEqual(t, events[0].Attrs["outcome"], call.outcome)
		toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	}
	rows, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, len(rows), 0)
}
