package tools_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func registryExpectedMetadata() []struct {
	name, description, input, output string
	effect                           mcp.Effect
} {
	return []struct {
		name, description, input, output string
		effect                           mcp.Effect
	}{
		{"list", "The repositories you own, by name.\n\nTakes no arguments. Each repository has its id, its name, size_bytes, its size on disk, head, the sha its main branch points at (absent before the first push), and available, false when repos found it damaged at startup and will not serve it. Use show for one repository's clone URL.", "{\"type\":\"object\",\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"repos\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"head\":{\"type\":\"string\"},\"available\":{\"type\":\"boolean\"}},\"required\":[\"id\",\"name\",\"size_bytes\",\"available\"],\"additionalProperties\":false}}},\"required\":[\"repos\"],\"additionalProperties\":false}", mcp.Read},
		{"show", "One of your repositories, with its clone URL and how to give git your token.\n\nPass repo, the repository's id or its name. The result has its id, name, default_branch (always main), head (absent before the first push), size_bytes, available, created, clone_url, and credentials. The clone URL never holds a credential: credentials tells how to give git your personal access token without putting it in the URL or on a command line. Do not use credential.helper store, which writes the token to disk.", "{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"The repository's id (rep_ and 16 hexadecimal digits) or its name.\"}},\"required\":[\"repo\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"default_branch\":{\"type\":\"string\"},\"head\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"created\":{\"type\":\"string\"},\"clone_url\":{\"type\":\"string\"},\"credentials\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"default_branch\",\"size_bytes\",\"available\",\"created\",\"clone_url\",\"credentials\"],\"additionalProperties\":false}", mcp.Read},
		{"status", "How busy repos is, and how close each of your repositories is to its size limit.\n\nTakes no arguments. read covers clones and fetches, write covers pushes and maintenance: slots is how many run at once, active how many are running, and queued how many wait for a slot or their repository's lock. repos lists each of your repositories with size_bytes, limit_bytes, the size at which pushes to it are refused, available, and busy, true while a git operation or maintenance runs on it; delete is refused while busy.", "{\"type\":\"object\",\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"read\":{\"type\":\"object\",\"properties\":{\"slots\":{\"type\":\"integer\"},\"active\":{\"type\":\"integer\"},\"queued\":{\"type\":\"integer\"}},\"required\":[\"slots\",\"active\",\"queued\"],\"additionalProperties\":false},\"write\":{\"type\":\"object\",\"properties\":{\"slots\":{\"type\":\"integer\"},\"active\":{\"type\":\"integer\"},\"queued\":{\"type\":\"integer\"}},\"required\":[\"slots\",\"active\",\"queued\"],\"additionalProperties\":false},\"repos\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"limit_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"busy\":{\"type\":\"boolean\"}},\"required\":[\"id\",\"name\",\"size_bytes\",\"limit_bytes\",\"available\",\"busy\"],\"additionalProperties\":false}}},\"required\":[\"read\",\"write\",\"repos\"],\"additionalProperties\":false}", mcp.Read},
		{"create", "Create an empty repository and return it, with its clone URL and how to give git your token.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, and must not already be one of your repositories. The repository starts with no commits; its default branch is main. Push to clone_url to fill it. The result is what show returns: credentials tells how to give git your personal access token without putting it in the URL or on a command line. Do not use credential.helper store, which writes the token to disk.", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"The new repository's name: 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, not already one of your repositories.\"}},\"required\":[\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"default_branch\":{\"type\":\"string\"},\"head\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"created\":{\"type\":\"string\"},\"clone_url\":{\"type\":\"string\"},\"credentials\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"default_branch\",\"size_bytes\",\"available\",\"created\",\"clone_url\",\"credentials\"],\"additionalProperties\":false}", mcp.Additive},
		{"rename", "Give one of your repositories a new name; its id does not change.\n\nPass repo, its id or current name, and name, the new name, under the rules of create. The clone URL follows the name, so a clone made under the old name must have its remote's URL updated before it can fetch or push again. The result is what show returns.", "{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"The repository's id (rep_ and 16 hexadecimal digits) or its name.\"},\"name\":{\"type\":\"string\",\"description\":\"The repository's new name, under the rules of create.\"}},\"required\":[\"repo\",\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"default_branch\":{\"type\":\"string\"},\"head\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"created\":{\"type\":\"string\"},\"clone_url\":{\"type\":\"string\"},\"credentials\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"default_branch\",\"size_bytes\",\"available\",\"created\",\"clone_url\",\"credentials\"],\"additionalProperties\":false}", mcp.Destructive},
		{"delete", "Delete one of your repositories and everything in it.\n\nPass repo, its id or name. The repository and its history are gone for good; this cannot be undone. Refused while a git operation or maintenance runs on it; check busy with status and try again once it is false. The result is the id and name of the deleted repository.", "{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"The repository's id (rep_ and 16 hexadecimal digits) or its name.\"}},\"required\":[\"repo\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\"],\"additionalProperties\":false}", mcp.Destructive},
	}
}

func assertRegistryMetadata(t *testing.T, f *toolsFixture) {
	t.Helper()
	got, err := f.Client.ListTools(toolsContext(t), f.Caller)
	toolsMust(t, err)
	want := registryExpectedMetadata()
	toolsEqual(t, len(got), len(want))
	for i, w := range want {
		g := got[i]
		toolsEqual(t, g.Name, w.name)
		toolsEqual(t, g.Description, w.description)
		toolsEqual(t, string(g.InputSchema), w.input)
		toolsEqual(t, string(g.OutputSchema), w.output)
		toolsEqual(t, g.Effect(), w.effect)
		if g.Annotations.ReadOnlyHint == nil || g.Annotations.DestructiveHint == nil || g.Annotations.OpenWorldHint == nil {
			t.Fatalf("annotations incomplete: %#v", g.Annotations)
		}
		toolsEqual(t, *g.Annotations.ReadOnlyHint, w.effect == mcp.Read)
		toolsEqual(t, *g.Annotations.DestructiveHint, w.effect == mcp.Destructive)
		toolsEqual(t, *g.Annotations.OpenWorldHint, false)
		toolsEqual(t, g.Annotations.IdempotentHint, (*bool)(nil))
	}
}

func TestRegistryExactMetadataAndStability(t *testing.T) {
	// R-0QW7-YLRA R-LS48-WEAA R-LTC5-A60Z R-LUK1-NXRO R-LWZU-FH92 R-LY7Q-T8ZR R-LZFN-70QG R-M0NJ-KSH5
	// R-0S44-CDHZ R-0TC0-Q58O R-0UJX-3WZD R-0VRT-HOQ2 R-0Y7M-987G R-0ZFI-MZY5 R-10NF-0ROU R-11VB-EJFJ R-16QW-XMEB
	f := newToolsFixture(t)
	assertRegistryMetadata(t, f)
	successObject(t, f.call(t, "list", `{}`))
	assertRegistryMetadata(t, f)
	refusal(t, f.call(t, "create", `{"name":"BAD"}`), "invalid arguments:\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit")
	assertRegistryMetadata(t, f)
	toolsMust(t, f.Store.Close())
	assertRegistryMetadata(t, f)
	for _, c := range registryClosedCalls() {
		refusal(t, f.call(t, c.name, c.args), "cannot reach the repositories; try again later")
		assertRegistryMetadata(t, f)
	}
}

func TestRegistrySuccessAndRefusalResultShapes(t *testing.T) {
	// R-MBMN-0Q5E
	f := newToolsFixture(t)
	toolsEqual(t, string(successObject(t, f.call(t, "list", `{}`))), `{"repos":[]}`)
	created := successObject(t, f.call(t, "create", `{"name":"notes"}`))
	var values map[string]json.RawMessage
	toolsMust(t, json.Unmarshal(created, &values))
	var id string
	toolsMust(t, json.Unmarshal(values["id"], &id))
	r, err := f.Store.Find(toolsContext(t), f.Caller.UserID, id)
	toolsMust(t, err)
	size, err := f.Store.Size(toolsContext(t), id)
	toolsMust(t, err)
	toolsEqual(t, string(created), registryObject(f, r, size, ""))
	toolsEqual(t, string(successObject(t, f.call(t, "show", toolsRepoArgument(id)))), registryObject(f, r, size, ""))
	dir := f.Store.Dir(id)
	tree := strings.TrimSpace(f.git(t, dir, "mktree"))
	head := strings.TrimSpace(f.git(t, dir, "commit-tree", tree, "-m", "fixture"))
	f.git(t, dir, "update-ref", "refs/heads/main", head)
	size, err = f.Store.Size(toolsContext(t), id)
	toolsMust(t, err)
	toolsEqual(t, string(successObject(t, f.call(t, "show", toolsRepoArgument(id)))), registryObject(f, r, size, head))
	toolsEqual(t, string(successObject(t, f.call(t, "list", `{}`))), fmt.Sprintf(`{"repos":[{"id":%q,"name":"notes","size_bytes":%d,"head":%q,"available":true}]}`, id, size, head))
	renamed := successObject(t, f.call(t, "rename", toolsArguments(id, "journal")))
	r, err = f.Store.Find(toolsContext(t), f.Caller.UserID, id)
	toolsMust(t, err)
	size, err = f.Store.Size(toolsContext(t), id)
	toolsMust(t, err)
	toolsEqual(t, string(renamed), registryObject(f, r, size, head))
	toolsEqual(t, string(successObject(t, f.call(t, "status", `{}`))), fmt.Sprintf(`{"read":{"slots":8,"active":0,"queued":0},"write":{"slots":2,"active":0,"queued":0},"repos":[{"id":%q,"name":"journal","size_bytes":%d,"limit_bytes":1073741824,"available":true,"busy":false}]}`, id, size))
	toolsEqual(t, string(successObject(t, f.call(t, "delete", toolsRepoArgument(id)))), fmt.Sprintf(`{"id":%q,"name":"journal"}`, id))
	refusal(t, f.call(t, "show", toolsRepoArgument(id)), "invalid arguments:\nrepo: no repository '"+id+"'")
}

func registryObject(f *toolsFixture, r store.Repo, size int64, head string) string {
	part := ""
	if head != "" {
		part = fmt.Sprintf(`,"head":%q`, head)
	}
	credentials, _ := json.Marshal(clone.Guidance(f.Base).Text())
	return fmt.Sprintf(`{"id":%q,"name":%q,"default_branch":"main"%s,"size_bytes":%d,"available":%t,"created":%q,"clone_url":%q,"credentials":%s}`, r.ID, r.Name, part, size, r.Available, r.Created.UTC().Format("2006-01-02T15:04:05Z"), clone.URL(f.Base, r.Name), credentials)
}

func TestRegistryDecoderErrorsAndPrecedence(t *testing.T) {
	// R-MCUJ-EHW3 R-1337-SB68 R-MGI8-JT46
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	catalog, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	for _, c := range []struct{ name, args, text string }{
		{"rename", `{"name":5,"bogus":true}`, "invalid arguments:\nrepo: missing required field\nname: expected string, got number\nbogus: unknown field"},
		{"show", `{"repo":null}`, "invalid arguments:\nrepo: expected string, got null"},
		{"delete", `{"repo":null}`, "invalid arguments:\nrepo: expected string, got null"},
		{"create", `{"name":null}`, "invalid arguments:\nname: expected string, got null"},
		{"rename", `{"repo":null,"name":null}`, "invalid arguments:\nrepo: expected string, got null\nname: expected string, got null"},
		{"create", `{"name":"Not A Name","bogus":1}`, "invalid arguments:\nbogus: unknown field"},
	} {
		offset := len(f.events(t))
		refusal(t, f.call(t, c.name, c.args), c.text)
		events := f.events(t)[offset:]
		toolsEqual(t, len(events), 1)
		toolsEqual(t, events[0].Name, "tool.called")
		toolsEqual(t, events[0].Attrs["outcome"], "invalid_arguments")
		toolsEqual(t, events[0].Attrs["duration_us"], int64(0))
		got, err := f.Store.All(toolsContext(t))
		toolsMust(t, err)
		toolsEqual(t, got, catalog)
		toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	}
	toolsMust(t, f.Store.Close())
	refusal(t, f.call(t, "create", `{"name":"Not A Name","bogus":1}`), "invalid arguments:\nbogus: unknown field")
}

func registryClosedCalls() []struct{ name, args string } {
	return []struct{ name, args string }{
		{"list", `{}`}, {"show", `{"repo":"missing"}`}, {"status", `{}`}, {"create", `{"name":"taken"}`}, {"rename", `{"repo":"missing","name":"taken"}`}, {"delete", `{"repo":"notes"}`},
	}
}

func TestRegistryClosedStoreRefusesAndPreservesState(t *testing.T) {
	// R-14B4-62WX R-15J0-JUNM
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	f.create(t, f.Caller.UserID, "taken")
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	for _, c := range []struct{ name, args, text string }{
		{"show", `{"repo":"missing"}`, "invalid arguments:\nrepo: no repository 'missing'"},
		{"delete", `{"repo":"missing"}`, "invalid arguments:\nrepo: no repository 'missing'"},
		{"create", `{"name":"taken"}`, "invalid arguments:\nname: 'taken' is already one of your repositories"},
		{"rename", `{"repo":"notes","name":"taken"}`, "invalid arguments:\nname: 'taken' is already one of your repositories"},
	} {
		refusal(t, f.call(t, c.name, c.args), c.text)
	}
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	toolsMust(t, f.Store.Close())
	for _, c := range registryClosedCalls() {
		offset := len(f.events(t))
		refusal(t, f.call(t, c.name, c.args), "cannot reach the repositories; try again later")
		events := f.events(t)[offset:]
		toolsEqual(t, len(events), 1)
		toolsEqual(t, events[0].Name, "tool.called")
		toolsEqual(t, events[0].Attrs["outcome"], "error")
		toolsEqual(t, toolsSnapshot(t, f.Root), disk)
		reopened, err := store.Open(toolsContext(t), f.StoreConfig)
		toolsMust(t, err)
		got, err := reopened.All(toolsContext(t))
		toolsMust(t, err)
		toolsMust(t, reopened.Close())
		toolsEqual(t, got, before)
	}
}

func TestRegistryNamingWinsOverUnreachableStore(t *testing.T) {
	// R-MIY1-BCLK
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	for _, closed := range []bool{false, true} {
		if closed {
			toolsMust(t, f.Store.Close())
		}
		for _, args := range []string{`{"repo":"notes","name":"Not A Name"}`, `{"repo":"missing","name":"Not A Name"}`} {
			text := "invalid arguments:\n"
			if !closed && strings.Contains(args, `"missing"`) {
				text += "repo: no repository 'missing'\n"
			}
			text += "name: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit"
			refusal(t, f.call(t, "rename", args), text)
		}
		refusal(t, f.call(t, "create", `{"name":"Not A Name"}`), "invalid arguments:\nname: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit")
	}
}

func TestRegistryFailedWritesAfterReadPreserveState(t *testing.T) {
	// R-14B4-62WX R-15J0-JUNM R-17YT-BE50
	for _, tool := range []string{"create", "rename", "delete"} {
		t.Run(tool, func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			before, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			disk := toolsSnapshot(t, f.Root)
			info, err := os.Stat(f.DBParent)
			toolsMust(t, err)
			mode := info.Mode().Perm()
			t.Cleanup(func() { toolsMust(t, os.Chmod(f.DBParent, mode)) })
			toolsMust(t, os.Chmod(f.DBParent, mode&^0222))
			offset := len(f.events(t))
			args := `{"name":"new"}`
			switch tool {
			case "rename":
				args = toolsArguments(r.ID, "new")
			case "delete":
				args = toolsRepoArgument(r.ID)
			}
			refusal(t, f.call(t, tool, args), "cannot reach the repositories; try again later")
			events := f.events(t)[offset:]
			toolsEqual(t, len(events), 1)
			toolsEqual(t, events[0].Name, "tool.called")
			toolsEqual(t, events[0].Attrs["outcome"], "error")
			toolsMust(t, os.Chmod(f.DBParent, mode))
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			toolsMust(t, f.Store.Close())
			reopened, err := store.Open(toolsContext(t), f.StoreConfig)
			toolsMust(t, err)
			got, err := reopened.All(toolsContext(t))
			toolsMust(t, err)
			toolsMust(t, reopened.Close())
			toolsEqual(t, got, before)
			_, err = os.Stat(filepath.Join(f.Root, r.ID+".git"))
			toolsMust(t, err)
		})
	}
}
