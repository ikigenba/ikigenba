package gmail_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

const syntheticToken = "synthetic-access-value"

type call struct {
	method, path, query, body string
	headers                   http.Header
}
type reply struct {
	status  int
	body    string
	headers http.Header
}
type fake struct {
	mu       sync.Mutex
	requests []call
	replies  []reply
	server   *httptest.Server
}

func newFake(t *testing.T, replies ...reply) (*fake, *gmail.Client) {
	t.Helper()
	f := &fake{replies: replies}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		f.mu.Lock()
		f.requests = append(f.requests, call{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, string(body), r.Header.Clone()})
		i := len(f.requests) - 1
		response := reply{status: 500}
		if i < len(f.replies) {
			response = f.replies[i]
		}
		f.mu.Unlock()
		for k, vs := range response.headers {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		if response.status == 0 {
			response.status = 200
		}
		w.WriteHeader(response.status)
		if _, err = io.WriteString(w, response.body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(f.server.Close)
	return f, gmail.NewClient(gmail.Config{HTTP: f.server.Client(), BaseURL: f.server.URL})
}

func (f *fake) calls() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.requests...)
}
func okJSON(s string) reply { return reply{body: s} }
func labelsJSON() reply {
	return okJSON(`{"labels":[{"id":"INBOX","name":"Inbox","type":"system"},{"id":"custom","name":"Work","type":"user"},{"id":"UNREAD","name":"Unread","type":"system"}]}`)
}
func messageJSON(id string, millis int) string {
	return fmt.Sprintf(`{"id":%q,"threadId":"thread-one","internalDate":%q,"labelIds":["INBOX","UNREAD","missing"],"snippet":"a &amp; b","payload":{"headers":[{"name":"fRoM","value":"Sender <sender@synthetic.invalid>"},{"name":"From","value":"ignored"},{"name":"To","value":"recipient@synthetic.invalid"},{"name":"Cc","value":"copy@synthetic.invalid"},{"name":"Subject","value":"Synthetic subject"}],"parts":[{"filename":"one.bin","partId":"1","body":{"size":3}},{"parts":[{"parts":[{"filename":"two.bin","partId":"2.0.0","body":{"size":4}}]}]}]}}`, id, strconv.Itoa(millis))
}
func requireError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	count := 0
	for _, kind := range []error{gmail.ErrNotFound, gmail.ErrUnauthorized, gmail.ErrTooLarge, gmail.ErrBadPage, gmail.ErrPartSize, gmail.ErrProvider} {
		if errors.Is(err, kind) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("error matches %d categories: %v", count, err)
	}
}

func assertCall(t *testing.T, c call, method, path string, params map[string]string) {
	t.Helper()
	if c.method != method || c.path != path {
		t.Fatalf("request %s %s, want %s %s", c.method, c.path, method, path)
	}
	q, err := url.ParseQuery(c.query)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != len(params) {
		t.Fatalf("query %v, want %v", q, params)
	}
	for k, v := range params {
		if len(q[k]) != 1 || q[k][0] != v {
			t.Fatalf("query %v, want %v", q, params)
		}
	}
	if c.headers.Get("Authorization") != "Bearer "+syntheticToken {
		t.Fatalf("authorization %q", c.headers.Get("Authorization"))
	}
}

