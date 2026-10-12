package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
)

func composition() gmail.Composition {
	return gmail.Composition{From: mail.Address{Address: "sender@synthetic.invalid"}, To: []mail.Address{{Address: "recipient@synthetic.invalid"}}, Subject: "Synthetic subject", Text: "Synthetic body"}
}

const draftResponse = `{"id":"draft-one","message":{"id":"message-new","threadId":"thread-new"}}`

func TestCreateDraft(t *testing.T) {
	// R-HL63-QVDQ R-HME0-4N4F
	for _, thread := range []string{"", "thread-original"} {
		for _, status := range []int{200, 201} {
			f, c := newFake(t, reply{status: status, body: draftResponse})
			d, err := c.CreateDraft(context.Background(), syntheticToken, thread, gmail.Build(composition()))
			if err != nil || !reflect.DeepEqual(d, gmail.Draft{"draft-one", "message-new", "thread-new"}) {
				t.Fatal(d, err)
			}
			call := f.calls()[0]
			assertCall(t, call, "POST", "/gmail/v1/users/me/drafts", nil)
			if call.headers.Get("Content-Type") != "application/json" {
				t.Fatal(call.headers)
			}
			var data map[string]map[string]string
			if json.Unmarshal([]byte(call.body), &data) != nil || len(data) != 1 {
				t.Fatal(call.body)
			}
			m := data["message"]
			want, err := io.ReadAll(gmail.Build(composition()))
			if err != nil {
				t.Fatal(err)
			}
			if m["raw"] != base64.URLEncoding.EncodeToString(want) || m["threadId"] != thread || len(m) != 1+boolInt(thread != "") {
				t.Fatal(m)
			}
		}
	}
	for _, body := range []string{`{}`, `{"id":"draft"}`, `{"id":"draft","message":{}}`, `{"id":"","message":{"id":"message"}}`, `[]`, `{"id":"draft","message":null}`} {
		_, c := newFake(t, okJSON(body))
		d, err := c.CreateDraft(context.Background(), syntheticToken, "", gmail.Build(composition()))
		requireError(t, err, gmail.ErrProvider)
		if !reflect.DeepEqual(d, gmail.Draft{}) {
			t.Fatal(d)
		}
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestDraftOperations(t *testing.T) {
	// R-HTPE-F9KL R-HUXA-T1BA R-HW57-6T1Z R-HME0-4N4F
	f, c := newFake(t, okJSON(draftResponse), okJSON(`{"id":"sent-message","threadId":"sent-thread"}`), reply{status: 204})
	d, err := c.GetDraft(context.Background(), syntheticToken, "draft-one")
	if err != nil || !reflect.DeepEqual(d, gmail.Draft{"draft-one", "message-new", "thread-new"}) {
		t.Fatal(d, err)
	}
	d, err = c.SendDraft(context.Background(), syntheticToken, "draft-one")
	if err != nil || !reflect.DeepEqual(d, gmail.Draft{"draft-one", "sent-message", "sent-thread"}) {
		t.Fatal(d, err)
	}
	if err = c.DeleteDraft(context.Background(), syntheticToken, "draft-one"); err != nil {
		t.Fatal(err)
	}
	cs := f.calls()
	assertCall(t, cs[0], "GET", "/gmail/v1/users/me/drafts/draft-one", map[string]string{"format": "minimal"})
	assertCall(t, cs[1], "POST", "/gmail/v1/users/me/drafts/send", nil)
	assertCall(t, cs[2], "DELETE", "/gmail/v1/users/me/drafts/draft-one", nil)
	var body map[string]string
	if json.Unmarshal([]byte(cs[1].body), &body) != nil || !reflect.DeepEqual(body, map[string]string{"id": "draft-one"}) || cs[1].headers.Get("Content-Type") != "application/json" {
		t.Fatal(cs[1])
	}
	for _, body := range []string{`{}`, `{"id":""}`, `{"id":7}`, `null`, `[]`} {
		_, c := newFake(t, okJSON(body))
		d, err := c.SendDraft(context.Background(), syntheticToken, "draft-one")
		requireError(t, err, gmail.ErrProvider)
		if !reflect.DeepEqual(d, gmail.Draft{}) {
			t.Fatal(d)
		}
	}
}

func largeComposition(n int64) gmail.Composition {
	c := composition()
	c.Attachments = []gmail.Part{{Name: "synthetic.bin", ContentType: "application/octet-stream", Bytes: n, Open: func() (io.ReadCloser, error) { return io.NopCloser(io.LimitReader(repeatReader{}, n)), nil }}}
	return c
}

type repeatReader struct{}

func (repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'z'
	}
	return len(p), nil
}

func TestUploadCap(t *testing.T) {
	// R-HL63-QVDQ R-HNLW-IEV4
	f, c := newFake(t)
	o := gmail.Build(largeComposition(gmail.UploadCap))
	if o.Size() <= gmail.UploadCap {
		t.Fatal(o.Size())
	}
	d, err := c.CreateDraft(context.Background(), syntheticToken, "", o)
	requireError(t, err, gmail.ErrTooLarge)
	if !reflect.DeepEqual(d, gmail.Draft{}) {
		t.Fatal(d)
	}
	d, err = c.UpdateDraft(context.Background(), syntheticToken, "draft", "", gmail.Build(largeComposition(gmail.UploadCap)))
	requireError(t, err, gmail.ErrTooLarge)
	if !reflect.DeepEqual(d, gmail.Draft{}) || len(f.calls()) != 0 {
		t.Fatal(d, f.calls())
	}
}

func TestUpdateDraft(t *testing.T) {
	// R-HNLW-IEV4 R-HOTS-W6LT R-HME0-4N4F R-GN0X-1AMA
	for _, thread := range []string{"", "original-thread"} {
		for _, large := range []bool{false, true} {
			comp := composition()
			if large {
				comp = largeComposition(7 << 20)
			}
			want, err := io.ReadAll(gmail.Build(comp))
			if err != nil {
				t.Fatal(err)
			}
			pieces := 0
			session, sender := newFake(t)
			session.replies = []reply{{status: 200, body: draftResponse}}
			if large {
				session.replies = []reply{{status: 308, headers: http.Header{"Range": {fmt.Sprintf("bytes=0-%d", gmail.PushChunk-1)}}}, {status: 201, body: draftResponse}}
			}
			opening, c := newFake(t, reply{headers: http.Header{"Location": {session.server.URL + "/session?secret=synthetic"}}})
			_ = sender
			d, err := c.UpdateDraft(context.Background(), syntheticToken, "draft/segment", thread, gmail.Build(comp))
			if err != nil || !reflect.DeepEqual(d, gmail.Draft{"draft-one", "message-new", "thread-new"}) {
				t.Fatal(d, err)
			}
			init := opening.calls()[0]
			assertCall(t, init, "PUT", "/upload/gmail/v1/users/me/drafts/draft%2Fsegment", map[string]string{"uploadType": "resumable"})
			if init.headers.Get("Content-Type") != "application/json; charset=UTF-8" || init.headers.Get("X-Upload-Content-Type") != "message/rfc822" || init.headers.Get("X-Upload-Content-Length") != strconv.Itoa(len(want)) {
				t.Fatal(init.headers)
			}
			var metadata map[string]any
			if json.Unmarshal([]byte(init.body), &metadata) != nil || metadata["id"] != "draft/segment" || len(metadata) != 1+boolInt(thread != "") {
				t.Fatal(init.body)
			}
			if thread != "" {
				if !reflect.DeepEqual(metadata["message"], map[string]any{"threadId": thread}) {
					t.Fatal(metadata)
				}
			}
			var assembled strings.Builder
			for i, call := range session.calls() {
				pieces++
				start := i * gmail.PushChunk
				end := min(start+gmail.PushChunk, len(want))
				if call.method != "PUT" || call.path != "/session" || call.query != "secret=synthetic" || call.headers.Get("Authorization") != "" || call.headers.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(want)) || call.body != string(want[start:end]) {
					t.Fatalf("piece %d differs", i)
				}
				assembled.WriteString(call.body)
			}
			if assembled.String() != string(want) || pieces != (len(want)+gmail.PushChunk-1)/gmail.PushChunk {
				t.Fatal("upload differs")
			}
		}
	}
}

