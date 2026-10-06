package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

func TestMutationLateHeadFailurePreservesState(t *testing.T) {
	// R-ZFD5-R5BC: a refusal after a response read fails preserves the catalog and tree.
	f := newToolsFixture(t)
	var armed atomic.Bool
	var calls atomic.Int64
	g, err := git.Find(filepath.Dir(f.GitPath), func() []string {
		env := append([]string(nil), f.Env...)
		if armed.Load() && calls.Add(1) >= 3 {
			for i, value := range env {
				if strings.HasPrefix(value, "GIT_CONFIG_KEY_0=") {
					env[i] = "GIT_CONFIG_KEY_0=invalid"
				}
			}
		}
		return env
	})
	toolsMust(t, err)
	f.StoreConfig.Git = g
	s, err := store.Open(toolsContext(t), f.DB, f.StoreConfig)
	toolsMust(t, err)

	f.Store = s
	f.Client = serveTools(t, tools.Config{Store: s, Limits: f.Limits, Telemetry: f.Writer}, f.Base, true)
	r := f.create(t, f.Caller.UserID, "notes")
	before, err := s.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	eventOffset := len(f.events(t))
	armed.Store(true)
	result := f.call(t, "rename", toolsArguments(r.ID, "journal"))
	refusal(t, result, "cannot reach the repositories; try again later")
	if calls.Load() < 3 {
		t.Fatal("failure did not reach the late Head read")
	}
	events := f.events(t)[eventOffset:]
	if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != "tool.called" || events[1].Attrs["outcome"] != "error" || events[2].Name != "request.finished" {
		t.Fatalf("refusal events: %#v", events)
	}
	// Keep the failing environment armed until the complete MCP response, so
	// compensation that repeats git validation meets the same failure.
	armed.Store(false)
	after, err := s.All(toolsContext(t))
	toolsMust(t, err)
	if !reflect.DeepEqual(after, before) {
		t.Errorf("catalog after late-read refusal: got %#v, want %#v", after, before)
	}
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
}

func mutationObject(t *testing.T, result mcp.Result) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	toolsMust(t, json.Unmarshal(successObject(t, result), &object))
	return object
}

func mutationString(t *testing.T, object map[string]json.RawMessage, name string) string {
	t.Helper()
	var value string
	toolsMust(t, json.Unmarshal(object[name], &value))
	return value
}

func mutationDomain(t *testing.T, f *toolsFixture, offset int, name, id string) {
	t.Helper()
	events := f.events(t)[offset:]
	toolsEqual(t, len(events), 2)
	toolsEqual(t, events[0].Name, name)
	toolsEqual(t, events[0].Attrs, telemetry.Attrs{"repo": id, "owner": f.Caller.UserID})
	toolsEqual(t, events[1].Name, "tool.called")
	toolsEqual(t, events[1].Attrs["outcome"], "ok")
}

func mutationRefusalUnchanged(t *testing.T, f *toolsFixture, tool, args, text string) {
	t.Helper()
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	offset := len(f.events(t))
	refusal(t, f.call(t, tool, args), text)
	after, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, after, before)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	assertOnlyToolCalls(t, f, offset, "error")
}

func mutationHoldReleased(t *testing.T, f *toolsFixture, id string) {
	t.Helper()
	release, ok := f.Limits.TryHold(id)
	if !ok {
		t.Fatal("delete left the repository held")
	}
	release()
}