func TestDeclarations(t *testing.T) {
	// R-G8E4-G1PY R-G9M0-TTGN R-GATX-7L7C R-GC1T-LCY1
	// R-GD9P-Z4OQ R-GFPI-QO64 R-37EG-G3BC R-GKL4-9R4W R-5Q48-1PRA
	_ = gmail.Config{http.DefaultClient, "", nil}
	_ = gmail.Label{"id", "label", true}
	_ = gmail.Summary{"id", "thread", time.Time{}, "from", "to", "cc", "subject", "snippet", []string{}, true, 1, "link"}
	_ = gmail.Page{[]gmail.Summary{}, "next"}
	_ = gmail.Message{"id", "thread", time.Time{}, "from", "to", "cc", "subject", "snippet", []string{}, true, []gmail.Attachment{}, "link", "message-id", "reply", "reply-to", "refs", "text", true}
	_ = gmail.Attachment{"part", "name", "type", 1}
	q := gmail.Query{"query", "label", "page", 1}
	if q.Text != "query" || q.Label != "label" || q.Page != "page" || q.Limit != 1 {
		t.Fatalf("query fields %#v", q)
	}
	instant := time.UnixMilli(1234).UTC()
	stat := gmail.Stat{"id", "thread", instant, 7, true}
	if stat.ID != "id" || stat.Thread != "thread" || !stat.Date.Equal(instant) || stat.SizeEstimate != 7 || !stat.Sent {
		t.Fatalf("stat fields %#v", stat)
	}
	draft := gmail.Draft{"draft", "message", "thread"}
	if draft.ID != "draft" || draft.Message != "message" || draft.Thread != "thread" {
		t.Fatalf("draft fields %#v", draft)
	}
	reader := strings.NewReader("content")
	content := gmail.Content{7, reader}
	if content.Bytes != 7 || content.Reader != reader {
		t.Fatalf("content fields %#v", content)
	}
	if gmail.DefaultBaseURL != "https://gmail.googleapis.com" || gmail.PageSize != 25 || gmail.BodyCap != 65536 || gmail.UploadCap != 36700160 || gmail.PushChunk != 8<<20 || gmail.Called != "gmail.called" {
		t.Fatal("constants differ")
	}
	if got := []string{gmail.EndpointLabels, gmail.EndpointList, gmail.EndpointGet, gmail.EndpointAttachment, gmail.EndpointThread, gmail.EndpointCreate, gmail.EndpointDraft, gmail.EndpointUpdate, gmail.EndpointChunk, gmail.EndpointSend, gmail.EndpointDelete}; !reflect.DeepEqual(got, []string{"labels.list", "messages.list", "messages.get", "messages.attachments.get", "threads.get", "drafts.create", "drafts.get", "drafts.update", "upload.chunk", "drafts.send", "drafts.delete"}) {
		t.Fatal(got)
	}
	if gmail.SummaryFields != "id,threadId,labelIds,snippet,internalDate,payload(headers,filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename,parts(filename)))))))))" || gmail.ThreadFields != "messages("+gmail.SummaryFields+")" {
		t.Fatal("fields differ")
	}
	kinds := []error{gmail.ErrNotFound, gmail.ErrUnauthorized, gmail.ErrTooLarge, gmail.ErrBadPage, gmail.ErrPartSize, gmail.ErrProvider}
	for i, e := range kinds {
		if e == nil {
			t.Fatal("nil error")
		}
		for j, f := range kinds {
			if i != j && errors.Is(e, f) {
				t.Fatal("errors overlap")
			}
		}
	}
	_ = struct {
		Method func(*gmail.Client, context.Context, string) ([]gmail.Label, error)
	}{Method: (*gmail.Client).Labels}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, gmail.Query) (gmail.Page, error)
	}{Method: (*gmail.Client).Search}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string, string) (gmail.Page, error)
	}{Method: (*gmail.Client).Thread}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string) (gmail.Message, error)
	}{Method: (*gmail.Client).Show}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string) (gmail.Stat, error)
	}{Method: (*gmail.Client).Stat}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string) ([]gmail.Stat, error)
	}{Method: (*gmail.Client).ThreadStats}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string, string, int64) (gmail.Content, error)
	}{Method: (*gmail.Client).Attachment}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string, int64) (gmail.Content, error)
	}{Method: (*gmail.Client).Raw}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string, *gmail.Outgoing) (gmail.Draft, error)
	}{Method: (*gmail.Client).CreateDraft}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string) (gmail.Draft, error)
	}{Method: (*gmail.Client).GetDraft}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string, string, *gmail.Outgoing) (gmail.Draft, error)
	}{Method: (*gmail.Client).UpdateDraft}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string) (gmail.Draft, error)
	}{Method: (*gmail.Client).SendDraft}
	_ = struct {
		Method func(*gmail.Client, context.Context, string, string) error
	}{Method: (*gmail.Client).DeleteDraft}
}

func TestLabels(t *testing.T) {
	// R-GQOM-6LUD R-GO8T-F2CZ
	for _, s := range []string{labelsJSON().body, `{}`} {
		f, c := newFake(t, okJSON(s))
		ls, err := c.Labels(context.Background(), syntheticToken)
		if err != nil {
			t.Fatal(err)
		}
		assertCall(t, f.calls()[0], "GET", "/gmail/v1/users/me/labels", nil)
		if s == `{}` {
			if len(ls) != 0 {
				t.Fatal(ls)
			}
			continue
		}
		if !reflect.DeepEqual(ls, []gmail.Label{{"INBOX", "Inbox", true}, {"custom", "Work", false}, {"UNREAD", "Unread", true}}) {
			t.Fatal(ls)
		}
	}
}

