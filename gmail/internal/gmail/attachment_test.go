package gmail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

type fakeAttachmentSource struct {
	message         Message
	messageErr      error
	attachment      []byte
	attachmentErr   error
	messageCalls    int
	attachmentCalls int
}

func (f *fakeAttachmentSource) MessageGet(_ context.Context, _, _ string) (Message, error) {
	f.messageCalls++
	return f.message, f.messageErr
}

func (f *fakeAttachmentSource) AttachmentGet(_ context.Context, _, _ string) ([]byte, error) {
	f.attachmentCalls++
	return f.attachment, f.attachmentErr
}

func attachmentRequest(messageID, attachmentID string) *http.Request {
	q := url.Values{"message_id": {messageID}, "attachment_id": {attachmentID}}
	return httptest.NewRequest(http.MethodGet, "/attachment?"+q.Encode(), nil)
}

func TestAttachmentHandlerIdentityGuard(t *testing.T) {
	// R-WX7D-ZS9N
	src := &fakeAttachmentSource{
		message:    Message{Payload: MessagePart{MimeType: "application/pdf", Body: Body{AttachmentID: "a"}}},
		attachment: []byte("pdf"),
	}
	h := AttachmentHandler(src)
	for header, value := range map[string]string{"X-Owner-Email": "a@b.c", "X-Forwarded-Proto": "https"} {
		r := attachmentRequest("m", "a")
		r.Header.Set(header, value)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s guard status = %d, want 404", header, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, attachmentRequest("m", "a"))
	if rec.Code != http.StatusOK || rec.Body.String() != "pdf" {
		t.Fatalf("bare loopback response = %d %q, want 200 pdf", rec.Code, rec.Body.String())
	}
}

func TestAttachmentHandlerServesMatchedPart(t *testing.T) {
	// R-WYFA-DK0C
	bytes := []byte("%PDF-1.7\n")
	src := &fakeAttachmentSource{
		message:    Message{Payload: MessagePart{Parts: []MessagePart{{Parts: []MessagePart{{MimeType: "application/pdf", Body: Body{AttachmentID: "a"}}}}}}},
		attachment: bytes,
	}
	rec := httptest.NewRecorder()
	AttachmentHandler(src).ServeHTTP(rec, attachmentRequest("m", "a"))
	if rec.Code != http.StatusOK || rec.Body.String() != string(bytes) {
		t.Fatalf("response = %d %q, want 200 attachment bytes", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(bytes)) {
		t.Fatalf("Content-Length = %q", got)
	}
}

func TestAttachmentHandlerMissingParametersDoNotCallSource(t *testing.T) {
	// R-WZN6-RBR1
	for _, target := range []string{"/attachment", "/attachment?attachment_id=a", "/attachment?message_id=m", "/attachment?message_id=&attachment_id=a", "/attachment?message_id=m&attachment_id="} {
		src := &fakeAttachmentSource{}
		rec := httptest.NewRecorder()
		AttachmentHandler(src).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusNotFound || src.messageCalls != 0 || src.attachmentCalls != 0 {
			t.Fatalf("%s = status %d, calls %d/%d; want 404 and no calls", target, rec.Code, src.messageCalls, src.attachmentCalls)
		}
	}
}

func TestAttachmentHandlerMapsAbsenceAndUpstreamFailures(t *testing.T) {
	// R-X0V3-53HQ
	for name, src := range map[string]*fakeAttachmentSource{
		"message missing": {messageErr: ErrNotFound},
		"part missing":    {message: Message{}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			AttachmentHandler(src).ServeHTTP(rec, attachmentRequest("m", "a"))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
		})
	}
	rec := httptest.NewRecorder()
	AttachmentHandler(&fakeAttachmentSource{messageErr: errors.New("upstream 503")}).ServeHTTP(rec, attachmentRequest("m", "a"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("upstream failure status = %d, want 502", rec.Code)
	}
	rec = httptest.NewRecorder()
	AttachmentHandler(&fakeAttachmentSource{
		message:       Message{Payload: MessagePart{Body: Body{AttachmentID: "a"}}},
		attachmentErr: errors.New("upstream 503"),
	}).ServeHTTP(rec, attachmentRequest("m", "a"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("attachment upstream failure status = %d, want 502", rec.Code)
	}
}
