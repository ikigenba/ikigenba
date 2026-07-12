package gmail

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// AttachmentSource is the portion of the Gmail client needed to serve an
// attachment without coupling handler tests to HTTP or OAuth.
type AttachmentSource interface {
	MessageGet(ctx context.Context, id, format string) (Message, error)
	AttachmentGet(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

// AttachmentHandler returns the loopback-only GET /attachment handler.
func AttachmentHandler(src AttachmentSource) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Owner-Email") != "" || r.Header.Get("X-Forwarded-Proto") != "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		messageID := r.URL.Query().Get("message_id")
		attachmentID := r.URL.Query().Get("attachment_id")
		if messageID == "" || attachmentID == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		message, err := src.MessageGet(r.Context(), messageID, "full")
		if err != nil {
			attachmentError(w, err)
			return
		}
		mimeType, ok := attachmentMIMEType(message.Payload, attachmentID)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, err := src.AttachmentGet(r.Context(), messageID, attachmentID)
		if err != nil {
			attachmentError(w, err)
			return
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
}

func attachmentError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, "bad gateway", http.StatusBadGateway)
}

func attachmentMIMEType(part MessagePart, attachmentID string) (string, bool) {
	if part.Body.AttachmentID == attachmentID {
		return part.MimeType, true
	}
	for _, child := range part.Parts {
		if mimeType, ok := attachmentMIMEType(child, attachmentID); ok {
			return mimeType, true
		}
	}
	return "", false
}
