package space_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type populatedJar struct{}

func (*populatedJar) Cookies(*url.URL) []*http.Cookie {
	return []*http.Cookie{{Name: "saved", Value: "cookie-value", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}}
}

func (*populatedJar) SetCookies(*url.URL, []*http.Cookie) {}

func assertRequestHeaders(t *testing.T, headers http.Header, gateway string) {
	t.Helper()
	canonical := make(http.Header)
	for name, values := range headers {
		key := http.CanonicalHeaderKey(name)
		canonical[key] = append(canonical[key], values...)
	}
	want := http.Header{"Authorization": {"Bearer " + testToken}, "Content-Type": {"application/json"}, "Accept": {"application/json"}, "Mcp-Protocol-Version": {space.ProtocolVersion}, "Mcp-Method": {"tools/call"}, "Mcp-Name": {gateway}}
	if !reflect.DeepEqual(canonical, want) {
		t.Fatalf("wrong headers: %#v", headers)
	}
}

const testToken = "private-client-credential"
const testSpace = "example.invalid"

var refs = []session.Ref{{Tool: session.ToolClaude, ID: "one"}, {Tool: session.ToolCodex, ID: "two"}}

type method struct {
	name    string
	gateway string
	tool    string
	call    func(*space.Client, context.Context) ([]space.Held, error)
}

func methods() []method {
	return []method{
		{"latest", "call", space.LatestTool, func(c *space.Client, ctx context.Context) ([]space.Held, error) { return c.Latest(ctx, refs) }},
		{"push", "mutate", space.PushTool, func(c *space.Client, ctx context.Context) ([]space.Held, error) {
			return c.Push(ctx, "laptop", []session.Session{{Ref: refs[0], Directory: "/first"}, {Ref: refs[1], Directory: "/second", Headless: true}})
		}},
	}
}
func response(status int, contentType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}
func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func decode(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var value map[string]any
	d := json.NewDecoder(r)
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing JSON: %v", err)
	}
	return value
}
func answer() map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"structuredContent": map[string]any{"sessions": []any{map[string]any{"tool": "claude", "id": "one"}, map[string]any{"tool": "codex", "id": "two"}}}}}
}
func result(a map[string]any) map[string]any { return a["result"].(map[string]any) }
func structured(a map[string]any) map[string]any {
	return result(a)["structuredContent"].(map[string]any)
}
func entries(a map[string]any) []any { return structured(a)["sessions"].([]any) }

// R-A9RX-O0CZ R-AAZU-1S3O R-AC7Q-FJUD R-ADFM-TBL2 R-AENJ-73BR
// R-AFVF-KV2G R-AH3B-YMT5 R-AIB8-CEJU R-AJJ4-Q6AJ R-AKR1-3Y18
func TestPublicSurface(t *testing.T) {
	type word string
	type count int
	var p word = space.ProtocolVersion
	var s word = space.Service
	var push word = space.PushTool
	var latest word = space.LatestTool
	var ml count = space.MaxLatest
	var ms count = space.MaxPushSessions
	var mt count = space.MaxPushTurns
	if p != "2026-07-28" || s != "sessions" || push != "push_turns" || latest != "get_latest_turns" || ml != 500 || ms != 500 || mt != 1000 {
		t.Fatal("wrong protocol constants")
	}
	var (
		endpoint     func(string) string
		constructor  func(string, string, *http.Client) *space.Client
		latestMethod func(*space.Client, context.Context, []session.Ref) ([]space.Held, error)
		pushMethod   func(*space.Client, context.Context, string, []session.Session) ([]space.Held, error)
	)
	endpoint, constructor, latestMethod, pushMethod = space.Endpoint, space.New, (*space.Client).Latest, (*space.Client).Push
	if endpoint("us.ikigenba.dev") != "https://mcp.us.ikigenba.dev/mcp" {
		t.Fatal("wrong endpoint")
	}
	client := constructor(testSpace, testToken, &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("constructor performed I/O")
		return nil, errors.New("unexpected request")
	})})
	if client == nil || latestMethod == nil || pushMethod == nil {
		t.Fatal("missing methods")
	}
	clientType := reflect.TypeFor[space.Client]()
	if clientType.Kind() != reflect.Struct {
		t.Fatal("Client is not a struct")
	}
	for i := range clientType.NumField() {
		if clientType.Field(i).IsExported() {
			t.Fatal("Client exposes a field")
		}
	}
	held := space.Held{Ref: refs[0], Latest: time.Time{}}
	shape := struct {
		Ref    session.Ref
		Latest time.Time
	}(held)
	if shape.Ref != refs[0] || !shape.Latest.IsZero() {
		t.Fatal("wrong Held fields")
	}
	failure := space.Error{Reason: "raw\nreason\x00", Detail: "detail", Quoted: true}
	errorShape := struct {
		Reason string
		Detail string
		Quoted bool
	}(failure)
	var asError error = &failure
	if errorShape.Reason != failure.Reason || errorShape.Detail != "detail" || !errorShape.Quoted || asError.Error() != failure.Reason {
		t.Fatal("wrong error fields or Error result")
	}
}

