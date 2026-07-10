package dropbox

// client.go is the only HTTP-to-Dropbox site (PLAN.md §2/§8). It holds a
// tokenSource (refresh -> short-lived access token, cached, refreshed on expiry
// or a 401) and the Dropbox API v2 calls the sync engine needs:
//
//   - list_folder (first boot, recursive) + list_folder/continue (drain HasMore)
//   - list_folder/longpoll on a cursor — the ONE Dropbox endpoint that carries
//     NO Authorization header (the cursor is the capability) and that uses a
//     dedicated long-timeout (>=600s) HTTP client to survive Dropbox's
//     timeout(<=480s) + up to ~90s of jitter.
//   - download of a file (bytes + the Dropbox content_hash from metadata),
//     plus block-SHA256 verification of the bytes against that hash.
//
// Three distinct Dropbox hosts are in play (verified against live API v2 docs):
//   - OAuth:   https://api.dropboxapi.com/oauth2/token
//   - RPC:     https://api.dropboxapi.com/2/...        (bearer)
//   - content: https://content.dropboxapi.com/2/...    (bearer)
//   - longpoll:https://notify.dropboxapi.com/2/...      (NO bearer)
//
// No disk, no db, no HTTP server. Pure API client + hashing. Token values are
// never logged.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Dropbox hosts (§2). Kept as constants so the rpc/content/longpoll split is
// explicit and the longpoll-no-auth rule is impossible to apply to the wrong
// host by accident.
const (
	hostOAuth   = "https://api.dropboxapi.com/oauth2/token"
	hostRPC     = "https://api.dropboxapi.com"
	hostContent = "https://content.dropboxapi.com"
)

// hostNotifyVar is the longpoll host. It is a var (not a const) solely so tests
// can redirect the longpoll call at a stub; production always uses the real
// notify.dropboxapi.com host (the only Dropbox host that serves the
// unauthenticated longpoll endpoint).
var hostNotifyVar = "https://notify.dropboxapi.com"

// contentHashBlockSize is Dropbox's content-hash block size: 4 MiB. The file is
// split into blocks of this size, each block is SHA-256'd, the digests are
// concatenated, and that concatenation is SHA-256'd and hex-encoded.
const contentHashBlockSize = 4 * 1024 * 1024

// uploadSimpleLimit is Dropbox's maximum size for the one-request upload API.
// Larger files must use an upload session.
const uploadSimpleLimit int64 = 150 * 1024 * 1024

// uploadChunkSize bounds the memory retained while streaming an upload session.
const uploadChunkSize = 8 * 1024 * 1024

// Longpoll timeout policy (§2). Dropbox blocks for up to LongpollTimeoutSeconds
// (max 480) plus up to ~90s of random jitter, so the longpoll HTTP client needs
// a wall-clock budget comfortably above 480+90.
const (
	defaultLongpollTimeoutSeconds = 480
	maxLongpollTimeoutSeconds     = 480
	minLongpollTimeoutSeconds     = 30
	// longpollClientTimeout is the dedicated longpoll client's wall-clock
	// timeout: >=600s so a parked longpoll read survives 480s + ~90s jitter
	// rather than being torn down each cycle.
	longpollClientTimeout = 630 * time.Second
)

// Config carries the credentials and tuning the Client needs. The three secrets
// are read from the environment at the main.go boundary (they arrive via
// .envrc/direnv in dev, the launcher on the box) and passed in here — the Client
// never reads the environment itself and never logs these values.
type Config struct {
	AppKey       string // DROPBOX_APP_KEY
	AppSecret    string // DROPBOX_APP_SECRET
	RefreshToken string // DROPBOX_REFRESH_TOKEN
	// AppFolderRoot is the path the engine roots list_folder at (DROPBOX_APP_
	// FOLDER_ROOT); "" means the app-folder root, which is what Dropbox expects
	// for an enumeration of the whole App-folder-scoped folder.
	AppFolderRoot string
	// LongpollTimeoutSeconds is the timeout param sent to list_folder/longpoll
	// (clamped to [30,480]); 0 -> 480.
	LongpollTimeoutSeconds int
}

// Client is the Dropbox API v2 client. It is safe for concurrent use; the
// embedded tokenSource serializes refreshes.
type Client struct {
	cfg Config

	// rpc is the client for rpc + content + oauth calls (bearer-bearing). A
	// modest timeout is fine — these are not parked reads.
	rpc *http.Client
	// longpoll is the dedicated client for list_folder/longpoll: a long
	// (>=600s) timeout so the parked read survives Dropbox's jitter. NO bearer
	// is ever sent on calls made with this client.
	longpoll *http.Client

	token *tokenSource
}