// R-ZLGN-O00T R-8E0I-4B32 R-8K40-15SJ
func TestMutationCreateAnswersAndPersistsEmptyRepository(t *testing.T) {
	f := newToolsFixture(t)
	existing := f.create(t, "other", "existing")
	toolsMust(t, os.WriteFile(filepath.Join(f.Root, "unmanaged"), []byte("keep"), 0600))
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	offset := len(f.events(t))
	result := f.call(t, "create", `{"name":"notes"}`)
	object := mutationObject(t, result)
	id := mutationString(t, object, "id")
	toolsEqual(t, mutationString(t, object, "name"), "notes")
	if !store.ValidID(id) || id == existing.ID {
		t.Fatalf("creation did not mint a new valid id: %q", id)
	}
	r, err := f.Store.Find(toolsContext(t), f.Caller.UserID, id)
	toolsMust(t, err)
	toolsEqual(t, r.Name, "notes")
	toolsEqual(t, r.Owner, f.Caller.UserID)
	toolsEqual(t, r.Available, true)
	toolsEqual(t, r.Created, f.StoreConfig.Now().UTC().Truncate(time.Second))
	toolsEqual(t, string(successObject(t, result)), queryRepository(t, f, r, f.Base, ""))
	if _, present := object["head"]; present {
		t.Fatal("empty created repository has head")
	}
	var size int64
	toolsMust(t, json.Unmarshal(object["size_bytes"], &size))
	if size <= 0 {
		t.Fatal("empty bare repository has no measured files")
	}
	byName, err := f.Store.Find(toolsContext(t), f.Caller.UserID, r.Name)
	toolsMust(t, err)
	toolsEqual(t, byName, r)
	all, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, len(all), len(before)+1)
	toolsEqual(t, f.git(t, f.Store.Dir(id), "for-each-ref"), "")
	toolsEqual(t, f.git(t, f.Store.Dir(id), "symbolic-ref", "HEAD"), "refs/heads/"+store.DefaultBranch+"\n")
	for key, want := range map[string]string{"id": id, "name": r.Name, "owner": r.Owner, "created": r.Created.Format("2006-01-02T15:04:05Z")} {
		toolsEqual(t, f.git(t, f.Root, "config", "--file", filepath.Join(f.Store.Dir(id), "config"), "--get", "ikigenba."+key), want+"\n")
	}
	after := toolsSnapshot(t, f.Root)
	for path, entry := range disk {
		toolsEqual(t, after[path], entry)
	}
	entries, err := os.ReadDir(f.Root)
	toolsMust(t, err)
	toolsEqual(t, len(entries), 3)
	for _, entry := range entries {
		if entry.Name() == r.Name {
			t.Fatal("created directory is named by mutable repository name")
		}
	}
	mutationDomain(t, f, offset, "repo.created", id)
}

// R-ZTZY-CE7O R-QFGU-JHA3
func TestMutationCreateOwnerBoundariesAndConcurrentName(t *testing.T) {
	f := newToolsFixture(t)
	other := f.create(t, "other", "notes")
	queryHead(t, f, other, "other history")
	otherDisk := toolsSnapshot(t, f.Store.Dir(other.ID))
	result := f.call(t, "create", `{"name":"notes"}`)
	id := mutationString(t, mutationObject(t, result), "id")
	if id == other.ID {
		t.Fatal("different owners share an id")
	}
	created, err := f.Store.Find(toolsContext(t), f.Caller.UserID, id)
	toolsMust(t, err)
	toolsEqual(t, created.Name, "notes")
	toolsEqual(t, created.Owner, f.Caller.UserID)
	toolsEqual(t, created.Available, true)
	toolsEqual(t, string(successObject(t, result)), queryRepository(t, f, created, f.Base, ""))
	stillOther, err := f.Store.Find(toolsContext(t), "other", other.ID)
	toolsMust(t, err)
	toolsEqual(t, stillOther, other)
	toolsEqual(t, toolsSnapshot(t, f.Store.Dir(other.ID)), otherDisk)
	entries, err := os.ReadDir(f.Root)
	toolsMust(t, err)
	before := len(entries)
	offset := len(f.events(t))
	const count = 8
	type response struct {
		result mcp.Result
		err    error
	}
	replies := make(chan response, count)
	ctx := toolsContext(t)
	for i := range count {
		caller := identity.Caller{UserID: f.Caller.UserID, RequestID: fmt.Sprintf("concurrent-%d", i)}
		go func() {
			result, err := f.Client.CallTool(ctx, caller, "create", json.RawMessage(`{"name":"shared"}`))
			replies <- response{result, err}
		}()
	}
	successes := 0
	for range count {
		reply := mutationTake(t, replies)
		toolsMust(t, reply.err)
		if _, failed := resultMembers(t, reply.result)["isError"]; failed {
			refusal(t, reply.result, "invalid arguments:\nname: 'shared' is already one of your repositories")
		} else {
			successes++
			object := mutationObject(t, reply.result)
			r, err := f.Store.Find(toolsContext(t), f.Caller.UserID, mutationString(t, object, "id"))
			toolsMust(t, err)
			toolsEqual(t, string(successObject(t, reply.result)), queryRepository(t, f, r, f.Base, ""))
		}
	}
	toolsEqual(t, successes, 1)
	entries, err = os.ReadDir(f.Root)
	toolsMust(t, err)
	toolsEqual(t, len(entries), before+1)
	events := f.events(t)[offset:]
	domains, calls, errorsSeen := 0, 0, 0
	for _, event := range events {
		switch event.Name {
		case "repo.created":
			domains++
		case "tool.called":
			calls++
			if event.Attrs["outcome"] == "error" {
				errorsSeen++
			}
		default:
			t.Fatalf("unexpected create event: %#v", event)
		}
	}
	toolsEqual(t, domains, 1)
	toolsEqual(t, calls, count)
	toolsEqual(t, errorsSeen, count-1)
}