// R-7MEB-CUSI R-AOEQ-999B R-Q896-34E1 R-AS2F-EKHE R-ATAB-SC83
// R-AUI8-63YS R-AVQ4-JVPH R-AWY0-XNG6 R-AY5X-BF6V R-AZDT-P6XK R-B0LQ-2YO9
func TestRequests(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			hc := &http.Client{Jar: &populatedJar{}, Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPost || req.URL.String() != space.Endpoint(testSpace) || req.Context() != ctx {
					t.Fatal("wrong HTTP request or context")
				}
				assertRequestHeaders(t, req.Header, m.gateway)
				body := decode(t, req.Body)
				id, ok := body["id"].(json.Number)
				if !ok {
					t.Fatal("id is not a JSON number")
				}
				if _, err := strconv.ParseInt(id.String(), 10, 64); err != nil {
					t.Fatal("id is not a JSON integer")
				}
				wireEntries := []any{map[string]any{"tool": "claude", "id": "one"}, map[string]any{"tool": "codex", "id": "two"}}
				if m.name == "push" {
					wireEntries = []any{
						map[string]any{"tool": "claude", "id": "one", "host": "laptop", "directory": "/first", "headless": false, "turns": []any{}},
						map[string]any{"tool": "codex", "id": "two", "host": "laptop", "directory": "/second", "headless": true, "turns": []any{}},
					}
				}
				want := map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": m.gateway, "arguments": map[string]any{"service": space.Service, "tool": m.tool, "args": map[string]any{"sessions": wireEntries}}, "_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": space.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}}
				if !reflect.DeepEqual(body, want) {
					t.Fatalf("body = %#v; want %#v", body, want)
				}
				a := answer()
				a["id"] = id
				return response(200, "application/json", encode(t, a)), nil
			})}
			c := space.New(testSpace, testToken, hc)
			if calls != 0 {
				t.Fatal("New sent a request")
			}
			if _, err := m.call(c, ctx); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("sent %d requests", calls)
			}
		})
	}
}

