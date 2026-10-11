package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type hiddenError struct{ cause error }

func (e hiddenError) Error() string { return "drive: upload body failed" }
func (e hiddenError) Unwrap() error { return e.cause }

// Upload streams a body into a resumable create or replacement session.
func (c *Client) Upload(ctx context.Context, token string, u Upload, body io.Reader) (File, error) {
	root, err := c.root(ctx, token)
	if err != nil {
		return File{}, err
	}
	path := "/upload/drive/v3/files"
	method := http.MethodPost
	endpoint := EndpointCreate
	meta := map[string]any{}
	if u.File == "" {
		folder := u.Folder
		if folder == "" {
			folder = root
		}
		meta["name"] = u.Name
		meta["parents"] = []string{folder}
	} else {
		path += "/" + url.PathEscape(u.File)
		method = http.MethodPatch
		endpoint = EndpointUpdate
		if u.Name != "" {
			meta["name"] = u.Name
		}
	}
	encoded, _ := json.Marshal(meta)
	headers := http.Header{"Content-Type": {"application/json; charset=UTF-8"}, "X-Upload-Content-Type": {u.ContentType}, "X-Upload-Content-Length": {strconv.FormatInt(u.Bytes, 10)}}
	resp, err := c.request(ctx, token, method, path, endpoint, url.Values{"uploadType": {"resumable"}, "fields": {fileFields}}, bytes.NewReader(encoded), headers)
	if err != nil {
		return File{}, err
	}
	location := resp.Header.Get("Location")
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return File{}, statusError(resp.StatusCode)
	}
	session, e := url.Parse(location)
	if e != nil || !session.IsAbs() || session.Host == "" {
		return File{}, ErrProvider
	}
	if u.Bytes < 0 {
		return File{}, ErrProvider
	}
	for offset := int64(0); ; {
		size := min(int64(PushChunk), u.Bytes-offset)
		piece := make([]byte, int(size))
		if size > 0 {
			e = fill(body, piece)
			if e != nil {
				if errors.Is(e, io.EOF) {
					e = io.ErrUnexpectedEOF
				}
				return File{}, hiddenError{e}
			}
		}
		rangeValue := "bytes */0"
		if size > 0 {
			rangeValue = fmt.Sprintf("bytes %d-%d/%d", offset, offset+size-1, u.Bytes)
		}
		resp, err = c.request(ctx, "", http.MethodPut, location, EndpointChunk, nil, bytes.NewReader(piece), http.Header{"Content-Range": {rangeValue}})
		if err != nil {
			return File{}, err
		}
		last := offset+size == u.Bytes
		if !last {
			_ = resp.Body.Close()
			if resp.StatusCode != 308 || resp.Header.Get("Range") != fmt.Sprintf("bytes=0-%d", offset+size-1) {
				if resp.StatusCode == 403 {
					return File{}, ErrForbidden
				}
				return File{}, ErrProvider
			}
			offset += size
			continue
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 && resp.StatusCode != 201 {
			if resp.StatusCode == 403 {
				return File{}, ErrForbidden
			}
			return File{}, ErrProvider
		}
		var r resource
		if object(resp.Body, &r) != nil {
			return File{}, ErrProvider
		}
		return reduce(r, root)
	}
}

// fill preserves a reader failure even when it accompanies the final bytes.
func fill(body io.Reader, piece []byte) error {
	offset, empty := 0, 0
	for offset < len(piece) {
		n, err := body.Read(piece[offset:])
		offset += n
		if err != nil {
			if errors.Is(err, io.EOF) {
				if offset == len(piece) {
					return nil
				}
				return io.ErrUnexpectedEOF
			}
			return err
		}
		if n == 0 {
			empty++
			if empty == 100 {
				return io.ErrNoProgress
			}
		} else {
			empty = 0
		}
	}
	return nil
}
