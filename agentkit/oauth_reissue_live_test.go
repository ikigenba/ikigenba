//go:build live

package agentkit

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
)

// oauthRejectionObserver forwards the real exchange and records rejection of
// the altered bearer, so proactive refresh cannot satisfy a reactive test.
type oauthRejectionObserver struct {
	next       http.RoundTripper
	bearer     string
	classifier rejectedCredentialClassifier
	rejected   bool
}

func (o *oauthRejectionObserver) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := o.next.RoundTrip(request)
	if err != nil || request.Header.Get("Authorization") != "Bearer "+o.bearer || response.StatusCode < 400 {
		return response, err
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	o.rejected = o.classifier.isRejectedCredential(response.StatusCode, body)
	return response, nil
}

func observeOAuthRejection(t *testing.T, conversation *Conversation, rotator Rotator) *oauthRejectionObserver {
	t.Helper()
	token, err := rotator.Token(context.Background())
	if err != nil {
		t.Fatalf("read altered OAuth token: %v", err)
	}
	client := *conversation.client
	next := client.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	classifier, ok := conversation.provider.(rejectedCredentialClassifier)
	if !ok {
		t.Fatal("OAuth wire does not classify rejected credentials")
	}
	observer := &oauthRejectionObserver{next: next, bearer: token.Bearer, classifier: classifier}
	client.Transport = observer
	conversation.client = &client
	return observer
}
