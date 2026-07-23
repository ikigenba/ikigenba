package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"

	appkitmcp "appkit/mcp"
	"appkit/server"

	sitefiles "sites/internal/files"
	"sites/internal/sites"
)

// toolPrefix brands every MCP tool name (DECISIONS §1). It is the suite name
// ikigenba + the service name; HTTP route paths are NOT branded.
const toolPrefix = ""

// tool returns the branded, fully-qualified MCP tool name. Used by BOTH
// toolDescriptors and dispatchTool so the two sites cannot drift.
func tool(verb string) string { return toolPrefix + verb }

type toolHandlers struct {
	store    *sites.Store
	layout   sites.Layout
	baseURL  string
	mirror   sites.MirrorClient
	newToken func() string
}

//go:embed guide.md
var guideDoc string

// Tools returns sites's service-owned MCP tool declarations. The shared appkit
// MCP transport prepends the chassis health and reflection tools.
func Tools(store *sites.Store, layout sites.Layout, baseURL string, mirror sites.MirrorClient) []appkitmcp.Tool {
	return toolsWithToken(store, layout, baseURL, mirror, sites.NewToken)
}

func toolsWithToken(store *sites.Store, layout sites.Layout, baseURL string, mirror sites.MirrorClient, newToken func() string) []appkitmcp.Tool {
	if store == nil {
		panic("mcp: sites store is required")
	}
	if newToken == nil {
		newToken = sites.NewToken
	}
	h := &toolHandlers{store: store, layout: layout, baseURL: baseURL, mirror: mirror, newToken: newToken}
	return []appkitmcp.Tool{
		desc(tool("guide"), "Return the sites usage guide — the site model, slug and "+
			"confinement rules, and worked basic/advanced examples (create a public page in "+
			"one call, import from Dropbox). Read once before your first create.",
			obj(map[string]any{}), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
				return appkitmcp.TextResult(guideDoc), nil
			}),
		descOut(tool("create"), "Create a static website/site owned by the authenticated caller at an explicit visibility. Public and private sites require a caller-chosen name; unlisted sites forbid name and receive a generated secret token. Inserts the registry row and creates its empty live directory. Returns the created site.", obj(map[string]any{
			"name":       descTyp("string", "the site slug for a public or private site; omit for unlisted"),
			"visibility": enumString("public", "private", "unlisted"),
		}, "visibility"), siteOutputSchema(), func(ctx context.Context, args json.RawMessage, id server.Identity) (map[string]any, error) {
			return h.toolCreate(ctx, args, id)
		}),
		descOut(tool("list"), "List every site with its public/private/unlisted visibility, creator, URL, and timestamps. Takes no inputs.", obj(map[string]any{}), obj(map[string]any{
			"sites": map[string]any{"type": "array", "items": siteOutputSchema()},
		}, "sites"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolList(ctx)
		}),
		descOut(tool("delete"), "Delete a site: remove its registry row and its live directory for the current visibility. Idempotent: tolerates an already-removed directory or row.", obj(map[string]any{
			"name": descTyp("string", "the site slug to delete"),
		}, "name"), obj(map[string]any{"deleted": map[string]any{"type": "string"}}, "deleted"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolDelete(ctx, args)
		}),
		descOut(tool("mkdir"), "Create a directory (and any missing parents) inside a site's live directory for its current visibility. 'path' is relative to that site root and is confined to it (absolute paths and any escape via '..' are rejected). file_write already creates parent dirs, so this is only needed to make an empty directory.", obj(map[string]any{
			"name": descTyp("string", "the site slug whose directory to create the directory in"),
			"path": descTyp("string", "directory path relative to the site's current root"),
		}, "name", "path"), obj(map[string]any{"created": map[string]any{"type": "string"}, "site": map[string]any{"type": "string"}}, "created", "site"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolMkdir(ctx, args)
		}),
		descOut(tool("set_visibility"), "Set a site's visibility and move its live directory. Entering unlisted generates a fresh secret token; leaving unlisted requires new_name. Returns the site with its current name and URL.", obj(map[string]any{
			"name":       descTyp("string", "the site's current name or unlisted token"),
			"visibility": enumString("public", "private", "unlisted"),
			"new_name":   descTyp("string", "required caller-chosen slug when leaving unlisted; otherwise forbidden"),
		}, "name", "visibility"), siteOutputSchema(), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolSetVisibility(ctx, args)
		}),
		descOut(tool("sync"), "Import a static website/site from a Dropbox-mirrored folder into an existing site's live directory for its current visibility. 'source_path' is the mirror folder to sync from (e.g. \"/sites/marketing\"); 'slug' names the target site and defaults to the source_path basename when that is a valid slug, else it is required. Returns not_found if the site does not already exist. For an existing site, reconciles its current site directory to match the subtree: every upstream file is (over)written and every site file absent upstream is deleted. Visibility is unchanged. Returns {slug, written, deleted}.", obj(map[string]any{
			"source_path": descTyp("string", "the mirror folder path to sync from"),
			"slug":        descTyp("string", "target site slug; defaults to the source_path basename"),
		}, "source_path"), obj(map[string]any{"slug": map[string]any{"type": "string"}, "written": map[string]any{"type": "integer"}, "deleted": map[string]any{"type": "integer"}}, "slug", "written", "deleted"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolSync(ctx, args)
		}),
		descOut(tool("file_write"), "Write content to file_path inside the site's live directory for its current visibility. Creates parent dirs; overwrites by default, or appends when append:true.", obj(map[string]any{
			"site":      descTyp("string", "site slug whose current directory is the sandbox root"),
			"file_path": descTyp("string", "path relative to the site's current root (confined; absolute and '..' rejected)"),
			"content":   descTyp("string", "the bytes to write"),
			"append":    descTyp("boolean", "append to the file instead of overwriting; creates the file if missing (default false)"),
		}, "site", "file_path", "content"), obj(map[string]any{"written": map[string]any{"type": "string"}, "site": map[string]any{"type": "string"}, "appended": map[string]any{"type": "boolean"}}, "written", "site", "appended"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolFileWrite(ctx, args)
		}),
		desc(tool("file_read"), "Read a file inside a site's live directory for its current visibility. Optional offset/limit page large files.", obj(map[string]any{
			"site":      descTyp("string", "site slug whose current directory is the sandbox root"),
			"file_path": descTyp("string", "path relative to the site's current root (confined; absolute and '..' rejected)"),
			"offset":    descTyp("number", "1-based line offset to start reading from"),
			"limit":     descTyp("number", "maximum number of lines to return"),
		}, "site", "file_path"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolFileRead(ctx, args)
		}),
		descOut(tool("file_edit"), "Edit a file inside a site's live directory for its current visibility by replacing old_string with new_string.", obj(map[string]any{
			"site":        descTyp("string", "site slug whose current directory is the sandbox root"),
			"file_path":   descTyp("string", "path relative to the site's current root (confined; absolute and '..' rejected)"),
			"old_string":  descTyp("string", "existing text to replace"),
			"new_string":  descTyp("string", "replacement text"),
			"replace_all": descTyp("boolean", "replace every occurrence instead of only the first"),
		}, "site", "file_path", "old_string", "new_string"), obj(map[string]any{"edited": map[string]any{"type": "string"}, "site": map[string]any{"type": "string"}, "replaced": map[string]any{"type": "integer"}}, "edited", "site", "replaced"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolFileEdit(ctx, args)
		}),
		descOut(tool("file_glob"), "Glob for files inside a site's live directory for its current visibility.", obj(map[string]any{
			"site":    descTyp("string", "site slug whose current directory is the sandbox root"),
			"pattern": descTyp("string", "glob pattern to match"),
			"path":    descTyp("string", "optional directory path relative to the site's current root"),
		}, "site", "pattern"), obj(map[string]any{"site": map[string]any{"type": "string"}, "matches": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "site", "matches"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolFileGlob(ctx, args)
		}),
		descOut(tool("file_grep"), "Grep file contents inside a site's live directory for its current visibility.", obj(map[string]any{
			"site":    descTyp("string", "site slug whose current directory is the sandbox root"),
			"pattern": descTyp("string", "regular expression to search for"),
			"path":    descTyp("string", "optional file or directory path relative to the site's current root"),
			"glob":    descTyp("string", "optional filename glob filter"),
		}, "site", "pattern"), obj(map[string]any{"site": map[string]any{"type": "string"}, "matches": map[string]any{"type": "array", "items": obj(map[string]any{"path": map[string]any{"type": "string"}, "line": map[string]any{"type": "integer"}, "text": map[string]any{"type": "string"}}, "path", "line", "text")}}, "site", "matches"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolFileGrep(ctx, args)
		}),
		descOut(tool("file_list"), "List every regular file under the site's live directory for its current visibility with its size and md5, for reconciliation against local files. 'path' optionally scopes the walk; returned paths are relative to the site root.", obj(map[string]any{
			"site": descTyp("string", "site slug whose current directory is the sandbox root"),
			"path": descTyp("string", "optional subdirectory (relative to the current root) to scope the walk"),
		}, "site"), obj(map[string]any{"site": map[string]any{"type": "string"}, "files": map[string]any{"type": "array", "items": obj(map[string]any{"path": map[string]any{"type": "string"}, "size": map[string]any{"type": "integer"}, "md5": map[string]any{"type": "string"}}, "path", "size", "md5")}}, "site", "files"), func(ctx context.Context, args json.RawMessage, _ server.Identity) (map[string]any, error) {
			return h.toolFileList(ctx, args)
		}),
	}
}