// NewClient builds a Client from cfg. httpClient, if non-nil, is used as the
// base for the rpc/content client (tests inject a stub); the longpoll client is
// always given the long timeout. A nil httpClient yields a default rpc client.
func NewClient(cfg Config, httpClient *http.Client) *Client {
	rpc := httpClient
	if rpc == nil {
		rpc = &http.Client{Timeout: 100 * time.Second}
	}
	c := &Client{
		cfg:      cfg,
		rpc:      rpc,
		longpoll: &http.Client{Timeout: longpollClientTimeout},
	}
	c.token = &tokenSource{
		appKey:       cfg.AppKey,
		appSecret:    cfg.AppSecret,
		refreshToken: cfg.RefreshToken,
		httpClient:   rpc,
	}
	return c
}

// ---------------------------------------------------------------------------
// tokenSource: refresh_token -> short-lived access token, cached, refreshed on
// expiry or on a 401 (§2). The bearer never touches disk or logs.
// ---------------------------------------------------------------------------

type tokenSource struct {
	appKey       string
	appSecret    string
	refreshToken string
	httpClient   *http.Client

	mu        sync.Mutex
	accessTok string
	expiry    time.Time
}

// tokenExpiryslack refreshes a little before the real expiry so an in-flight
// call doesn't race the boundary.
const tokenExpirySlack = 60 * time.Second

// token returns a valid access token, refreshing if the cache is empty or
// within tokenExpirySlack of expiry. Pass force=true (used on a 401) to discard
// the cache and refresh unconditionally.
func (t *tokenSource) token(ctx context.Context, force bool) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !force && t.accessTok != "" && time.Now().Before(t.expiry.Add(-tokenExpirySlack)) {
		return t.accessTok, nil
	}
	if err := t.refreshLocked(ctx); err != nil {
		return "", err
	}
	return t.accessTok, nil
}

// invalidate drops the cached token so the next token() forces a refresh. Called
// after a 401 from an rpc/content call.
func (t *tokenSource) invalidate() {
	t.mu.Lock()
	t.accessTok = ""
	t.mu.Unlock()
}

// refreshLocked exchanges the refresh token for a fresh access token. Caller
// holds t.mu. Credentials go in the POST body (client_id/client_secret) — never
// logged. Endpoint: POST https://api.dropboxapi.com/oauth2/token.
func (t *tokenSource) refreshLocked(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", t.refreshToken)
	form.Set("client_id", t.appKey)
	form.Set("client_secret", t.appSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hostOAuth, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dropbox token refresh: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		// Body may echo the error description but never the token; safe to surface.
		return fmt.Errorf("dropbox token refresh: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("dropbox token refresh: decode: %w", err)
	}
	if tr.AccessToken == "" {
		return fmt.Errorf("dropbox token refresh: empty access_token")
	}
	t.accessTok = tr.AccessToken
	exp := tr.ExpiresIn
	if exp <= 0 {
		exp = 14400 // Dropbox short-lived tokens are ~4h; default if omitted.
	}
	t.expiry = time.Now().Add(time.Duration(exp) * time.Second)
	return nil
}

// ---------------------------------------------------------------------------
// rpcCall: POST a JSON body to an rpc/content host with the bearer, retrying
// once on a 401 with a forced token refresh (§2). Returns the decoded JSON.
// ---------------------------------------------------------------------------

// apiError is a Dropbox error envelope; error_summary is human-readable.
type apiError struct {
	Summary string          `json:"error_summary"`
	Detail  json.RawMessage `json:"error"`
}

// rpcCall posts reqBody (JSON-marshaled) to host+path with the bearer and
// decodes the response into out. On a 401 it forces one token refresh and
// retries once.
func (c *Client) rpcCall(ctx context.Context, host, path string, reqBody, out any) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	do := func() (*http.Response, error) {
		tok, terr := c.token.token(ctx, false)
		if terr != nil {
			return nil, terr
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, host+path, bytes.NewReader(payload))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		return c.rpc.Do(req)
	}

	resp, err := do()
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		c.token.invalidate()
		// Force a refresh and retry once.
		if _, terr := c.token.token(ctx, true); terr != nil {
			return terr
		}
		resp, err = do()
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return c.statusError(resp.StatusCode, body)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("dropbox: decode %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) statusError(status int, body []byte) error {
	var ae apiError
	if json.Unmarshal(body, &ae) == nil && ae.Summary != "" {
		return fmt.Errorf("dropbox: status %d: %s", status, strings.TrimSpace(ae.Summary))
	}
	return fmt.Errorf("dropbox: status %d: %s", status, strings.TrimSpace(string(body)))
}

// contentUpload posts bytes to a Dropbox content endpoint. Upload bodies are
// streams, so unlike rpcCall they cannot be replayed after a 401; acquire the
// bearer before consuming the reader and let the caller retry with a fresh
// reader if Dropbox rejects it.
func (c *Client) contentUpload(ctx context.Context, path string, arg any, body io.Reader, out any) error {
	argJSON, err := json.Marshal(arg)
	if err != nil {
		return err
	}
	tok, err := c.token.token(ctx, false)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hostContent+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Dropbox-API-Arg", httpHeaderSafeJSON(argJSON))

	resp, err := c.rpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return c.statusError(resp.StatusCode, response)
	}
	if out != nil {
		if err := json.Unmarshal(response, out); err != nil {
			return fmt.Errorf("dropbox: decode %s: %w", path, err)
		}
	}
	return nil
}

