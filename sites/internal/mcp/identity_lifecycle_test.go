package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sites/internal/sites"
)

func assertEmptyRegistryAndTrees(t *testing.T, h *testHandler) {
	t.Helper()
	all, err := h.store.List(context.Background())
	if err != nil || len(all) != 0 {
		t.Fatalf("registry after rejected create = %+v, err=%v", all, err)
	}
	for _, visibility := range []sites.Visibility{sites.Public, sites.Private} {
		entries, err := os.ReadDir(h.layout.SiteBase(visibility))
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read %s tree: %v", visibility, err)
		}
		if len(entries) != 0 {
			t.Fatalf("rejected create left entries in %s tree: %+v", visibility, entries)
		}
	}
}

func TestCreateValidatesDisplayNameAtEveryVisibility(t *testing.T) {
	badNames := []string{"", "   ", strings.Repeat("界", 101), "bad\x00name"}
	for _, visibility := range []string{"public", "private", "unlisted"} {
		for i, name := range badNames {
			t.Run(visibility+string(rune('a'+i)), func(t *testing.T) {
				h, _ := newTestHandler(t)
				args := map[string]any{"name": name, "visibility": visibility}
				if visibility != "unlisted" {
					args["slug"] = "valid-slug"
				}
				if got := callErr(t, h, "create", args)["code"]; got != "validation" {
					t.Fatalf("error code = %v, want validation", got)
				}
				assertEmptyRegistryAndTrees(t, h)
			})
		}
	}

	// R-ZO82-ALU2
	h, _ := newTestHandler(t)
	created := callOK(t, h, "create", map[string]any{"name": "  Launch  ", "slug": "launch", "visibility": "public"})
	stored, err := h.store.Get(context.Background(), "launch")
	if err != nil || created["name"] != "Launch" || stored.Name != "Launch" {
		t.Fatalf("trimmed name not persisted and returned: created=%+v stored=%+v err=%v", created, stored, err)
	}
}

func TestCreateSeparatesNameAndSlugForPublicAndPrivate(t *testing.T) {
	// R-ZQNV-25BG
	for _, visibility := range []sites.Visibility{sites.Public, sites.Private} {
		t.Run(string(visibility), func(t *testing.T) {
			h, _ := newTestHandler(t)
			created := callOK(t, h, "create", map[string]any{"name": "Launch Display", "slug": "launch", "visibility": visibility})
			stored, err := h.store.Get(context.Background(), "launch")
			if err != nil || stored.Name != "Launch Display" || stored.Visibility != visibility {
				t.Fatalf("stored site = %+v, err=%v", stored, err)
			}
			if created["slug"] != "launch" || created["name"] != "Launch Display" || created["url"] != testBaseURL+sites.Seg(visibility)+"/launch/" {
				t.Fatalf("created projection = %+v", created)
			}
			if info, err := os.Stat(h.layout.SiteDir(visibility, "launch")); err != nil || !info.IsDir() {
				t.Fatalf("site directory missing: %v", err)
			}
		})
	}
	for _, args := range []map[string]any{
		{"name": "Missing Slug", "visibility": "public"},
		{"name": "Invalid Slug", "slug": "Bad Slug", "visibility": "private"},
		{"name": "Reserved Slug", "slug": "mcp", "visibility": "public"},
	} {
		h, _ := newTestHandler(t)
		if got := callErr(t, h, "create", args)["code"]; got != "validation" {
			t.Fatalf("create(%+v) code = %v", args, got)
		}
		assertEmptyRegistryAndTrees(t, h)
	}
}

func TestRenameChangesOnlyDisplayNameAndIsStructured(t *testing.T) {
	const token = "ffffffffffffffffffffffffffffff"
	h, _ := newTestHandlerWithToken(t, func() string { return token })
	created := call(t, h, "create", map[string]any{"name": "Client Preview", "visibility": "unlisted"})
	if created.IsError {
		t.Fatalf("create failed: %+v", created)
	}
	if err := os.WriteFile(filepath.Join(h.layout.SiteDir(sites.Unlisted, token), "index.html"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := created.StructuredContent

	// R-ZZ75-QJIB
	renamed := call(t, h, "rename", map[string]any{"slug": token, "name": "  Customer Preview  "})
	if renamed.IsError || renamed.StructuredContent["name"] != "Customer Preview" {
		t.Fatalf("rename result = %+v", renamed)
	}
	for _, key := range []string{"slug", "visibility", "url", "owner_id", "owner_email", "created_at"} {
		if renamed.StructuredContent[key] != before[key] {
			t.Errorf("rename changed %s: before=%v after=%v", key, before[key], renamed.StructuredContent[key])
		}
	}
	if data, err := os.ReadFile(filepath.Join(h.layout.SiteDir(sites.Unlisted, token), "index.html")); err != nil || string(data) != "kept" {
		t.Fatalf("rename changed files: data=%q err=%v", data, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 101), "bad\nname"} {
		if got := callErr(t, h, "rename", map[string]any{"slug": token, "name": bad})["code"]; got != "validation" {
			t.Fatalf("bad rename code = %v", got)
		}
		stored, err := h.store.Get(context.Background(), token)
		if err != nil || stored.Name != "Customer Preview" {
			t.Fatalf("bad rename changed site: %+v err=%v", stored, err)
		}
	}
	if got := callErr(t, h, "rename", map[string]any{"slug": "missing", "name": "Valid"})["code"]; got != "not_found" {
		t.Fatalf("missing rename code = %v", got)
	}

	// R-0A69-6H6K
	resp := rpc(t, h, "tools/list", nil)
	var listed struct {
		Tools []struct {
			Name         string         `json:"name"`
			OutputSchema map[string]any `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &listed); err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	for _, descriptor := range listed.Tools {
		if descriptor.Name == "rename" {
			schema = descriptor.OutputSchema
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	wantKeys := []string{"slug", "name", "visibility", "owner_id", "owner_email", "url", "created_at", "updated_at"}
	if schema["type"] != "object" || len(properties) != len(wantKeys) || len(required) != len(wantKeys) {
		t.Fatalf("rename output schema = %+v", schema)
	}
	for _, key := range wantKeys {
		if _, ok := properties[key]; !ok {
			t.Errorf("rename output schema missing %q", key)
		}
	}
	var mirrored map[string]any
	if err := json.Unmarshal([]byte(payloadText(renamed)), &mirrored); err != nil || !reflect.DeepEqual(mirrored, renamed.StructuredContent) {
		t.Fatalf("rename channels differ: text=%+v structured=%+v err=%v", mirrored, renamed.StructuredContent, err)
	}
	if got := callErr(t, h, "rename", map[string]any{"slug": token, "name": ""})["code"]; got != "validation" {
		t.Fatalf("invalid rename code = %v", got)
	}
	if got := callErr(t, h, "rename", map[string]any{"slug": "gone", "name": "Valid"})["code"]; got != "not_found" {
		t.Fatalf("missing rename code = %v", got)
	}
}

func TestDeleteRemovesSlugRowAndDirectory(t *testing.T) {
	h, _ := newTestHandler(t)
	callOK(t, h, "create", map[string]any{"name": "Display", "slug": "delete-me", "visibility": "private"})
	dir := h.layout.SiteDir(sites.Private, "delete-me")

	// R-00F2-4B90
	callOK(t, h, "delete", map[string]any{"slug": "delete-me"})
	if _, err := h.store.Get(context.Background(), "delete-me"); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("deleted row still resolves: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("deleted directory remains: %v", err)
	}
}
