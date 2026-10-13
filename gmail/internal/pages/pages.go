// Package pages renders the connector's read-only browser pages.
package pages

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

// ServiceName names the service in the shared banner and runtime.
const ServiceName = "gmail"

// Description is the service description shared with its manifest.
const Description = "Gmail messages read, attachments pulled into artifacts, drafts composed and sent"

// MinuteLayout formats stored times on the landing page.
const MinuteLayout = "2006-01-02 15:04"

// SecondLayout formats stored times on record pages.
const SecondLayout = "2006-01-02 15:04:05 UTC"

// ToolsLevel locates the tools page in the banner trail.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel locates the about page in the banner trail.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Set holds the embedded templates and shared page chrome.
type Set struct{ templates *template.Template }

// Load parses the embedded templates independently of the working directory.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(app.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{t}, nil
}

// Write renders a named template, suppressing the body for HEAD.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var b bytes.Buffer
	if err := s.templates.ExecuteTemplate(&b, name, data); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b.Bytes())
	}
}

// Config provides the page renderer, identity banner and read-only store.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
}

// LandingData is the landing template input.
type LandingData struct {
	Banner        page.Banner
	Drafts        []DraftRow
	DraftsPage    string
	DraftsNext    string
	Transfers     []TransferRow
	TransfersPage string
	TransfersNext string
}

// DraftRow is a draft summary with minute-resolution display times.
type DraftRow struct {
	ID, Status, Subject                        string
	To, Cc, Bcc                                []string
	AttachmentCount                            int
	Link, Created, CreatedText, Sent, SentText string
}

// TransferRow is a transfer summary with minute-resolution display times.
type TransferRow struct {
	ID, Direction, Status, Reason, Name, Link, Path, ArtifactPage, URL string
	Bytes                                                              int64
	Started, StartedText, Finished, FinishedText                       string
}

// DraftData is the draft template input.
type DraftData struct {
	Banner page.Banner
	Draft  DraftView
}

// DraftView is the complete stored draft metadata, excluding its body.
type DraftView struct {
	ID, Status, GmailDraft, Message, Thread                                                string
	To, Cc, Bcc                                                                            []string
	Subject, InReplyTo, Link, Created, CreatedText, Modified, ModifiedText, Sent, SentText string
	Attachments                                                                            []AttachmentRow
}

// AttachmentRow describes a draft attachment without carrying its bytes.
type AttachmentRow struct {
	Path, ArtifactPage, Name, ContentType string
	Bytes                                 int64
}

// TransferData is the transfer template input.
type TransferData struct {
	Banner   page.Banner
	Transfer TransferView
}

// TransferView is the complete stored transfer metadata.
type TransferView struct {
	ID, Direction, Status, Reason, Message, Attachment, Draft, Name, Path, ArtifactPage, ContentType string
	Bytes                                                                                            int64
	Started, StartedText, Finished, FinishedText, URL, Link                                          string
}

// ToolsData is the tools template input.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow describes a catalog entry.
type ToolRow struct{ Name, Description string }

// AboutData is the about template input.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData is the not-found template input.
type NotFoundData struct{ Banner page.Banner }

func stamp(t time.Time, layout string) (string, string) {
	if t.IsZero() {
		return "", ""
	}
	return t.UTC().Format(time.RFC3339), t.UTC().Format(layout)
}
func artifactBase(path string) string {
	list, err := services.Read(path)
	if path == "" || err != nil {
		return ""
	}
	e, _ := list.Find("artifacts")
	return urls.Base(e.URL)
}
func draftRow(d store.Draft) DraftRow {
	r := DraftRow{ID: d.ID, Status: d.Status, Subject: d.Subject, To: d.To, Cc: d.Cc, Bcc: d.Bcc, AttachmentCount: len(d.Attachments), Link: d.Link}
	r.Created, r.CreatedText = stamp(d.Created, MinuteLayout)
	r.Sent, r.SentText = stamp(d.Sent, MinuteLayout)
	return r
}
func draftView(d store.Draft, base string) DraftView {
	v := DraftView{ID: d.ID, Status: d.Status, GmailDraft: d.GmailDraft, Message: d.Message, Thread: d.Thread, To: d.To, Cc: d.Cc, Bcc: d.Bcc, Subject: d.Subject, InReplyTo: d.ReplyTo, Link: d.Link, Attachments: []AttachmentRow{}}
	v.Created, v.CreatedText = stamp(d.Created, SecondLayout)
	v.Modified, v.ModifiedText = stamp(d.Modified, SecondLayout)
	v.Sent, v.SentText = stamp(d.Sent, SecondLayout)
	for _, a := range d.Attachments {
		v.Attachments = append(v.Attachments, AttachmentRow{Path: a.Path, ArtifactPage: urls.ArtifactFile(base, a.Path), Name: a.Name, ContentType: a.ContentType, Bytes: a.Bytes})
	}
	return v
}
func transferRow(t store.Transfer, base string) TransferRow {
	r := TransferRow{ID: t.ID, Direction: t.Direction, Status: t.Status, Reason: t.Reason, Name: t.Name, Link: t.Link, Path: t.Path, ArtifactPage: urls.ArtifactPage(base, t.ArtifactID), URL: t.URL, Bytes: t.Bytes}
	r.Started, r.StartedText = stamp(t.Started, MinuteLayout)
	r.Finished, r.FinishedText = stamp(t.Finished, MinuteLayout)
	return r
}
func transferView(t store.Transfer, base string) TransferView {
	v := TransferView{ID: t.ID, Direction: t.Direction, Status: t.Status, Reason: t.Reason, Message: t.Message, Attachment: t.Attachment, Draft: t.Draft, Name: t.Name, Path: t.Path, ArtifactPage: urls.ArtifactPage(base, t.ArtifactID), ContentType: t.ContentType, Bytes: t.Bytes, URL: t.URL, Link: t.Link}
	v.Started, v.StartedText = stamp(t.Started, SecondLayout)
	v.Finished, v.FinishedText = stamp(t.Finished, SecondLayout)
	return v
}

