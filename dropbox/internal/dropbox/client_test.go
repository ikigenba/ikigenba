package dropbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestContentHashEmpty pins the documented empty-file case: with no blocks, the
// content hash is the SHA-256 of the empty string.
func TestContentHashEmpty(t *testing.T) {
	want := hex.EncodeToString(sha256.New().Sum(nil))
	if got := ContentHash(nil); got != want {
		t.Fatalf("empty content hash = %s, want %s", got, want)
	}
	if got := ContentHash([]byte{}); got != want {
		t.Fatalf("empty-slice content hash = %s, want %s", got, want)
	}
}

// TestContentHashSingleBlock: for data smaller than one 4 MiB block, the result
// is SHA-256(SHA-256(data)) — one block digest, concatenated (trivially), then
// hashed.
func TestContentHashSingleBlock(t *testing.T) {
	data := []byte("hello dropbox content hash")
	block := sha256.Sum256(data)
	overall := sha256.Sum256(block[:])
	want := hex.EncodeToString(overall[:])
	if got := ContentHash(data); got != want {
		t.Fatalf("single-block content hash = %s, want %s", got, want)
	}
}

// TestContentHashMultiBlock exercises the block-splitting path with a
// self-consistent vector: 2.5 blocks of bytes, hashed the long way (split into
// 4 MiB blocks, SHA-256 each, concat the raw digests, SHA-256 the concat) and
// compared to ContentHash. This proves the block boundary and the
// concat-then-hash steps against an independent reference computation.
func TestContentHashMultiBlock(t *testing.T) {
	const bs = contentHashBlockSize
	data := make([]byte, bs*2+bs/2) // two full blocks + a partial third
	for i := range data {
		data[i] = byte(i*31 + 7)
	}

	// Independent reference: hash each block, concat digests, hash that.
	overall := sha256.New()
	for off := 0; off < len(data); off += bs {
		end := off + bs
		if end > len(data) {
			end = len(data)
		}
		d := sha256.Sum256(data[off:end])
		overall.Write(d[:])
	}
	want := hex.EncodeToString(overall.Sum(nil))

	if got := ContentHash(data); got != want {
		t.Fatalf("multi-block content hash = %s, want %s", got, want)
	}
	// And the exported verify helper agrees.
	if err := VerifyContentHash(data, want); err != nil {
		t.Fatalf("VerifyContentHash(correct) = %v, want nil", err)
	}
	if err := VerifyContentHash(data, "deadbeef"); err == nil {
		t.Fatalf("VerifyContentHash(wrong) = nil, want mismatch error")
	}
}