// R-AY5X-BF6V R-AZDT-P6XK R-B0LQ-2YO9 R-AWY0-XNG6
func TestPushCompleteEntries(t *testing.T) {
	moment := time.Date(2026, 10, 13, 16, 2, 11, 250999000, time.FixedZone("east", 7200))
	sessions := []session.Session{
		{Ref: refs[0], Parent: &refs[1], Directory: "/one", Headless: true, Command: &session.Command{Name: "", Argument: ""}, Turns: []session.Turn{
			{Moment: moment, Whose: session.WhoseAgent, Model: "model", Tokens: session.Tokens{Input: 1000000000000, Cached: 22, Output: 33, Reasoning: 44}},
			{Moment: moment.Add(time.Second), Whose: session.WhosePerson},
		}},
		{Ref: refs[1], Directory: "/two", Command: &session.Command{Name: "/run", Argument: "a b"}},
	}
	hc := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		body := decode(t, req.Body)
		got := body["params"].(map[string]any)["arguments"].(map[string]any)["args"].(map[string]any)["sessions"]
		want := []any{
			map[string]any{"tool": "claude", "id": "one", "host": "desktop", "directory": "/one", "headless": true, "parent": map[string]any{"tool": "codex", "id": "two"}, "command": map[string]any{"name": "", "argument": ""}, "turns": []any{
				map[string]any{"moment": "2026-10-13T14:02:11.250Z", "whose": "agent", "model": "model", "input": json.Number("1000000000000"), "cached": json.Number("22"), "output": json.Number("33"), "reasoning": json.Number("44")},
				map[string]any{"moment": "2026-10-13T14:02:12.250Z", "whose": "person", "model": "", "input": json.Number("0"), "cached": json.Number("0"), "output": json.Number("0"), "reasoning": json.Number("0")},
			}},
			map[string]any{"tool": "codex", "id": "two", "host": "desktop", "directory": "/two", "headless": false, "command": map[string]any{"name": "/run", "argument": "a b"}, "turns": []any{}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("push sessions = %#v; want %#v", got, want)
		}
		a := answer()
		a["id"] = body["id"]
		return response(200, "application/json", encode(t, a)), nil
	})}
	if _, err := space.New(testSpace, testToken, hc).Push(context.Background(), "desktop", sessions); err != nil {
		t.Fatal(err)
	}
}

// R-7MEB-CUSI R-Q896-34E1
func TestNilClientUsesDefaultClientTransport(t *testing.T) {
	previousClientTransport := http.DefaultClient.Transport
	previousJar := http.DefaultClient.Jar
	previousTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultClient.Transport = previousClientTransport
		http.DefaultClient.Jar = previousJar
		http.DefaultTransport = previousTransport
	})
	calls := 0
	http.DefaultClient.Jar = &populatedJar{}
	http.DefaultTransport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("used DefaultTransport instead of DefaultClient.Transport")
		return nil, errors.New("offline")
	})
	http.DefaultClient.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		assertRequestHeaders(t, req.Header, methods()[calls-1].gateway)
		a := answer()
		a["id"] = decode(t, req.Body)["id"]
		return response(200, "application/json", encode(t, a)), nil
	})
	c := space.New(testSpace, testToken, nil)
	if calls != 0 {
		t.Fatal("New performed I/O")
	}
	for _, m := range methods() {
		if _, err := m.call(c, context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("default transport called %d times", calls)
	}
}

// R-TSN2-ABQ5 R-B31I-UI5N R-B49F-89WC R-B5HB-M1N1
func TestSuccessfulAnswers(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
				t.Run(ct, func(t *testing.T) {
					for _, latest := range []string{"2026-10-13T16:02:11.250+02:00", "2026-10-13T14:02:11.250Z"} {
						for _, extra := range []bool{false, true} {
							a := answer()
							entries(a)[0].(map[string]any)["latest"] = latest
							if extra {
								a["future"] = []any{1, "extra"}
								result(a)["future"] = true
								structured(a)["future"] = nil
								for _, entry := range entries(a) {
									entry.(map[string]any)["future"] = map[string]any{"x": false}
								}
							}
							c := space.New(testSpace, testToken, &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
								a["id"] = decode(t, req.Body)["id"]
								return response(200, ct, encode(t, a)), nil
							})})
							got, err := m.call(c, context.Background())
							want := []space.Held{{Ref: refs[0], Latest: time.Date(2026, 10, 13, 14, 2, 11, 250000000, time.UTC)}, {Ref: refs[1]}}
							if err != nil || !reflect.DeepEqual(got, want) || got[0].Latest.Location() != time.UTC {
								t.Fatalf("got %#v, %v; want %#v", got, err, want)
							}
						}
					}
				})
			}
		})
	}
}

// R-TSN2-ABQ5
func TestOnlyBooleanTrueIsToolFailure(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, flag := range []any{false, nil, "true", 0} {
				a := answer()
				result(a)["isError"] = flag
				c := space.New(testSpace, testToken, &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
					a["id"] = decode(t, req.Body)["id"]
					return response(200, "application/json", encode(t, a)), nil
				})})
				got, err := m.call(c, context.Background())
				want := []space.Held{{Ref: refs[0]}, {Ref: refs[1]}}
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("isError %#v: got %#v, %v", flag, got, err)
				}
			}
		})
	}
}

