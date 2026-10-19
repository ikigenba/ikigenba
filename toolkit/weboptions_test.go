package toolkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/toolkit"
)

// R-A9NQ-79HA: consumer calls use the declared constructor signatures.
var _ func(string, ...toolkit.WebSearchOption) (agentkit.Tool, error) = toolkit.WebSearch

// R-AAVM-L17Z: WebFetch takes only its tool-specific options.
var _ func(...toolkit.WebFetchOption) (agentkit.Tool, error) = toolkit.WebFetch

// R-AC3I-YSYO: the shared exported option satisfies both sealed interfaces.
var _ func(*http.Client) toolkit.HTTPClientOption = toolkit.WithHTTPClient
var _ toolkit.WebSearchOption = toolkit.HTTPClientOption{}
var _ toolkit.WebFetchOption = toolkit.HTTPClientOption{}

type sharedWebTransport func(*http.Request) (*http.Response, error)

func (transport sharedWebTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func sharedWebConstructors() []struct {
	name string
	make func(*http.Client) (agentkit.Tool, error)
	args json.RawMessage
} {
	return []struct {
		name string
		make func(*http.Client) (agentkit.Tool, error)
		args json.RawMessage
	}{
		{"WebSearch", func(c *http.Client) (agentkit.Tool, error) {
			return toolkit.WebSearch("test-key", toolkit.WithHTTPClient(c))
		}, json.RawMessage(`{"query":"test"}`)},
		{"WebFetch", func(c *http.Client) (agentkit.Tool, error) { return toolkit.WebFetch(toolkit.WithHTTPClient(c)) }, json.RawMessage(`{"url":"http://127.0.0.1/test"}`)},
	}
}

func TestWebToolsRejectNilClient(t *testing.T) {
	// R-AFR8-446R: a supplied nil client is a constructor failure for both tools.
	for _, tc := range sharedWebConstructors() {
		t.Run(tc.name, func(t *testing.T) {
			tool, err := tc.make(nil)
			if tool != nil || err == nil {
				t.Fatalf("constructor = (%v, %v), want nil tool and error", tool, err)
			}
		})
	}
}

func TestWebToolsSharedClientLastOptionWins(t *testing.T) {
	// R-AJEX-9FEU: all requests use the final supplied client, including loopback redirects.
	var fetchRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchRequests.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "loopback-content")
	}))
	defer server.Close()
	badClient := &http.Client{Transport: sharedWebTransport(func(*http.Request) (*http.Response, error) {
		t.Error("request used an earlier HTTP client option")
		return nil, errors.New("unexpected earlier client")
	})}
	var searchRequests atomic.Int32
	searchClient := &http.Client{Transport: sharedWebTransport(func(r *http.Request) (*http.Response, error) {
		searchRequests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"web":{"results":[{"title":"client-result","url":"https://example.com"}]}}`)), Request: r}, nil
	})}
	search, err := toolkit.WebSearch("test-key", toolkit.WithHTTPClient(badClient), toolkit.WithHTTPClient(searchClient))
	if err != nil {
		t.Fatal(err)
	}
	got, err := search.Call(context.Background(), json.RawMessage(`{"query":"test"}`))
	if err != nil || !strings.Contains(got, "client-result") || searchRequests.Load() != 1 {
		t.Fatalf("search = %q, %v; requests %d", got, err, searchRequests.Load())
	}
	fetch, err := toolkit.WebFetch(toolkit.WithHTTPClient(badClient), toolkit.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	got, err = fetch.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"url":%q}`, server.URL+"/start")))
	if err != nil || !strings.Contains(got, "loopback-content") || fetchRequests.Load() != 2 {
		t.Fatalf("fetch = %q, %v; requests %d", got, err, fetchRequests.Load())
	}
}

func TestWebToolsNeverBlock(t *testing.T) {
	// R-BDWE-TOY7: access is unrestricted for populated, empty, and unrecognised argument fields.
	for _, tc := range sharedWebConstructors() {
		tool, err := tc.make(&http.Client{})
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range []json.RawMessage{tc.args, json.RawMessage(`{}`), json.RawMessage(`{"unexpected":true}`)} {
			if got := tool.Access(args); !reflect.DeepEqual(got, agentkit.BlocksNone()) {
				t.Errorf("%s.Access(%s) = %#v", tc.name, args, got)
			}
		}
	}
}

func TestWebToolsCancellationWrapsContextError(t *testing.T) {
	// R-BF4B-7GOW: cancel an in-flight request and preserve the caller's error.
	// R-CL5N-3R8K: these failed calls return no partial output.
	for _, tc := range sharedWebConstructors() {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			client := &http.Client{Transport: sharedWebTransport(func(r *http.Request) (*http.Response, error) {
				close(started)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			tool, err := tc.make(client)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			var got string
			var callErr error
			go func() { defer close(done); got, callErr = tool.Call(ctx, tc.args) }()
			<-started
			cancel()
			<-done
			if got != "" || !errors.Is(callErr, ctx.Err()) {
				t.Fatalf("Call = (%q,%v), want empty and wrapped %v", got, callErr, ctx.Err())
			}
		})
	}
}

func TestWebToolOutputCapAndSuccessInvariant(t *testing.T) {
	// R-EUUW-QDX3: both web tools apply the Unicode cap to the complete answer.
	// R-CL5N-3R8K: successful web calls return text and a nil error.
	for _, tc := range sharedWebConstructors() {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Repeat("é", 30001)
			mediaType := "text/plain"
			if tc.name == "WebSearch" {
				body = `{"web":{"results":[{"title":"` + body + `","url":"https://example.com"}]}}`
				mediaType = "application/json"
			}
			client := &http.Client{Transport: sharedWebTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {mediaType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			tool, err := tc.make(client)
			if err != nil {
				t.Fatal(err)
			}
			got, err := tool.Call(context.Background(), tc.args)
			if err != nil {
				t.Fatal(err)
			}
			full := "1. " + strings.Repeat("é", 30001) + "\nhttps://example.com"
			if tc.name == "WebFetch" {
				full = "URL: http://127.0.0.1/test\nStatus: 200\nType: text/plain\n\n" + strings.Repeat("é", 30001)
			}
			want := string([]rune(full)[:30000]) + fmt.Sprintf("\n[output truncated: showing 30000 of %d characters]", len([]rune(full)))
			if got != want {
				t.Fatalf("output differs: got %d characters, want exact capped %d-character answer", len([]rune(got)), len([]rune(want)))
			}
		})
	}
}
