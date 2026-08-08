package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"sites/internal/sites"
)

type recordedVersionCall struct {
	operation string
	slug      string
	oldSlug   string
	newSlug   string
	owner     sites.Owner
}

type recordingVersionClient struct {
	calls         []recordedVersionCall
	failOperation string
	failureUsed   bool
}

func (c *recordingVersionClient) record(call recordedVersionCall) error {
	c.calls = append(c.calls, call)
	if c.failOperation == call.operation && !c.failureUsed {
		c.failureUsed = true
		return errors.New("repos unavailable")
	}
	return nil
}

func (c *recordingVersionClient) Create(_ context.Context, slug string, owner sites.Owner) error {
	return c.record(recordedVersionCall{operation: "create", slug: slug, owner: owner})
}

func (*recordingVersionClient) Commit(_ context.Context, _ string, _ string, _ []sites.FileChange) (sites.Commit, error) {
	return sites.Commit{Sha: "recording-commit"}, nil
}

func (*recordingVersionClient) Export(context.Context, string) ([]sites.TreeEntry, sites.Commit, error) {
	return nil, sites.Commit{}, nil
}

func (c *recordingVersionClient) Rename(_ context.Context, oldSlug, newSlug string, owner sites.Owner) error {
	return c.record(recordedVersionCall{operation: "rename", oldSlug: oldSlug, newSlug: newSlug, owner: owner})
}

func (c *recordingVersionClient) Delete(_ context.Context, slug string, owner sites.Owner) error {
	return c.record(recordedVersionCall{operation: "delete", slug: slug, owner: owner})
}

func (c *recordingVersionClient) reset() { c.calls = nil }

func assertVersionCalls(t *testing.T, got, want []recordedVersionCall) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("version calls = %+v, want %+v", got, want)
	}
}

func TestCreateCreatesRepositoryAndRollsBackWhenPlaneFails(t *testing.T) {
	// R-FARO-E6HT
	owner := sites.Owner{ID: testOwnerID, Email: testOwner}
	version := &recordingVersionClient{}
	h, _ := newTestHandlerWithVersion(t, version)
	callOK(t, h, "create", map[string]any{"name": "Launch", "slug": "launch", "visibility": "public"})
	assertVersionCalls(t, version.calls, []recordedVersionCall{{operation: "create", slug: "launch", owner: owner}})

	const token = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unlistedVersion := &recordingVersionClient{}
	unlisted, _ := newTestHandlerWithVersionAndToken(t, unlistedVersion, func() string { return token })
	created := callOK(t, unlisted, "create", map[string]any{"name": "Preview", "visibility": "unlisted"})
	if created["slug"] != token {
		t.Fatalf("unlisted slug = %v, want %q", created["slug"], token)
	}
	assertVersionCalls(t, unlistedVersion.calls, []recordedVersionCall{{operation: "create", slug: token, owner: owner}})

	failingVersion := &recordingVersionClient{failOperation: "create"}
	failing, _ := newTestHandlerWithVersion(t, failingVersion)
	errPayload := callErr(t, failing, "create", map[string]any{"name": "Broken", "slug": "broken", "visibility": "public"})
	if errPayload["code"] != "internal" {
		t.Fatalf("create failure code = %v, want internal", errPayload["code"])
	}
	assertVersionCalls(t, failingVersion.calls, []recordedVersionCall{{operation: "create", slug: "broken", owner: owner}})
	if _, err := failing.store.Get(context.Background(), "broken"); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("plane-failed create left row: %v", err)
	}
	if _, err := os.Stat(failing.layout.SiteDir(sites.Public, "broken")); !os.IsNotExist(err) {
		t.Fatalf("plane-failed create left directory: %v", err)
	}
}