// R-8GGA-VUKG R-QE8Y-5PJE R-Q9DC-MMKM R-ZNWG-FJI7
func TestMutationRuleRefusalsPreserveAllState(t *testing.T) {
	f := newToolsFixture(t)
	notes := f.create(t, f.Caller.UserID, "notes")
	f.create(t, f.Caller.UserID, "site")
	other := f.create(t, "other", "private")
	queryHead(t, f, notes, "keep history")
	for _, name := range []string{" notes", "Notes", "My Drafts", "-notes", "notes.git", "no_tes", "", strings.Repeat("a", 65)} {
		args, err := json.Marshal(map[string]string{"name": name})
		toolsMust(t, err)
		mutationRefusalUnchanged(t, f, "create", string(args), "invalid arguments:\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit")
	}
	for _, c := range []struct{ tool, args, text string }{
		{"create", `{"name":"notes"}`, "invalid arguments:\nname: 'notes' is already one of your repositories"},
		{"rename", toolsArguments(notes.ID, "site"), "invalid arguments:\nname: 'site' is already one of your repositories"},
		{"rename", toolsArguments(notes.Name, "Bad Name"), "invalid arguments:\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit"},
		{"rename", `{"repo":"drafts","name":"site"}`, "invalid arguments:\nrepo: no repository 'drafts'\nname: 'site' is already one of your repositories"},
		{"rename", `{"repo":"drafts","name":"Bad Name"}`, "invalid arguments:\nrepo: no repository 'drafts'\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit"},
		{"rename", toolsArguments(other.ID, "site"), "invalid arguments:\nrepo: no repository '" + other.ID + "'\nname: 'site' is already one of your repositories"},
		{"rename", toolsArguments(other.Name, "free"), "invalid arguments:\nrepo: no repository 'private'"},
		{"delete", toolsRepoArgument(other.ID), "invalid arguments:\nrepo: no repository '" + other.ID + "'"},
		{"delete", toolsRepoArgument("rep_zz"), "invalid arguments:\nrepo: no repository 'rep_zz'"},
	} {
		mutationRefusalUnchanged(t, f, c.tool, c.args, c.text)
	}
	release, ok := f.Limits.TryHold(notes.ID)
	toolsEqual(t, ok, true)
	mutationRefusalUnchanged(t, f, "delete", toolsRepoArgument(notes.Name), "repository 'notes' is busy; try again once its git operations finish")
	release()
	mutationHoldReleased(t, f, notes.ID)
}

// R-ZP4C-TB8W
func TestMutationFailingStoreRenameKeepsNamingPriority(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	disk := toolsSnapshot(t, f.Root)
	f.DB.SetFailing(true)
	for _, ref := range []string{r.ID, r.Name, "missing", "rep_zz"} {
		offset := len(f.events(t))
		refusal(t, f.call(t, "rename", toolsArguments(ref, "Bad Name")), "invalid arguments:\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit")
		assertOnlyToolCalls(t, f, offset, "error")
		toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	}
}