func TestSearch(t *testing.T) {
	// R-29FQ-KLVZ R-FYYQ-C9XO R-4MZO-A1QM R-GZ7W-V018 R-4O7K-NTHB
	f, c := newFake(t, labelsJSON(), okJSON(`{"messages":[{"id":"first"},{"id":"vanished"},{"id":"third"}],"nextPageToken":"opaque-next"}`), okJSON(messageJSON("first", 1234)), reply{status: 404}, okJSON(messageJSON("third", 2345)))
	p, err := c.Search(context.Background(), syntheticToken, gmail.Query{Limit: gmail.PageSize, Text: `from:(synthetic) "quoted"`, Label: "wOrK", Page: "opaque-current"})
	if err != nil {
		t.Fatal(err)
	}
	cs := f.calls()
	if len(cs) != 5 {
		t.Fatal(cs)
	}
	assertCall(t, cs[0], "GET", "/gmail/v1/users/me/labels", nil)
	assertCall(t, cs[1], "GET", "/gmail/v1/users/me/messages", map[string]string{"maxResults": "25", "includeSpamTrash": "false", "q": `from:(synthetic) "quoted"`, "labelIds": "custom", "pageToken": strings.Join([]string{"opaque", "current"}, "-")})
	for i, id := range []string{"first", "vanished", "third"} {
		assertCall(t, cs[i+2], "GET", "/gmail/v1/users/me/messages/"+id, map[string]string{"format": "full", "fields": gmail.SummaryFields})
	}
	if p.Next != "opaque-next" || len(p.Messages) != 2 || p.Messages[1].ID != "third" {
		t.Fatal(p)
	}
	s := p.Messages[0]
	want := gmail.Summary{ID: "first", Thread: "thread-one", Date: time.UnixMilli(1234).UTC(), From: "Sender <sender@synthetic.invalid>", To: "recipient@synthetic.invalid", Cc: "copy@synthetic.invalid", Subject: "Synthetic subject", Snippet: "a & b", Labels: []string{"Inbox", "Unread", "missing"}, Unread: true, Attachments: 2, Link: urls.MessageLink("first")}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("summary %#v, want %#v", s, want)
	}
	for _, input := range []struct {
		label, list string
		want        error
	}{{"unknown-label", "", gmail.ErrNotFound}, {"", `{}`, nil}} {
		f, c := newFake(t, labelsJSON(), okJSON(input.list))
		p, err := c.Search(context.Background(), syntheticToken, gmail.Query{Limit: gmail.PageSize, Label: input.label})
		if input.want != nil {
			requireError(t, err, input.want)
			if len(f.calls()) != 1 || !reflect.DeepEqual(p, gmail.Page{}) {
				t.Fatal(p, f.calls())
			}
			continue
		}
		if err != nil || len(p.Messages) != 0 || p.Next != "" {
			t.Fatal(p, err)
		}
		assertCall(t, f.calls()[1], "GET", "/gmail/v1/users/me/messages", map[string]string{"maxResults": "25", "includeSpamTrash": "false"})
	}
}

func TestSearchQuery(t *testing.T) {
	// R-38MC-TV21
	for limit := 1; limit <= paging.MaxLimit; limit++ {
		for options := 0; options < 8; options++ {
			f, c := newFake(t, labelsJSON(), okJSON(`{}`))
			q := gmail.Query{Limit: limit}
			params := map[string]string{"maxResults": strconv.Itoa(limit), "includeSpamTrash": "false"}
			if options&1 != 0 {
				q.Text = "  from:(synthetic) \"quoted\" + & % é\n"
				params["q"] = q.Text
			}
			if options&2 != 0 {
				q.Label = "wOrK"
				params["labelIds"] = "custom"
			}
			if options&4 != 0 {
				q.Page = "opaque +/&=%é"
				params["pageToken"] = q.Page
			}
			if _, err := c.Search(context.Background(), syntheticToken, q); err != nil {
				t.Fatalf("limit %d options %d: %v", limit, options, err)
			}
			calls := f.calls()
			if len(calls) != 2 {
				t.Fatalf("limit %d options %d: requests %v", limit, options, calls)
			}
			assertCall(t, calls[0], "GET", "/gmail/v1/users/me/labels", nil)
			assertCall(t, calls[1], "GET", "/gmail/v1/users/me/messages", params)
		}
	}
}