func TestDeleteArchivesBeforeRemovingLocalSite(t *testing.T) {
	// R-FBZK-RY8I
	owner := sites.Owner{ID: testOwnerID, Email: testOwner}
	version := &recordingVersionClient{}
	h, _ := newTestHandlerWithVersion(t, version)
	callOK(t, h, "create", map[string]any{"name": "Delete", "slug": "delete-me", "visibility": "private"})
	version.reset()
	dir := h.layout.SiteDir(sites.Private, "delete-me")
	callOK(t, h, "delete", map[string]any{"slug": "delete-me"})
	assertVersionCalls(t, version.calls, []recordedVersionCall{{operation: "delete", slug: "delete-me", owner: owner}})
	if _, err := h.store.Get(context.Background(), "delete-me"); !errors.Is(err, sites.ErrNotFound) {
		t.Fatalf("successful delete left row: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("successful delete left directory: %v", err)
	}

	failingVersion := &recordingVersionClient{}
	failing, _ := newTestHandlerWithVersion(t, failingVersion)
	callOK(t, failing, "create", map[string]any{"name": "Keep", "slug": "keep-me", "visibility": "public"})
	path := filepath.Join(failing.layout.SiteDir(sites.Public, "keep-me"), "index.html")
	wantBytes := []byte("preserve these exact bytes\n")
	if err := os.WriteFile(path, wantBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	failingVersion.reset()
	failingVersion.failOperation = "delete"
	if got := callErr(t, failing, "delete", map[string]any{"slug": "keep-me"})["code"]; got != "internal" {
		t.Fatalf("delete failure code = %v, want internal", got)
	}
	assertVersionCalls(t, failingVersion.calls, []recordedVersionCall{{operation: "delete", slug: "keep-me", owner: owner}})
	if _, err := failing.store.Get(context.Background(), "keep-me"); err != nil {
		t.Fatalf("plane-failed delete removed row: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !reflect.DeepEqual(got, wantBytes) {
		t.Fatalf("plane-failed delete changed file: bytes=%q err=%v", got, err)
	}
}

func TestVisibilitySlugChangesRenameRepositoryKey(t *testing.T) {
	// R-FD7H-5PZ7
	owner := sites.Owner{ID: testOwnerID, Email: testOwner}
	tokens := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccccccccc"}
	next := 0
	version := &recordingVersionClient{}
	h, _ := newTestHandlerWithVersionAndToken(t, version, func() string {
		token := tokens[next]
		next++
		return token
	})
	callOK(t, h, "create", map[string]any{"name": "Launch", "slug": "original", "visibility": "public"})
	version.reset()

	first := callOK(t, h, "set_visibility", map[string]any{"slug": "original", "visibility": "unlisted"})
	if first["slug"] != tokens[0] {
		t.Fatalf("first token = %v, want %q", first["slug"], tokens[0])
	}
	assertVersionCalls(t, version.calls, []recordedVersionCall{{operation: "rename", oldSlug: "original", newSlug: tokens[0], owner: owner}})
	version.reset()

	second := callOK(t, h, "set_visibility", map[string]any{"slug": tokens[0], "visibility": "unlisted"})
	if second["slug"] != tokens[1] {
		t.Fatalf("rotated token = %v, want %q", second["slug"], tokens[1])
	}
	assertVersionCalls(t, version.calls, []recordedVersionCall{{operation: "rename", oldSlug: tokens[0], newSlug: tokens[1], owner: owner}})
	version.reset()

	public := callOK(t, h, "set_visibility", map[string]any{"slug": tokens[1], "visibility": "public", "new_slug": "launch"})
	if public["slug"] != "launch" {
		t.Fatalf("public slug = %v, want launch", public["slug"])
	}
	assertVersionCalls(t, version.calls, []recordedVersionCall{{operation: "rename", oldSlug: tokens[1], newSlug: "launch", owner: owner}})
}

func TestLifecycleWithoutSlugChangeMakesNoPlaneCall(t *testing.T) {
	// R-FEFD-JHPW
	version := &recordingVersionClient{}
	h, _ := newTestHandlerWithVersion(t, version)
	callOK(t, h, "create", map[string]any{"name": "Old Label", "slug": "stable", "visibility": "private"})
	privateFile := filepath.Join(h.layout.SiteDir(sites.Private, "stable"), "index.html")
	if err := os.WriteFile(privateFile, []byte("moved"), 0o644); err != nil {
		t.Fatal(err)
	}
	version.reset()

	callOK(t, h, "set_visibility", map[string]any{"slug": "stable", "visibility": "public"})
	assertVersionCalls(t, version.calls, nil)
	publicFile := filepath.Join(h.layout.SiteDir(sites.Public, "stable"), "index.html")
	if got, err := os.ReadFile(publicFile); err != nil || string(got) != "moved" {
		t.Fatalf("tier-only visibility did not move file: bytes=%q err=%v", got, err)
	}
	if _, err := os.Stat(privateFile); !os.IsNotExist(err) {
		t.Fatalf("tier-only visibility left private file: %v", err)
	}

	callOK(t, h, "set_visibility", map[string]any{"slug": "stable", "visibility": "public"})
	assertVersionCalls(t, version.calls, nil)
	if _, err := os.Stat(publicFile); err != nil {
		t.Fatalf("idempotent visibility changed directory: %v", err)
	}

	callOK(t, h, "rename", map[string]any{"slug": "stable", "name": "New Label"})
	assertVersionCalls(t, version.calls, nil)
	stored, err := h.store.Get(context.Background(), "stable")
	if err != nil || stored.Name != "New Label" {
		t.Fatalf("display rename not stored: site=%+v err=%v", stored, err)
	}
}
