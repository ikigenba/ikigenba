package drafts_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

func TestPublicAPI(t *testing.T) {
	// R-JK61-IS9V R-JLDX-WK0K
	f := fixture(t)
	svc := drafts.New(drafts.Config{f.st, f.cfg.Gmail, f.cfg.Connections, f.writer, f.emitter, f.cfg.ConnectURL})
	var service transfers.Drafts = svc
	subject := "chosen-subject"
	r := drafts.ComposeRequest{[]string{"Display <recipient@example.invalid>"}, []string{"cc@example.invalid"}, []string{"bcc@example.invalid"}, &subject, "synthetic-composition", ""}
	d, e := svc.Compose(f.ctx, f.caller, r)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(d.To, []string{"Display <recipient@example.invalid>"}) || !reflect.DeepEqual(d.Cc, []string{"cc@example.invalid"}) || !reflect.DeepEqual(d.Bcc, []string{"bcc@example.invalid"}) || d.Subject != subject || d.Body != "synthetic-composition" || d.ReplyTo != "" {
		t.Fatal(d)
	}
	if _, e = f.svc.Get(f.ctx, f.caller, d.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = service.Attachable(f.ctx, f.caller, f.token, d.ID, store.Attachment{Name: "part", Bytes: 1}); e != nil {
		t.Fatal(e)
	}
}

func TestTokenFirstAndNoMutation(t *testing.T) {
	// R-JRHF-TEQ1 R-JWD1-CHOT R-69MM-61ME R-676T-EI50 R-JP1N-1V8N
	for _, which := range []string{"compose", "send", "discard"} {
		for _, status := range []int{404, 409, 503} {
			t.Run(which+http.StatusText(status), func(t *testing.T) {
				f := fixture(t)
				f.mu.Lock()
				f.tokenStatus = status
				word, url := transfers.RefusalNotConnected, f.cfg.ConnectURL
				f.tokenBody = `{"error":"not_connected"}`
				switch status {
				case 409:
					word, url = transfers.RefusalReconnect, "http://repair.example.invalid"
					f.tokenBody = `{"error":"reconnect","url":"http://repair.example.invalid"}`
				case 503:
					word, url = transfers.RefusalUnavailable, ""
					f.tokenBody = `{"error":"unavailable"}`
				}
				f.mu.Unlock()
				f.db.SetFailing(true)
				var d store.Draft
				var e error
				if which == "compose" {
					d, e = f.svc.Compose(f.ctx, f.caller, drafts.ComposeRequest{Cc: []string{"invalid"}})
				} else {
					d, e = operation(f, which, "unknown")
				}
				refused(t, d, e, word, url)
				if len(f.snapshot()) != 0 {
					t.Fatal(f.snapshot())
				}
				f.noEvents(t)
			})
		}
	}
}

func TestComposeRefusals(t *testing.T) {
	// R-JSPC-76GQ R-JTX8-KY7F R-JWD1-CHOT R-676T-EI50
	for _, r := range []drafts.ComposeRequest{{}, {Cc: []string{"invalid"}}, {To: []string{"bare-name"}}, {To: []string{"first@example.invalid, second@example.invalid"}}, {To: []string{""}}, {To: []string{"ok@example.invalid"}, Cc: []string{"bad"}}, {To: []string{"ok@example.invalid"}, Bcc: []string{"bad"}}, {ReplyTo: "original", To: []string{"bad"}}} {
		f := fixture(t)
		got, e := f.svc.Compose(f.ctx, f.caller, r)
		word := transfers.RefusalAddress
		if len(r.To) == 0 && r.ReplyTo == "" {
			word = transfers.RefusalTo
		}
		refused(t, got, e, word, "")
		if len(f.snapshot()) != 0 {
			t.Fatal(f.snapshot())
		}
		f.noEvents(t)
		held, e := f.st.ListDrafts(f.ctx, f.caller.UserID)
		if e != nil || len(held) != 0 {
			t.Fatal(held, e)
		}
	}
}

func original(w http.ResponseWriter, status int, subject, from, replyTo, messageID, references string) {
	if status != 200 {
		w.WriteHeader(status)
		return
	}
	headers := []map[string]string{{"name": "Subject", "value": subject}, {"name": "From", "value": from}, {"name": "Reply-To", "value": replyTo}, {"name": "Message-ID", "value": messageID}, {"name": "References", "value": references}}
	writeJSON(w, 200, map[string]any{"id": "original", "threadId": "original-thread", "internalDate": "1700000000000", "labelIds": []string{}, "payload": map[string]any{"mimeType": "text/plain", "headers": headers, "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("synthetic-original"))}}})
}
func fakeCreate(w http.ResponseWriter) {
	writeJSON(w, 200, map[string]any{"id": "provider-draft", "message": map[string]any{"id": "draft-message", "threadId": "draft-thread"}})
}

func assertCompositionMessage(t *testing.T, raw string, account string, want store.Draft) {
	t.Helper()
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	sender, err := mail.ParseAddress(m.Header.Get("From"))
	if err != nil || sender.Address != account || sender.Name != "" {
		t.Fatal(sender, err)
	}
	for _, group := range []struct {
		name string
		want []string
	}{{"To", want.To}, {"Cc", want.Cc}, {"Bcc", want.Bcc}} {
		if len(group.want) == 0 {
			if m.Header.Get(group.name) != "" {
				t.Fatal(m.Header)
			}
			continue
		}
		got, err := m.Header.AddressList(group.name)
		if err != nil || len(got) != len(group.want) {
			t.Fatalf("%s = %v, %v; want %v", group.name, got, err, group.want)
		}
		for i, address := range group.want {
			parsed, err := mail.ParseAddress(address)
			if err != nil || *got[i] != *parsed {
				t.Fatalf("%s[%d] = %v; want %v (%v)", group.name, i, got[i], parsed, err)
			}
		}
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || subject != strings.Trim(want.Subject, " \t") || m.Header.Get("In-Reply-To") != want.HeaderInReplyTo || m.Header.Get("References") != want.HeaderReferences {
		t.Fatal(m.Header, err)
	}
	contentType, _, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || contentType != "text/plain" || m.Header.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatal(m.Header, err)
	}
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, m.Body))
	if err != nil || string(body) != want.Body {
		t.Fatalf("body = %q, %v; want %q", body, err, want.Body)
	}
}

func TestReplyDefaultsAndComposition(t *testing.T) {
	// R-JXKX-Q9FI R-JYSU-4167 R-K00Q-HSWW R-X14B-KE98 R-X3K4-BXQM R-K7C4-SFD2
	for _, tc := range []struct {
		subject, from, reply, mid, refs string
		explicit                        *string
		to                              []string
		wantTo                          []string
		wantSubject, wantRefs           string
	}{
		{"Invoice", "Desk <desk@example.invalid>", "", "<new@example.invalid>", " <old@example.invalid> ", nil, nil, []string{"desk@example.invalid"}, "Re: Invoice", "<old@example.invalid> <new@example.invalid>"},
		{"Re: Invoice", "ignored@example.invalid", "One <one@example.invalid>, Two <two@example.invalid>", "<new@example.invalid>", "", nil, nil, []string{"one@example.invalid", "two@example.invalid"}, "Re: Invoice", "<new@example.invalid>"},
		{" RE: Invoice", "ignored@example.invalid", "reply@example.invalid", "", " <old@example.invalid> ", nil, []string{"Named <given@example.invalid>"}, []string{"Named <given@example.invalid>"}, " RE: Invoice", "<old@example.invalid>"},
	} {
		f := fixture(t)
		f.setHandler(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				original(w, 200, tc.subject, tc.from, tc.reply, tc.mid, tc.refs)
			} else {
				fakeCreate(w)
			}
		})
		got, e := f.svc.Compose(f.ctx, f.caller, drafts.ComposeRequest{ReplyTo: "original", To: tc.to, Cc: []string{"copy@example.invalid"}, Bcc: []string{"blind@example.invalid"}, Body: "synthetic-composition"})
		if e != nil {
			t.Fatal(e)
		}
		want := store.Draft{To: tc.wantTo, Cc: []string{"copy@example.invalid"}, Bcc: []string{"blind@example.invalid"}, Subject: tc.wantSubject, Body: "synthetic-composition", HeaderInReplyTo: tc.mid, HeaderReferences: tc.wantRefs}
		if !reflect.DeepEqual(got.To, want.To) || !reflect.DeepEqual(got.Cc, want.Cc) || !reflect.DeepEqual(got.Bcc, want.Bcc) || got.Subject != tc.wantSubject || got.HeaderInReplyTo != tc.mid || got.HeaderReferences != tc.wantRefs || got.Body != want.Body || got.ReplyTo != "original" || got.UserID != f.caller.UserID || got.Status != store.DraftOpen || got.GmailDraft != "provider-draft" || got.Message != "draft-message" || got.Thread != "draft-thread" || len(got.Attachments) != 0 {
			t.Fatal(got)
		}
		held, e := f.st.GetDraft(f.ctx, f.caller.UserID, got.ID)
		if e != nil || !reflect.DeepEqual(held, got) {
			t.Fatal(held, e)
		}
		requests := f.snapshot()
		if len(requests) != 3 || requests[0].Method != "GET" || requests[2].Method != "POST" || requests[2].Authorization != "Bearer "+f.token.AccessToken {
			t.Fatal(requests)
		}
		var wire map[string]map[string]string
		if e = json.Unmarshal(requests[2].Body, &wire); e != nil {
			t.Fatal(e)
		}
		if wire["message"]["threadId"] != "original-thread" {
			t.Fatal(wire)
		}
		assertCompositionMessage(t, wire["message"]["raw"], f.token.Account, want)
		f.events(t, trail.DraftCreated, got)
	}
	for _, explicit := range []string{"", "chosen"} {
		f := fixture(t)
		f.setHandler(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				original(w, 200, "old", "from@example.invalid", "", "", "")
			} else {
				fakeCreate(w)
			}
		})
		got, e := f.svc.Compose(f.ctx, f.caller, drafts.ComposeRequest{ReplyTo: "original", Subject: &explicit})
		if e != nil || got.Subject != explicit {
			t.Fatal(got, e)
		}
		requests := f.snapshot()
		if len(requests) != 3 || requests[2].Method != "POST" || requests[2].Authorization != "Bearer "+f.token.AccessToken {
			t.Fatal(requests)
		}
		var wire map[string]map[string]string
		if e = json.Unmarshal(requests[2].Body, &wire); e != nil {
			t.Fatal(e)
		}
		if wire["message"]["threadId"] != "original-thread" {
			t.Fatal(wire)
		}
		assertCompositionMessage(t, wire["message"]["raw"], f.token.Account, store.Draft{To: []string{"from@example.invalid"}, Subject: explicit})
	}
	f := fixture(t)
	got, e := f.svc.Compose(f.ctx, f.caller, drafts.ComposeRequest{To: []string{"ok@example.invalid"}})
	if e != nil || got.Subject != "" || got.HeaderInReplyTo != "" || got.HeaderReferences != "" {
		t.Fatal(got, e)
	}
	requests := f.snapshot()
	if len(requests) != 1 || requests[0].Method != "POST" || requests[0].Authorization != "Bearer "+f.token.AccessToken {
		t.Fatal(requests)
	}
	var wire map[string]map[string]string
	if e = json.Unmarshal(requests[0].Body, &wire); e != nil {
		t.Fatal(e)
	}
	if _, ok := wire["message"]["threadId"]; ok {
		t.Fatal(wire)
	}
	assertCompositionMessage(t, wire["message"]["raw"], f.token.Account, store.Draft{To: []string{"ok@example.invalid"}})
}