// TestTokenRefreshAndListFolder drives the rpc path against a stub: a token
// refresh (POST /oauth2/token) feeds the bearer used on /2/files/list_folder.
// It asserts the bearer is present on the rpc call, the refresh body carries the
// refresh_token grant, and the entries normalize correctly.
func TestTokenRefreshAndListFolder(t *testing.T) {
	var sawBearer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if r.Form.Get("grant_type") != "refresh_token" {
				t.Errorf("grant_type = %q, want refresh_token", r.Form.Get("grant_type"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "ACCESS-XYZ", "expires_in": 14400, "token_type": "bearer",
			})
		case "/2/files/list_folder":
			sawBearer = r.Header.Get("Authorization")
			json.NewEncoder(w).Encode(map[string]any{
				"cursor": "CURSOR-1", "has_more": false,
				"entries": []map[string]any{
					{".tag": "file", "name": "a.txt", "path_lower": "/a.txt",
						"path_display": "/A.txt", "rev": "01ab", "size": 3, "content_hash": "hh"},
					{".tag": "deleted", "name": "g", "path_lower": "/g", "path_display": "/G"},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	res, err := c.ListFolder(context.Background())
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	if sawBearer != "Bearer ACCESS-XYZ" {
		t.Fatalf("rpc Authorization = %q, want Bearer ACCESS-XYZ", sawBearer)
	}
	if res.Cursor != "CURSOR-1" || res.HasMore {
		t.Fatalf("cursor/has_more = %q/%v", res.Cursor, res.HasMore)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(res.Entries))
	}
	if res.Entries[0].Tag != TagFile || res.Entries[0].PathDisplay != "/A.txt" || res.Entries[0].Rev != "01ab" {
		t.Fatalf("file entry wrong: %+v", res.Entries[0])
	}
	if res.Entries[1].Tag != TagDeleted {
		t.Fatalf("second entry tag = %q, want deleted", res.Entries[1].Tag)
	}
}

// TestLongpollNoAuth asserts the load-bearing rule: list_folder/longpoll carries
// NO Authorization header.
func TestLongpollNoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/files/list_folder/longpoll" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("longpoll carried Authorization = %q, want none", auth)
		}
		json.NewEncoder(w).Encode(map[string]any{"changes": true})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	// Point the longpoll client at the stub host (it normally hits notify.*).
	res, err := c.longpollAt(context.Background(), srv.URL, "CURSOR")
	if err != nil {
		t.Fatalf("longpoll: %v", err)
	}
	if !res.Changes {
		t.Fatalf("changes = false, want true")
	}
}

// newTestClient builds a Client whose hosts are redirected to srv via a
// RoundTripper rewrite, so the stub server sees rpc/oauth/longpoll calls.
func newTestClient(srv *httptest.Server) *Client {
	base, _ := url.Parse(srv.URL)
	rt := &rewriteTransport{target: base, inner: http.DefaultTransport}
	hc := &http.Client{Transport: rt}
	c := NewClient(Config{
		AppKey: "k", AppSecret: "s", RefreshToken: "r",
	}, hc)
	// Route the longpoll client through the same rewrite so the no-auth assertion
	// reaches the stub.
	c.longpoll = &http.Client{Transport: rt}
	return c
}

// rewriteTransport rewrites every request's scheme+host to target, preserving
// the path — lets the stub server stand in for api/content/notify hosts.
type rewriteTransport struct {
	target *url.URL
	inner  http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = t.target.Scheme
	r.URL.Host = t.target.Host
	r.Host = t.target.Host
	return t.inner.RoundTrip(r)
}

// longpollAt issues the longpoll against an explicit base host (the stub),
// exercising the same no-auth code path as Longpoll. The rewriteTransport
// already redirects host, so base is informational; we still override
// hostNotifyVar to prove the production host is the single override point.
func (c *Client) longpollAt(ctx context.Context, base, cursor string) (LongpollResult, error) {
	saved := hostNotifyVar
	hostNotifyVar = base
	defer func() { hostNotifyVar = saved }()
	return c.Longpoll(ctx, cursor)
}

// ---- Live integration probe (skipped without credentials) ----

// TestLiveProbe is the Phase 1 gate: with the DROPBOX_* secrets in the env, it
// refreshes a token and enumerates the ikigai-onebox app folder, asserting a
// non-empty cursor comes back (an empty listing is success). Skipped when the
// secrets are absent. Never prints token or secret values.
func TestLiveProbe(t *testing.T) {
	key := os.Getenv("DROPBOX_APP_KEY")
	secret := os.Getenv("DROPBOX_APP_SECRET")
	refresh := os.Getenv("DROPBOX_REFRESH_TOKEN")
	if key == "" || secret == "" || refresh == "" {
		t.Skip("DROPBOX_* secrets not present; skipping live probe")
	}
	root := os.Getenv("DROPBOX_APP_FOLDER_ROOT") // "" == app-folder root

	c := NewClient(Config{
		AppKey: key, AppSecret: secret, RefreshToken: refresh, AppFolderRoot: root,
	}, nil)

	ctx := context.Background()
	tok, err := c.token.token(ctx, false)
	if err != nil {
		t.Fatalf("token refresh failed: %v", err)
	}
	if tok == "" {
		t.Fatal("token refresh returned empty token")
	}
	t.Logf("token refreshed: yes (len redacted)")

	res, err := c.ListFolder(ctx)
	if err != nil {
		t.Fatalf("list_folder failed: %v", err)
	}
	if strings.TrimSpace(res.Cursor) == "" {
		t.Fatal("list_folder returned an empty cursor")
	}
	t.Logf("list_folder returned cursor: yes; entry_count=%d has_more=%v", len(res.Entries), res.HasMore)
}
