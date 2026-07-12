//go:build live

package gmail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"testing"
	"time"
)

// R-3NGL-AMPW
func TestLiveAttachmentRoundTrip(t *testing.T) {
	cfg := Config{
		ClientID:     requiredLiveCredential(t, "GMAIL_CLIENT_ID"),
		ClientSecret: requiredLiveCredential(t, "GMAIL_CLIENT_SECRET"),
		RefreshToken: requiredLiveCredential(t, "GMAIL_REFRESH_TOKEN"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	client := NewClient(cfg, nil)
	profile, err := client.GetProfile(ctx)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}

	attachment := make([]byte, 1024)
	if _, err := rand.Read(attachment); err != nil {
		t.Fatalf("generate attachment: %v", err)
	}
	unique := make([]byte, 8)
	if _, err := rand.Read(unique); err != nil {
		t.Fatalf("generate subject suffix: %v", err)
	}
	raw, err := liveMultipartMessage(profile.EmailAddress, "live-attachment-"+hex.EncodeToString(unique), attachment)
	if err != nil {
		t.Fatalf("build message: %v", err)
	}

	sent, err := client.MessagesSend(ctx, raw)
	if err != nil {
		t.Fatalf("send fixture: %v", err)
	}
	deleted := false
	cleanup := func() {
		if deleted {
			return
		}
		if err := deleteLiveFixture(client, sent.ID); err != nil {
			t.Errorf("delete fixture %q: %v", sent.ID, err)
			return
		}
		deleted = true
	}
	defer cleanup()

	message, partID := waitForLiveAttachment(t, ctx, client, sent.ID)
	if partID == "" {
		t.Fatal("sent fixture did not expose an attachment part")
	}

	query := url.Values{
		"message_id": []string{message.ID},
		"part_id":    []string{partID},
	}.Encode()
	recorder := httptest.NewRecorder()
	AttachmentHandler(client).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/attachment?"+query, nil))
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("attachment status = %d, want %d; body: %s", response.StatusCode, http.StatusOK, recorder.Body.String())
	}
	if !bytes.Equal(recorder.Body.Bytes(), attachment) {
		t.Fatalf("attachment body differed from the %d bytes sent", len(attachment))
	}

	cleanup()
	if _, err := client.MessageGet(ctx, sent.ID, "full"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("message get after delete = %v, want ErrNotFound", err)
	}
}

func deleteLiveFixture(client *Client, messageID string) error {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for {
		err := client.MessageDelete(context.Background(), messageID)
		if err == nil || errors.Is(err, ErrNotFound) {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(time.Second)
	}
}

func requiredLiveCredential(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("live test requires %s", name)
	}
	return value
}

func liveMultipartMessage(to, subject string, attachment []byte) (string, error) {
	var message bytes.Buffer
	writer := multipart.NewWriter(&message)
	fmt.Fprintf(&message, "To: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", to, subject, writer.Boundary())

	textHeader := textproto.MIMEHeader{}
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	text, err := writer.CreatePart(textHeader)
	if err != nil {
		return "", err
	}
	if _, err := text.Write([]byte("live attachment round-trip fixture\r\n")); err != nil {
		return "", err
	}

	attachmentHeader := textproto.MIMEHeader{}
	attachmentHeader.Set("Content-Type", "application/octet-stream")
	attachmentHeader.Set("Content-Transfer-Encoding", "base64")
	attachmentHeader.Set("Content-Disposition", `attachment; filename="round-trip.bin"`)
	part, err := writer.CreatePart(attachmentHeader)
	if err != nil {
		return "", err
	}
	if _, err := part.Write([]byte(base64.StdEncoding.EncodeToString(attachment))); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(message.Bytes()), nil
}

func waitForLiveAttachment(t *testing.T, ctx context.Context, client *Client, messageID string) (Message, string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		message, err := client.MessageGet(ctx, messageID, "full")
		if err == nil {
			if partID := liveAttachmentPartID(message.Payload); partID != "" {
				return message, partID
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("attachment did not become visible before deadline: %v", err)
			}
			t.Fatal("attachment did not become visible before deadline")
		}
		select {
		case <-ctx.Done():
			t.Fatalf("attachment polling context ended: %v", ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

func liveAttachmentPartID(part MessagePart) string {
	if part.Filename == "round-trip.bin" && part.Body.AttachmentID != "" {
		return part.PartID
	}
	for _, child := range part.Parts {
		if partID := liveAttachmentPartID(child); partID != "" {
			return partID
		}
	}
	return ""
}
