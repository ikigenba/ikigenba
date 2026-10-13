package pages

import (
	"bytes"
	"context"
	"encoding/binary"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
)

func banner(u page.User) page.Banner {
	return page.Banner{Service: ServiceName, Release: "chosen-release", Commit: "chosen-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true, Trail: []page.Level{{Name: "original", URL: "/original"}}}
}
func expectedBanner() page.Banner {
	return banner(page.User{Email: "person@example.test", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
}
func templates(t *testing.T) *template.Template {
	t.Helper()
	x, err := page.Templates().ParseFS(app.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return x
}
func expected(t *testing.T, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := templates(t).ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://gmail.sbx.ikigenba.dev:443"+path, nil)
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Email", "person@example.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func check(t *testing.T, w *httptest.ResponseRecorder, status int, name string, data any) {
	t.Helper()
	if w.Code != status || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || w.Body.String() != expected(t, name, data) {
		t.Fatalf("%s response mismatch: status %d", name, w.Code)
	}
}
func fixture(t *testing.T) (*store.Store, *db.DB, func() time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)
	clock := func() time.Time { return now }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state.db"), Migrations: app.Migrations(), Now: clock, Service: ServiceName, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	seq := make([]byte, 4096)
	for i := range seq {
		seq[i] = byte(i + 0xa0)
	}
	return store.New(d, store.Config{Now: clock, Rand: bytes.NewReader(seq)}), d, clock
}
func load(t *testing.T) *Set {
	t.Helper()
	s, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	return s
}

// R-W1L5-PYP4 R-W2T2-3QFT R-3DHY-CY0T R-W58U-V9X7 R-W6GR-91NW
func TestLoadAndWrite(t *testing.T) {
	t.Chdir(t.TempDir())
	if ServiceName != "gmail" || MinuteLayout != "2006-01-02 15:04" || SecondLayout != "2006-01-02 15:04:05 UTC" || Description == "" || ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("constants")
	}
	manifest, err := fs.ReadFile(app.Etc(), "manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	descriptionFound := false
	for _, line := range strings.Split(string(manifest), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "description" {
			text, err := strconv.Unquote(strings.TrimSpace(value))
			if err != nil || text != Description {
				t.Fatal("manifest description", err)
			}
			descriptionFound = true
		}
	}
	if !descriptionFound {
		t.Fatal("manifest description absent")
	}
	s := load(t)
	b := expectedBanner()
	cases := []struct {
		name string
		data any
	}{{"landing", LandingData{b, []DraftRow{}, "", []TransferRow{}, ""}}, {"tools", ToolsData{b, []ToolRow{{"chosen-tool", "chosen-description"}}}}, {"about", AboutData{b, Description}}, {"notfound", NotFoundData{b}}}
	for _, status := range []string{store.DraftOpen, store.DraftSent, store.DraftDiscarded} {
		for _, filled := range []bool{false, true} {
			v := DraftView{ID: "drf_1111111111111111", Status: status, To: []string{}, Cc: []string{}, Bcc: []string{}, Attachments: []AttachmentRow{}}
			if filled {
				v.GmailDraft = "provider-draft"
				v.Message = "message"
				v.Thread = "thread"
				v.To = []string{"to@example.test"}
				v.Cc = []string{"cc@example.test"}
				v.Bcc = []string{"bcc@example.test"}
				v.Subject = "chosen-subject"
				v.InReplyTo = "reply"
				v.Link = "https://mail.example.test/"
				v.Created = "created"
				v.CreatedText = "created text"
				v.Modified = "modified"
				v.ModifiedText = "modified text"
				v.Sent = "sent"
				v.SentText = "sent text"
				v.Attachments = []AttachmentRow{{"one", "https://art.example.test/one", "name", "type", 2}, {"two", "", "name2", "type2", 3}}
			}
			cases = append(cases, struct {
				name string
				data any
			}{"draft", DraftData{b, v}}, struct {
				name string
				data any
			}{"landing", LandingData{b, []DraftRow{{ID: v.ID, Status: status, Subject: v.Subject, To: v.To, Cc: v.Cc, Bcc: v.Bcc, AttachmentCount: len(v.Attachments), Link: v.Link, Created: v.Created, CreatedText: v.CreatedText, Sent: v.Sent, SentText: v.SentText}}, "", []TransferRow{}, ""}})
		}
	}
	for _, status := range []string{store.StatusQueued, store.StatusRunning, store.StatusDone, store.StatusFailed} {
		for _, dir := range []string{store.DirectionPull, store.DirectionPush} {
			for _, reason := range []string{"", store.ReasonInterrupted, store.ReasonProvider, store.ReasonTooLarge, store.ReasonNotFound, store.ReasonForbidden, store.ReasonReconnect, store.ReasonArtifacts} {
				for _, filled := range []bool{false, true} {
					v := TransferView{ID: "xfr_1111111111111111", Status: status, Direction: dir, Reason: reason}
					if filled {
						v.Attachment = "part"
						v.Draft = "draft"
						v.Finished = "finished"
						v.FinishedText = "finished text"
						v.URL = "https://file.example.test/"
						v.Link = "https://mail.example.test/"
						v.ArtifactPage = "https://artifact.example.test/"
						v.Name = "chosen-name"
						v.Path = "out/file"
						v.ContentType = "chosen-type"
						v.Bytes = 9
						v.Message = "message"
						v.Started = "started"
						v.StartedText = "started text"
					}
					cases = append(cases, struct {
						name string
						data any
					}{"transfer", TransferData{b, v}}, struct {
						name string
						data any
					}{"landing", LandingData{b, []DraftRow{}, "", []TransferRow{{ID: v.ID, Direction: dir, Status: status, Reason: reason, Name: v.Name, Link: v.Link, Path: v.Path, ArtifactPage: v.ArtifactPage, URL: v.URL, Bytes: v.Bytes, Started: v.Started, StartedText: v.StartedText, Finished: v.Finished, FinishedText: v.FinishedText}}, ""}})
				}
			}
		}
	}
	for _, c := range cases {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			s.Write(w, httptest.NewRequest(method, "/", nil), 202, c.name, c.data)
			want := expected(t, c.name, c.data)
			if method == http.MethodHead {
				want = ""
			}
			if w.Code != 202 || w.Body.String() != want || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) {
				t.Fatalf("write %s %s", c.name, method)
			}
		}
	}
}

// R-W7ON-MTEL R-3EPU-QPRI R-DTYQ-Z6E4 R-WHFU-OZC5 R-WOR8-ZLSB
// R-WBCC-S4MO R-DRIY-7MWQ R-WA4G-ECVZ R-WUUQ-WGHS
func TestOwnerRecords(t *testing.T) {
	st, _, now := fixture(t)
	ctx := context.Background()
	d, e := st.AddDraft(ctx, store.Draft{UserID: "owner", GmailDraft: "provider-draft", Message: "message-one", Thread: "thread-one", To: []string{"recipient-one"}, Cc: []string{"recipient-two"}, Bcc: []string{"recipient-three"}, Subject: "chosen subject", Body: strings.Repeat("privateZ9", 16), ReplyTo: "reply-source"})
	if e != nil {
		t.Fatal(e)
	}
	d, e = st.AttachToDraft(ctx, d.ID, store.Attachment{Path: "out/a.pdf", Name: "chosen-attachment", ContentType: "application/pdf", Bytes: 29}, "updated-message", "updated-thread")
	if e != nil {
		t.Fatal(e)
	}
	x, e := st.AddTransfer(ctx, store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, UserID: "owner", Message: "source-message", Attachment: "source-part", Name: "chosen transfer", Path: "out/result", ContentType: "application/pdf"})
	if e != nil {
		t.Fatal(e)
	}
	x, e = st.FinishTransfer(ctx, x.ID, store.Ending{Status: store.StatusDone, Bytes: 71, URL: "https://files.example.test/result", Link: "https://mail.example.test/result", ArtifactID: "art_5555555555555555"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = st.AddDraft(ctx, store.Draft{UserID: "other", GmailDraft: "other-draft", Message: "other-message", Subject: "other secret"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = st.AddTransfer(ctx, store.Transfer{Direction: store.DirectionPush, Status: store.StatusQueued, UserID: "other", Message: "other-message", Name: "other transfer secret", Path: "other/result"})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "services.json")
	writeServices := func(url string) {
		t.Helper()
		if e := os.WriteFile(path, []byte(`{"services":[{"name":"artifacts","url":"`+url+`","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	writeServices("https://artifacts.example.test/")
	h := identity.Require(Handler(Config{Banner: banner, Pages: load(t), ServicesPath: path, Store: st}))
	b := expectedBanner()
	b.Trail = nil
	dr := DraftRow{ID: d.ID, Status: d.Status, Subject: d.Subject, To: d.To, Cc: d.Cc, Bcc: d.Bcc, AttachmentCount: 1, Link: d.Link, Created: now().Format(time.RFC3339), CreatedText: now().Format(MinuteLayout)}
	xr := TransferRow{ID: x.ID, Direction: x.Direction, Status: x.Status, Name: x.Name, Link: x.Link, Path: x.Path, ArtifactPage: "https://artifacts.example.test/" + x.ArtifactID, URL: x.URL, Bytes: 71, Started: now().Format(time.RFC3339), StartedText: now().Format(MinuteLayout), Finished: now().Format(time.RFC3339), FinishedText: now().Format(MinuteLayout)}
	check(t, request(h, "GET", "/", "owner"), 200, "landing", LandingData{b, []DraftRow{dr}, "", []TransferRow{xr}, ""})
	check(t, request(h, "GET", "/", "empty"), 200, "landing", LandingData{b, []DraftRow{}, "", []TransferRow{}, ""})
	dv := DraftView{ID: d.ID, Status: d.Status, GmailDraft: d.GmailDraft, Message: d.Message, Thread: d.Thread, To: d.To, Cc: d.Cc, Bcc: d.Bcc, Subject: d.Subject, InReplyTo: d.ReplyTo, Link: d.Link, Created: now().Format(time.RFC3339), CreatedText: now().Format(SecondLayout), Modified: now().Format(time.RFC3339), ModifiedText: now().Format(SecondLayout), Attachments: []AttachmentRow{{"out/a.pdf", "https://artifacts.example.test/files/out/a.pdf", "chosen-attachment", "application/pdf", 29}}}
	b.Trail = []page.Level{{Name: d.ID, URL: "/drafts/" + d.ID}}
	check(t, request(h, "GET", "/drafts/"+d.ID, "owner"), 200, "draft", DraftData{b, dv})
	xv := TransferView{ID: x.ID, Direction: x.Direction, Status: x.Status, Message: x.Message, Attachment: x.Attachment, Name: x.Name, Path: x.Path, ArtifactPage: xr.ArtifactPage, ContentType: x.ContentType, Bytes: 71, Started: xr.Started, StartedText: now().Format(SecondLayout), Finished: xr.Finished, FinishedText: now().Format(SecondLayout), URL: x.URL, Link: x.Link}
	b.Trail = []page.Level{{Name: x.ID, URL: "/transfers/" + x.ID}}
	check(t, request(h, "GET", "/transfers/"+x.ID, "owner"), 200, "transfer", TransferData{b, xv})
	for _, url := range []string{"https://new-artifacts.example.test/", ""} {
		writeServices(url)
		b.Trail = []page.Level{{Name: d.ID, URL: "/drafts/" + d.ID}}
		dv.Attachments[0].ArtifactPage = ""
		if url != "" {
			dv.Attachments[0].ArtifactPage = url + "files/out/a.pdf"
		}
		check(t, request(h, "GET", "/drafts/"+d.ID, "owner"), 200, "draft", DraftData{b, dv})
	}
	for _, p := range []string{"/", "/drafts/" + d.ID, "/transfers/" + x.ID, "/tools", "/about", "/missing"} {
		for _, m := range []string{"GET", "HEAD", "POST"} {
			w := request(h, m, p, "owner")
			if len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("cookie")
			}
		}
	}
	gotD, e := st.GetDraft(ctx, "owner", d.ID)
	if e != nil || !reflect.DeepEqual(gotD, d) {
		t.Fatal("draft changed", e)
	}
	gotX, e := st.GetTransfer(ctx, "owner", x.ID)
	if e != nil || !reflect.DeepEqual(gotX, x) {
		t.Fatal("transfer changed", e)
	}
	ds, e := st.ListDrafts(ctx, "owner")
	if e != nil || !reflect.DeepEqual(ds, []store.Draft{d}) {
		t.Fatal("draft list changed")
	}
	xs, e := st.ListTransfers(ctx, "owner")
	if e != nil || !reflect.DeepEqual(xs, []store.Transfer{x}) {
		t.Fatal("transfer list changed")
	}
}

// R-3EPU-QPRI R-3DHY-CY0T
func TestLandingFirstPages(t *testing.T) {
	for _, counts := range [][2]int{{0, 0}, {paging.DefaultLimit, paging.DefaultLimit}, {paging.DefaultLimit + 1, 0}, {0, paging.DefaultLimit + 1}, {paging.DefaultLimit + 1, paging.DefaultLimit + 1}} {
		t.Run(strconv.Itoa(counts[0])+"/"+strconv.Itoa(counts[1]), func(t *testing.T) {
			_, db, clock := fixture(t)
			random := make([]byte, (2*paging.DefaultLimit+4)*8)
			for i := 0; i < len(random)/8; i++ {
				binary.BigEndian.PutUint64(random[i*8:], uint64(i+1))
			}
			now := clock()
			st := store.New(db, store.Config{Now: func() time.Time { return now }, Rand: bytes.NewReader(random)})
			ctx := context.Background()
			for _, user := range []string{"owner", "other"} {
				drafts, transfers := counts[0], counts[1]
				if user == "other" {
					drafts, transfers = 1, 1
				}
				for i := 0; i < drafts; i++ {
					now = clock().Add(time.Duration(i/2) * time.Second)
					if _, err := st.AddDraft(ctx, store.Draft{UserID: user, GmailDraft: "provider-" + strconv.Itoa(i), Message: "message-" + strconv.Itoa(i), Subject: user + "-subject-" + strconv.Itoa(i), To: []string{user + "-recipient"}}); err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < transfers; i++ {
					now = clock().Add(time.Duration(i/2) * time.Second)
					if _, err := st.AddTransfer(ctx, store.Transfer{UserID: user, Direction: store.DirectionPull, Status: store.StatusQueued, Message: "message-" + strconv.Itoa(i), Name: user + "-file-" + strconv.Itoa(i), Path: "out/" + strconv.Itoa(i)}); err != nil {
						t.Fatal(err)
					}
				}
			}
			ds, err := st.ListDrafts(ctx, "owner")
			if err != nil {
				t.Fatal(err)
			}
			xs, err := st.ListTransfers(ctx, "owner")
			if err != nil {
				t.Fatal(err)
			}
			b := expectedBanner()
			b.Trail = nil
			data := LandingData{b, []DraftRow{}, "", []TransferRow{}, ""}
			for _, d := range ds[:min(len(ds), paging.DefaultLimit)] {
				data.Drafts = append(data.Drafts, DraftRow{ID: d.ID, Status: d.Status, Subject: d.Subject, To: d.To, Cc: d.Cc, Bcc: d.Bcc, AttachmentCount: len(d.Attachments), Link: d.Link, Created: d.Created.UTC().Format(time.RFC3339), CreatedText: d.Created.UTC().Format(MinuteLayout)})
			}
			if len(ds) > paging.DefaultLimit {
				last := ds[paging.DefaultLimit-1]
				data.DraftsNext = (paging.Token{Tool: "drafts"}).Encode(last.Created.UTC().Format(time.RFC3339), last.ID)
			}
			for _, x := range xs[:min(len(xs), paging.DefaultLimit)] {
				data.Transfers = append(data.Transfers, TransferRow{ID: x.ID, Direction: x.Direction, Status: x.Status, Reason: x.Reason, Name: x.Name, Link: x.Link, Path: x.Path, URL: x.URL, Bytes: x.Bytes, Started: x.Started.UTC().Format(time.RFC3339), StartedText: x.Started.UTC().Format(MinuteLayout)})
			}
			if len(xs) > paging.DefaultLimit {
				last := xs[paging.DefaultLimit-1]
				data.TransfersNext = (paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
			}
			h := identity.Require(Handler(Config{Banner: banner, Pages: load(t), Store: st}))
			check(t, request(h, http.MethodGet, "/", "owner"), http.StatusOK, "landing", data)
			check(t, request(h, http.MethodGet, "/", "empty"), http.StatusOK, "landing", LandingData{b, []DraftRow{}, "", []TransferRow{}, ""})
		})
	}
}

// R-3EPU-QPRI
func TestLandingEmptySlices(t *testing.T) {
	st, _, _ := fixture(t)
	var captured *LandingData
	tpl := template.Must(template.New("landing").Funcs(template.FuncMap{
		"capture": func(data LandingData) string {
			captured = &data
			return ""
		},
	}).Parse("{{capture .}}"))
	h := identity.Require(Handler(Config{Banner: banner, Pages: &Set{templates: tpl}, Store: st}))
	w := request(h, http.MethodGet, "/", "empty")
	if w.Code != http.StatusOK || captured == nil {
		t.Fatal("landing data was not captured", w.Code)
	}
	if captured.Drafts == nil || len(captured.Drafts) != 0 || captured.Transfers == nil || len(captured.Transfers) != 0 || captured.DraftsNext != "" || captured.TransfersNext != "" {
		t.Fatal("empty landing lists or tokens", captured)
	}
}

// R-WF01-XFUR R-WINR-2R2U R-WJVN-GITJ R-WMBG-82AX R-WPZ5-DDJ0
// R-WSEY-4X0E R-WTMU-IOR3 R-W8WK-0L5A R-WL3J-UAK8 R-WNJC-LU1M
func TestRoutingMethodsAndFailure(t *testing.T) {
	st, d, _ := fixture(t)
	ctx := context.Background()
	draft, e := st.AddDraft(ctx, store.Draft{UserID: "owner", GmailDraft: "provider", Message: "message"})
	if e != nil {
		t.Fatal(e)
	}
	x, e := st.AddTransfer(ctx, store.Transfer{UserID: "owner", Message: "message", Path: "path", Direction: store.DirectionPull, Status: store.StatusQueued})
	if e != nil {
		t.Fatal(e)
	}
	h := identity.Require(Handler(Config{Banner: banner, Pages: load(t), Store: st}))
	b := expectedBanner()
	unknown := request(h, "GET", "/nope", "owner")
	for _, p := range []string{"/drafts/drf_0000000000000000", "/transfers/xfr_0000000000000000", "/drafts/" + draft.ID, "/transfers/" + x.ID} {
		w := request(h, "GET", p, "other")
		if w.Code != 404 || !reflect.DeepEqual(w.Header(), unknown.Header()) || w.Body.String() != unknown.Body.String() {
			t.Fatal("ownership leaked", p)
		}
	}
	invalid := []string{"/drafts/" + strings.ToUpper(draft.ID[4:]), "/drafts/drf_" + strings.ToUpper(draft.ID[4:]), "/drafts/" + draft.ID[:len(draft.ID)-1], "/drafts/" + draft.ID + "a", "/drafts/" + draft.ID + "/", "/drafts/%64" + draft.ID[1:], "/drafts/" + x.ID, "/transfers/" + draft.ID, "/" + draft.ID, "/" + x.ID, "/drafts", "/drafts/", "/transfers", "/transfers/", "/tools/", "/about/", "/%61bout", "/missing"}
	d.SetFailing(true)
	for _, p := range invalid {
		for _, m := range []string{"GET", "POST", "HEAD", "DELETE"} {
			w := request(h, m, p, "owner")
			want := unknown.Body.String()
			if m == "HEAD" {
				want = ""
			}
			if w.Code != 404 || w.Body.String() != want || !reflect.DeepEqual(w.Header(), unknown.Header()) {
				t.Fatalf("invalid path %s %s: %d", m, p, w.Code)
			}
		}
	}
	for _, p := range []string{"/", "/tools", "/about", "/drafts/" + draft.ID, "/transfers/" + x.ID, "/drafts/drf_0000000000000000"} {
		for _, m := range []string{"POST", "DELETE", "PUT", "OPTIONS"} {
			w := request(h, m, p, "other")
			if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || w.Body.Len() != 0 {
				t.Fatal("method", m, p, w.Code)
			}
		}
	}
	for _, p := range []string{"/", "/drafts/" + draft.ID, "/transfers/" + x.ID} {
		for _, m := range []string{"GET", "HEAD"} {
			w := request(h, m, p, "owner")
			body := store.Unreachable + "\n"
			if m == "HEAD" {
				body = ""
			}
			if w.Code != 503 || w.Body.String() != body || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) {
				t.Fatal("refusal", m, p)
			}
		}
	}
	b.Trail = []page.Level{AboutLevel}
	check(t, request(h, "GET", "/about", "owner"), 200, "about", AboutData{b, Description})
	b.Trail = []page.Level{ToolsLevel}
	rows := []ToolRow{}
	for _, v := range tools.Catalog() {
		rows = append(rows, ToolRow{v.Name, v.Description})
	}
	check(t, request(h, "GET", "/tools", "owner"), 200, "tools", ToolsData{b, rows})
	check(t, unknown, 404, "notfound", NotFoundData{expectedBanner()})
	d.SetFailing(false)
	for _, p := range []string{"/", "/drafts/" + draft.ID, "/transfers/" + x.ID, "/tools", "/about", "/missing"} {
		g := request(h, "GET", p, "owner")
		head := request(h, "HEAD", p, "owner")
		if g.Code != head.Code || !reflect.DeepEqual(g.Header(), head.Header()) || head.Body.Len() != 0 {
			t.Fatal("head", p)
		}
	}
}

// R-WW2N-A88H R-WXAJ-NZZ6
func TestMailPrivacy(t *testing.T) {
	st, _, _ := fixture(t)
	ctx := context.Background()
	body := strings.Repeat("ZyxWvu9876543210", 8)
	d, e := st.AddDraft(ctx, store.Draft{UserID: "owner", GmailDraft: "provider", Message: "message", Subject: "secret-subject-value", To: []string{"secret-to-value"}, Cc: []string{"secret-cc-value"}, Bcc: []string{"secret-bcc-value"}, Body: body})
	if e != nil {
		t.Fatal(e)
	}
	x, e := st.AddTransfer(ctx, store.Transfer{UserID: "owner", Message: "message", Path: "path", Name: "secret-transfer-name", Direction: store.DirectionPull, Status: store.StatusQueued})
	if e != nil {
		t.Fatal(e)
	}
	h := identity.Require(Handler(Config{Banner: banner, Pages: load(t), Store: st}))
	paths := []string{"/", "/drafts/" + d.ID, "/transfers/" + x.ID, "/tools", "/about", "/missing", "/drafts/drf_0000000000000000"}
	for _, user := range []string{"owner", "other"} {
		for _, p := range paths {
			for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
				w := request(h, method, p, user)
				s := w.Body.String()
				for i := 0; i+16 <= len(body); i++ {
					if strings.Contains(s, body[i:i+16]) {
						t.Fatal("body disclosed", user, p)
					}
				}
				if user == "other" || p == "/tools" || p == "/about" || p == "/missing" {
					for _, secret := range []string{d.Subject, d.To[0], d.Cc[0], d.Bcc[0], x.Name} {
						if strings.Contains(s, secret) {
							t.Fatal("mail disclosed", user, p)
						}
					}
				}
			}
		}
	}
}

// R-3DHY-CY0T R-WBCC-S4MO R-DRIY-7MWQ R-WA4G-ECVZ
func TestDataConversions(t *testing.T) {
	z := time.Date(2026, 10, 5, 11, 0, 7, 0, time.FixedZone("offset", 2*60*60))
	short, long := z.UTC().Format(time.RFC3339), z.UTC().Format(SecondLayout)
	d := store.Draft{ID: "drf_5555555555555555", Status: store.DraftSent, GmailDraft: "provider", Message: "message", Thread: "thread", Subject: "subject", ReplyTo: "reply", To: []string{"one", "two"}, Cc: []string{"three"}, Bcc: []string{"four"}, Attachments: []store.Attachment{{Path: "out/a", Name: "name-a", ContentType: "type-a", Bytes: 17}, {Path: "out/b", Name: "name-b", ContentType: "type-b", Bytes: 19}}, Link: "https://mail.example.test/draft", Created: z, Modified: z, Sent: z}
	dr := DraftRow{d.ID, d.Status, d.Subject, d.To, d.Cc, d.Bcc, 2, d.Link, short, "2026-10-05 09:00", short, "2026-10-05 09:00"}
	if got := draftRow(d); !reflect.DeepEqual(got, dr) {
		t.Fatalf("draft row %+v", got)
	}
	dv := DraftView{d.ID, d.Status, d.GmailDraft, d.Message, d.Thread, d.To, d.Cc, d.Bcc, d.Subject, d.ReplyTo, d.Link, short, long, short, long, short, long, []AttachmentRow{{"out/a", "https://artifacts.example.test/files/out/a", "name-a", "type-a", 17}, {"out/b", "https://artifacts.example.test/files/out/b", "name-b", "type-b", 19}}}
	if got := draftView(d, "https://artifacts.example.test"); !reflect.DeepEqual(got, dv) {
		t.Fatalf("draft view %+v", got)
	}
	x := store.Transfer{ID: "xfr_5555555555555555", Direction: store.DirectionPush, Status: store.StatusFailed, Reason: store.ReasonForbidden, Message: "message", Attachment: "attachment", Draft: d.ID, Name: "transfer-name", Path: "out/c", ContentType: "type-c", Bytes: 21, Started: z, Finished: z, URL: "https://files.example.test/c", Link: "https://mail.example.test/c", ArtifactID: "art_5555555555555555"}
	tr := TransferRow{x.ID, x.Direction, x.Status, x.Reason, x.Name, x.Link, x.Path, "https://artifacts.example.test/" + x.ArtifactID, x.URL, 21, short, "2026-10-05 09:00", short, "2026-10-05 09:00"}
	if got := transferRow(x, "https://artifacts.example.test"); !reflect.DeepEqual(got, tr) {
		t.Fatalf("transfer row %+v", got)
	}
	tv := TransferView{x.ID, x.Direction, x.Status, x.Reason, x.Message, x.Attachment, x.Draft, x.Name, x.Path, tr.ArtifactPage, x.ContentType, 21, short, long, short, long, x.URL, x.Link}
	if got := transferView(x, "https://artifacts.example.test"); !reflect.DeepEqual(got, tv) {
		t.Fatalf("transfer view %+v", got)
	}
	d.Sent = time.Time{}
	d.Attachments = nil
	dr.Sent = ""
	dr.SentText = ""
	dr.AttachmentCount = 0
	dv.Sent = ""
	dv.SentText = ""
	dv.Attachments = []AttachmentRow{}
	if !reflect.DeepEqual(draftRow(d), dr) || !reflect.DeepEqual(draftView(d, ""), dv) {
		t.Fatal("zero draft times")
	}
	x.Finished = time.Time{}
	tr.Finished = ""
	tr.FinishedText = ""
	tr.ArtifactPage = ""
	tv.Finished = ""
	tv.FinishedText = ""
	tv.ArtifactPage = ""
	if !reflect.DeepEqual(transferRow(x, ""), tr) || !reflect.DeepEqual(transferView(x, ""), tv) {
		t.Fatal("zero transfer times")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "services.json")
	for _, doc := range []string{`{"services":[]}`, `{"services":[{"name":"elsewhere","url":"https://elsewhere.example.test","description":"","socket":"","enabled":true,"mcp":false}]}`, `{"services":[{"name":"artifacts","url":"","description":"","socket":"","enabled":true,"mcp":false}]}`, `invalid`} {
		if err := os.WriteFile(path, []byte(doc), 0600); err != nil {
			t.Fatal(err)
		}
		if artifactBase(path) != "" {
			t.Fatal("absent base")
		}
	}
	for _, p := range []string{"", filepath.Join(dir, "missing")} {
		if artifactBase(p) != "" {
			t.Fatal("unreadable base")
		}
	}
	// Positional construction checks the remaining page contracts' exact field order.
	landing := LandingData{page.Banner{}, []DraftRow{}, "chosen-drafts-token", []TransferRow{}, "chosen-transfers-token"}
	if landing.DraftsNext != "chosen-drafts-token" || landing.TransfersNext != "chosen-transfers-token" {
		t.Fatal("landing token field order")
	}
	_ = DraftData{page.Banner{}, dv}
	_ = TransferData{page.Banner{}, tv}
	_ = ToolsData{page.Banner{}, []ToolRow{{"chosen", "description"}}}
	_ = AboutData{page.Banner{}, Description}
	_ = NotFoundData{page.Banner{}}
	_ = Config{banner, load(t), "", nil}
}

// R-3EPU-QPRI R-WINR-2R2U R-WUUQ-WGHS
func TestStoredStatesOrderAndPrunedRecords(t *testing.T) {
	st, _, now := fixture(t)
	ctx := context.Background()
	cfg := Config{banner, load(t), "", st}
	h := identity.Require(Handler(cfg))
	for _, status := range []string{store.DraftOpen, store.DraftSent, store.DraftDiscarded} {
		d, e := st.AddDraft(ctx, store.Draft{UserID: "owner", GmailDraft: "provider-" + status, Message: "message-" + status, Subject: "subject-" + status})
		if e != nil {
			t.Fatal(e)
		}
		switch status {
		case store.DraftSent:
			_, e = st.MarkDraftSent(ctx, d.ID, "sent-message", "sent-thread", now())
		case store.DraftDiscarded:
			_, e = st.MarkDraftDiscarded(ctx, d.ID)
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, status := range []string{store.StatusQueued, store.StatusRunning, store.StatusDone, store.StatusFailed} {
		initial := status
		if status == store.StatusDone || status == store.StatusFailed {
			initial = store.StatusRunning
		}
		x, e := st.AddTransfer(ctx, store.Transfer{UserID: "owner", Message: "message-" + status, Path: "out/" + status, Name: "name-" + status, Direction: store.DirectionPull, Status: initial})
		if e != nil {
			t.Fatal(e)
		}
		switch status {
		case store.StatusDone:
			_, e = st.FinishTransfer(ctx, x.ID, store.Ending{Status: store.StatusDone, Bytes: 11})
		case store.StatusFailed:
			_, e = st.FinishTransfer(ctx, x.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	ds, e := st.ListDrafts(ctx, "owner")
	if e != nil {
		t.Fatal(e)
	}
	xs, e := st.ListTransfers(ctx, "owner")
	if e != nil {
		t.Fatal(e)
	}
	b := expectedBanner()
	b.Trail = nil
	data := LandingData{b, []DraftRow{}, "", []TransferRow{}, ""}
	for _, d := range ds {
		r := DraftRow{ID: d.ID, Status: d.Status, Subject: d.Subject, To: d.To, Cc: d.Cc, Bcc: d.Bcc, AttachmentCount: len(d.Attachments), Link: d.Link, Created: now().Format(time.RFC3339), CreatedText: now().Format(MinuteLayout)}
		if d.Status == store.DraftSent {
			r.Sent = now().Format(time.RFC3339)
			r.SentText = now().Format(MinuteLayout)
		}
		data.Drafts = append(data.Drafts, r)
	}
	for _, x := range xs {
		r := TransferRow{ID: x.ID, Direction: x.Direction, Status: x.Status, Reason: x.Reason, Name: x.Name, Link: x.Link, Path: x.Path, URL: x.URL, Bytes: x.Bytes, Started: now().Format(time.RFC3339), StartedText: now().Format(MinuteLayout)}
		if x.Status == store.StatusDone || x.Status == store.StatusFailed {
			r.Finished = now().Format(time.RFC3339)
			r.FinishedText = now().Format(MinuteLayout)
		}
		data.Transfers = append(data.Transfers, r)
	}
	check(t, request(h, "GET", "/", "owner"), 200, "landing", data)
	afterD, e := st.ListDrafts(ctx, "owner")
	if e != nil || !reflect.DeepEqual(afterD, ds) {
		t.Fatal("draft states changed")
	}
	afterX, e := st.ListTransfers(ctx, "owner")
	if e != nil || !reflect.DeepEqual(afterX, xs) {
		t.Fatal("transfer states changed")
	}
	later := now().Add(30 * 24 * time.Hour)
	prunedD, e := st.PruneDrafts(ctx, later, 1, 1)
	if e != nil || len(prunedD) != 1 {
		t.Fatal("draft prune setup", e, len(prunedD))
	}
	prunedX, e := st.PruneTransfers(ctx, later, 1, 1)
	if e != nil || len(prunedX) != 1 {
		t.Fatal("transfer prune setup", e, len(prunedX))
	}
	want := request(h, "GET", "/unknown", "owner")
	for _, p := range []string{"/drafts/" + prunedD[0].ID, "/transfers/" + prunedX[0].ID} {
		got := request(h, "GET", p, "owner")
		if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
			t.Fatal("pruned record answer", p)
		}
	}
}