// Handler serves the caller’s records without modifying them.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.EscapedPath()
		draftID := strings.TrimPrefix(p, "/drafts/")
		transferID := strings.TrimPrefix(p, "/transfers/")
		isDraft := strings.HasPrefix(p, "/drafts/") && store.ValidDraftID(draftID)
		isTransfer := strings.HasPrefix(p, "/transfers/") && store.ValidTransferID(transferID)
		known := p == "/" || p == "/tools" || p == "/about" || isDraft || isTransfer
		if known && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		auth := urls.Auth(r, cfg.ServicesPath)
		b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		notfound := func() { cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{b}) }
		refusal := func() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, store.Unreachable+"\n")
			}
		}
		c, _ := identity.FromContext(r.Context())
		switch {
		case p == "/":
			draftsPage := r.URL.Query().Get("drafts_page")
			transfersPage := r.URL.Query().Get("transfers_page")
			var created, draftPosition, started, transferPosition string
			if draftsPage != "" {
				if err := (paging.Token{Tool: "drafts"}).Decode(draftsPage, &created, &draftPosition); err != nil {
					notfound()
					return
				}
			}
			if transfersPage != "" {
				if err := (paging.Token{Tool: "transfers"}).Decode(transfersPage, &started, &transferPosition); err != nil {
					notfound()
					return
				}
			}
			ds, err := cfg.Store.ListDraftsAfter(r.Context(), c.UserID, created, draftPosition, paging.DefaultLimit+1)
			if err != nil {
				refusal()
				return
			}
			ts, err := cfg.Store.ListTransfersAfter(r.Context(), c.UserID, started, transferPosition, paging.DefaultLimit+1)
			if err != nil {
				refusal()
				return
			}
			b.Trail = nil
			d := LandingData{Banner: b, Drafts: []DraftRow{}, DraftsPage: draftsPage, Transfers: []TransferRow{}, TransfersPage: transfersPage}
			if len(ds) > paging.DefaultLimit {
				ds = ds[:paging.DefaultLimit]
				last := ds[len(ds)-1]
				d.DraftsNext = (paging.Token{Tool: "drafts"}).Encode(last.Created.UTC().Format(time.RFC3339), last.ID)
			}
			if len(ts) > paging.DefaultLimit {
				ts = ts[:paging.DefaultLimit]
				last := ts[len(ts)-1]
				d.TransfersNext = (paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
			}
			base := artifactBase(cfg.ServicesPath)
			for _, x := range ds {
				d.Drafts = append(d.Drafts, draftRow(x))
			}
			for _, x := range ts {
				d.Transfers = append(d.Transfers, transferRow(x, base))
			}
			cfg.Pages.Write(w, r, 200, "landing", d)
		case isDraft:
			d, err := cfg.Store.GetDraft(r.Context(), c.UserID, draftID)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				refusal()
				return
			}
			b.Trail = []page.Level{{Name: draftID, URL: urls.DraftPage(draftID)}}
			cfg.Pages.Write(w, r, 200, "draft", DraftData{b, draftView(d, artifactBase(cfg.ServicesPath))})
		case isTransfer:
			t, err := cfg.Store.GetTransfer(r.Context(), c.UserID, transferID)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				refusal()
				return
			}
			b.Trail = []page.Level{{Name: transferID, URL: urls.TransferPage(transferID)}}
			cfg.Pages.Write(w, r, 200, "transfer", TransferData{b, transferView(t, artifactBase(cfg.ServicesPath))})
		case p == "/tools":
			b.Trail = []page.Level{ToolsLevel}
			d := ToolsData{b, []ToolRow{}}
			for _, e := range tools.Catalog() {
				d.Tools = append(d.Tools, ToolRow{e.Name, e.Description})
			}
			cfg.Pages.Write(w, r, 200, "tools", d)
		case p == "/about":
			b.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, 200, "about", AboutData{b, Description})
		default:
			notfound()
		}
	})
}