func TestSearchRejectsInvalidLimit(t *testing.T) {
	// R-39U9-7MSQ
	for _, limit := range []int{-100, -1, 0, paging.MaxLimit + 1, paging.MaxLimit * 2} {
		f, c := newFake(t)
		_, err := c.Search(context.Background(), syntheticToken, gmail.Query{Text: "query", Label: "Work", Page: "opaque", Limit: limit})
		requireError(t, err, gmail.ErrProvider)
		if calls := f.calls(); len(calls) != 0 {
			t.Fatalf("limit %d: requests %v", limit, calls)
		}
	}
}

func TestReadStopsOnFailures(t *testing.T) {
	// R-29FQ-KLVZ R-287U-6U5A R-5TRX-70ZD
	for _, name := range []string{"search", "thread", "show"} {
		f, c := newFake(t, reply{status: 401})
		var err error
		switch name {
		case "search":
			p, e := c.Search(context.Background(), syntheticToken, gmail.Query{Limit: gmail.PageSize})
			err = e
			if !reflect.DeepEqual(p, gmail.Page{}) {
				t.Fatal(p)
			}
		case "thread":
			p, e := c.Thread(context.Background(), syntheticToken, "thread", "")
			err = e
			if !reflect.DeepEqual(p, gmail.Page{}) {
				t.Fatal(p)
			}
		case "show":
			m, e := c.Show(context.Background(), syntheticToken, "message")
			err = e
			if !reflect.DeepEqual(m, gmail.Message{}) {
				t.Fatal(m)
			}
		}
		requireError(t, err, gmail.ErrUnauthorized)
		if len(f.calls()) != 1 {
			t.Fatal(f.calls())
		}
	}
	for _, status := range []int{400, 401, 403, 413, 500} {
		f, c := newFake(t, labelsJSON(), okJSON(`{"messages":[{"id":"first"},{"id":"later"}]}`), reply{status: status})
		p, err := c.Search(context.Background(), syntheticToken, gmail.Query{Limit: gmail.PageSize})
		want := gmail.ErrProvider
		switch status {
		case 400:
			want = gmail.ErrNotFound
		case 401:
			want = gmail.ErrUnauthorized
		case 413:
			want = gmail.ErrTooLarge
		}
		requireError(t, err, want)
		if len(f.calls()) != 3 || !reflect.DeepEqual(p, gmail.Page{}) {
			t.Fatal(p, f.calls())
		}
	}
}

func TestThread(t *testing.T) {
	// R-H2VM-0B9B R-4PFH-1L80
	for _, page := range []string{"0", "01", "+1", "-1", "one", "1.0", " 1", "9999999999999999999999999"} {
		f, c := newFake(t)
		p, err := c.Thread(context.Background(), syntheticToken, "thread", page)
		requireError(t, err, gmail.ErrBadPage)
		if len(f.calls()) != 0 || !reflect.DeepEqual(p, gmail.Page{}) {
			t.Fatal(p)
		}
	}
	var messages []string
	for i := 0; i < 28; i++ {
		messages = append(messages, messageJSON(fmt.Sprintf("m-%02d", i), 28-i))
	}
	body := `{"messages":[` + strings.Join(messages, ",") + `]}`
	for _, page := range []string{"", "25", "28", "99"} {
		f, c := newFake(t, labelsJSON(), okJSON(body))
		p, err := c.Thread(context.Background(), syntheticToken, "thread", page)
		if err != nil {
			t.Fatal(err)
		}
		assertCall(t, f.calls()[1], "GET", "/gmail/v1/users/me/threads/thread", map[string]string{"format": "full", "fields": gmail.ThreadFields})
		switch page {
		case "":
			if len(p.Messages) != 25 || p.Next != "25" || p.Messages[0].ID != "m-27" || p.Messages[24].ID != "m-03" {
				t.Fatal(p)
			}
		case "25":
			if len(p.Messages) != 3 || p.Next != "" || p.Messages[0].ID != "m-02" {
				t.Fatal(p)
			}
		default:
			if len(p.Messages) != 0 || p.Next != "" {
				t.Fatal(p)
			}
		}
	}
	_, c := newFake(t, labelsJSON(), okJSON(`{"messages":[`+messageJSON("equal-first", 3)+`,`+messageJSON("earlier", 1)+`,`+messageJSON("equal-last", 3)+`]}`))
	p, err := c.Thread(context.Background(), syntheticToken, "thread", "")
	if err != nil || p.Messages[1].ID != "equal-first" || p.Messages[2].ID != "equal-last" {
		t.Fatal(p, err)
	}
}

