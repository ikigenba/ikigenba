package gmail_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
)

func attachmentJSON(size int, data, attachmentID *string) string {
	body := fmt.Sprintf(`"size":%d`, size)
	if data != nil {
		body += fmt.Sprintf(`,"data":%q`, *data)
	}
	if attachmentID != nil {
		body += fmt.Sprintf(`,"attachmentId":%q`, *attachmentID)
	}
	return fmt.Sprintf(`{"id":"message","payload":{"parts":[{"partId":"1","filename":"synthetic.bin","body":{%s}}]}}`, body)
}

func readContent(t *testing.T, c gmail.Content, want string) {
	t.Helper()
	b, err := io.ReadAll(c.Reader)
	if err != nil || string(b) != want || c.Bytes != int64(len(want)) {
		t.Fatalf("content %q %d %v, want %q", b, c.Bytes, err, want)
	}
	var one [1]byte
	n, err := c.Reader.Read(one[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("after content %d %v", n, err)
	}
}

func TestAttachment(t *testing.T) {
	// R-N914-JHAP R-NA90-X91E R-NBGX-B0S3
	aid := "current/attachment"
	for _, pad := range []bool{false, true} {
		enc := base64.RawURLEncoding
		if pad {
			enc = base64.URLEncoding
		}
		f, c := newFake(t, okJSON(attachmentJSON(4, nil, &aid)), okJSON(fmt.Sprintf(`{"size":4,"data":%q}`, enc.EncodeToString([]byte("data")))))
		body, err := c.Attachment(context.Background(), syntheticToken, "message", "1", 4)
		if err != nil {
			t.Fatal(err)
		}
		readContent(t, body, "data")
		cs := f.calls()
		assertCall(t, cs[0], "GET", "/gmail/v1/users/me/messages/message", map[string]string{"format": "full"})
		assertCall(t, cs[1], "GET", "/gmail/v1/users/me/messages/message/attachments/current%2Fattachment", nil)
	}
	data := base64.URLEncoding.EncodeToString([]byte("data"))
	for _, tc := range []struct {
		body  string
		limit int64
		want  error
		count int
		bytes string
	}{{attachmentJSON(5, nil, &aid), 4, gmail.ErrTooLarge, 1, ""}, {`{"id":"message","payload":{"parts":[{"partId":"1","body":{"size":3}}]}}`, 4, gmail.ErrNotFound, 1, ""}, {attachmentJSON(4, &data, nil), 4, nil, 1, "data"}, {attachmentJSON(0, nil, nil), 4, nil, 1, ""}, {attachmentJSON(1, nil, nil), 4, gmail.ErrProvider, 1, ""}, {attachmentJSON(1, &data, nil), 3, gmail.ErrTooLarge, 1, ""}, {attachmentJSON(1, nil, &aid), 3, gmail.ErrTooLarge, 2, ""}} {
		f, c := newFake(t, okJSON(tc.body), okJSON(fmt.Sprintf(`{"data":%q}`, data)))
		content, err := c.Attachment(context.Background(), syntheticToken, "message", "1", tc.limit)
		if tc.want == nil {
			if err != nil {
				t.Fatal(err)
			}
			readContent(t, content, tc.bytes)
		} else {
			requireError(t, err, tc.want)
			if !reflect.DeepEqual(content, gmail.Content{}) {
				t.Fatal(content)
			}
		}
		if len(f.calls()) != tc.count {
			t.Fatal(f.calls())
		}
	}
}

func TestRaw(t *testing.T) {
	// R-NCOT-OSIS
	for _, enc := range []*base64.Encoding{base64.URLEncoding, base64.RawURLEncoding} {
		for _, limit := range []int64{4, 3} {
			f, c := newFake(t, okJSON(fmt.Sprintf(`{"raw":%q}`, enc.EncodeToString([]byte("data")))))
			content, err := c.Raw(context.Background(), syntheticToken, "message", limit)
			assertCall(t, f.calls()[0], "GET", "/gmail/v1/users/me/messages/message", map[string]string{"format": "raw"})
			if limit == 3 {
				requireError(t, err, gmail.ErrTooLarge)
				if !reflect.DeepEqual(content, gmail.Content{}) {
					t.Fatal(content)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			readContent(t, content, "data")
		}
	}
}

type endlessBody struct {
	read   int64
	closed bool
}

func (b *endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	b.read += int64(len(p))
	return len(p), nil
}
func (b *endlessBody) Close() error { b.closed = true; return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEncodedResponseBound(t *testing.T) {
	// R-NDWQ-2K9H R-5TRX-70ZD
	for _, attachment := range []bool{false, true} {
		body := &endlessBody{}
		n := 0
		aid := "current"
		c := gmail.NewClient(gmail.Config{HTTP: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			n++
			var reader io.ReadCloser = body
			if attachment && n == 1 {
				reader = io.NopCloser(strings.NewReader(attachmentJSON(1, nil, &aid)))
			}
			return &http.Response{StatusCode: 200, Body: reader, Header: make(http.Header)}, nil
		})}})
		var value gmail.Content
		var err error
		const limit int64 = 7
		if attachment {
			value, err = c.Attachment(context.Background(), syntheticToken, "message", "1", limit)
		} else {
			value, err = c.Raw(context.Background(), syntheticToken, "message", limit)
		}
		requireError(t, err, gmail.ErrTooLarge)
		bound := 4*((limit+2)/3) + 65536
		if body.read != bound+1 || !body.closed || !reflect.DeepEqual(value, gmail.Content{}) {
			t.Fatal(body, value)
		}
	}
}