func desc(name, description string, schema map[string]any, handler func(context.Context, json.RawMessage, server.Identity) (map[string]any, error)) appkitmcp.Tool {
	return appkitmcp.Tool{Name: name, Description: description, InputSchema: schema, Handler: handler}
}

func descOut(name, description string, in, out map[string]any, handler func(context.Context, json.RawMessage, server.Identity) (map[string]any, error)) appkitmcp.Tool {
	return appkitmcp.Tool{Name: name, Description: description, InputSchema: in, OutputSchema: out, Handler: handler}
}

func siteOutputSchema() map[string]any {
	return obj(map[string]any{
		"name":        map[string]any{"type": "string"},
		"visibility":  map[string]any{"type": "string"},
		"owner_id":    map[string]any{"type": "string"},
		"owner_email": map[string]any{"type": "string"},
		"url":         map[string]any{"type": "string"},
		"created_at":  map[string]any{"type": "string"},
		"updated_at":  map[string]any{"type": "string"},
	}, "name", "visibility", "owner_id", "owner_email", "url", "created_at", "updated_at")
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func descTyp(t, description string) map[string]any {
	return map[string]any{"type": t, "description": description}
}

func enumString(values ...string) map[string]any {
	enum := make([]any, len(values))
	for i, value := range values {
		enum[i] = value
	}
	return map[string]any{"type": "string", "enum": enum}
}

// toolCreate validates the slug, inserts the row, then
// creates the site directory at the requested visibility.
func (h *toolHandlers) toolCreate(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
	var a struct {
		Name       *string `json:"name"`
		Visibility string  `json:"visibility"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	visibility, err := sites.ParseVisibility(a.Visibility)
	if err != nil {
		return errResultMsg(appkitmcp.ErrValidation, err.Error()), nil
	}
	if visibility == sites.Unlisted {
		if a.Name != nil {
			return errResultMsg(appkitmcp.ErrValidation, "name is forbidden for unlisted sites"), nil
		}
		return h.createUnlisted(ctx, id)
	}
	if a.Name == nil {
		return errResultMsg(appkitmcp.ErrValidation, "name is required for public and private sites"), nil
	}
	return h.createNamed(ctx, *a.Name, visibility, id)
}

func (h *toolHandlers) createNamed(ctx context.Context, name string, visibility sites.Visibility, id server.Identity) (map[string]any, error) {
	if err := sites.ValidateSlug(name); err != nil {
		return errResult(err), nil
	}
	site, err := h.store.Create(ctx, name, name, id.OwnerID, id.OwnerEmail, visibility)
	if err != nil {
		return errResult(err), nil
	}
	if err := os.MkdirAll(h.layout.SiteDir(visibility, name), 0o755); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "create_site_dir: "+err.Error()), nil
	}
	return appkitmcp.StructuredResult(h.renderSite(site))
}

func (h *toolHandlers) createUnlisted(ctx context.Context, id server.Identity) (map[string]any, error) {
	for attempt := 0; attempt < 2; attempt++ {
		result, err := h.createNamed(ctx, h.newToken(), sites.Unlisted, id)
		if err != nil {
			return nil, err
		}
		if !isErrorCode(result, appkitmcp.ErrConflict) || attempt == 1 {
			return result, nil
		}
	}
	panic("unreachable")
}

// toolList renders every site as structured JSON.
func (h *toolHandlers) toolList(ctx context.Context) (map[string]any, error) {
	all, err := h.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(all))
	for _, s := range all {
		out = append(out, h.renderSite(s))
	}
	return appkitmcp.StructuredResult(map[string]any{"sites": out})
}

// toolDelete removes the row and current visibility directory. A missing row or
// directory is a successful idempotent delete at the MCP surface.
func (h *toolHandlers) toolDelete(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	site, err := h.store.Get(ctx, a.Name)
	if err != nil {
		if errors.Is(err, sites.ErrNotFound) {
			return appkitmcp.StructuredResult(map[string]any{"deleted": a.Name})
		}
		return errResult(err), nil
	}
	if err := h.store.Delete(ctx, a.Name); err != nil && !errors.Is(err, sites.ErrNotFound) {
		return errResult(err), nil
	}
	if err := os.RemoveAll(h.layout.SiteDir(site.Visibility, a.Name)); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "remove_site_dir: "+err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]any{"deleted": a.Name})
}

// toolMkdir creates a directory (and parents) confined to the current site
// directory. The path is attacker-controlled, so confinement is delegated to
// internal/files.
func (h *toolHandlers) toolMkdir(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	site, err := h.store.Get(ctx, a.Name)
	if err != nil {
		return errResult(err), nil
	}
	root := h.layout.SiteDir(site.Visibility, a.Name)
	if err := sitefiles.Mkdir(root, a.Path); err != nil {
		if errors.Is(err, sitefiles.ErrEscapes) {
			return errResultMsg(appkitmcp.ErrValidation, "path_escapes_working_dir: "+err.Error()), nil
		}
		return errResultMsg(appkitmcp.ErrInternal, "mkdir: "+err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]any{"created": a.Path, "site": a.Name})
}

// toolSetVisibility enforces the naming invariant while changing visibility.
func (h *toolHandlers) toolSetVisibility(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var a struct {
		Name       string  `json:"name"`
		Visibility string  `json:"visibility"`
		NewName    *string `json:"new_name"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	site, err := h.store.Get(ctx, a.Name)
	if err != nil {
		return errResult(err), nil
	}
	visibility, err := sites.ParseVisibility(a.Visibility)
	if err != nil {
		return errResultMsg(appkitmcp.ErrValidation, err.Error()), nil
	}
	newName := a.Name
	if visibility == sites.Unlisted {
		if a.NewName != nil {
			return errResultMsg(appkitmcp.ErrValidation, "new_name is forbidden when entering unlisted"), nil
		}
		return h.setUnlisted(ctx, site)
	}
	if site.Visibility == sites.Unlisted {
		if a.NewName == nil {
			return errResultMsg(appkitmcp.ErrValidation, "new_name is required when leaving unlisted"), nil
		}
		if err := sites.ValidateSlug(*a.NewName); err != nil {
			return errResult(err), nil
		}
		newName = *a.NewName
	} else if a.NewName != nil {
		return errResultMsg(appkitmcp.ErrValidation, "new_name is forbidden unless leaving unlisted"), nil
	}
	return h.applyVisibility(ctx, site, visibility, newName)
}

func (h *toolHandlers) setUnlisted(ctx context.Context, site sites.Site) (map[string]any, error) {
	for attempt := 0; attempt < 2; attempt++ {
		result, err := h.applyVisibility(ctx, site, sites.Unlisted, h.newToken())
		if err != nil {
			return nil, err
		}
		if !isErrorCode(result, appkitmcp.ErrConflict) || attempt == 1 {
			return result, nil
		}
	}
	panic("unreachable")
}

func (h *toolHandlers) applyVisibility(ctx context.Context, site sites.Site, visibility sites.Visibility, newName string) (map[string]any, error) {
	if err := h.store.SetVisibility(ctx, site.Slug, visibility, newName); err != nil {
		return errResult(err), nil
	}
	if err := h.layout.Move(site.Slug, site.Visibility, newName, visibility); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "move_site_dir: "+err.Error()), nil
	}
	site, err := h.store.Get(ctx, newName)
	if err != nil {
		return nil, err
	}
	return appkitmcp.StructuredResult(h.renderSite(site))
}

