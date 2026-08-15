package mcp

// sync.go wires the `sync` verb: it ties together the pieces Phase 6 built —
// the dropbox loopback MirrorClient (enumerate /list + fetch /content) and the
// pure sites.Reconcile routine — behind the MCP surface. sync adopts a Dropbox
// mirror subtree for an existing site's current public/private directory:
// enumerate the subtree, overwrite every upstream file and delete every site
// file absent upstream (the subtree owns the tree). It leaves visibility
// unchanged.

import (
	"appkit/server"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sites/internal/sites"
	"sort"
	"strings"

	appkitmcp "appkit/mcp"
)

// toolSync reconciles a dropbox mirror subtree into a site's current directory. It
// requires source_path; slug defaults to the source_path basename (and must be
// a valid slug, else the caller must pass slug explicitly — ADR "Slug
// derivation"). The flow: validate → load the existing row → stamp source_path
// → enumerate upstream → fetch each file's bytes keyed by its path relative to
// source_path → walk the current site directory for the existing path set →
// Reconcile.
func (h *toolHandlers) toolSync(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
	a, slug, result, err := h.syncTarget(raw)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}

	site, err := h.store.Get(ctx, slug)
	if err != nil {
		return errResult(err), nil
	}
	// Stamp the originating subtree on the row (marks the site import-managed and
	// records provenance — ADR Decision 2). It applies to an existing site whose
	// subtree may have moved.
	if err := h.store.SetSourcePath(ctx, slug, a.SourcePath); err != nil {
		return errResult(err), nil
	}

	desired, result := h.syncDesired(ctx, a.SourcePath)
	if result != nil {
		return result, nil
	}

	workingDir := h.layout.SiteDir(site.Visibility, slug)
	existingRel, existingData, err := syncExisting(workingDir)
	if err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "walk_working: "+err.Error()), nil
	}

	if syncTreesMatch(desired, existingData) {
		return appkitmcp.StructuredResult(map[string]any{"slug": slug, "written": 0, "deleted": 0})
	}

	ctx = sites.WithVersionActor(ctx, id.ClientID)
	commit, err := h.version.Commit(ctx, slug, "sync "+a.SourcePath, syncChanges(site.Path, desired, existingRel))
	if err != nil {
		return versionErrorResult(err), nil
	}
	if err := h.store.SetRepoSha(ctx, slug, commit.Sha); err != nil {
		return errResultMsg(appkitmcp.ErrInternal, "record repo sha: "+err.Error()), nil
	}

	written, deleted, rerr := sites.Reconcile(workingDir, desired, existingRel)
	if rerr != nil {
		return errResultMsg(appkitmcp.ErrInternal, "reconcile: "+rerr.Error()), nil
	}

	return appkitmcp.StructuredResult(map[string]any{"slug": slug, "written": written, "deleted": deleted})
}

type syncArgs struct {
	SourcePath string `json:"source_path"`
	Slug       string `json:"slug"`
}

func (h *toolHandlers) syncTarget(raw json.RawMessage) (args syncArgs, slugResult string, result map[string]any, resultErr error) {
	var a syncArgs
	if err := unmarshalArgs(raw, &a); err != nil {
		return a, "", nil, err
	}
	if h.mirror == nil {
		return a, "", errResultMsg(appkitmcp.ErrSourceUnavailable, "sync_unconfigured: sync is not wired: no dropbox mirror client (DROPBOX_BASE_URL)"), nil
	}
	if a.SourcePath == "" {
		return a, "", errResultMsg(appkitmcp.ErrValidation, "missing required \"source_path\" argument"), nil
	}
	slug := a.Slug
	derived := slug == ""
	if derived {
		slug = path.Base(a.SourcePath)
	}
	if err := sites.ValidateSlug(slug); err != nil {
		if derived {
			message := "derived slug " + jsonQuote(slug) + " from source_path is not a valid slug; pass \"slug\" explicitly"
			return a, "", errResultMsg(appkitmcp.ErrValidation, message), nil
		}
		return a, "", errResult(err), nil
	}
	return a, slug, nil, nil
}

func (h *toolHandlers) syncDesired(ctx context.Context, sourcePath string) (filesByPath map[string][]byte, result map[string]any) {
	files, err := h.mirror.List(ctx, sourcePath)
	if err != nil {
		return nil, errResultMsg(appkitmcp.ErrSourceUnavailable, "list_upstream: "+err.Error())
	}
	desired := make(map[string][]byte, len(files))
	for _, file := range files {
		data, err := h.mirror.Fetch(ctx, file.Path)
		if err != nil {
			return nil, errResultMsg(appkitmcp.ErrSourceUnavailable, "fetch_upstream: "+err.Error())
		}
		desired[relUnder(sourcePath, file.Path)] = data
	}
	return desired, nil
}

func syncExisting(workingDir string) (existingPaths []string, existingData map[string][]byte, resultErr error) {
	var paths []string
	dataByPath := make(map[string][]byte)
	err := filepath.WalkDir(workingDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(workingDir, filePath)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		paths = append(paths, rel)
		dataByPath[rel] = data
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return paths, dataByPath, nil
	}
	return paths, dataByPath, err
}

func syncTreesMatch(desired, existing map[string][]byte) bool {
	if len(desired) != len(existing) {
		return false
	}
	for rel, data := range desired {
		if current, ok := existing[rel]; !ok || !bytes.Equal(data, current) {
			return false
		}
	}
	return true
}

func syncChanges(root string, desired map[string][]byte, existing []string) []sites.FileChange {
	desiredPaths := make([]string, 0, len(desired))
	for rel := range desired {
		desiredPaths = append(desiredPaths, rel)
	}
	sort.Strings(desiredPaths)
	changes := make([]sites.FileChange, 0, len(desired)+len(existing))
	for _, rel := range desiredPaths {
		changes = append(changes, sites.FileChange{Path: repositoryPath(root, rel), Data: desired[rel]})
	}
	sort.Strings(existing)
	for _, rel := range existing {
		if _, ok := desired[rel]; !ok {
			changes = append(changes, sites.FileChange{Path: repositoryPath(root, rel), Delete: true})
		}
	}
	return changes
}

// relUnder returns child's path relative to prefix (both mirror paths, slash-
// separated). When child sits directly under prefix the result is the suffix
// with leading slashes trimmed; when prefix is a non-prefix (defensive) the
// child's basename is used so nothing escapes the working tree.
func relUnder(prefix, child string) string {
	p := strings.TrimRight(prefix, "/")
	if p == "" {
		return strings.TrimLeft(child, "/")
	}
	if strings.HasPrefix(child, p+"/") {
		return strings.TrimPrefix(child, p+"/")
	}
	// child is not under prefix (defensive — the upstream list is scoped to
	// prefix): fall back to the basename so nothing escapes the working tree.
	return path.Base(child)
}

// jsonQuote renders s as a JSON string literal (with surrounding quotes) for
// embedding the offending slug in a validation message safely.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