func TestBadUploadOpening(t *testing.T) {
	// R-2ANM-YDMO
	for _, tc := range []struct {
		status   int
		location string
		want     error
	}{{200, "", gmail.ErrProvider}, {200, "/relative", gmail.ErrProvider}, {200, ":bad-url", gmail.ErrProvider}, {401, "", gmail.ErrUnauthorized}, {413, "", gmail.ErrTooLarge}, {404, "", gmail.ErrNotFound}, {400, "", gmail.ErrProvider}, {500, "", gmail.ErrProvider}} {
		f, c := newFake(t, reply{status: tc.status, headers: http.Header{"Location": {tc.location}}})
		d, err := c.UpdateDraft(context.Background(), syntheticToken, "draft", "", gmail.Build(composition()))
		requireError(t, err, tc.want)
		if len(f.calls()) != 1 || !reflect.DeepEqual(d, gmail.Draft{}) {
			t.Fatal(d, f.calls())
		}
	}
}

func TestBadUploadPieces(t *testing.T) {
	// R-HR9L-NQ37
	for _, large := range []bool{false, true} {
		for _, tc := range []struct {
			status      int
			rangeHeader string
			want        error
		}{{200, "", gmail.ErrProvider}, {308, "", gmail.ErrProvider}, {308, "bytes=0-1", gmail.ErrProvider}, {401, "", gmail.ErrUnauthorized}, {413, "", gmail.ErrTooLarge}, {404, "", gmail.ErrProvider}, {500, "", gmail.ErrProvider}} {
			if !large && tc.status == 200 {
				continue
			}
			session, _ := newFake(t, reply{status: tc.status, headers: http.Header{"Range": {tc.rangeHeader}}})
			_, c := newFake(t, reply{headers: http.Header{"Location": {session.server.URL + "/session"}}})
			comp := composition()
			if large {
				comp = largeComposition(7 << 20)
			}
			d, err := c.UpdateDraft(context.Background(), syntheticToken, "draft", "", gmail.Build(comp))
			requireError(t, err, tc.want)
			if len(session.calls()) != 1 || !reflect.DeepEqual(d, gmail.Draft{}) {
				t.Fatal(d, session.calls())
			}
		}
	}
}

