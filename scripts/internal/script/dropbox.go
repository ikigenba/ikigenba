package script

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ContentFetcher fetches the current bytes of one file from the dropbox mirror.
// It is the seam the import path depends on: the service holds a ContentFetcher
// (field-injected at the composition root, like dropbox's svc.Mirror), so Import
// is unit-testable with a fake — no live dropbox, no network. The HTTP impl is
// per-service and intentionally duplicated across consumers (scripts ≈ prompts);
// scripts is not agent-backed, so agentkit is the wrong home, and appkit is the
// chassis for a service's own server, not for calling peers.
type ContentFetcher interface {
	// Fetch returns the file's bytes at the given mirror path, or ErrNotFound if
	// the path is absent from the mirror.
	Fetch(ctx context.Context, path string) ([]byte, error)
}

// httpFetcher fetches over loopback HTTP from dropbox's read-only GET /content
// route (the loopback twin of the MCP get tool — streamed, un-base64'd, uncapped).
type httpFetcher struct {
	base string       // DROPBOX_BASE_URL, default registry.BaseURL("dropbox")
	hc   *http.Client // shared client
}

// NewHTTPFetcher builds a ContentFetcher that fetches from <base>/content. base
// is DROPBOX_BASE_URL (default registry.BaseURL("dropbox")), read at main's
// composition root.
func NewHTTPFetcher(base string) ContentFetcher {
	return &httpFetcher{base: base, hc: http.DefaultClient}
}

// Fetch GETs <base>/content?path=<path>: 200 → body, 404 → ErrNotFound, any other
// status → a typed error. The request carries no nginx-injected identity headers,
// so dropbox's self-guard treats it as the loopback caller it is.
func (f *httpFetcher) Fetch(ctx context.Context, path string) ([]byte, error) {
	u := f.base + "/content?path=" + url.QueryEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("script: fetch %q: %w", path, err)
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("script: fetch %q: %w", path, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("script: fetch %q read: %w", path, err)
		}
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: dropbox mirror has no file at %q", ErrNotFound, path)
	default:
		return nil, fmt.Errorf("script: fetch %q: dropbox returned %s", path, resp.Status)
	}
}