// R-ZMOK-1RRI R-8MJS-SP9X R-ZYVJ-VH6G R-ZWFR-3XP2 R-8V33-H3GS
func TestMutationRenamePreservesIdentityAndHistoryWhileBusy(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	other := f.create(t, "other", "journal")
	head := queryHead(t, f, r, "keep this history")
	queryHead(t, f, other, "other history")
	otherDisk := toolsSnapshot(t, f.Store.Dir(other.ID))
	identityConfig := map[string]string{}
	for _, key := range []string{"id", "owner", "created"} {
		identityConfig[key] = f.git(t, f.Root, "config", "--file", filepath.Join(f.Store.Dir(r.ID), "config"), "--get", "ikigenba."+key)
	}
	refs := f.git(t, f.Store.Dir(r.ID), "for-each-ref", "--format=%(refname) %(objectname)")
	disk := toolsSnapshot(t, f.Root)
	release, ok := f.Limits.TryHold(r.ID)
	toolsEqual(t, ok, true)
	defer release()
	offset := len(f.events(t))
	result := f.call(t, "rename", toolsArguments(r.ID, "journal"))
	renamed, err := f.Store.Find(toolsContext(t), f.Caller.UserID, r.ID)
	toolsMust(t, err)
	want := r
	want.Name = "journal"
	toolsEqual(t, renamed, want)
	owned, err := f.Store.List(toolsContext(t), f.Caller.UserID)
	toolsMust(t, err)
	toolsEqual(t, owned, []store.Repo{want})
	toolsEqual(t, string(successObject(t, result)), queryRepository(t, f, renamed, f.Base, head))
	toolsEqual(t, f.Limits.Busy(r.ID), true)
	_, err = f.Store.Find(toolsContext(t), f.Caller.UserID, r.Name)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old name still resolves: %v", err)
	}
	toolsEqual(t, f.git(t, f.Store.Dir(r.ID), "for-each-ref", "--format=%(refname) %(objectname)"), refs)
	for key, value := range identityConfig {
		toolsEqual(t, f.git(t, f.Root, "config", "--file", filepath.Join(f.Store.Dir(r.ID), "config"), "--get", "ikigenba."+key), value)
	}
	toolsEqual(t, f.git(t, f.Root, "config", "--file", filepath.Join(f.Store.Dir(r.ID), "config"), "--get", "ikigenba.name"), "journal\n")
	after := toolsSnapshot(t, f.Root)
	config := filepath.Join(r.ID+".git", "config")
	delete(after, config)
	delete(disk, config)
	toolsEqual(t, after, disk)
	stillOther, err := f.Store.Find(toolsContext(t), "other", other.ID)
	toolsMust(t, err)
	toolsEqual(t, stillOther, other)
	toolsEqual(t, toolsSnapshot(t, f.Store.Dir(other.ID)), otherDisk)
	mutationDomain(t, f, offset, "repo.renamed", r.ID)
	// The mutable name also resolves the same repository for a subsequent call.
	offset = len(f.events(t))
	result = f.call(t, "rename", toolsArguments("journal", "notebook"))
	renamed.Name = "notebook"
	toolsEqual(t, string(successObject(t, result)), queryRepository(t, f, renamed, f.Base, head))
	mutationDomain(t, f, offset, "repo.renamed", r.ID)
}

// R-8OZL-K8RB
func TestMutationRenameCurrentNameWritesNothingAndRecordsNoDomain(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	head := queryHead(t, f, r, "history")
	f.git(t, f.Root, "config", "--file", filepath.Join(f.Store.Dir(r.ID), "config"), "ikigenba.name", "disk-only")
	disk := toolsSnapshot(t, f.Root)
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	offset := len(f.events(t))
	for _, ref := range []string{r.ID, r.Name} {
		toolsEqual(t, string(successObject(t, f.call(t, "rename", toolsArguments(ref, r.Name)))), queryRepository(t, f, r, f.Base, head))
	}
	after, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	toolsEqual(t, after, before)
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	toolsEqual(t, f.git(t, f.Root, "config", "--file", filepath.Join(f.Store.Dir(r.ID), "config"), "--get", "ikigenba.name"), "disk-only\n")
	assertOnlyToolCalls(t, f, offset, "ok", "ok")
}