// Upload writes src to path using Dropbox's overwrite conflict policy. Files
// over 150 MiB are streamed through an upload session in bounded chunks.
func (c *Client) Upload(ctx context.Context, path string, src io.Reader, size int64) (string, error) {
	if size < 0 {
		return "", fmt.Errorf("dropbox upload: negative size %d", size)
	}
	commit := map[string]any{"path": path, "mode": "overwrite", "autorename": false, "mute": false}
	var metadata struct {
		Rev string `json:"rev"`
	}
	if size <= uploadSimpleLimit {
		if err := c.contentUpload(ctx, "/2/files/upload", commit, io.LimitReader(src, size), &metadata); err != nil {
			return "", fmt.Errorf("dropbox upload: %w", err)
		}
		return metadata.Rev, nil
	}

	buf := make([]byte, uploadChunkSize)
	first := int64(uploadChunkSize)
	if first > size {
		first = size
	}
	if _, err := io.ReadFull(src, buf[:first]); err != nil {
		return "", fmt.Errorf("dropbox upload: read first chunk: %w", err)
	}
	var start struct {
		SessionID string `json:"session_id"`
	}
	if err := c.contentUpload(ctx, "/2/files/upload_session/start", map[string]bool{"close": false}, bytes.NewReader(buf[:first]), &start); err != nil {
		return "", fmt.Errorf("dropbox upload session start: %w", err)
	}
	if start.SessionID == "" {
		return "", fmt.Errorf("dropbox upload session start: empty session_id")
	}

	for offset := first; offset < size; {
		n := int64(uploadChunkSize)
		if remaining := size - offset; n > remaining {
			n = remaining
		}
		if _, err := io.ReadFull(src, buf[:n]); err != nil {
			return "", fmt.Errorf("dropbox upload session: read chunk at %d: %w", offset, err)
		}
		cursor := map[string]any{"session_id": start.SessionID, "offset": offset}
		if offset+n == size {
			finish := map[string]any{"cursor": cursor, "commit": commit}
			if err := c.contentUpload(ctx, "/2/files/upload_session/finish", finish, bytes.NewReader(buf[:n]), &metadata); err != nil {
				return "", fmt.Errorf("dropbox upload session finish: %w", err)
			}
		} else if err := c.contentUpload(ctx, "/2/files/upload_session/append_v2", map[string]any{"cursor": cursor, "close": false}, bytes.NewReader(buf[:n]), nil); err != nil {
			return "", fmt.Errorf("dropbox upload session append: %w", err)
		}
		offset += n
	}
	return metadata.Rev, nil
}

// CreateFolder creates path in the app folder.
func (c *Client) CreateFolder(ctx context.Context, path string) error {
	return c.rpcCall(ctx, hostRPC, "/2/files/create_folder_v2", map[string]any{"path": path, "autorename": false}, nil)
}

// DeletePath removes path from the app folder.
func (c *Client) DeletePath(ctx context.Context, path string) error {
	return c.rpcCall(ctx, hostRPC, "/2/files/delete_v2", map[string]string{"path": path}, nil)
}

// Move relocates from to in the app folder without creating a conflict copy.
func (c *Client) Move(ctx context.Context, from, to string) error {
	return c.rpcCall(ctx, hostRPC, "/2/files/move_v2", map[string]any{"from_path": from, "to_path": to, "autorename": false}, nil)
}

// ---------------------------------------------------------------------------
// list_folder / continue (§2)
// ---------------------------------------------------------------------------