func assertFailure(t *testing.T, held []space.Held, err error, reason, detail string, quoted bool) {
	t.Helper()
	if held != nil {
		t.Fatalf("failure returned non-nil slice: %#v", held)
	}
	var failure *space.Error
	if !errors.As(err, &failure) || reflect.TypeOf(err) != reflect.TypeFor[*space.Error]() {
		t.Fatalf("error = %T; want *space.Error", err)
	}
	want := &space.Error{Reason: reason, Detail: detail, Quoted: quoted}
	if !reflect.DeepEqual(failure, want) {
		t.Fatalf("error = %#v; want %#v", failure, want)
	}
	if strings.Contains(failure.Reason, testToken) || strings.Contains(failure.Detail, testToken) {
		t.Fatal("credential leaked")
	}
}

// R-ALYX-HPRX R-BACX-54LT R-BCSP-WO37 R-TV2V-1V7J R-BK44-7AJD
func TestHTTPFailuresAndRedirects(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, status := range []int{401, 400, 403, 500, 204, 301, 302, 303, 307, 308} {
				t.Run(strconv.Itoa(status), func(t *testing.T) {
					for _, body := range []string{"", "unchanged\r\nbody\x00", `{"jsonrpc":"2.0","error":{"message":"refused"}}`, strings.Repeat("x", 5000)} {
						calls := 0
						hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
							t.Fatal("caller redirect policy must be overridden")
							return nil
						}, Transport: transportFunc(func(*http.Request) (*http.Response, error) {
							calls++
							res := response(status, "text/plain", body)
							res.Header.Set("Location", "https://other.invalid/target")
							return res, nil
						})}
						held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
						reason := "the space answered HTTP " + strconv.Itoa(status)
						if status == 401 {
							reason = "the space refused the token"
						}
						wantBody := body
						if len(wantBody) > 4096 {
							wantBody = wantBody[:4096]
						}
						assertFailure(t, held, err, reason, wantBody, true)
						if calls != 1 {
							t.Fatalf("sent %d requests", calls)
						}
					}
				})
			}
		})
	}
}

// R-ALYX-HPRX R-B6P7-ZTDQ R-BK44-7AJD
func TestTransportFailure(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			boom := errors.New("boom")
			hc := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, boom })}
			held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
			var failure *space.Error
			if !errors.As(err, &failure) {
				t.Fatalf("wrong error: %v", err)
			}
			// net/http wraps the transport's error with the request method and URL.
			detail := `Post "` + space.Endpoint(testSpace) + `": boom`
			assertFailure(t, held, err, "cannot reach "+space.Endpoint(testSpace), detail, false)
		})
	}
}

type failingReader struct {
	remaining string
	failure   error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.remaining != "" {
		n := copy(p, r.remaining)
		r.remaining = r.remaining[n:]
		return n, nil
	}
	return 0, r.failure
}
func (*failingReader) Close() error { return nil }

// R-ALYX-HPRX R-B7X4-DL4F R-BK44-7AJD
func TestBodyReadFailures(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, status := range []int{200, 401, 400, 302} {
				t.Run(strconv.Itoa(status), func(t *testing.T) {
					for _, prefix := range []string{"", "partial", strings.Repeat("a", 4095)} {
						hc := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
							res := response(status, "application/json", "")
							res.Body = &failingReader{remaining: prefix, failure: errors.New("body failed")}
							return res, nil
						})}
						held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
						assertFailure(t, held, err, "cannot reach "+space.Endpoint(testSpace), "body failed", false)
					}
				})
			}
		})
	}
}

// R-B7X4-DL4F
func TestNonSuccessReadStopsAtCap(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			prefix := strings.Repeat("z", 4096)
			hc := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				res := response(400, "text/plain", "")
				res.Body = &failingReader{remaining: prefix, failure: errors.New("must not read beyond cap")}
				return res, nil
			})}
			held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
			assertFailure(t, held, err, "the space answered HTTP 400", prefix, true)
		})
	}
}

