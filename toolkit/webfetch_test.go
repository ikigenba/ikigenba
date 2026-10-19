package toolkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/agentkit"
)

func fetchTool(t *testing.T, client *http.Client) agentkit.Tool {
	t.Helper()
	tool, err := WebFetch(WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func fetchCall(t *testing.T, tool agentkit.Tool, url string, offset *int) (string, error) {
	t.Helper()
	args := map[string]any{"url": url}
	if offset != nil {
		args["offset"] = *offset
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tool.Call(context.Background(), raw)
}

func fetchServer(t *testing.T, mediaType string, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if mediaType != "" {
			w.Header().Set("Content-Type", mediaType)
		} else {
			w.Header()["Content-Type"] = nil
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func fetchContentOnly(t *testing.T, answer string) string {
	t.Helper()
	_, content, ok := strings.Cut(answer, "\n\n")
	if !ok {
		t.Fatalf("missing header separator: %q", answer)
	}
	return content
}

func TestWebFetchConstantsAndSchema(t *testing.T) {
	// R-ADBF-CKPD
	const bodyLimit int = WebFetchBodyBytes
	const timeout time.Duration = WebFetchTimeout
	if bodyLimit != 4194304 || timeout != 30*time.Second {
		t.Fatalf("limits = %d, %v", bodyLimit, timeout)
	}
	tool, err := WebFetch()
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	// R-AI70-VNO5
	props := schema["properties"].(map[string]any)
	if !reflect.DeepEqual(sortedKeys(props), []string{"offset", "url"}) || !reflect.DeepEqual(schema["required"], []any{"url"}) {
		t.Fatalf("schema = %#v", schema)
	}
	url := props["url"].(map[string]any)
	offset := props["offset"].(map[string]any)
	if url["type"] != "string" || url["minLength"] != float64(1) || offset["type"] != "integer" || offset["minimum"] != float64(0) {
		t.Fatalf("properties = %#v", props)
	}
}

func TestWebFetchRejectsInvalidURLsBeforeRequest(t *testing.T) {
	// R-AT64-BLCE
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { count.Add(1) }))
	defer server.Close()
	tool := fetchTool(t, server.Client())
	for _, address := range []string{"relative", "://bad", "http:///missing", "ftp://" + strings.TrimPrefix(server.URL, "http://"), "https:", "//example.com/x", "http://["} {
		answer, err := fetchCall(t, tool, address, nil)
		if err == nil || !strings.Contains(err.Error(), address) || answer != "" {
			t.Fatalf("%q: answer %q error %v", address, answer, err)
		}
	}
	if count.Load() != 0 {
		t.Fatal("invalid URL sent a request")
	}
	address := strings.Replace(server.URL, "http:", "HTTP:", 1)
	if _, err := fetchCall(t, tool, address, nil); err != nil {
		t.Fatalf("uppercase HTTP: %v", err)
	}
	if count.Load() != 1 {
		t.Fatal("uppercase HTTP was not fetched")
	}
}

func TestWebFetchDefaultGuard(t *testing.T) {
	// R-KG28-NF7K: the default client refuses literal and resolved loopback,
	// including IPv4-mapped IPv6, before a connection reaches the listener.
	var connections atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { connections.Add(1) }))
	defer server.Close()
	tool, err := WebFetch()
	if err != nil {
		t.Fatal(err)
	}
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	for _, host := range []string{"127.0.0.1", "127.23.45.67", "localhost", "[::1]", "[::ffff:127.0.0.1]", "10.0.0.1", "172.16.0.1", "192.168.0.1", "100.64.0.1", "169.254.0.1", "0.0.0.0", "0.2.3.4", "224.0.0.1", "[fc00::1]", "[fe80::1]", "[::]", "[ff00::1]", "[::ffff:10.0.0.1]"} {
		address := "http://" + host + ":" + port + "/"
		_, err := fetchCall(t, tool, address, nil)
		if err == nil || !strings.Contains(err.Error(), address) {
			t.Fatalf("%q: %v", address, err)
		}
		if host == "localhost" {
			if !strings.Contains(err.Error(), "127.0.0.1") && !strings.Contains(err.Error(), "::1") {
				t.Fatalf("missing resolved address: %v", err)
			}
		} else if !strings.Contains(err.Error(), strings.Trim(host, "[]")) {
			t.Fatalf("missing refused address: %v", err)
		}
	}
	if connections.Load() != 0 {
		t.Fatal("guard connected to loopback")
	}
}

func TestWebFetchRedirectLimitsAndHeaders(t *testing.T) {
	// R-KII1-EYOY, R-KJPX-SQFN
	final := fetchServer(t, "Text/Plain; charset=utf-8", []byte("arrived"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/%d", &n); err != nil {
			t.Error(err)
			return
		}
		if n == 0 {
			http.Redirect(w, r, final.URL+"/destination", http.StatusFound)
		} else {
			http.Redirect(w, r, fmt.Sprintf("/%d", n-1), http.StatusFound)
		}
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	tool := fetchTool(t, client)
	got, err := fetchCall(t, tool, server.URL+"/9", nil)
	want := "URL: " + final.URL + "/destination\nStatus: 200\nType: text/plain\n\narrived"
	if err != nil || got != want {
		t.Fatalf("ten redirects = %q, %v; want %q", got, err, want)
	}
	address := server.URL + "/10"
	if _, err := fetchCall(t, tool, address, nil); err == nil || !strings.Contains(err.Error(), address) {
		t.Fatalf("eleventh redirect error = %v", err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/0", nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusFound {
		t.Fatal("consumer redirect policy changed")
	}
}

// fetchDeadlineTransport records the context actually handed to the transport
// while allowing only the deterministic loopback server to carry HTTP.
type fetchDeadlineTransport struct {
	base      http.RoundTripper
	deadlines []time.Time
}

func (transport *fetchDeadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	deadline, ok := req.Context().Deadline()
	if !ok {
		return nil, fmt.Errorf("missing request deadline")
	}
	transport.deadlines = append(transport.deadlines, deadline)
	return transport.base.RoundTrip(req)
}

func TestWebFetchDeadlineCoversEveryHop(t *testing.T) {
	// R-KHA5-16Y9
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/end", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
	}))
	defer server.Close()
	transport := &fetchDeadlineTransport{base: server.Client().Transport}
	client := &http.Client{Transport: transport}
	tool := fetchTool(t, client)
	start := time.Now()
	if _, err := fetchCall(t, tool, server.URL, nil); err != nil {
		t.Fatal(err)
	}
	if len(transport.deadlines) != 2 {
		t.Fatalf("requests = %d", len(transport.deadlines))
	}
	if transport.deadlines[0].After(time.Now().Add(WebFetchTimeout)) || transport.deadlines[0].Before(start) || !transport.deadlines[0].Equal(transport.deadlines[1]) {
		t.Fatalf("deadlines = %v", transport.deadlines)
	}
	// An earlier caller deadline is retained on every hop as well.
	transport.deadlines = nil
	deadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	args, _ := json.Marshal(map[string]any{"url": server.URL})
	if _, err := tool.Call(ctx, args); err != nil {
		t.Fatal(err)
	}
	for _, got := range transport.deadlines {
		if !got.Equal(deadline) {
			t.Fatalf("deadline %v != %v", got, deadline)
		}
	}
}

