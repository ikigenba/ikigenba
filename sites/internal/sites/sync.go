package sites

// sync.go holds the sites-side machinery for the `sync` verb (ADR
// dropbox-import-sync, Decisions 3/6): a loopback HTTP client that enumerates a
// dropbox mirror subtree (GET /list, cursor-paginated) and fetches a file's
// bytes (GET /content), plus a pure in-place reconcile routine that mutates a
// site's working tree to match the subtree (overwrite-all-present +
// delete-absent). The MCP verb that wires these together lives in package mcp
// (a later phase); everything here is HTTP/filesystem only, unit-tested with a
// fake client and a temp dir.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// MirrorFile is one entry from the dropbox loopback /list route. Hash is the
// FULL Dropbox content_hash (block-SHA256) — the loopback route does not
// abbreviate it the way the MCP `list` tool does (ADR Decision 4). sites does
// not diff on it today (overwrite-all sidesteps change detection), but the field
// is carried for the deferred per-file sync manifest.
type MirrorFile struct {
	Path      string
	Size      int64
	Hash      string
	Rev       string
	UpdatedAt string
}

// MirrorClient is the seam the sync verb depends on: enumerate a subtree and
// fetch a file's bytes over the dropbox loopback plane. Injected so the verb is
// unit-testable with a fake (no network, no live dropbox).
type MirrorClient interface {
	// List enumerates every mirror file under prefix, following next_cursor to
	// completion (the caller sees one stitched set, never a page).
	List(ctx context.Context, prefix string) ([]MirrorFile, error)
	// Fetch returns the current bytes of one mirror file by its full mirror path.
	Fetch(ctx context.Context, path string) ([]byte, error)
}

// httpMirrorClient is the production MirrorClient: it talks to the dropbox
// service's loopback routes derived from a base URL (DROPBOX_BASE_URL, read at
// the composition root). It carries no identity headers — /list and /content
// are loopback-only and self-guard against any nginx-injected identity.
type httpMirrorClient struct {
	base string
	hc   *http.Client
}

// NewMirrorClient builds a MirrorClient against base (e.g.
// "http://127.0.0.1:3005"); it derives <base>/list and <base>/content.
func NewMirrorClient(base string) MirrorClient {
	return &httpMirrorClient{base: strings.TrimRight(base, "/"), hc: http.DefaultClient}
}

// listPage is the on-wire page shape (the route renders snake_case keys).
// NextCursor empty/absent ⇒ the last page.
type listPage struct {
	Files []struct {
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		Hash      string `json:"hash"`
		Rev       string `json:"rev"`
		UpdatedAt string `json:"updated_at"`
	} `json:"files"`
	NextCursor string `json:"next_cursor"`
}

// List follows the cursor to completion: it loops GET <base>/list?path=&cursor=,
// accumulating files, until a page returns no next_cursor.
func (c *httpMirrorClient) List(ctx context.Context, prefix string) ([]MirrorFile, error) {
	var out []MirrorFile
	cursor := ""
	for {
		u := c.base + "/list?path=" + url.QueryEscape(prefix)
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("list %q: %w", prefix, err)
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list %q: %w", prefix, err)
		}
		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("list %q: %w", prefix, rerr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list %q: unexpected status %d", prefix, resp.StatusCode)
		}
		var page listPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("list %q: decode: %w", prefix, err)
		}
		for _, f := range page.Files {
			out = append(out, MirrorFile{
				Path:      f.Path,
				Size:      f.Size,
				Hash:      f.Hash,
				Rev:       f.Rev,
				UpdatedAt: f.UpdatedAt,
			})
		}
		if page.NextCursor == "" {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

// Fetch GETs <base>/content?path=…, returning the body on 200 and a typed error
// otherwise. import/sync fetch current bytes (no rev pin); the response reports
// what landed, so mirror-lag is visible and a re-run is cheap (ADR defaults).
func (c *httpMirrorClient) Fetch(ctx context.Context, path string) ([]byte, error) {
	u := c.base + "/content?path=" + url.QueryEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", path, err)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", path, err)
	}
	defer resp.Body.Close()
	body, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, fmt.Errorf("fetch %q: %w", path, rerr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %q: unexpected status %d", path, resp.StatusCode)
	}
	return body, nil
}

// confineUnder resolves rel against root and verifies the result stays inside
// root, defending against escapes via absolute paths or '..'. Replicated from
// the unexported confinePath in package mcp (files.go / tools.go) — that helper
// is package-private there, so the same confinement logic is mirrored here
// rather than exported across the package boundary. Rejects absolute rel and any
// rel whose cleaned join escapes root.
func confineUnder(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", errors.New("path must be relative to the working dir: " + rel)
	}
	abs := filepath.Clean(filepath.Join(root, rel))
	r, err := filepath.Rel(filepath.Clean(root), abs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes the working dir: " + rel)
	}
	return abs, nil
}

// Reconcile mutates workingDir in place to match `desired` (the subtree files,
// keyed by their path RELATIVE to source_path, with already-fetched bytes as the
// value): it (over)writes every desired file and deletes every working file
// present in existingRel but absent from desired. This is the overwrite-all +
// delete-absent reconcile (ADR Decision 6): because every present file is
// rewritten, only the path *set* matters — no content hash / md5 comparison is
// needed (the two services have no shared hash to diff against anyway).
//
// existingRel is the working tree's current relative file set (slash-separated),
// obtained by the caller walking workingDir. Every path — desired keys and
// existingRel entries alike — is confined under workingDir before any write or
// delete, so a malicious upstream key cannot escape the site's sandbox.
//
// sync is binary-safe: no UTF-8 or size validation (site assets — images, fonts).
// Files are written 0o644 under MkdirAll'd parents; deletes use os.Remove. Empty
// parent dirs left behind by a delete are NOT pruned in v1 (an empty dir is inert
// for nginx serving; pruning is a deferred nicety per the plan).
func Reconcile(workingDir string, desired map[string][]byte, existingRel []string) (written, deleted int, err error) {
	// Confine and write every desired file. Confinement happens first for the
	// whole step so an escape attempt fails loudly before any bytes hit disk.
	for rel, data := range desired {
		abs, cerr := confineUnder(workingDir, rel)
		if cerr != nil {
			return written, deleted, cerr
		}
		if mderr := os.MkdirAll(filepath.Dir(abs), 0o755); mderr != nil {
			return written, deleted, fmt.Errorf("reconcile mkdir %q: %w", rel, mderr)
		}
		if werr := os.WriteFile(abs, data, 0o644); werr != nil {
			return written, deleted, fmt.Errorf("reconcile write %q: %w", rel, werr)
		}
		written++
	}

	// Delete every working file absent from desired (the subtree owns the tree).
	for _, rel := range existingRel {
		if _, keep := desired[rel]; keep {
			continue
		}
		abs, cerr := confineUnder(workingDir, rel)
		if cerr != nil {
			return written, deleted, cerr
		}
		if rmerr := os.Remove(abs); rmerr != nil && !os.IsNotExist(rmerr) {
			return written, deleted, fmt.Errorf("reconcile delete %q: %w", rel, rmerr)
		}
		deleted++
	}

	return written, deleted, nil
}