// R-ALYX-HPRX R-B950-RCV4 R-BK44-7AJD
func TestCancellationWhileTransportWaits(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			hc := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				close(entered)
				<-req.Context().Done()
				return nil, req.Context().Err()
			})}
			type outcome struct {
				held []space.Held
				err  error
			}
			done := make(chan outcome, 1)
			go func() { held, err := m.call(space.New(testSpace, testToken, hc), ctx); done <- outcome{held, err} }()
			<-entered
			cancel()
			got := <-done
			detail := `Post "` + space.Endpoint(testSpace) + `": context canceled`
			assertFailure(t, got.held, got.err, "cannot reach "+space.Endpoint(testSpace), detail, false)
		})
	}
}

// R-ALYX-HPRX R-BE0M-AFTW R-TTUY-O3GU R-BK44-7AJD
func TestGatewayAndToolRefusals(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, long := range []bool{false, true} {
				message := "gateway refusal\nunchanged"
				if long {
					message = strings.Repeat("g", 5000)
				}
				a := answer()
				delete(a, "result")
				a["error"] = map[string]any{"message": message, "code": -32602}
				hc := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
					a["id"] = decode(t, req.Body)["id"]
					return response(200, "application/json", encode(t, a)), nil
				})}
				held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
				detail := message
				if len(detail) > 4096 {
					detail = detail[:4096]
				}
				assertFailure(t, held, err, "the gateway refused the request", detail, true)
				a = answer()
				result(a)["isError"] = true
				result(a)["structuredContent"] = "ignored even when malformed"
				result(a)["content"] = []any{
					map[string]any{"type": "text", "text": "first"},
					map[string]any{"type": "image", "text": "ignored"},
					map[string]any{"type": "text", "text": 99},
					nil,
					map[string]any{"type": "text", "text": ""},
					map[string]any{"type": "text", "text": message},
				}
				held, err = m.call(space.New(testSpace, testToken, hc), context.Background())
				detail = "first\n\n" + message
				if len(detail) > 4096 {
					detail = detail[:4096]
				}
				reason := "sessions refused the query"
				if m.name == "push" {
					reason = "sessions refused a batch"
				}
				assertFailure(t, held, err, reason, detail, true)
			}
		})
	}
}

type invalidCase struct {
	name   string
	mutate func(map[string]any)
}