func TestWebFetchStatusAndMediaTypes(t *testing.T) {
	// R-AY1P-UOB6
	for _, status := range []int{299, 300, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "private body")
			}))
			defer server.Close()
			got, err := fetchCall(t, fetchTool(t, server.Client()), server.URL, nil)
			if status == 299 {
				if err != nil || !strings.HasSuffix(got, "private body") {
					t.Fatalf("2xx = %q, %v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "private body") || got != "" {
				t.Fatalf("status = %q, %v", got, err)
			}
		})
	}
	// R-B1PE-ZZJ9, R-KJPX-SQFN
	for _, media := range []string{"text/plain", "text/css", "application/json", "application/xml", "application/javascript", "image/png", "application/pdf", "application/octet-stream", "application/zip", "", "not a media type"} {
		t.Run(media, func(t *testing.T) {
			server := fetchServer(t, media, []byte("alpha é\nsecond line"))
			got, err := fetchCall(t, fetchTool(t, server.Client()), server.URL, nil)
			if strings.HasPrefix(media, "image/") || media == "application/pdf" || media == "application/octet-stream" || media == "application/zip" {
				if err == nil || !strings.Contains(err.Error(), media) {
					t.Fatalf("media error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expectedMedia := media
			if media == "" || media == "not a media type" {
				expectedMedia = "text/plain"
			}
			want := "URL: " + server.URL + "\nStatus: 200\nType: " + expectedMedia + "\n\nalpha é\nsecond line"
			if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		})
	}
}

func TestWebFetchMarkdown(t *testing.T) {
	// R-B457-RJ0N, R-B1PE-ZZJ9
	body := `<h1>One</h1><h2>Two</h2><h3>Three</h3><h4>Four</h4><h5>Five</h5><h6>Six</h6><p>A paragraph <a href="../next?q=a">target</a> <em>soft</em> <strong>bold</strong> <code>inline</code>.</p><ul><li>Unordered</li></ul><ol><li>First</li><li>Second</li></ol><table><tr><th>A</th><th>B</th></tr><tr><td>C</td><td>D</td></tr></table><pre>block
code</pre><script>script-secret</script><style>style-secret</style><!--comment-secret-->`
	for _, media := range []string{"text/html", "application/xhtml+xml"} {
		server := fetchServer(t, media, []byte(body))
		got, err := fetchCall(t, fetchTool(t, server.Client()), server.URL+"/dir/page", nil)
		if err != nil {
			t.Fatal(err)
		}
		content := fetchContentOnly(t, got)
		for _, want := range []string{"# One", "## Two", "### Three", "#### Four", "##### Five", "###### Six", "A paragraph", "[target](" + server.URL + "/next?q=a)", "*soft*", "**bold**", "`inline`", "- Unordered", "1. First", "2. Second", "| A | B |", "|---|---|", "| C | D |", "```\nblock\ncode\n```"} {
			if !strings.Contains(content, want) {
				t.Errorf("missing %q in %q", want, content)
			}
		}
		for _, forbidden := range []string{"script-secret", "style-secret", "comment-secret", "<", ">"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("unexpected %q in %q", forbidden, content)
			}
		}
	}
}

func TestWebFetchCharsetPrecedence(t *testing.T) {
	// R-KKXU-6I6C
	tests := []struct {
		name, media string
		body        []byte
		want        string
	}{
		{"BOM before header", "text/plain; charset=windows-1252", append([]byte{0xef, 0xbb, 0xbf}, []byte("é")...), "é"},
		{"UTF16 little BOM", "text/plain; charset=utf-8", []byte{0xff, 0xfe, 'A', 0, 0xe9, 0}, "Aé"},
		{"UTF16 big BOM", "text/plain; charset=utf-8", []byte{0xfe, 0xff, 0, 'A', 0, 0xe9}, "Aé"},
		{"header before meta", "text/plain; charset=windows-1252", []byte("<meta charset=utf-8>caf\xe9"), "<meta charset=utf-8>café"},
		{"meta before valid UTF8", "text/plain", []byte("<meta charset=windows-1252>é"), "<meta charset=windows-1252>Ã©"},
		{"meta pragma", "text/plain", []byte("<meta http-equiv=content-type content='text/html; charset=windows-1252'>\x80"), "<meta http-equiv=content-type content='text/html; charset=windows-1252'>€"},
		{"unrecognized header", "text/plain; charset=unknown", []byte("é"), "é"},
		{"UTF8 after ASCII preview", "text/plain", []byte(strings.Repeat("a", 1200) + "日本"), strings.Repeat("a", 1200) + "日本"},
		{"invalid after ASCII preview", "text/plain", append([]byte(strings.Repeat("a", 1200)), 0x80), strings.Repeat("a", 1200) + "€"},
		{"incomplete ending", "text/plain", []byte{'a', 0xc3, 0xa9, 0xe6, 0x97}, "aé"},
		{"UTF8 preview invalid tail", "text/plain", []byte("é" + strings.Repeat("a", 1200) + "\x80"), "Ã©" + strings.Repeat("a", 1200) + "€"},
		{"Windows fallback", "text/plain", []byte{0x80, 0xe9}, "€é"},
		{"meta outside preview", "text/plain", []byte(strings.Repeat("a", 1024) + "<meta charset=windows-1252>é"), strings.Repeat("a", 1024) + "<meta charset=windows-1252>é"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := fetchServer(t, test.media, test.body)
			got, err := fetchCall(t, fetchTool(t, server.Client()), server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if content := fetchContentOnly(t, got); content != test.want {
				t.Fatalf("content %q want %q", content, test.want)
			}
		})
	}
}

func TestWebFetchOffsetAndContinuation(t *testing.T) {
	// R-BA8P-ODQ4, R-BBGM-25GT
	server := fetchServer(t, "text/plain", []byte("α🙂文z"))
	tool := fetchTool(t, server.Client())
	got, err := fetchCall(t, tool, server.URL, nil)
	if err != nil || fetchContentOnly(t, got) != "α🙂文z" {
		t.Fatalf("absent offset = %q, %v", got, err)
	}
	for _, offset := range []int{0, 1, 3, 4, 9} {
		got, err := fetchCall(t, tool, server.URL, &offset)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("[no content at offset %d of 4 characters]", offset)
		if offset < 4 {
			want = string([]rune("α🙂文z")[offset:])
		}
		if content := fetchContentOnly(t, got); content != want {
			t.Fatalf("offset %d = %q want %q", offset, content, want)
		}
	}
	// R-KM5Q-K9X1
	body := strings.Repeat("α🙂文z", 15000)
	large := fetchServer(t, "text/plain", []byte(body))
	tool = fetchTool(t, large.Client())
	offset := 17
	got, err = fetchCall(t, tool, large.URL, &offset)
	if err != nil {
		t.Fatal(err)
	}
	header, _, _ := strings.Cut(got, "\n\n")
	headerChars := utf8.RuneCountInString(header + "\n\n")
	shown := string([]rune(got)[:maxOutputChars])
	if fetchContentOnly(t, shown) != string([]rune(body)[offset:offset+maxOutputChars-headerChars]) || !strings.Contains(got, "[output truncated:") {
		t.Fatal("incorrect truncated content prefix")
	}
	next := offset + 30000 - headerChars
	continuation, err := fetchCall(t, tool, large.URL, &next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fetchContentOnly(t, continuation), string([]rune(body)[next:next+40])) {
		t.Fatal("continuation skipped or repeated content")
	}
}

func TestWebFetchBodyReadLimit(t *testing.T) {
	// R-B0HI-M7SK
	// The body is larger than the visible output; paging near its end proves
	// which bytes were retained independently of the result cap.
	body := strings.Repeat("a", WebFetchBodyBytes-2) + "é" + "forbidden-tail"
	server := fetchServer(t, "text/plain", []byte(body))
	transport := &fetchCountingTransport{base: server.Client().Transport}
	offset := WebFetchBodyBytes - 4
	got, err := fetchCall(t, fetchTool(t, &http.Client{Transport: transport}), server.URL, &offset)
	if err != nil {
		t.Fatal(err)
	}
	if transport.read != WebFetchBodyBytes {
		t.Fatalf("body read %d bytes, want %d", transport.read, WebFetchBodyBytes)
	}
	if content := fetchContentOnly(t, got); content != "aaé" {
		t.Fatalf("bounded body tail = %q", content)
	}
}

// fetchCountingTransport counts response bytes read by the tool, independently
// of the HTTP server buffering and the rendered character output.
type fetchCountingTransport struct {
	base http.RoundTripper
	read int
}

func (transport *fetchCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(req)
	if err == nil {
		response.Body = &fetchCountingBody{ReadCloser: response.Body, count: &transport.read}
	}
	return response, err
}

type fetchCountingBody struct {
	io.ReadCloser
	count *int
}

func (body *fetchCountingBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	*body.count += n
	return n, err
}
