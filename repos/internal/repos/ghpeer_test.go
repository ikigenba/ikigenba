package repos

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"eventplane/correlation"
)

type peerRecordingTransport struct {
	base http.RoundTripper

	mu       sync.Mutex
	requests []*http.Request
}

func (t *peerRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, request)
	t.mu.Unlock()
	return t.base.RoundTrip(request)
}

func (t *peerRecordingTransport) recordedRequests() []*http.Request {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*http.Request(nil), t.requests...)
}

func TestGitHubPeerIssueGetUsesInjectedClientWithCallContext(t *testing.T) {
	// R-BT9N-POGV
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/mcp" {
			t.Errorf("request = %s %s, want POST /mcp", request.Method, request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"number":17,"title":"fixture","body":"body"}}`)
	}))
	defer server.Close()

	transport := &peerRecordingTransport{base: http.DefaultTransport}
	client := &http.Client{Transport: transport}
	peer := NewGitHubPeerAt(server.URL, client)
	correlationID := "01K1C5Y7N8E9F0G1H2J3K4M5NP"
	ctx := correlation.WithContext(context.Background(), correlationID)

	issue, err := peer.IssueGet(ctx, Identity{OwnerID: "owner", OwnerEmail: "owner@example.com", SessionID: "session"}, "fixture", 17)
	if err != nil {
		t.Fatalf("IssueGet: %v", err)
	}
	if issue.Number != 17 || issue.Title != "fixture" {
		t.Fatalf("issue = %#v, want decoded fixture issue", issue)
	}
	requests := transport.recordedRequests()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(requests))
	}
	if got := correlation.FromContext(requests[0].Context()); got != correlationID {
		t.Fatalf("request context correlation id = %q, want %q", got, correlationID)
	}
}