// R-ALYX-HPRX R-BGGF-1ZBA R-BHOB-FR1Z R-BIW7-TISO R-BK44-7AJD
func TestUnreadableAnswers(t *testing.T) {
	envelopeCases := []invalidCase{
		{"missing-jsonrpc", func(a map[string]any) { delete(a, "jsonrpc") }},
		{"wrong-jsonrpc", func(a map[string]any) { a["jsonrpc"] = "1.0" }},
		{"missing-id", func(a map[string]any) { delete(a, "id") }},
		{"string-id", func(a map[string]any) { a["id"] = a["id"].(json.Number).String() }},
		{"wrong-id", func(a map[string]any) {
			n, err := strconv.ParseInt(a["id"].(json.Number).String(), 10, 64)
			if err != nil {
				panic(err)
			}
			a["id"] = n + 1
		}},
		{"nearby-numeric-id", func(a map[string]any) {
			a["id"] = json.Number(a["id"].(json.Number).String() + ".00000000000000000000000000000001")
		}},
		{"no-result-or-error", func(a map[string]any) { delete(a, "result") }},
		{"both-result-and-error", func(a map[string]any) { a["error"] = map[string]any{"message": "failure"} }},
		{"result-array", func(a map[string]any) { a["result"] = []any{} }},
		{"result-null", func(a map[string]any) { a["result"] = nil }},
		{"result-string", func(a map[string]any) { a["result"] = "bad" }},
		{"error-missing-message", func(a map[string]any) { delete(a, "result"); a["error"] = map[string]any{} }},
		{"error-number-message", func(a map[string]any) { delete(a, "result"); a["error"] = map[string]any{"message": 1} }},
		{"error-null-message", func(a map[string]any) { delete(a, "result"); a["error"] = map[string]any{"message": nil} }},
		{"error-not-object", func(a map[string]any) { delete(a, "result"); a["error"] = "bad" }},
	}
	resultCases := []invalidCase{
		{"missing-structured", func(a map[string]any) { delete(result(a), "structuredContent") }},
		{"structured-string", func(a map[string]any) { result(a)["structuredContent"] = "bad" }},
		{"structured-null", func(a map[string]any) { result(a)["structuredContent"] = nil }},
		{"missing-sessions", func(a map[string]any) { delete(structured(a), "sessions") }},
		{"sessions-object", func(a map[string]any) { structured(a)["sessions"] = map[string]any{} }},
		{"sessions-null", func(a map[string]any) { structured(a)["sessions"] = nil }},
		{"too-many", func(a map[string]any) {
			structured(a)["sessions"] = append(entries(a), map[string]any{"tool": "grok", "id": "three"})
		}},
		{"too-few", func(a map[string]any) { structured(a)["sessions"] = entries(a)[:1] }},
		{"entry-string", func(a map[string]any) { entries(a)[0] = "bad" }},
		{"entry-null", func(a map[string]any) { entries(a)[0] = nil }},
		{"missing-tool", func(a map[string]any) { delete(entries(a)[0].(map[string]any), "tool") }},
		{"missing-ref-id", func(a map[string]any) { delete(entries(a)[0].(map[string]any), "id") }},
		{"tool-number", func(a map[string]any) { entries(a)[0].(map[string]any)["tool"] = 1 }},
		{"ref-id-number", func(a map[string]any) { entries(a)[0].(map[string]any)["id"] = 1 }},
		{"tool-null", func(a map[string]any) { entries(a)[0].(map[string]any)["tool"] = nil }},
		{"ref-id-null", func(a map[string]any) { entries(a)[0].(map[string]any)["id"] = nil }},
		{"tool-mismatch", func(a map[string]any) { entries(a)[0].(map[string]any)["tool"] = "grok" }},
		{"ref-id-mismatch", func(a map[string]any) { entries(a)[0].(map[string]any)["id"] = "different" }},
		{"answers-swapped", func(a map[string]any) { e := entries(a); e[0], e[1] = e[1], e[0] }},
		{"latest-null", func(a map[string]any) { entries(a)[0].(map[string]any)["latest"] = nil }},
		{"latest-number", func(a map[string]any) { entries(a)[0].(map[string]any)["latest"] = 1 }},
		{"latest-date", func(a map[string]any) { entries(a)[0].(map[string]any)["latest"] = "2026-10-13" }},
	}
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, tc := range append(envelopeCases, resultCases...) {
				t.Run(tc.name, func(t *testing.T) {
					a := answer()
					var body string
					hc := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
						a["id"] = decode(t, req.Body)["id"]
						tc.mutate(a)
						body = encode(t, a)
						return response(200, "application/json", body), nil
					})}
					held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
					assertFailure(t, held, err, "the space's answer could not be read", body, true)
				})
			}
			for _, ct := range []string{"", "text/plain", "text/event-stream", "application/octet-stream"} {
				t.Run("content-type-"+ct, func(t *testing.T) {
					var body string
					hc := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
						a := answer()
						a["id"] = decode(t, req.Body)["id"]
						body = encode(t, a)
						return response(200, ct, body), nil
					})}
					held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
					assertFailure(t, held, err, "the space's answer could not be read", body, true)
				})
			}
			for _, body := range []string{"not JSON", "[]", `"string"`, "1", "true", "null", `{} {}`, strings.Repeat("z", 5000)} {
				t.Run("body-"+body[:min(8, len(body))], func(t *testing.T) {
					hc := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(200, "application/json", body), nil })}
					held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
					detail := body
					if len(detail) > 4096 {
						detail = detail[:4096]
					}
					assertFailure(t, held, err, "the space's answer could not be read", detail, true)
				})
			}
		})
	}
}