func TestShow(t *testing.T) {
	// R-4QND-FCYP R-H6JB-5MHE R-H7R7-JE83
	body := `{"id":"message","threadId":"thread","internalDate":"7","payload":{"filename":"root.bin","partId":"root","mimeType":"binary/root","body":{"size":9},"headers":[{"name":"Message-ID","value":"synthetic-message"},{"name":"In-Reply-To","value":"synthetic-parent"},{"name":"Reply-To","value":"reply@synthetic.invalid"},{"name":"References","value":"synthetic-refs"}],"parts":[{"filename":"attached.txt","mimeType":"text/plain","partId":"1","body":{"size":8,"data":"YXR0YWNoZWQ="}},{"mimeType":"text/html","body":{"data":"PHA-aHRtbDwvcD4="}},{"mimeType":"TEXT/PLAIN","body":{"data":"cGxhaW4NCm5leHQ="}},{"filename":"inline.png","mimeType":"image/png","partId":"2.1","body":{"size":4}},{"mimeType":"text/plain","body":{"data":"aWdub3JlZA=="}}]}}`
	f, c := newFake(t, labelsJSON(), okJSON(body))
	m, err := c.Show(context.Background(), syntheticToken, "message")
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, f.calls()[1], "GET", "/gmail/v1/users/me/messages/message", map[string]string{"format": "full"})
	if m.ID != "message" || m.Thread != "thread" || !m.Date.Equal(time.UnixMilli(7)) || m.Link != urls.MessageLink("message") || m.MessageID != "synthetic-message" || m.InReplyTo != "synthetic-parent" || m.ReplyTo != "reply@synthetic.invalid" || m.References != "synthetic-refs" || m.Body != "plain\nnext" || m.Truncated {
		t.Fatal(m)
	}
	if !reflect.DeepEqual(m.Attachments, []gmail.Attachment{{"root", "root.bin", "binary/root", 9}, {"1", "attached.txt", "text/plain", 8}, {"2.1", "inline.png", "image/png", 4}}) {
		t.Fatal(m.Attachments)
	}
	for _, tc := range []struct {
		data, mime, want string
		cap              bool
	}{{"a\r\nb\xffc", "text/plain", "a\nb�c", false}, {"<p>HTML &amp; body</p>", "text/html", "HTML & body", false}, {strings.Repeat("a", gmail.BodyCap-1) + "é" + "x", "text/plain", strings.Repeat("a", gmail.BodyCap-1), true}} {
		data := base64.RawURLEncoding.EncodeToString([]byte(tc.data))
		_, c := newFake(t, labelsJSON(), okJSON(fmt.Sprintf(`{"id":"message","payload":{"mimeType":%q,"body":{"data":%q}}}`, tc.mime, data)))
		m, err := c.Show(context.Background(), syntheticToken, "message")
		if err != nil || m.Body != tc.want || m.Truncated != tc.cap {
			t.Fatalf("body mismatch: %d %v %v", len(m.Body), m.Truncated, err)
		}
	}
	_, c = newFake(t, labelsJSON(), okJSON(`{"id":"message"}`))
	m, err = c.Show(context.Background(), syntheticToken, "message")
	if err != nil || m.Body != "" || m.Truncated || len(m.Attachments) != 0 {
		t.Fatal(m, err)
	}
}