func TestOriginalRefusals(t *testing.T) {
	// R-WZWF-6MIJ R-JXKX-Q9FI R-676T-EI50
	for _, status := range []int{404, 401, 500, 200} {
		f := fixture(t)
		f.setHandler(func(w http.ResponseWriter, _ *http.Request) {
			original(w, status, "subject", "invalid-header", "invalid-reply", "", "")
		})
		got, e := f.svc.Compose(f.ctx, f.caller, drafts.ComposeRequest{ReplyTo: "original"})
		word, url := transfers.RefusalProvider, ""
		switch status {
		case 404:
			word = transfers.RefusalNotFound
		case 401:
			word, url = transfers.RefusalReconnect, f.cfg.ConnectURL
		case 200:
			word = transfers.RefusalTo
		}
		refused(t, got, e, word, url)
		if len(f.snapshot()) != 2 || f.snapshot()[1].Method != "GET" {
			t.Fatal(f.snapshot())
		}
		f.noEvents(t)
	}
}

func TestCreateFailuresAndCleanup(t *testing.T) {
	// R-X2C7-Y5ZX R-K648-ENMD
	for _, status := range []int{401, 413, 500, 200} {
		f := fixture(t)
		f.setHandler(func(w http.ResponseWriter, _ *http.Request) {
			if status == 200 {
				f.db.SetFailing(true)
				fakeCreate(w)
			} else {
				w.WriteHeader(status)
			}
		})
		got, e := f.svc.Compose(f.ctx, f.caller, drafts.ComposeRequest{To: []string{"ok@example.invalid"}})
		if status == 200 {
			var r *transfers.Refusal
			if !reflect.DeepEqual(got, store.Draft{}) || !errors.Is(e, store.ErrUnreachable) || errors.As(e, &r) {
				t.Fatal(got, e)
			}
			requests := f.snapshot()
			if len(requests) != 2 || requests[1].Method != "DELETE" || !strings.HasSuffix(requests[1].Path, "/provider-draft") {
				t.Fatal(requests)
			}
			f.db.SetFailing(false)
		} else {
			word, url := transfers.RefusalProvider, ""
			switch status {
			case 401:
				word, url = transfers.RefusalReconnect, f.cfg.ConnectURL
			case 413:
				word = transfers.RefusalTooLarge
			}
			refused(t, got, e, word, url)
		}
		f.noEvents(t)
	}
}
