package tools_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
)

func typedServer(t *testing.T) *mcp.Server {
	t.Helper()
	w := telemetry.New(telemetry.Config{Service: "gmail", Sink: &telemetry.Capture{}, Now: func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }, Rand: &typedRandom{}, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	return mcp.NewServer(mcp.ServerConfig{Name: "gmail", Telemetry: w})
}
func typedEqual(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v want %#v", a, b)
	}
}
func typedClient(t *testing.T, s *mcp.Server) *mcp.Client {
	t.Helper()
	h := httptest.NewServer(identity.Require(s))
	t.Cleanup(h.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: h.URL, HTTPClient: h.Client()})
}
func typedCall(t *testing.T, c *mcp.Client, name string, a any) json.RawMessage {
	t.Helper()
	b, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.CallTool(context.Background(), identity.Caller{UserID: "owner"}, name, b)
	if e != nil {
		t.Fatal(e)
	}
	if r.IsError() {
		t.Fatalf("error %v", r)
	}
	wire, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if e = json.Unmarshal(wire, &v); e != nil {
		t.Fatal(e)
	}
	return v.Structured
}

// R-XZXU-T34I R-YI8C-JN8X R-YJG8-XEZM R-YC4U-MSJG
func TestTypedCatalog(t *testing.T) {
	t.Setenv(services.Variable, "")
	cfg := tools.Config{nil, nil, nil, nil}
	s := typedServer(t)
	tools.Register(s, cfg)
	cat := tools.Catalog()
	want := []tools.Entry{{"search", tools.DescribeSearch}, {"thread", tools.DescribeThread}, {"show", tools.DescribeShow}, {"labels", tools.DescribeLabels}, {"pull", tools.DescribePull}, {"compose", tools.DescribeCompose}, {"push", tools.DescribePush}, {"send", tools.DescribeSend}, {"discard", tools.DescribeDiscard}, {"drafts", tools.DescribeDrafts}, {"draft", tools.DescribeDraft}, {"transfers", tools.DescribeTransfers}, {"transfer", tools.DescribeTransfer}}
	typedEqual(t, cat, want)
	info, e := typedClient(t, s).ListTools(context.Background(), identity.Caller{UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	if len(info) != len(want) {
		t.Fatal(info)
	}
	seen := map[string]bool{tools.MailGuide: true}
	if tools.MailGuide == "" || strings.Contains(tools.MailGuide, "\n") {
		t.Fatal("guide")
	}
	effects := []mcp.Effect{mcp.Read, mcp.Read, mcp.Read, mcp.Read, mcp.Destructive, mcp.Additive, mcp.Destructive, mcp.Destructive, mcp.Destructive, mcp.Read, mcp.Read, mcp.Read, mcp.Read}
	for i, v := range info {
		typedEqual(t, v.Name, want[i].Name)
		typedEqual(t, v.Description, want[i].Description+"\n\n"+tools.MailGuide)
		typedEqual(t, v.Effect(), effects[i])
		d := cat[i].Description
		if seen[d] || strings.Contains(d, "\n") || strings.Contains(d, ". ") || len([]rune(d)) > 120 || !strings.HasSuffix(d, ".") || strings.TrimSpace(d) != d {
			t.Fatal("description", d)
		}
		seen[d] = true
	}
}

// R-MP8E-M8U1
func TestTypedArguments(t *testing.T) {
	t.Setenv(services.Variable, "")
	s := typedServer(t)
	tools.Register(s, tools.Config{})
	info, e := typedClient(t, s).ListTools(context.Background(), identity.Caller{UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	props := [][]string{{"query", "label", "page"}, {"thread", "page"}, {"message"}, {}, {"message", "path", "attachment", "expires"}, {"to", "cc", "bcc", "subject", "body", "reply_to"}, {"path", "draft"}, {"draft"}, {"draft"}, {}, {"draft"}, {}, {"transfer"}}
	required := [][]string{{"query"}, {"thread"}, {"message"}, {}, {"message", "path"}, {}, {"path", "draft"}, {"draft"}, {"draft"}, {}, {"draft"}, {}, {"transfer"}}
	for i, v := range info {
		var schema struct {
			Properties json.RawMessage `json:"properties"`
			Required   []string        `json:"required"`
		}
		if e = json.Unmarshal(v.InputSchema, &schema); e != nil {
			t.Fatal(e)
		}
		var names []string
		rawProperties := schema.Properties
		if len(rawProperties) == 0 {
			rawProperties = json.RawMessage(`{}`)
		}
		dec := json.NewDecoder(strings.NewReader(string(rawProperties)))
		if _, e = dec.Token(); e != nil {
			t.Fatal(e)
		}
		for dec.More() {
			n, e := dec.Token()
			if e != nil {
				t.Fatal(e)
			}
			names = append(names, n.(string))
			var raw json.RawMessage
			if e = dec.Decode(&raw); e != nil {
				t.Fatal(e)
			}
		}
		typedEqual(t, append([]string{}, names...), props[i])
		typedEqual(t, append([]string{}, schema.Required...), required[i])
	}
	_ = tools.NoArgs{}
	_ = tools.SearchArgs{"", "", ""}
	_ = tools.ThreadArgs{"", ""}
	_ = tools.ShowArgs{""}
	_ = tools.PullArgs{"", "", "", nil}
	_ = tools.ComposeArgs{nil, nil, nil, nil, "", ""}
	_ = tools.PushArgs{"", ""}
	_ = tools.DraftArgs{""}
	_ = tools.TransferArgs{""}
}

// R-YDCR-0KA5
func TestTypedRefusalCopy(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []string{tools.RefuseNotConnected, tools.RefuseReconnect, tools.RefuseNotFound, tools.RefuseTooLarge, tools.RefuseProvider, tools.RefusePath, tools.RefuseExpires, tools.RefuseUnavailable, tools.RefuseStopping, tools.RefuseQueueFull, tools.RefuseNotOpen, tools.RefuseBusy, tools.RefuseTo, tools.RefuseAddress} {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal("copy", s)
		}
		seen[s] = true
	}
}

// R-Y61C-PXTZ R-MRO7-DSBF R-Y8H5-HHBD R-YAWY-90SR
func TestTypedConversions(t *testing.T) {
	at := time.Date(2025, 2, 3, 4, 5, 6, 0, time.FixedZone("zone", 3600))
	formatted := at.UTC().Format(time.RFC3339)
	modified := at.Add(time.Hour)
	sentAt := at.Add(2 * time.Hour)
	modifiedText, sentText := modified.UTC().Format(time.RFC3339), sentAt.UTC().Format(time.RFC3339)
	s := gmail.Summary{ID: "message", Thread: "thread", Date: at, From: "from", To: "to", Cc: "cc", Subject: "subject", Snippet: "snippet", Labels: []string{"one", "two"}, Unread: true, Attachments: 9, Link: "link"}
	typedEqual(t, tools.SummaryOf(s), tools.Summary{"message", "thread", formatted, "from", "to", "cc", "subject", "snippet", []string{"one", "two"}, true, 9, "link"})
	m := gmail.Message{ID: "message", Thread: "thread", Date: at, From: "from", To: "to", Cc: "cc", Subject: "subject", Snippet: "snippet", Labels: []string{"one", "two"}, Unread: true, Attachments: []gmail.Attachment{{ID: "part", Name: "name", MimeType: "mime", Bytes: 5}, {ID: "second-part", Name: "second-name", MimeType: "second-mime", Bytes: 11}}, Link: "link", MessageID: "header", InReplyTo: "reply", ReplyTo: "private", References: "private-ref", Body: "body", Truncated: true}
	typedEqual(t, tools.MessageOf(m), tools.Message{"message", "thread", formatted, "from", "to", "cc", "subject", "snippet", []string{"one", "two"}, true, "link", "header", "reply", "body", true, []tools.Part{{"part", "name", "mime", 5}, {"second-part", "second-name", "second-mime", 11}}})
	typedEqual(t, tools.LabelOf(gmail.Label{ID: "private-id", Name: "label", System: true}), tools.Label{"label", true})
	d := store.Draft{ID: "local", GmailDraft: "remote", Message: "message", Thread: "thread", Status: store.DraftOpen, To: []string{"one", "two"}, Cc: []string{"cc", "second-cc"}, Bcc: []string{"bcc", "second-bcc"}, Subject: "subject", Body: "body", ReplyTo: "reply", Attachments: []store.Attachment{{Path: "path", Name: "name", ContentType: "mime", Bytes: 5}, {Path: "second-path", Name: "second-name", ContentType: "second-mime", Bytes: 11}}, Link: "link", Created: at, Modified: modified, Sent: sentAt}
	remote, reply, link, body := "remote", "reply", "link", "body"
	typedEqual(t, tools.DraftOf(d, true), tools.Draft{"local", &remote, "message", "thread", store.DraftOpen, []string{"one", "two"}, []string{"cc", "second-cc"}, []string{"bcc", "second-bcc"}, "subject", &reply, []tools.Attachment{{"path", "name", "mime", 5}, {"second-path", "second-name", "second-mime", 11}}, &link, formatted, modifiedText, &sentText, &body})
	if tools.DraftOf(d, false).Body != nil {
		t.Fatal("body present when excluded")
	}
	d.GmailDraft = ""
	d.ReplyTo = ""
	d.Link = ""
	d.Sent = time.Time{}
	d.Body = ""
	d.To = nil
	d.Cc = nil
	d.Bcc = nil
	d.Attachments = nil
	v := tools.DraftOf(d, false)
	if v.Draft != nil || v.ReplyTo != nil || v.Link != nil || v.Sent != nil || v.Body != nil || v.To == nil || v.Cc == nil || v.Bcc == nil || v.Attachments == nil {
		t.Fatal(v)
	}
	typedEqual(t, *tools.DraftOf(d, true).Body, "")
	tr := store.Transfer{ID: "id", Direction: "pull", Status: "done", Reason: "reason", Message: "message", Attachment: "part", Draft: "draft", Name: "name", Path: "path", ContentType: "mime", Bytes: 6, Started: at, Finished: sentAt, URL: "url", Link: "link"}
	reason, part, draft, url := "reason", "part", "draft", "url"
	typedEqual(t, tools.RecordOf(tr), tools.Record{"id", "pull", "done", &reason, "message", &part, &draft, "name", "path", "mime", 6, formatted, &sentText, &url, &link})
	tr.Reason = ""
	tr.Attachment = ""
	tr.Draft = ""
	tr.URL = ""
	tr.Link = ""
	tr.Finished = time.Time{}
	r := tools.RecordOf(tr)
	if r.Reason != nil || r.Attachment != nil || r.Draft != nil || r.URL != nil || r.Link != nil || r.Finished != nil {
		t.Fatal(r)
	}
	if tools.SummaryOf(gmail.Summary{}).Labels == nil || tools.MessageOf(gmail.Message{}).Labels == nil || tools.MessageOf(gmail.Message{}).Attachments == nil {
		t.Fatal("nil lists")
	}
}
func typedEncoding[O any](t *testing.T, v O, want string) {
	t.Helper()
	t.Setenv(services.Variable, "")
	s := typedServer(t)
	mcp.AddTool(s, mcp.Tool[tools.NoArgs, O]{Name: "probe", Description: tools.DescribeShow, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, tools.NoArgs) (O, error) { return v, nil }})
	typedEqual(t, string(typedCall(t, typedClient(t, s), "probe", tools.NoArgs{})), want)
}

// R-MQGB-00KQ
func TestTypedMailEncoding(t *testing.T) {
	typedEncoding(t, tools.Summary{}, `{"id":"","thread":"","date":"","from":"","to":"","cc":"","subject":"","snippet":"","labels":[],"unread":false,"attachments":0,"link":""}`)
	typedEncoding(t, tools.Part{}, `{"id":"","name":"","mime_type":"","bytes":0}`)
	typedEncoding(t, tools.Message{}, `{"id":"","thread":"","date":"","from":"","to":"","cc":"","subject":"","snippet":"","labels":[],"unread":false,"link":"","message_id":"","in_reply_to":"","body":"","truncated":false,"attachments":[]}`)
	typedEncoding(t, tools.Label{}, `{"name":"","system":false}`)
	typedEncoding(t, tools.MessageList{}, `{"messages":[]}`)
	next := "page"
	typedEncoding(t, tools.MessageList{Next: &next}, `{"messages":[],"next":"page"}`)
	typedEncoding(t, tools.LabelList{}, `{"labels":[]}`)
}

// R-Y3LJ-YECL
func TestTypedDraftEncoding(t *testing.T) {
	typedEncoding(t, tools.Attachment{}, `{"path":"","name":"","content_type":"","bytes":0}`)
	typedEncoding(t, tools.Draft{}, `{"id":"","message":"","thread":"","status":"","to":[],"cc":[],"bcc":[],"subject":"","attachments":[],"created":"","modified":""}`)
	x := "x"
	typedEncoding(t, tools.Draft{Draft: &x, ReplyTo: &x, Link: &x, Sent: &x, Body: &x}, `{"id":"","draft":"x","message":"","thread":"","status":"","to":[],"cc":[],"bcc":[],"subject":"","reply_to":"x","attachments":[],"link":"x","created":"","modified":"","sent":"x","body":"x"}`)
	typedEncoding(t, tools.DraftList{}, `{"drafts":[]}`)
}

// R-Y4TG-C63A
func TestTypedTransferEncoding(t *testing.T) {
	typedEncoding(t, tools.Record{}, `{"id":"","direction":"","status":"","message":"","name":"","path":"","content_type":"","bytes":0,"started":""}`)
	x := "x"
	typedEncoding(t, tools.Record{Reason: &x, Attachment: &x, Draft: &x, Finished: &x, URL: &x, Link: &x}, `{"id":"","direction":"","status":"","reason":"x","message":"","attachment":"x","draft":"x","name":"","path":"","content_type":"","bytes":0,"started":"","finished":"x","url":"x","link":"x"}`)
	typedEncoding(t, tools.TransferList{}, `{"transfers":[]}`)
}