// R-LCM8-PI21
func TestMutationRenameUnavailableChangesCatalogAlone(t *testing.T) {
	for _, damage := range []string{"missing", "broken"} {
		t.Run(damage, func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			if damage == "missing" {
				toolsMust(t, os.RemoveAll(f.Store.Dir(r.ID)))
			} else {
				toolsMust(t, os.Remove(filepath.Join(f.Store.Dir(r.ID), "HEAD")))
			}
			toolsMust(t, f.Store.Verify(toolsContext(t), f.Writer))
			r, err := f.Store.Find(toolsContext(t), f.Caller.UserID, r.ID)
			toolsMust(t, err)
			toolsEqual(t, r.Available, false)
			disk := toolsSnapshot(t, f.Root)
			offset := len(f.events(t))
			result := f.call(t, "rename", toolsArguments(r.Name, "journal"))
			want := r
			want.Name = "journal"
			got, err := f.Store.Find(toolsContext(t), f.Caller.UserID, r.ID)
			toolsMust(t, err)
			toolsEqual(t, got, want)
			toolsEqual(t, string(successObject(t, result)), queryRepository(t, f, want, f.Base, ""))
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			_, err = f.Store.Find(toolsContext(t), f.Caller.UserID, r.Name)
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("old unavailable name still resolves: %v", err)
			}
			mutationDomain(t, f, offset, "repo.renamed", r.ID)
		})
	}
}

// R-ZQC9-72ZL R-ZXNN-HPFR R-ZRK5-KUQA
func TestMutationDeleteOnlyTheNamedRepositoryAndReleaseHold(t *testing.T) {
	for _, byID := range []bool{false, true} {
		t.Run(fmt.Sprint(byID), func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			queryHead(t, f, r, "deleted history")
			other := f.create(t, "other", "keep")
			queryHead(t, f, other, "keep history")
			toolsMust(t, os.WriteFile(filepath.Join(f.Root, "unmanaged"), []byte("keep bytes"), 0600))
			disk := toolsSnapshot(t, f.Root)
			ref := r.Name
			if byID {
				ref = r.ID
			}
			offset := len(f.events(t))
			toolsEqual(t, string(successObject(t, f.call(t, "delete", toolsRepoArgument(ref)))), fmt.Sprintf(`{"id":%q,"name":%q}`, r.ID, r.Name))
			all, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, all, []store.Repo{other})
			owned, err := f.Store.List(toolsContext(t), f.Caller.UserID)
			toolsMust(t, err)
			toolsEqual(t, len(owned), 0)
			_, err = os.Lstat(f.Store.Dir(r.ID))
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("deleted directory remains: %v", err)
			}
			for path := range disk {
				if path == r.ID+".git" || strings.HasPrefix(path, r.ID+".git"+string(filepath.Separator)) {
					delete(disk, path)
				}
			}
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			mutationHoldReleased(t, f, r.ID)
			mutationDomain(t, f, offset, "repo.deleted", r.ID)
			mutationRefusalUnchanged(t, f, "delete", toolsRepoArgument(r.ID), "invalid arguments:\nrepo: no repository '"+r.ID+"'")
			mutationHoldReleased(t, f, r.ID)
		})
	}
}

// R-ZV7U-Q5YD
func TestMutationDeleteUnavailableMissingBrokenAndRegularFile(t *testing.T) {
	for _, damage := range []string{"missing", "broken", "regular-file"} {
		t.Run(damage, func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			toolsMust(t, os.RemoveAll(f.Store.Dir(r.ID)))
			switch damage {
			case "broken":
				toolsMust(t, os.MkdirAll(filepath.Join(f.Store.Dir(r.ID), "arbitrary", "nested"), 0700))
				toolsMust(t, os.WriteFile(filepath.Join(f.Store.Dir(r.ID), "arbitrary", "nested", "data"), []byte("not git"), 0600))
			case "regular-file":
				toolsMust(t, os.WriteFile(f.Store.Dir(r.ID), []byte("not a directory"), 0600))
			}
			toolsMust(t, f.Store.Verify(toolsContext(t), f.Writer))
			unavailable, err := f.Store.Find(toolsContext(t), f.Caller.UserID, r.ID)
			toolsMust(t, err)
			toolsEqual(t, unavailable.Available, false)
			toolsMust(t, os.WriteFile(filepath.Join(f.Root, "keep"), []byte("unmanaged bytes"), 0600))
			offset := len(f.events(t))
			toolsEqual(t, string(successObject(t, f.call(t, "delete", toolsRepoArgument(r.Name)))), fmt.Sprintf(`{"id":%q,"name":%q}`, r.ID, r.Name))
			_, err = os.Lstat(f.Store.Dir(r.ID))
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unavailable entry remains: %v", err)
			}
			all, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, len(all), 0)
			owned, err := f.Store.List(toolsContext(t), f.Caller.UserID)
			toolsMust(t, err)
			toolsEqual(t, len(owned), 0)
			entries, err := os.ReadDir(f.Root)
			toolsMust(t, err)
			toolsEqual(t, len(entries), 1)
			keep, err := os.ReadFile(filepath.Join(f.Root, "keep"))
			toolsMust(t, err)
			toolsEqual(t, string(keep), "unmanaged bytes")
			mutationHoldReleased(t, f, r.ID)
			mutationDomain(t, f, offset, "repo.deleted", r.ID)
		})
	}
}