// dbxEntry is the raw shape of one list_folder entry; normalized into a
// DeltaEntry.
type dbxEntry struct {
	Tag         string `json:".tag"`
	Name        string `json:"name"`
	PathLower   string `json:"path_lower"`
	PathDisplay string `json:"path_display"`
	ID          string `json:"id"`
	Rev         string `json:"rev"`
	Size        uint64 `json:"size"`
	ContentHash string `json:"content_hash"`
}

type listResp struct {
	Entries []dbxEntry `json:"entries"`
	Cursor  string     `json:"cursor"`
	HasMore bool       `json:"has_more"`
}

func toDelta(e dbxEntry) DeltaEntry {
	return DeltaEntry{
		Tag:         e.Tag,
		Name:        e.Name,
		PathDisplay: e.PathDisplay,
		PathLower:   e.PathLower,
		ID:          e.ID,
		Rev:         e.Rev,
		Size:        e.Size,
		ContentHash: e.ContentHash,
	}
}

func toListResult(r listResp) ListResult {
	out := ListResult{Cursor: r.Cursor, HasMore: r.HasMore}
	out.Entries = make([]DeltaEntry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out.Entries = append(out.Entries, toDelta(e))
	}
	return out
}

// ListFolder begins an enumeration of the app folder rooted at AppFolderRoot
// (recursive). It returns the first page of entries plus the cursor; callers
// loop ListFolderContinue while HasMore. First-boot enumeration (§2).
func (c *Client) ListFolder(ctx context.Context) (ListResult, error) {
	reqBody := map[string]any{
		"path":      c.cfg.AppFolderRoot, // "" == app-folder root
		"recursive": true,
	}
	var r listResp
	if err := c.rpcCall(ctx, hostRPC, "/2/files/list_folder", reqBody, &r); err != nil {
		return ListResult{}, err
	}
	return toListResult(r), nil
}

// ListFolderContinue drains the next page for cursor (§2: loop while HasMore,
// persisting the returned cursor per page).
func (c *Client) ListFolderContinue(ctx context.Context, cursor string) (ListResult, error) {
	reqBody := map[string]any{"cursor": cursor}
	var r listResp
	if err := c.rpcCall(ctx, hostRPC, "/2/files/list_folder/continue", reqBody, &r); err != nil {
		return ListResult{}, err
	}
	return toListResult(r), nil
}

// ---------------------------------------------------------------------------
// list_folder/longpoll — NO Authorization header, dedicated long-timeout client
// ---------------------------------------------------------------------------

// LongpollResult is the longpoll outcome: Changes signals continue is worth
// calling; Backoff (seconds, may be 0) asks the caller to wait before the next
// longpoll (Dropbox returns it under load).
type LongpollResult struct {
	Changes bool
	Backoff int
}

// Longpoll parks on cursor until Dropbox reports changes or the timeout
// elapses. CRITICAL (§2): this call carries NO Authorization header — the cursor
// is the capability — and is issued to notify.dropboxapi.com via the dedicated
// long-timeout client (>=600s) so the parked read survives the timeout +
// ~90s jitter. The bearer is deliberately never attached here.
func (c *Client) Longpoll(ctx context.Context, cursor string) (LongpollResult, error) {
	timeout := c.cfg.LongpollTimeoutSeconds
	if timeout <= 0 {
		timeout = defaultLongpollTimeoutSeconds
	}
	if timeout > maxLongpollTimeoutSeconds {
		timeout = maxLongpollTimeoutSeconds
	}
	if timeout < minLongpollTimeoutSeconds {
		timeout = minLongpollTimeoutSeconds
	}
	reqBody := map[string]any{"cursor": cursor, "timeout": timeout}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return LongpollResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hostNotifyVar+"/2/files/list_folder/longpoll", bytes.NewReader(payload))
	if err != nil {
		return LongpollResult{}, err
	}
	// NO Authorization header here — by design (§2). Use the long-timeout client.
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.longpoll.Do(req)
	if err != nil {
		return LongpollResult{}, fmt.Errorf("dropbox longpoll: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return LongpollResult{}, c.statusError(resp.StatusCode, body)
	}
	var lr struct {
		Changes bool `json:"changes"`
		Backoff int  `json:"backoff"`
	}
	if err := json.Unmarshal(body, &lr); err != nil {
		return LongpollResult{}, fmt.Errorf("dropbox longpoll: decode: %w", err)
	}
	return LongpollResult{Changes: lr.Changes, Backoff: lr.Backoff}, nil
}

// ---------------------------------------------------------------------------
// download — content host, bearer, Dropbox-API-Arg/Result headers, hash verify
// ---------------------------------------------------------------------------