func TestOutgoingFailureIsNotProvider(t *testing.T) {
	// R-F98V-108S
	partFailure := errors.New("synthetic part failure")
	for _, readFailure := range []bool{false, true} {
		comp := composition()
		comp.Attachments = []gmail.Part{{Name: "synthetic.bin", Bytes: 4, Open: func() (io.ReadCloser, error) {
			if readFailure {
				return io.NopCloser(failedReader{partFailure}), nil
			}
			return nil, partFailure
		}}}
		f, c := newFake(t)
		d, err := c.CreateDraft(context.Background(), syntheticToken, "", gmail.Build(comp))
		assertPartFailure(t, err, partFailure)
		if !reflect.DeepEqual(d, gmail.Draft{}) || len(f.calls()) != 0 {
			t.Fatal(d, f.calls())
		}
		session, _ := newFake(t)
		f, c = newFake(t, reply{headers: http.Header{"Location": {session.server.URL + "/session"}}})
		d, err = c.UpdateDraft(context.Background(), syntheticToken, "draft", "", gmail.Build(comp))
		assertPartFailure(t, err, partFailure)
		if !reflect.DeepEqual(d, gmail.Draft{}) || len(f.calls()) != 1 || len(session.calls()) != 0 {
			t.Fatal(d, f.calls(), session.calls())
		}
	}
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }
func assertPartFailure(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	for _, kind := range []error{gmail.ErrNotFound, gmail.ErrUnauthorized, gmail.ErrTooLarge, gmail.ErrBadPage, gmail.ErrProvider} {
		if errors.Is(err, kind) {
			t.Fatal(err, kind)
		}
	}
}
