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
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

func registryExpectedMetadata() []struct {
	name, input, output string
	effect              mcp.Effect
} {
	return []struct {
		name, input, output string
		effect              mcp.Effect
	}{
		{"list", "{\"type\":\"object\",\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"repos\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"head\":{\"type\":\"string\"},\"available\":{\"type\":\"boolean\"}},\"required\":[\"id\",\"name\",\"size_bytes\",\"available\"],\"additionalProperties\":false}}},\"required\":[\"repos\"],\"additionalProperties\":false}", mcp.Read},
		{"show", "{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"repo\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"default_branch\":{\"type\":\"string\"},\"head\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"created\":{\"type\":\"string\"},\"clone_url\":{\"type\":\"string\"},\"credentials\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"default_branch\",\"size_bytes\",\"available\",\"created\",\"clone_url\",\"credentials\"],\"additionalProperties\":false}", mcp.Read},
		{"status", "{\"type\":\"object\",\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"read\":{\"type\":\"object\",\"properties\":{\"slots\":{\"type\":\"integer\"},\"active\":{\"type\":\"integer\"},\"queued\":{\"type\":\"integer\"}},\"required\":[\"slots\",\"active\",\"queued\"],\"additionalProperties\":false},\"write\":{\"type\":\"object\",\"properties\":{\"slots\":{\"type\":\"integer\"},\"active\":{\"type\":\"integer\"},\"queued\":{\"type\":\"integer\"}},\"required\":[\"slots\",\"active\",\"queued\"],\"additionalProperties\":false},\"repos\":{\"type\":\"array\",\"items\":{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"limit_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"busy\":{\"type\":\"boolean\"}},\"required\":[\"id\",\"name\",\"size_bytes\",\"limit_bytes\",\"available\",\"busy\"],\"additionalProperties\":false}}},\"required\":[\"read\",\"write\",\"repos\"],\"additionalProperties\":false}", mcp.Read},
		{"create", "{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"default_branch\":{\"type\":\"string\"},\"head\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"created\":{\"type\":\"string\"},\"clone_url\":{\"type\":\"string\"},\"credentials\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"default_branch\",\"size_bytes\",\"available\",\"created\",\"clone_url\",\"credentials\"],\"additionalProperties\":false}", mcp.Additive},
		{"rename", "{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"fixture\"},\"name\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"repo\",\"name\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"},\"default_branch\":{\"type\":\"string\"},\"head\":{\"type\":\"string\"},\"size_bytes\":{\"type\":\"integer\"},\"available\":{\"type\":\"boolean\"},\"created\":{\"type\":\"string\"},\"clone_url\":{\"type\":\"string\"},\"credentials\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\",\"default_branch\",\"size_bytes\",\"available\",\"created\",\"clone_url\",\"credentials\"],\"additionalProperties\":false}", mcp.Destructive},
		{"delete", "{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"fixture\"}},\"required\":[\"repo\"],\"additionalProperties\":false}", "{\"type\":\"object\",\"properties\":{\"id\":{\"type\":\"string\"},\"name\":{\"type\":\"string\"}},\"required\":[\"id\",\"name\"],\"additionalProperties\":false}", mcp.Destructive},
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
		if g.Description == "" {
			t.Fatal("empty tool description")
		}
		assertRegistryInput(t, g.InputSchema, w.input)
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
	// R-UFI1-7ZPQ
	// R-0QW7-YLRA R-LS48-WEAA
	// R-0S44-CDHZ R-UHXT-ZJ74 R-UJ5Q-DAXT R-UKDM-R2OI R-0Y7M-987G R-0ZFI-MZY5 R-10NF-0ROU R-11VB-EJFJ R-UP98-A5NA
	f := newToolsFixture(t)
	assertRegistryMetadata(t, f)
	successObject(t, f.call(t, "list", `{}`))
	assertRegistryMetadata(t, f)
	refusal(t, f.call(t, "create", `{"name":"BAD"}`), "invalid arguments:\nname: "+tools.InvalidName)
	assertRegistryMetadata(t, f)
	f.DB.SetFailing(true)
	assertRegistryMetadata(t, f)
	for _, c := range registryFailingCalls() {
		refusal(t, f.call(t, c.name, c.args), tools.Unreachable)
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
	refusal(t, f.call(t, "show", toolsRepoArgument(id)), "invalid arguments:\nrepo: "+fmt.Sprintf(tools.NoRepository, id))
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
	f.DB.SetFailing(true)
	refusal(t, f.call(t, "create", `{"name":"Not A Name","bogus":1}`), "invalid arguments:\nbogus: unknown field")
}

func registryFailingCalls() []struct{ name, args string } {
	return []struct{ name, args string }{
		{"list", `{}`}, {"show", `{"repo":"missing"}`}, {"status", `{}`}, {"create", `{"name":"new"}`}, {"rename", `{"repo":"notes","name":"new"}`}, {"delete", `{"repo":"notes"}`},
	}
}

func TestRegistryFailingStoreRefusesAndPreservesState(t *testing.T) {
	// R-ULLJ-4UF7 R-UO1B-WDWL R-UQH4-NXDZ
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	f.create(t, f.Caller.UserID, "taken")
	before, err := f.Store.All(toolsContext(t))
	toolsMust(t, err)
	disk := toolsSnapshot(t, f.Root)
	for _, c := range []struct{ name, args, text string }{
		{"show", `{"repo":"missing"}`, "invalid arguments:\nrepo: " + fmt.Sprintf(tools.NoRepository, "missing")},
		{"delete", `{"repo":"missing"}`, "invalid arguments:\nrepo: " + fmt.Sprintf(tools.NoRepository, "missing")},
		{"create", `{"name":"taken"}`, "invalid arguments:\nname: " + fmt.Sprintf(tools.NameTaken, "taken")},
		{"rename", `{"repo":"notes","name":"taken"}`, "invalid arguments:\nname: " + fmt.Sprintf(tools.NameTaken, "taken")},
	} {
		refusal(t, f.call(t, c.name, c.args), c.text)
	}
	toolsEqual(t, toolsSnapshot(t, f.Root), disk)
	f.DB.SetFailing(true)
	for _, c := range registryFailingCalls() {
		offset := len(f.events(t))
		refusal(t, f.call(t, c.name, c.args), tools.Unreachable)
		events := f.events(t)[offset:]
		toolsEqual(t, len(events), 1)
		toolsEqual(t, events[0].Name, "tool.called")
		toolsEqual(t, events[0].Attrs["outcome"], "error")
		toolsEqual(t, toolsSnapshot(t, f.Root), disk)
		f.DB.SetFailing(false)
		got, err := f.Store.All(toolsContext(t))
		toolsMust(t, err)
		f.DB.SetFailing(true)
		toolsEqual(t, got, before)
	}
}

func TestRegistryNamingWinsOverUnreachableStore(t *testing.T) {
	// R-UMTF-IM5W
	f := newToolsFixture(t)
	f.create(t, f.Caller.UserID, "notes")
	for _, failing := range []bool{false, true} {
		if failing {
			f.DB.SetFailing(true)
		}
		for _, args := range []string{`{"repo":"notes","name":"Not A Name"}`, `{"repo":"missing","name":"Not A Name"}`} {
			text := "invalid arguments:\n"
			if !failing && strings.Contains(args, `"missing"`) {
				text += "repo: " + fmt.Sprintf(tools.NoRepository, "missing") + "\n"
			}
			text += "name: " + tools.InvalidName
			refusal(t, f.call(t, "rename", args), text)
		}
		refusal(t, f.call(t, "create", `{"name":"Not A Name"}`), "invalid arguments:\nname: "+tools.InvalidName)
	}
}

func TestRegistryFailedWritesAfterReadPreserveState(t *testing.T) {
	// R-ULLJ-4UF7 R-UO1B-WDWL
	for _, tool := range []string{"create", "rename", "delete"} {
		t.Run(tool, func(t *testing.T) {
			f := newToolsFixture(t)
			r := f.create(t, f.Caller.UserID, "notes")
			before, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			disk := toolsSnapshot(t, f.Root)
			path := f.Root
			if tool == "rename" {
				path = f.Store.Dir(r.ID)
			}
			info, err := os.Stat(path)
			toolsMust(t, err)
			mode := info.Mode().Perm()
			t.Cleanup(func() { toolsMust(t, os.Chmod(path, mode)) })
			toolsMust(t, os.Chmod(path, mode&^0222))
			offset := len(f.events(t))
			args := `{"name":"new"}`
			switch tool {
			case "rename":
				args = toolsArguments(r.ID, "new")
			case "delete":
				args = toolsRepoArgument(r.ID)
			}
			refusal(t, f.call(t, tool, args), tools.Unreachable)
			events := f.events(t)[offset:]
			toolsEqual(t, len(events), 1)
			toolsEqual(t, events[0].Name, "tool.called")
			toolsEqual(t, events[0].Attrs["outcome"], "error")
			toolsMust(t, os.Chmod(path, mode))
			toolsEqual(t, toolsSnapshot(t, f.Root), disk)
			got, err := f.Store.All(toolsContext(t))
			toolsMust(t, err)
			toolsEqual(t, got, before)
			_, err = os.Stat(filepath.Join(f.Root, r.ID+".git"))
			toolsMust(t, err)
		})
	}
}

func assertRegistryInput(t *testing.T, got []byte, want string) {
	t.Helper()
	var schema struct {
		Properties map[string]struct{ Description string }
	}
	toolsMust(t, json.Unmarshal(got, &schema))
	normalized := string(got)
	for _, field := range schema.Properties {
		if field.Description == "" {
			t.Fatal("empty argument description")
		}
		encoded, err := json.Marshal(field.Description)
		toolsMust(t, err)
		normalized = strings.ReplaceAll(normalized, `"description":`+string(encoded), `"description":"fixture"`)
	}
	toolsEqual(t, normalized, want)
}
