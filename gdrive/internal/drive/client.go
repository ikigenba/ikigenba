package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

const fileFields = "id,name,mimeType,size,modifiedTime,parents,driveId,webViewLink"

// Client reduces Drive REST resources and streams file bytes.
type Client struct{ cfg Config }

// NewClient constructs a client using the configured transport.
func NewClient(cfg Config) *Client {
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return &Client{cfg}
}
func (c *Client) request(ctx context.Context, token, method, path, endpoint string, q url.Values, body io.Reader, headers http.Header) (*http.Response, error) {
	address := path
	if endpoint != EndpointChunk {
		q.Set("supportsAllDrives", "true")
		address = strings.TrimRight(c.cfg.BaseURL, "/") + path + "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, errors.Join(ErrProvider, ctx.Err())
	}
	if headers != nil {
		req.Header = headers.Clone()
	}
	if endpoint != EndpointChunk {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	var start time.Time
	if c.cfg.Telemetry != nil {
		start = c.cfg.Telemetry.Now()
	}
	resp, err := c.cfg.HTTP.Do(req)
	if c.cfg.Telemetry != nil {
		duration := c.cfg.Telemetry.Now().Sub(start) / time.Microsecond
		if duration < 0 {
			duration = 0
		}
		status := int64(0)
		if resp != nil {
			status = int64(resp.StatusCode)
		}
		c.cfg.Telemetry.Emit(ctx, Called, telemetry.Attrs{"endpoint": endpoint, "status": status, "duration_us": int64(duration)})
	}
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, errors.Join(ErrProvider, ctx.Err())
	}
	return resp, nil
}
func statusError(status int) error {
	switch status {
	case 404:
		return ErrNotFound
	case 403:
		return ErrForbidden
	}
	return ErrProvider
}
func object(r io.Reader, dst any) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return ErrProvider
	}
	if len(strings.TrimSpace(string(b))) == 0 || strings.TrimSpace(string(b))[0] != '{' {
		return ErrProvider
	}
	if json.Unmarshal(b, dst) != nil {
		return ErrProvider
	}
	return nil
}
func (c *Client) root(ctx context.Context, token string) (string, error) {
	resp, err := c.request(ctx, token, "GET", "/drive/v3/files/root", EndpointGet, url.Values{"fields": {"id"}}, nil, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var root struct {
		ID string `json:"id"`
	}
	if resp.StatusCode/100 != 2 || object(resp.Body, &root) != nil || root.ID == "" {
		return "", ErrProvider
	}
	return root.ID, nil
}

type resource struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	MimeType string          `json:"mimeType"`
	Size     *string         `json:"size"`
	Modified string          `json:"modifiedTime"`
	Parents  []string        `json:"parents"`
	Drive    string          `json:"driveId"`
	Link     string          `json:"webViewLink"`
	Trashed  bool            `json:"trashed"`
	Shortcut json.RawMessage `json:"shortcutDetails"`
}

func (r *resource) UnmarshalJSON(data []byte) error {
	type wire resource
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return ErrProvider
	}
	var decoded wire
	if json.Unmarshal(data, &decoded) != nil {
		return ErrProvider
	}
	if value, ok := raw["size"]; ok {
		var size string
		if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &size) != nil {
			return ErrProvider
		}
		if _, err := strconv.ParseInt(size, 10, 64); err != nil {
			return ErrProvider
		}
	}
	if value, ok := raw["modifiedTime"]; ok {
		var modified string
		if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &modified) != nil {
			return ErrProvider
		}
		if _, err := time.Parse(time.RFC3339, modified); err != nil {
			return ErrProvider
		}
	}
	*r = resource(decoded)
	return nil
}

func reduce(r resource, root string) (File, error) {
	f := File{ID: r.ID, Name: r.Name, Kind: KindOf(r.MimeType), MimeType: r.MimeType, Drive: r.Drive, Link: r.Link}
	if r.Size != nil {
		n, err := strconv.ParseInt(*r.Size, 10, 64)
		if err != nil {
			return File{}, ErrProvider
		}
		if f.Kind == KindFile {
			f.Bytes = &n
		}
	}
	if r.Modified != "" {
		t, err := time.Parse(time.RFC3339, r.Modified)
		if err != nil {
			return File{}, ErrProvider
		}
		f.Modified = t.UTC()
	}
	if len(r.Parents) > 0 && r.Parents[0] != root && r.Parents[0] != r.Drive {
		f.Folder = r.Parents[0]
	}
	return f, nil
}
func quote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
}

// List returns one folder page in provider order.
func (c *Client) List(ctx context.Context, token, folder, page string, size int) (Page, error) {
	root, err := c.root(ctx, token)
	if err != nil {
		return Page{}, err
	}
	if folder == "" {
		folder = "root"
	}
	q := url.Values{"q": {quote(folder) + " in parents and trashed = false"}, "orderBy": {"folder,name"}, "corpora": {"allDrives"}}
	return c.files(ctx, token, root, page, size, q)
}