// R-ZRK5-KUQA
func TestMutationDeleteWriteFailuresReleaseHold(t *testing.T) {
	for _, failing := range []bool{false, true} {
		t.Run(fmt.Sprint(failing), func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			queryHead(t, f, r, "keep")
			before, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			disk := toolsSnapshot(t, f.Root)
			info, err := os.Stat(f.Root)
			toolsMust(t, err)
			mode := info.Mode().Perm()
			t.Cleanup(func() { toolsMust(t, os.Chmod(f.Root, mode)) })
			if failing {
				f.DB.SetFailing(true)
			} else {
				toolsMust(t, os.Chmod(f.Root, mode&^0222))
			}
			offset := len(f.events(t))
			refusal(t, f.call(t, "delete", toolsRepoArgument(r.ID)), "cannot reach the repositories; try again later")
			mutationHoldReleased(t, f, r.ID)
			assertOnlyToolCalls(t, f, offset, "error")
			f.DB.SetFailing(false)
			toolsMust(t, os.Chmod(f.Root, mode))
			after, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, after, before)
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			successObject(t, f.call(t, "delete", toolsRepoArgument(r.ID)))
			mutationHoldReleased(t, f, r.ID)
		})
	}
}

// R-8YQS-MEOV R-ZRK5-KUQA
func TestMutationDeleteTryHoldBusyUsesCurrentNameAndLeavesOwnerState(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	queryHead(t, f, r, "history")
	release, ok := f.Limits.TryHold(r.ID)
	toolsEqual(t, ok, true)
	for _, ref := range []string{r.ID, r.Name} {
		mutationRefusalUnchanged(t, f, "delete", toolsRepoArgument(ref), "repository 'notes' is busy; try again once its git operations finish")
		toolsEqual(t, f.Limits.Busy(r.ID), true)
	}
	release()
	mutationHoldReleased(t, f, r.ID)
	successObject(t, f.call(t, "delete", toolsRepoArgument(r.ID)))
	mutationHoldReleased(t, f, r.ID)
}

func mutationTake[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-toolsContext(t).Done():
		t.Fatal("mutation synchronization did not finish before deadline")
		var zero T
		return zero
	}
}

func mutationOperationalLimits(t *testing.T, f *toolsFixture) {
	t.Helper()
	f.Limits = limits.New(settings.Defaults(), limits.Clock{
		Now: f.StoreConfig.Now,
		After: func(time.Duration) <-chan time.Time {
			return make(chan time.Time)
		},
	})
	f.Client = serveTools(t, tools.Config{Store: f.Store, Limits: f.Limits, Telemetry: f.Writer}, f.Base, false)
}

func mutationPacket(payload string) string { return fmt.Sprintf("%04x%s", len(payload)+4, payload) }

func mutationPushFixture(t *testing.T, f *toolsFixture) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	f.git(t, dir, "init", "--bare")
	tree := strings.TrimSpace(f.git(t, dir, "hash-object", "-t", "tree", "--stdin"))
	sha := strings.TrimSpace(f.git(t, dir, "commit-tree", tree, "-m", "push fixture"))
	cmd := exec.CommandContext(toolsContext(t), f.GitPath, "pack-objects", "--stdout", "--revs")
	cmd.Dir, cmd.Env, cmd.Stdin = dir, append([]string(nil), f.Env...), strings.NewReader(sha+"\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pack, err := cmd.Output()
	if err != nil {
		t.Fatalf("git pack fixture: %v: %s", err, stderr.String())
	}
	return sha, pack
}

type mutationHTTPReply struct {
	response *http.Response
	err      error
}

func mutationPipeWrite(t *testing.T, writer *io.PipeWriter, data []byte) {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := writer.Write(data); done <- err }()
	toolsMust(t, mutationTake(t, done))
}