// Download fetches the file at path (optionally pinned to a specific rev) from
// content.dropboxapi.com, returning the bytes and the parsed file metadata
// (which carries Dropbox's content_hash and rev). The bytes are verified against
// the reported content_hash before returning: a mismatch yields
// ErrContentHashMismatch (retryable, §2). On a 401 it forces one token refresh
// and retries once.
func (c *Client) Download(ctx context.Context, path, rev string) ([]byte, FileMeta, error) {
	// Pin to a rev when supplied via the "rev:" path syntax so the bytes match
	// the metadata the delta referenced.
	target := path
	if rev != "" {
		target = "rev:" + rev
	}
	arg := map[string]string{"path": target}
	argJSON, err := json.Marshal(arg)
	if err != nil {
		return nil, FileMeta{}, err
	}

	do := func() (*http.Response, error) {
		tok, terr := c.token.token(ctx, false)
		if terr != nil {
			return nil, terr
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, hostContent+"/2/files/download", nil)
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		// Dropbox-API-Arg must be HTTP-header-safe ASCII; escape non-ASCII.
		req.Header.Set("Dropbox-API-Arg", httpHeaderSafeJSON(argJSON))
		return c.rpc.Do(req)
	}

	resp, err := do()
	if err != nil {
		return nil, FileMeta{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		c.token.invalidate()
		if _, terr := c.token.token(ctx, true); terr != nil {
			return nil, FileMeta{}, terr
		}
		resp, err = do()
		if err != nil {
			return nil, FileMeta{}, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, FileMeta{}, c.statusError(resp.StatusCode, body)
	}

	var meta dbxEntry
	if result := resp.Header.Get("Dropbox-API-Result"); result != "" {
		if err := json.Unmarshal([]byte(result), &meta); err != nil {
			return nil, FileMeta{}, fmt.Errorf("dropbox download: decode result header: %w", err)
		}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, FileMeta{}, fmt.Errorf("dropbox download: read body: %w", err)
	}

	fm := FileMeta{
		Name:        meta.Name,
		PathDisplay: meta.PathDisplay,
		PathLower:   meta.PathLower,
		ID:          meta.ID,
		Rev:         meta.Rev,
		Size:        meta.Size,
		ContentHash: meta.ContentHash,
	}

	// Verify integrity before handing the bytes to the engine (§2): a truncated
	// or corrupt download is a retryable error, not a stored decoration.
	if fm.ContentHash != "" {
		if got := ContentHash(data); got != fm.ContentHash {
			return nil, fm, fmt.Errorf("%w: path %q: want %s got %s", ErrContentHashMismatch, path, fm.ContentHash, got)
		}
	}
	return data, fm, nil
}

// httpHeaderSafeJSON escapes any non-ASCII byte in a JSON string as a \uXXXX
// escape so the value is safe to place in an HTTP header (Dropbox-API-Arg).
// Dropbox documents this requirement for the API-Arg header.
func httpHeaderSafeJSON(b []byte) string {
	var sb strings.Builder
	for _, r := range string(b) {
		if r < 0x20 || r > 0x7e {
			fmt.Fprintf(&sb, "\\u%04x", r)
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// content_hash (Dropbox block-SHA256) — exported for Phase 3/4 verification
// ---------------------------------------------------------------------------

// ContentHash computes Dropbox's content_hash for data: split into 4 MiB
// blocks, SHA-256 each block, concatenate the raw digests, SHA-256 that
// concatenation, and hex-encode the result (verified against live Dropbox docs
// and the official content-hasher reference). An empty input hashes the empty
// concatenation. Exposed so Phase 3/4 can verify a downloaded file against the
// metadata content_hash before the atomic rename; a mismatch is retryable.
func ContentHash(data []byte) string {
	overall := sha256.New()
	for off := 0; off < len(data); off += contentHashBlockSize {
		end := off + contentHashBlockSize
		if end > len(data) {
			end = len(data)
		}
		block := sha256.Sum256(data[off:end])
		overall.Write(block[:])
	}
	return hex.EncodeToString(overall.Sum(nil))
}

// VerifyContentHash reports whether data matches the Dropbox content_hash want,
// returning ErrContentHashMismatch on a mismatch (retryable, §2). A convenience
// wrapper Phase 3/4 can call before the atomic rename.
func VerifyContentHash(data []byte, want string) error {
	if got := ContentHash(data); got != want {
		return fmt.Errorf("%w: want %s got %s", ErrContentHashMismatch, want, got)
	}
	return nil
}