// Search returns one page matching the requested scope.
func (c *Client) Search(ctx context.Context, token string, query Query) (Page, error) {
	root, err := c.root(ctx, token)
	if err != nil {
		return Page{}, err
	}
	text := "trashed = false"
	if query.Text != "" {
		text = "fullText contains " + quote(query.Text) + " and " + text
	}
	if query.Folder != "" {
		text += " and " + quote(query.Folder) + " in parents"
	}
	q := url.Values{"q": {text}, "corpora": {"allDrives"}}
	if query.Drive != "" {
		q.Set("corpora", "drive")
		q.Set("driveId", query.Drive)
	}
	return c.files(ctx, token, root, query.Page, query.Size, q)
}
func (c *Client) files(ctx context.Context, token, root, page string, size int, q url.Values) (Page, error) {
	q.Set("fields", "nextPageToken,files("+fileFields+")")
	q.Set("pageSize", strconv.Itoa(size))
	q.Set("includeItemsFromAllDrives", "true")
	if page != "" {
		q.Set("pageToken", page)
	}
	resp, err := c.request(ctx, token, "GET", "/drive/v3/files", EndpointList, q, nil, nil)
	if err != nil {
		return Page{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return Page{}, statusError(resp.StatusCode)
	}
	var raw struct {
		Files []resource `json:"files"`
		Next  string     `json:"nextPageToken"`
	}
	if object(resp.Body, &raw) != nil || raw.Files == nil {
		return Page{}, ErrProvider
	}
	p := Page{Files: make([]File, 0, len(raw.Files)), Next: raw.Next}
	for _, r := range raw.Files {
		f, e := reduce(r, root)
		if e != nil {
			return Page{}, e
		}
		p.Files = append(p.Files, f)
	}
	return p, nil
}

// Show returns metadata and a shortcut target when available.
func (c *Client) Show(ctx context.Context, token, id string) (File, error) {
	if id == "" {
		return File{}, ErrNotFound
	}
	root, err := c.root(ctx, token)
	if err != nil {
		return File{}, err
	}
	resp, err := c.request(ctx, token, "GET", "/drive/v3/files/"+url.PathEscape(id), EndpointGet, url.Values{"fields": {fileFields + ",trashed,shortcutDetails"}}, nil, nil)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return File{}, statusError(resp.StatusCode)
	}
	var r resource
	if object(resp.Body, &r) != nil {
		return File{}, ErrProvider
	}
	if r.Trashed {
		return File{}, ErrNotFound
	}
	f, err := reduce(r, root)
	if err != nil {
		return File{}, err
	}
	if f.Kind == KindShortcut {
		var t struct {
			ID   string `json:"targetId"`
			Mime string `json:"targetMimeType"`
		}
		if json.Unmarshal(r.Shortcut, &t) == nil && t.ID != "" {
			f.Target = &Target{ID: t.ID, Kind: KindOf(t.Mime)}
		}
	}
	return f, nil
}

// Drives follows shared-drive pages.
func (c *Client) Drives(ctx context.Context, token string) ([]Drive, error) {
	out := []Drive{}
	page := ""
	for {
		q := url.Values{"pageSize": {"100"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		resp, err := c.request(ctx, token, "GET", "/drive/v3/drives", EndpointDrives, q, nil, nil)
		if err != nil {
			return nil, err
		}
		var r struct {
			Drives json.RawMessage `json:"drives"`
			Next   string          `json:"nextPageToken"`
		}
		e := object(resp.Body, &r)
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, statusError(resp.StatusCode)
		}
		if e != nil {
			return nil, e
		}
		var drives []json.RawMessage
		if r.Drives != nil {
			data := strings.TrimSpace(string(r.Drives))
			if len(data) == 0 || data[0] != '[' || json.Unmarshal(r.Drives, &drives) != nil {
				return nil, ErrProvider
			}
		}
		for _, raw := range drives {
			var d Drive
			if object(strings.NewReader(string(raw)), &d) != nil {
				return nil, ErrProvider
			}
			out = append(out, d)
		}
		page = r.Next
		if page == "" {
			return out, nil
		}
	}
}

// Media opens the byte stream of a stored file.
func (c *Client) Media(ctx context.Context, token, id string) (io.ReadCloser, error) {
	return c.stream(ctx, token, id, "", false)
}

// Export opens an exported native-file stream.
func (c *Client) Export(ctx context.Context, token, id, mime string) (io.ReadCloser, error) {
	return c.stream(ctx, token, id, mime, true)
}
func (c *Client) stream(ctx context.Context, token, id, mime string, export bool) (io.ReadCloser, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	path := "/drive/v3/files/" + url.PathEscape(id)
	endpoint := EndpointMedia
	q := url.Values{"alt": {"media"}}
	if export {
		path += "/export"
		endpoint = EndpointExport
		q = url.Values{"mimeType": {mime}}
	}
	resp, err := c.request(ctx, token, "GET", path, endpoint, q, nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer func() { _ = resp.Body.Close() }()
		if export && resp.StatusCode == 403 {
			var r struct {
				Error struct {
					Errors []struct {
						Reason string `json:"reason"`
					} `json:"errors"`
				} `json:"error"`
			}
			if object(resp.Body, &r) == nil {
				for _, e := range r.Error.Errors {
					if e.Reason == "exportSizeLimitExceeded" {
						return nil, ErrTooLarge
					}
				}
			}
		}
		return nil, statusError(resp.StatusCode)
	}
	return &stream{ReadCloser: resp.Body, ctx: ctx, export: export}, nil
}

type stream struct {
	io.ReadCloser
	ctx    context.Context
	export bool
	read   int64
}

func (s *stream) Read(p []byte) (int, error) {
	n, e := s.ReadCloser.Read(p)
	before := s.read
	s.read += int64(n)
	if e != nil && !errors.Is(e, io.EOF) {
		switch {
		case s.ctx.Err() != nil:
			e = errors.Join(ErrProvider, s.ctx.Err())
		case s.export && before >= ExportCap:
			e = ErrTooLarge
		default:
			e = ErrProvider
		}
	}
	return n, e
}
