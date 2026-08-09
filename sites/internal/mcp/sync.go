package mcp

// sync.go wires the `sync` verb: it ties together the pieces Phase 6 built —
// the dropbox loopback MirrorClient (enumerate /list + fetch /content) and the
// pure sites.Reconcile routine — behind the MCP surface. sync adopts a Dropbox
// mirror subtree for an existing site's current public/private directory:
// enumerate the subtree, overwrite every upstream file and delete every site
// file absent upstream (the subtree owns the tree). It leaves visibility
// unchanged.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	appkitmcp "appkit/mcp"
	"appkit/server"

	"sites/internal/sites"
)

// toolSync reconciles a dropbox mirror subtree into a site's current directory. It
// requires source_path; slug defaults to the source_path basename (and must be
// a valid slug, else the caller must pass slug explicitly — ADR "Slug
// derivation"). The flow: validate → load the existing row → stamp source_path
// → enumerate upstream → fetch each file's bytes keyed by its path relative to
// source_path → walk the current site directory for the existing path set →
// Reconcile.
func (h *toolHandlers) toolSync(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
	var a struct {
		SourcePath string `json:"source_path"`
		Slug       string `json:"slug"`
	}
	if err := unmarshalArgs(raw, &a); err != nil {
		return nil, err
	}
	if h.mirror == nil {
		return errResultMsg(appkitmcp.ErrSourceUnavailable, "sync_unconfigured: sync is not wired: no dropbox mirror client (DROPBOX_BASE_URL)"), nil
	}
	if a.SourcePath == "" {
		return errResultMsg(appkitmcp.ErrValidation, "missing required \"source_path\" argument"), nil
	}

	// Derive the slug from the source-path basename when none was given, then
	// pre-check it against the slug grammar. Store.Create enforces the same
	// grammar, but a pre-check turns a bad auto-derived slug into a clean
	// validation error that tells the caller to pass slug explicitly rather than
	// surfacing a raw create failure.
	slug := a.Slug
	derived := false
	if slug == "" {
		slug = path.Base(a.SourcePath)
		derived = true
	}
	if err := sites.ValidateSlug(slug); err != nil {
		if derived {
			return errResultMsg(appkitmcp.ErrValidation,
				"derived slug "+jsonQuote(slug)+" from source_path is not a valid slug; pass \"slug\" explicitly"), nil
		}
		return errResult(err), nil
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

	// Enumerate the subtree upstream and fetch each file's current bytes, keyed by
	// its path RELATIVE to source_path (that relative key is the in-working-tree
	// path Reconcile writes to). The client follows /list's cursor to completion.
	files, err := h.mirror.List(ctx, a.SourcePath)
	if err != nil {
		return errResultMsg(appkitmcp.ErrSourceUnavailable, "list_upstream: "+err.Error()), nil
	}
	desired := make(map[string][]byte, len(files))
	for _, f := range files {
		rel := relUnder(a.SourcePath, f.Path)
		data, ferr := h.mirror.Fetch(ctx, f.Path)
		if ferr != nil {
			return errResultMsg(appkitmcp.ErrSourceUnavailable, "fetch_upstream: "+ferr.Error()), nil
		}
		desired[rel] = data
	}

	// Walk the site directory for its current relative file set (the path set is all
	// Reconcile needs for delete-absent — overwrite-all means no md5 compare; same
	// WalkDir + Rel/ToSlash pattern as toolFileList).
	workingDir := h.layout.SiteDir(site.Visibility, slug)
	var existingRel []string
	existingData := make(map[string][]byte)
	walkErr := filepath.WalkDir(workingDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(workingDir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		existingRel = append(existingRel, rel)
		existingData[rel] = data
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		return errResultMsg(appkitmcp.ErrInternal, "walk_working: "+walkErr.Error()), nil
	}

	// A byte-identical tree is a true no-op. If anything differs, preserve sync's
	// overwrite-all behavior by putting every desired file in the one batch.
	matching := len(desired) == len(existingData)
	if matching {
		for rel, data := range desired {
			existing, ok := existingData[rel]
			if !ok || !bytes.Equal(data, existing) {
				matching = false
				break
			}
		}
	}
	if matching {
		return appkitmcp.StructuredResult(map[string]any{"slug": slug, "written": 0, "deleted": 0})
	}

	desiredPaths := make([]string, 0, len(desired))
	for rel := range desired {
		desiredPaths = append(desiredPaths, rel)
	}
	sort.Strings(desiredPaths)
	changes := make([]sites.FileChange, 0, len(desired)+len(existingRel))
	for _, rel := range desiredPaths {
		changes = append(changes, sites.FileChange{Path: repositoryPath(site.Path, rel), Data: desired[rel]})
	}
	sort.Strings(existingRel)
	for _, rel := range existingRel {
		if _, ok := desired[rel]; !ok {
			changes = append(changes, sites.FileChange{Path: repositoryPath(site.Path, rel), Delete: true})
		}
	}

	ctx = sites.WithVersionActor(ctx, id.ClientID)
	commit, err := h.version.Commit(ctx, slug, "sync "+a.SourcePath, changes)
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
