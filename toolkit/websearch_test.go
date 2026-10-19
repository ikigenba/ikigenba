package toolkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
)

type searchRoundTripper func(*http.Request) (*http.Response, error)

func (f searchRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// searchStub keeps the original Brave URL visible to check, then routes the
// request to loopback. No request can escape to an external service.
func searchStub(t *testing.T, handler http.HandlerFunc, check func(*http.Request)) *http.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	destination, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	transport := client.Transport
	client.Transport = searchRoundTripper(func(req *http.Request) (*http.Response, error) {
		if check != nil {
			check(req)
		}
		routed := req.Clone(req.Context())
		copiedURL := *req.URL
		copiedURL.Scheme, copiedURL.Host = destination.Scheme, destination.Host
		routed.URL = &copiedURL
		return transport.RoundTrip(routed)
	})
	return client
}

func newSearchStub(t *testing.T, status int, body string) agentkit.Tool {
	t.Helper()
	client := searchStub(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status); _, _ = w.Write([]byte(body)) }, nil)
	tool, err := WebSearch("test subscription", WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestWebSearchKey(t *testing.T) {
	// R-AEJB-QCG2: blank keys fail at construction, including Unicode whitespace.
	for _, key := range []string{"", " \r\n\t", "\u2003\u00a0"} {
		tool, err := WebSearch(key)
		if tool != nil || err == nil {
			t.Fatalf("blank key construction = (%v, %v)", tool, err)
		}
	}
}

func TestWebSearchSchema(t *testing.T) {
	// R-AGZ4-HVXG: only the three declared fields are exposed with required query.
	tool, err := WebSearch("test subscription")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for name := range schema.Properties {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	if got, want := keys, []string{"allowed_domains", "blocked_domains", "query"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("properties = %v", got)
	}
	if !reflect.DeepEqual(schema.Required, []string{"query"}) {
		t.Fatalf("required = %v", schema.Required)
	}
	if query := schema.Properties["query"]; query["type"] != "string" || query["minLength"] != float64(1) {
		t.Fatalf("query schema = %v", query)
	}
	for _, name := range []string{"allowed_domains", "blocked_domains"} {
		property := schema.Properties[name]
		if property["type"] != "array" || property["items"].(map[string]any)["type"] != "string" {
			t.Fatalf("%s schema = %v", name, property)
		}
	}
}

func TestWebSearchRequest(t *testing.T) {
	// R-ALUQ-0YW8: exact endpoint, method, query and credential/accept headers;
	// filtering arguments never alter the request shape.
	var count atomic.Int32
	const query = "café & software + agents"
	const key = "example subscription"
	client := searchStub(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }, func(req *http.Request) {
		count.Add(1)
		if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "api.search.brave.com" || req.URL.Path != "/res/v1/web/search" {
			t.Errorf("request = %s %s", req.Method, req.URL)
		}
		want := url.Values{"q": {query}, "count": {"20"}, "extra_snippets": {"true"}, "country": {"us"}, "search_lang": {"en"}}
		if !reflect.DeepEqual(req.URL.Query(), want) {
			t.Errorf("query parameters = %v", req.URL.Query())
		}
		if req.Header.Get("X-Subscription-Token") != key || req.Header.Get("Accept") != "application/json" {
			t.Error("missing required headers")
		}
	})
	tool, err := WebSearch(key, WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{"query": query, "allowed_domains": []string{"example.com"}, "blocked_domains": []string{"other.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Call(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 1 {
		t.Fatalf("request count = %d", count.Load())
	}
}

func TestWebSearchFiltering(t *testing.T) {
	// R-KDMF-VVQ6: filtering uses case-insensitive hostname boundaries without
	// ports, honors blocks over allows, retains source order, and caps after filtering.
	results := []map[string]any{}
	for _, host := range []string{"notexample.com", "blocked.example.com", "example.com:8443", "WWW.Example.COM", "deep.blocked.example.com", "other.test"} {
		results = append(results, map[string]any{"title": host, "url": "https://" + host + "/"})
	}
	results = append(results, map[string]any{"title": "hostless", "url": "/local"})
	for i := 0; i < 12; i++ {
		results = append(results, map[string]any{"title": fmt.Sprintf("result%d", i), "url": fmt.Sprintf("https://example.com/%d", i)})
	}
	body, err := json.Marshal(map[string]any{"web": map[string]any{"results": results}})
	if err != nil {
		t.Fatal(err)
	}
	tool := newSearchStub(t, 200, string(body))
	for _, tc := range []struct {
		name, args string
		titles     []string
	}{
		{"allow and block", `{"query":"x","allowed_domains":["EXAMPLE.com"],"blocked_domains":["BLOCKED.example.COM"]}`, []string{"example.com:8443", "WWW.Example.COM", "result0", "result1", "result2", "result3", "result4", "result5", "result6", "result7"}},
		{"no allow", `{"query":"x","blocked_domains":["example.com"]}`, []string{"notexample.com", "other.test", "hostless"}},
		{"empty allow", `{"query":"x","allowed_domains":[],"blocked_domains":["example.com"]}`, []string{"notexample.com", "other.test", "hostless"}},
		{"empty entry does not match hostless", `{"query":"x","allowed_domains":[""]}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tool.Call(context.Background(), json.RawMessage(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.titles) == 0 {
				if got != "No results." {
					t.Fatalf("result = %q", got)
				}
				return
			}
			blocks := strings.Split(got, "\n\n")
			if len(blocks) != len(tc.titles) {
				t.Fatalf("blocks = %d, want %d", len(blocks), len(tc.titles))
			}
			for i, title := range tc.titles {
				if first := strings.SplitN(blocks[i], "\n", 2)[0]; first != fmt.Sprintf("%d. %s", i+1, title) {
					t.Errorf("title line = %q", first)
				}
			}
		})
	}
}

func TestWebSearchRendering(t *testing.T) {
	// R-KEUC-9NGV: preserve Brave strings/highlighting and ordering, normalize
	// all newline forms in every field, and omit empty optional fields.
	body := `{"web":{"results":[{"title":"a\r\nb\rc\nd","url":"https://example.com/a\r\nb\rc\nd","description":"<strong>A</strong>\r\nB\rC\nD","extra_snippets":["","one\r\ntwo","three\rfour\nfive",""],"age":"1\r\n2\r3\n4"},{"title":"bare","url":"https://example.org","description":"","extra_snippets":[""],"age":""},{"title":"absent","url":"https://example.net"}]}}`
	got, err := newSearchStub(t, 200, body).Call(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "1. a b c d\nhttps://example.com/a b c d\n<strong>A</strong> B C D\none two\nthree four five\nAge: 1 2 3 4\n\n2. bare\nhttps://example.org\n\n3. absent\nhttps://example.net"
	if got != want {
		t.Fatalf("answer = %q, want %q", got, want)
	}
}

func TestWebSearchNoResults(t *testing.T) {
	// R-APIF-6A4B: absent, empty, and fully filtered results succeed with exact text.
	for _, body := range []string{`{}`, `{"web":{}}`, `{"web":{"results":[]}}`, `{"web":{"results":[{"url":"https://excluded.test"}]}}`} {
		got, err := newSearchStub(t, 200, body).Call(context.Background(), json.RawMessage(`{"query":"x","allowed_domains":["example.com"]}`))
		if err != nil || got != "No results." {
			t.Fatalf("Call() = (%q, %v)", got, err)
		}
	}
}

func TestWebSearchStatusErrors(t *testing.T) {
	// R-AQQB-K1V0: any non-200 status reports its code and structured Brave
	// detail when complete, otherwise exactly the body's first line.
	for _, tc := range []struct {
		status   int
		body     string
		contains []string
		excludes []string
	}{
		{422, `{"type":"ErrorResponse","error":{"code":"BAD_KEY","detail":"invalid subscription"}}`, []string{"422", "BAD_KEY", "invalid subscription"}, nil},
		{503, "busy now\nsecret later", []string{"503", "busy now"}, []string{"secret later"}},
		{201, "created\rignored", []string{"201", "created"}, []string{"ignored"}},
		{403, `{"type":"ErrorResponse","error":{"code":"ONLY_CODE"}}`, []string{"403", `{"type":"ErrorResponse","error":{"code":"ONLY_CODE"}}`}, nil},
		{404, `{"type":"Other","error":{"code":"OTHER","detail":"body"}}`, []string{"404", `{"type":"Other","error":{"code":"OTHER","detail":"body"}}`}, nil},
	} {
		got, err := newSearchStub(t, tc.status, tc.body).Call(context.Background(), json.RawMessage(`{"query":"x"}`))
		if err == nil || got != "" {
			t.Fatalf("Call() = (%q, %v)", got, err)
		}
		for _, value := range tc.contains {
			if !strings.Contains(err.Error(), value) {
				t.Errorf("error %q lacks %q", err, value)
			}
		}
		for _, value := range tc.excludes {
			if strings.Contains(err.Error(), value) {
				t.Errorf("error %q contains %q", err, value)
			}
		}
	}
}

func TestWebSearchRedirectIsOneRequest(t *testing.T) {
	// R-ALUQ-0YW8 and R-AQQB-K1V0: a redirect remains one request and a status error.
	var count atomic.Int32
	client := searchStub(t, func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Location", "https://other.example/")
		w.WriteHeader(302)
		_, _ = w.Write([]byte("moved"))
	}, nil)
	tool, err := WebSearch("test subscription", WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"query":"x"}`))
	if got != "" || err == nil || !strings.Contains(err.Error(), "302") || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("Call() = (%q, %v)", got, err)
	}
	if count.Load() != 1 {
		t.Fatalf("requests = %d", count.Load())
	}
}

func TestWebSearchInvalidJSON(t *testing.T) {
	// R-ARY7-XTLP: a successful response must hold a JSON object.
	for _, body := range []string{"", "null", "[]", `"text"`, "42", "true", "{broken", "{} trailing"} {
		got, err := newSearchStub(t, 200, body).Call(context.Background(), json.RawMessage(`{"query":"x"}`))
		if err == nil || got != "" {
			t.Fatalf("body %q: Call() = (%q, %v)", body, got, err)
		}
	}
}

func TestWebSearchTimeout(t *testing.T) {
	// R-IUTB-8PUQ: the default client really expires at the declared duration;
	// an injected client's timeout replaces it, including a longer deadline.
	for _, tc := range []struct {
		name     string
		timeout  time.Duration
		supplied bool
	}{
		{"default", WebFetchTimeout, false}, {"shorter client", time.Second, true}, {"longer client", WebFetchTimeout + time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := searchRoundTripper(func(req *http.Request) (*http.Response, error) {
					<-req.Context().Done()
					return nil, req.Context().Err()
				})
				var opts []WebSearchOption
				if tc.supplied {
					opts = append(opts, WithHTTPClient(&http.Client{Transport: transport, Timeout: tc.timeout}))
				} else {
					original := http.DefaultTransport
					http.DefaultTransport = transport
					t.Cleanup(func() { http.DefaultTransport = original })
				}
				tool, err := WebSearch("test subscription", opts...)
				if err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				got, err := tool.Call(context.Background(), json.RawMessage(`{"query":"x"}`))
				if err == nil || got != "" {
					t.Fatalf("Call() = (%q, %v)", got, err)
				}
				if elapsed := time.Since(start); elapsed != tc.timeout {
					t.Fatalf("elapsed = %v, want %v", elapsed, tc.timeout)
				}
			})
		})
	}
}

func TestWebSearchStatusWithBrokenBody(t *testing.T) {
	// R-AQQB-K1V0: an incomplete body still reports status and its available first line.
	client := &http.Client{Transport: searchRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(&searchBrokenBody{})}, nil
	})}
	tool, err := WebSearch("test subscription", WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"query":"x"}`))
	if got != "" || err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "available detail") {
		t.Fatalf("Call() = (%q, %v)", got, err)
	}
}

type searchBrokenBody struct{ read bool }

func (body *searchBrokenBody) Read(p []byte) (int, error) {
	if body.read {
		return 0, io.ErrUnexpectedEOF
	}
	body.read = true
	return copy(p, "available detail"), io.ErrUnexpectedEOF
}