// R-8YQS-MEOV
func TestMutationDeleteBusyFetchAndPushLeaveRealGitRunning(t *testing.T) {
	for _, route := range []string{"git-upload-pack", "git-receive-pack"} {
		t.Run(route, func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			mutationOperationalLimits(t, f)
			var request []byte
			var sha string
			if route == "git-upload-pack" {
				sha = queryHead(t, f, r, "fetch fixture")
				request = []byte(mutationPacket("want "+sha+" side-band-64k ofs-delta no-progress\n") + "0000" + mutationPacket("done\n"))
			} else {
				var pack []byte
				sha, pack = mutationPushFixture(t, f)
				request = append([]byte(mutationPacket(strings.Repeat("0", 40)+" "+sha+" refs/heads/main\x00report-status side-band-64k quiet\n")+"0000"), pack...)
			}
			ended := make(chan struct{})
			trace := filepath.Join(t.TempDir(), "busy-git-trace.json")
			global := filepath.Join(t.TempDir(), "busy-git-global")
			f.git(t, f.Root, "config", "--file", global, "trace2.eventTarget", trace)
			toolsMust(t, os.WriteFile(trace, nil, 0600))
			env := append([]string(nil), f.Env...)
			for i, value := range env {
				if strings.HasPrefix(value, "GIT_CONFIG_GLOBAL=") {
					env[i] = "GIT_CONFIG_GLOBAL=" + global
				}
			}
			g, err := git.Find(filepath.Dir(f.GitPath), func() []string { return append([]string(nil), env...) })
			toolsMust(t, err)
			f.Git = g
			bus := events.New(events.Config{Service: "repos", Sink: &events.Capture{}, Stderr: io.Discard, Now: f.StoreConfig.Now, Rand: toolsIDSource(), Telemetry: f.Writer, Emits: smarthttp.Emits()})
			t.Cleanup(func() { bus.Shutdown(context.Background()) })
			handler := identity.Require(smarthttp.Handler(smarthttp.Config{Store: f.Store, Git: f.Git, Limits: f.Limits, Telemetry: f.Writer, Events: bus}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				defer close(ended)
				handler.ServeHTTP(w, request)
			}))
			t.Cleanup(server.Close)
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
			httpRequest, err := http.NewRequestWithContext(toolsContext(t), http.MethodPost, server.URL+"/notes.git/"+route, reader)
			toolsMust(t, err)
			httpRequest.Header.Set("X-User-Id", f.Caller.UserID)
			httpRequest.Header.Set("X-Request-Id", "busy-git")
			httpRequest.Header.Set("Content-Type", "application/x-"+route+"-request")
			replies := make(chan mutationHTTPReply, 1)
			go func() {
				response, err := server.Client().Do(httpRequest)
				replies <- mutationHTTPReply{response, err}
			}()
			// A partial pkt-line keeps git waiting on the held request, before a
			// push can write any object or ref. Trace2 proves real git started.
			mutationPipeWrite(t, writer, request[:12])
			mutationAwaitGit(t, trace, strings.TrimPrefix(route, "git-"))
			toolsEqual(t, f.Limits.Busy(r.ID), true)
			for _, ref := range []string{r.Name, r.ID} {
				mutationRefusalUnchanged(t, f, "delete", toolsRepoArgument(ref), "repository 'notes' is busy; try again once its git operations finish")
				toolsEqual(t, f.Limits.Busy(r.ID), true)
				select {
				case <-ended:
					t.Fatal("delete ended the in-flight git operation")
				default:
				}
			}
			mutationPipeWrite(t, writer, request[12:])
			toolsMust(t, writer.Close())
			reply := mutationTake(t, replies)
			toolsMust(t, reply.err)
			t.Cleanup(func() { _ = reply.response.Body.Close() })
			toolsEqual(t, reply.response.StatusCode, http.StatusOK)
			toolsEqual(t, reply.response.Header.Get("Content-Type"), "application/x-"+route+"-result")
			body, err := io.ReadAll(reply.response.Body)
			toolsMust(t, err)
			toolsMust(t, reply.response.Body.Close())
			mutationTake(t, ended)
			if route == "git-receive-pack" {
				if !bytes.Contains(body, []byte("unpack ok")) || !bytes.Contains(body, []byte("ok refs/heads/main")) {
					t.Fatalf("busy-refused push did not finish successfully: %q", body)
				}
			} else if !bytes.Contains(body, []byte("PACK")) {
				t.Fatalf("busy-refused fetch did not finish its pack: %q", body)
			}
			toolsEqual(t, f.git(t, f.Store.Dir(r.ID), "rev-parse", "refs/heads/main"), sha+"\n")
			toolsEqual(t, f.Limits.Busy(r.ID), false)
			successObject(t, f.call(t, "delete", toolsRepoArgument(r.ID)))
			mutationHoldReleased(t, f, r.ID)
		})
	}
}