func isErrorCode(result map[string]any, code appkitmcp.ErrorCode) bool {
	structured, ok := result["structuredContent"].(map[string]any)
	return ok && structured["code"] == code
}

// unmarshalArgs decodes a tool's arguments, tolerating an absent params block.
func unmarshalArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// siteURL is the front-door URL for a site under a visibility segment:
// <baseURL><public|private>/<name>/. baseURL already carries the trailing slash.
func (h *toolHandlers) siteURL(tier, name string) string {
	return h.baseURL + tier + "/" + name + "/"
}

// renderSite maps a Site to its MCP JSON projection.
func (h *toolHandlers) renderSite(s sites.Site) map[string]any {
	tier := sites.Seg(s.Visibility)
	return map[string]any{
		"name":        s.Slug,
		"visibility":  string(s.Visibility),
		"owner_id":    s.OwnerID,
		"owner_email": s.OwnerEmail,
		"url":         h.siteURL(tier, s.Slug),
		"created_at":  s.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		"updated_at":  s.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
	}
}

// errResult maps a domain error to the corrective MCP error envelope,
// classifying the known sentinels into stable codes so an agent can
// self-correct.
func errResult(err error) map[string]any {
	switch {
	case errors.Is(err, sites.ErrInvalidSlug), errors.Is(err, sites.ErrReservedName):
		return errResultMsg(appkitmcp.ErrValidation, err.Error())
	case errors.Is(err, sites.ErrExists):
		return errResultMsg(appkitmcp.ErrConflict, err.Error())
	case errors.Is(err, sites.ErrNotFound):
		return errResultMsg(appkitmcp.ErrNotFound, err.Error())
	default:
		return errResultMsg(appkitmcp.ErrInternal, err.Error())
	}
}

// errResultMsg renders the {error:{code,message}} corrective envelope as the
// isError tool result.
func errResultMsg(code appkitmcp.ErrorCode, msg string) map[string]any {
	return appkitmcp.ErrorResult(code, msg)
}