func TestReduceHTML(t *testing.T) {
	// R-GJD7-VZE7 R-H8Z3-X5YS
	for _, tc := range []struct{ in, want string }{{`<html><head><style>p{}</style></head><body><p>Hello &amp;  welcome</p><p>a<br>b</p><script>x()</script></body></html>`, "Hello & welcome\n\na\nb"}, {"a < b", "a < b"}, {"a<!--hidden-->b", "ab"}, {"a<!--unterminated", "a"}, {"a<ScRiPt>x()</sCrIpT>b<STYLE>css</STYLE>c", "abc"}, {"a<style>unfinished", "a"}, {" a\t\r\n\fb <span title='x'> c </span>", "a b  c"}, {"<div>a</div><div></div><p>b</p>", "a\n\nb"}, {"a<unknown", "a"}, {"a<br>b<BR/>c", "a\nb\nc"}, {"&lt;tag&gt;&nbsp;x", "<tag>\u00a0x"}} {
		if got := gmail.ReduceHTML(tc.in); got != tc.want {
			t.Fatalf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tag := range strings.Fields("address article aside blockquote br dd div dl dt footer h1 h2 h3 h4 h5 h6 header hr li nav ol p pre section table td th tr ul") {
		if got := gmail.ReduceHTML("a<" + tag + ">b</" + tag + ">c"); got != "a\nb\nc" {
			t.Fatal(tag, got)
		}
	}
}

func TestHTMLRemovalBeforeWhitespace(t *testing.T) {
	// R-H8Z3-X5YS R-H7R7-JE83 R-4QND-FCYP
	for _, tc := range []struct{ in, want string }{
		{`<script>secret</scriptx>LEAK</script>`, ""},
		{`a <ScRiPt>secret</script-x>LEAK</sCrIpT> b`, "a b"},
		{`a <style>secret</stylex>LEAK</style> b`, "a b"},
		{`a <!--hidden--> b`, "a b"},
		{`a <style>x</style> b`, "a b"},
		{`a <script>x</script> b`, "a b"},
		{`a <script>x</scriptx>LEAK`, "a"},
		{`a<script>İ</script>b`, "ab"},
		{`a<script>secret</scrİpt>LEAK</script>b`, "ab"},
		{`a<scrİpt>visible</scrİpt>b`, "avisibleb"},
		{`a<script>secret</script:x>LEAK</script>b`, "ab"},
		{`a<script:x>visible</script:x>b`, "avisibleb"},
		{`<script-x>visible</script-x>`, "visible"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := gmail.ReduceHTML(tc.in); got != tc.want {
				t.Fatalf("reduced %q, want %q", got, tc.want)
			}
			data := base64.RawURLEncoding.EncodeToString([]byte(tc.in))
			_, c := newFake(t, labelsJSON(), okJSON(fmt.Sprintf(`{"id":"message","payload":{"mimeType":"text/html","body":{"data":%q}}}`, data)))
			m, err := c.Show(context.Background(), syntheticToken, "message")
			if err != nil || m.Body != tc.want || m.Truncated {
				t.Fatal(m, err)
			}
		})
	}
}

func TestStats(t *testing.T) {
	// R-4RV9-T4PE R-X4HJ-6DNB
	f, c := newFake(t, okJSON(`{"id":"message","threadId":"thread","internalDate":"3001","sizeEstimate":55,"labelIds":["SENT"]}`))
	s, err := c.Stat(context.Background(), syntheticToken, "message")
	if err != nil || !reflect.DeepEqual(s, gmail.Stat{"message", "thread", time.UnixMilli(3001).UTC(), 55, true}) {
		t.Fatal(s, err)
	}
	assertCall(t, f.calls()[0], "GET", "/gmail/v1/users/me/messages/message", map[string]string{"format": "minimal"})
	for _, body := range []string{`{}`, `{"messages":[{"id":"one","internalDate":"2","sizeEstimate":999,"labelIds":["SENT"]},{"id":"two","internalDate":"1"}]}`} {
		f, c := newFake(t, okJSON(body))
		stats, err := c.ThreadStats(context.Background(), syntheticToken, "thread")
		if err != nil || stats == nil {
			t.Fatal(stats, err)
		}
		assertCall(t, f.calls()[0], "GET", "/gmail/v1/users/me/threads/thread", map[string]string{"format": "full", "fields": "messages(id,threadId,labelIds,internalDate)"})
		if body == `{}` {
			if len(stats) != 0 {
				t.Fatal(stats)
			}
			continue
		}
		if len(stats) != 2 || stats[0].ID != "one" || !stats[0].Sent || stats[0].SizeEstimate != 0 || stats[1].ID != "two" || stats[1].Sent {
			t.Fatal(stats)
		}
	}
}