func mutationAwaitGit(t *testing.T, trace, service string) {
	t.Helper()
	ctx := toolsContext(t)
	for {
		data, err := os.ReadFile(filepath.Clean(trace))
		toolsMust(t, err)
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			var event struct {
				Event string
				Argv  []string
			}
			if json.Unmarshal(line, &event) == nil && event.Event == "child_start" && len(event.Argv) == 4 && event.Argv[0] == "git" && event.Argv[1] == service && event.Argv[2] == "--stateless-rpc" && event.Argv[3] == "." {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("real git did not start the held operation")
		default:
			runtime.Gosched()
		}
	}
}

// R-8YQS-MEOV
func TestMutationDeleteBusyMaintenanceLetsCycleFinish(t *testing.T) {
	f := newToolsFixture(t)
	r := f.create(t, f.Caller.UserID, "notes")
	queryHead(t, f, r, "maintenance fixture")
	mutationOperationalLimits(t, f)
	entered, released, done := make(chan string, 1), make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	ctx := toolsContext(t)
	go func() {
		defer close(done)
		maintenance.Cycle(ctx, maintenance.Config{Store: f.Store, Git: f.Git, Limits: f.Limits, Telemetry: f.Writer, Hold: func(ctx context.Context, id string) {
			entered <- id
			select {
			case <-released:
			case <-ctx.Done():
			}
		}})
	}()
	toolsEqual(t, mutationTake(t, entered), r.ID)
	toolsEqual(t, f.Limits.Busy(r.ID), true)
	for _, ref := range []string{r.Name, r.ID} {
		mutationRefusalUnchanged(t, f, "delete", toolsRepoArgument(ref), "repository 'notes' is busy; try again once its git operations finish")
		toolsEqual(t, f.Limits.Busy(r.ID), true)
		select {
		case <-done:
			t.Fatal("delete ended held maintenance")
		default:
		}
	}
	release()
	mutationTake(t, done)
	finished := 0
	for _, event := range f.events(t) {
		if event.Name == "maintenance.finished" && event.Attrs["repo"] == r.ID {
			finished++
		}
	}
	toolsEqual(t, finished, 1)
	toolsEqual(t, f.Limits.Busy(r.ID), false)
	successObject(t, f.call(t, "delete", toolsRepoArgument(r.ID)))
	mutationHoldReleased(t, f, r.ID)
}

// R-QD11-RXSP
func TestMutationDomainEventsShareMiddlewareEnvelopeAndOrder(t *testing.T) {
	f := newToolsFixture(t)
	f.Client = serveTools(t, tools.Config{Store: f.Store, Limits: f.Limits, Telemetry: f.Writer}, f.Base, true)
	var id string
	for _, call := range []struct{ tool, args, domain string }{
		{"create", `{"name":"notes"}`, "repo.created"},
		{"rename", `{"repo":"notes","name":"journal"}`, "repo.renamed"},
		{"delete", `{"repo":"journal"}`, "repo.deleted"},
	} {
		f.Caller.RequestID = "envelope-" + call.tool
		offset := len(f.events(t))
		result := f.call(t, call.tool, call.args)
		if call.tool == "create" {
			id = mutationString(t, mutationObject(t, result), "id")
		} else {
			successObject(t, result)
		}
		events := f.events(t)[offset:]
		toolsEqual(t, len(events), 4)
		for i, name := range []string{"request.started", call.domain, "tool.called", "request.finished"} {
			toolsEqual(t, events[i].Name, name)
			toolsEqual(t, events[i].RequestID, f.Caller.RequestID)
			toolsEqual(t, events[i].User, f.Caller.UserID)
		}
		toolsEqual(t, events[1].Attrs, telemetry.Attrs{"repo": id, "owner": f.Caller.UserID})
		toolsEqual(t, events[2].Attrs["outcome"], "ok")
	}
}
