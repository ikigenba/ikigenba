package pages

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/tools"
	"github.com/ikigenba/ikigenba/campaigns/internal/urls"
)

type pageFixture struct {
	d      *db.DB
	st     *store.Store
	set    *Set
	writer *telemetry.Writer
	cfg    Config
	now    time.Time
}

func pageSetup(t *testing.T) *pageFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &pageFixture{now: time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)}
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "campaigns.db"), Migrations: campaigns.Migrations(), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.Close() })
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(randomFixture(11))})
	f.set, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	f.writer = telemetry.New(telemetry.Config{Service: ServiceName, Sink: &telemetry.Capture{}, Now: func() time.Time { return time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC) }, Rand: bytes.NewReader(randomFixture(12)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { f.writer.Shutdown(context.Background(), "test") })
	f.cfg = Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Service: ServiceName, Release: "release-chosen", Commit: "commit-chosen", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true}
	}, Pages: f.set, Store: f.st, MCP: mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Telemetry: f.writer})}
	return f
}
func (f *pageFixture) create(t *testing.T, i int) store.Campaign {
	t.Helper()
	f.now = f.now.Add(time.Minute)
	c, err := f.st.CreateCampaign(context.Background(), store.Campaign{Name: fmt.Sprintf("Campaign %02d", i), Description: "description chosen", Channels: []string{store.ChannelMail, store.ChannelEmail}, Destination: "https://destination.example/", CreatedUserID: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (f *pageFixture) add(t *testing.T, id string, i int) store.MemberRow {
	t.Helper()
	f.now = f.now.Add(time.Minute)
	m, _, err := f.st.AddMember(context.Background(), store.MemberInput{Campaign: id, Candidate: fmt.Sprintf("cand_%016x", i+1), Cohort: "coh_2222222222222222", Channels: []string{store.ChannelEmail}, UserID: "adder"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func pageRequest(_ *pageFixture, method, target, user string) *http.Request {
	r := httptest.NewRequest(method, "https://campaigns.example.test"+target, nil)
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	r.Header.Set("X-User-Email", "mgreenly+fake00@gmail.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}
func pageResponse(f *pageFixture, method, target, user string) *httptest.ResponseRecorder {
	r := pageRequest(f, method, target, user)
	w := httptest.NewRecorder()
	identity.Optional(Handler(f.cfg)).ServeHTTP(w, r)
	return w
}
func expectedBanner(f *pageFixture, target string, trail []page.Level) page.Banner {
	r := pageRequest(f, "GET", target, "viewer")
	base := urls.Auth(r, f.cfg.ServicesPath)
	b := f.cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: base + "/", LogoutURL: base + "/logout"})
	b.Trail = trail
	return b
}
func assertTemplate(t *testing.T, f *pageFixture, target, name string, status int, data any) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		r := pageRequest(f, method, target, "viewer")
		want := httptest.NewRecorder()
		f.set.Write(want, r, status, name, data)
		got := pageResponse(f, method, target, "viewer")
		if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
			t.Fatalf("%s %s got %d %v\n%s\nwant %d %v\n%s", method, target, got.Code, got.Header(), got.Body.String(), want.Code, want.Header(), want.Body.String())
		}
	}
}
func assertBare(t *testing.T, w *httptest.ResponseRecorder, status int, headers http.Header) {
	t.Helper()
	if w.Code != status || !reflect.DeepEqual(w.Header(), headers) || w.Body.Len() != 0 {
		t.Fatalf("answer %d %v %q want %d %v", w.Code, w.Header(), w.Body.String(), status, headers)
	}
}
func expectedCampaign(c store.Campaign, n store.Counts) CampaignView {
	return CampaignView{ID: c.ID, Name: c.Name, Description: c.Description, Channels: c.Channels, Destination: c.Destination, CreatedBy: c.CreatedUserEmail, CreatedUser: c.CreatedUserID, Created: c.Created.UTC().Format(time.RFC3339), CreatedText: c.Created.UTC().Format(SecondLayout), Members: n.Members, Delivered: n.Delivered, Opened: n.Opened}
}
func expectedLanding(rows []store.CampaignRow) []CampaignRow {
	out := make([]CampaignRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, CampaignRow{ID: r.Campaign.ID, Name: r.Campaign.Name, Channels: r.Campaign.Channels, Created: r.Campaign.Created.UTC().Format(time.RFC3339), CreatedText: r.Campaign.Created.UTC().Format(MinuteLayout), Members: r.Counts.Members, Delivered: r.Counts.Delivered, Opened: r.Counts.Opened})
	}
	return out
}

// R-BCJ7-SKVH R-VVLC-LESB R-VWT8-Z6J0
func TestConstants(t *testing.T) {
	if ServiceName != "campaigns" || MinuteLayout != "2006-01-02 15:04" || SecondLayout != "2006-01-02 15:04:05 UTC" || ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || AboutLevel != (page.Level{Name: "about", URL: "/about"}) || Description == "" || strings.Contains(Description, "\n") {
		t.Fatal("page constants")
	}
}

// R-VZ91-QQ0E R-W0GY-4HR3 R-W1OU-I9HS R-W2WQ-W18H R-W44N-9SZ6 R-W5CJ-NKPV R-W6KG-1CGK
func TestEmbeddedTemplatesAndData(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	independent, err := page.Templates().ParseFS(campaigns.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	// Unkeyed construction makes the declared field lists and order compile contracts.
	b := page.Banner{Service: ServiceName}
	_ = Config{nil, set, "", nil, nil, nil}
	for _, chs := range [][]string{{store.ChannelMail}, {store.ChannelEmail}, {store.ChannelMail, store.ChannelEmail}} {
		for _, populated := range []bool{false, true} {
			for _, p := range []string{"", "page-chosen", "last"} {
				landing := LandingData{b, []CampaignRow{}, p, p, p}
				campaign := CampaignData{b, CampaignView{"id-chosen", "name-chosen", "", chs, "https://destination.example/", "", "user-chosen", "time-chosen", "text-chosen", 0, 0, 0}, []MaterialRow{}, []MemberRow{}, p, p, p}
				if populated {
					landing.Campaigns = []CampaignRow{{"id-chosen", "name-chosen", chs, "created-chosen", "shown-chosen", 3, 2, 1}}
					campaign.Campaign.Description = "description-chosen"
					campaign.Campaign.CreatedBy = "mgreenly+fake01@gmail.com"
					campaign.Material = []MaterialRow{{"label-chosen", "path-chosen", "https://artifact.example/chosen"}, {"label2", "path2", ""}}
					for _, ch := range chs {
						for _, state := range []string{"", store.StatePlanned, store.StateDelivered, store.StateDropped} {
							for opens := range 3 {
								for _, how := range []string{store.HowHand, store.HowSystem} {
									for _, named := range []bool{false, true} {
										m := MemberRow{"candidate-chosen", "", "", "", "", "added-chosen", "added-text-chosen", named, named, "", "user-chosen", "rejected-chosen", "rejected-text-chosen", "why-chosen", []PieceRow{{ch, state, "delivered-chosen", "delivery-text-chosen", "", "user-chosen", how, opens, "first-chosen", "first-text-chosen", "last-chosen", "last-text-chosen"}}}
										if named {
											m.CandidatePage = "https://candidate.example/chosen"
											m.Name = "business-chosen"
											m.Cohort = "cohort-chosen"
											m.CohortPage = "https://cohort.example/chosen"
											m.RejectedBy = "mgreenly+fake02@gmail.com"
											m.Pieces[0].DeliveredBy = "mgreenly+fake03@gmail.com"
										}
										campaign.Members = append(campaign.Members, m)
									}
								}
							}
						}
					}
				}
				for _, tc := range []struct {
					name string
					data any
				}{{"landing", landing}, {"campaign", campaign}, {"tools", ToolsData{b, []ToolRow{}}}, {"tools", ToolsData{b, []ToolRow{{"tool-chosen", "description-chosen"}}}}, {"about", AboutData{b, Description}}, {"notfound", NotFoundData{b}}} {
					var expected bytes.Buffer
					if err := independent.ExecuteTemplate(&expected, tc.name, tc.data); err != nil {
						t.Fatal(err)
					}
					for _, method := range []string{"GET", "HEAD"} {
						w := httptest.NewRecorder()
						set.Write(w, httptest.NewRequest(method, "/", nil), 207, tc.name, tc.data)
						want := expected.String()
						if method == "HEAD" {
							want = ""
						}
						if w.Code != 207 || w.Body.String() != want || !reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"text/html; charset=utf-8"}}) {
							t.Fatalf("template %s %s: %d %v", tc.name, method, w.Code, w.Header())
						}
					}
				}
			}
		}
	}
	for _, method := range []string{"GET", "HEAD"} {
		for _, name := range []string{"missing", "landing"} {
			w := httptest.NewRecorder()
			set.Write(w, httptest.NewRequest(method, "/", nil), 200, name, struct{}{})
			assertBare(t, w, 500, http.Header{"Content-Type": {"text/html; charset=utf-8"}})
		}
	}
	for _, release := range []string{"", "release-chosen"} {
		for _, commit := range []string{"", "commit-chosen"} {
			b.Release = release
			b.Commit = commit
			w := httptest.NewRecorder()
			set.Write(w, httptest.NewRequest("GET", "/", nil), 200, "about", AboutData{b, Description})
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
		}
	}
}

// R-W7SC-F479 R-WA85-6NON R-WL78-MLCW R-WMF5-0D3L R-WNN1-E4UA R-WOUX-RWKZ R-WQ2U-5OBO R-WRAQ-JG2D R-X3HQ-D5HB R-X4PM-QX80
func TestRouteDecisionsAndFailures(t *testing.T) {
	f := pageSetup(t)
	c := f.create(t, 0)
	deleted := f.create(t, 1)
	if _, _, err := f.st.DeleteCampaign(context.Background(), deleted.ID); err != nil {
		t.Fatal(err)
	}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, path := range []string{"/", "/campaigns/" + c.ID + "?page=last", "/about", "/nope"} {
			for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
				r := pageRequest(f, method, path, "")
				w := pageResponse(f, method, path, "")
				assertBare(t, w, 302, http.Header{"Location": {urls.SignIn(r, f.cfg.ServicesPath)}})
			}
		}
		for _, path := range []string{"/nope", "/campaigns", "/campaigns/", "/campaigns/" + strings.ToUpper(c.ID), "/campaigns/camp_12", "/campaigns/" + c.ID + "/", "/campaigns/" + c.ID + "/x", "/campaigns/%63" + c.ID[1:], "/t"} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				w := pageResponse(f, method, path, "viewer")
				expected := httptest.NewRecorder()
				f.set.Write(expected, pageRequest(f, method, path, "viewer"), 404, "notfound", NotFoundData{expectedBanner(f, path, nil)})
				if w.Code != 404 || w.Body.String() != expected.Body.String() || !reflect.DeepEqual(w.Header(), expected.Header()) {
					t.Fatalf("notfound %s %s %d", method, path, w.Code)
				}
			}
		}
		for _, path := range []string{"/", "/tools", "/about", "/campaigns/" + c.ID, "/campaigns/camp_ffffffffffffffff"} {
			for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
				assertBare(t, pageResponse(f, method, path, "viewer"), 405, http.Header{"Allow": {"GET, HEAD"}})
			}
		}
		for _, p := range []string{"bogus", "LAST", "last/", (paging.Token{Tool: "other-list"}).Encode("a", "b")} {
			for _, path := range []string{"/?page=" + p, "/campaigns/" + c.ID + "?page=" + p} {
				assertTemplate(t, f, path, "notfound", 404, NotFoundData{expectedBanner(f, path, nil)})
			}
		}
		if failing {
			for _, path := range []string{"/", "/?page=last", "/campaigns/" + c.ID, "/campaigns/camp_ffffffffffffffff"} {
				for _, method := range []string{"GET", "HEAD"} {
					w := pageResponse(f, method, path, "viewer")
					body := store.Unreachable + "\n"
					if method == "HEAD" {
						body = ""
					}
					if w.Code != 503 || w.Body.String() != body || !reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"text/plain; charset=utf-8"}}) {
						t.Fatal(w.Code, w.Header(), w.Body.String())
					}
				}
			}
		}
	}
	f.d.SetFailing(false)
	for _, id := range []string{deleted.ID, "camp_ffffffffffffffff"} {
		path := "/campaigns/" + id
		assertTemplate(t, f, path, "notfound", 404, NotFoundData{expectedBanner(f, path, nil)})
	}
}

// R-WZU1-7U98 R-X29T-ZDQM
func TestReferencePages(t *testing.T) {
	f := pageSetup(t)
	tools.Register(f.cfg.MCP, tools.Config{Store: f.st, Telemetry: f.writer})
	mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: "chosen_tool", Description: "Chosen summary.\nSecond line.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		rows := make([]ToolRow, 0)
		for _, tool := range f.cfg.MCP.Tools() {
			first, _, _ := strings.Cut(tool.Description, "\n")
			rows = append(rows, ToolRow{Name: tool.Name, Description: first})
		}
		assertTemplate(t, f, "/tools", "tools", 200, ToolsData{Banner: expectedBanner(f, "/tools", []page.Level{ToolsLevel}), Tools: rows})
		assertTemplate(t, f, "/about", "about", 200, AboutData{Banner: expectedBanner(f, "/about", []page.Level{AboutLevel}), Description: Description})
	}
}

func expectedMember(m store.MemberRow, channels []string, base, name string) MemberRow {
	x := m.Member
	out := MemberRow{Candidate: x.Candidate, CandidatePage: urls.CandidatePage(base, x.Candidate), Name: name, Cohort: x.Cohort, CohortPage: urls.CohortPage(base, x.Cohort), Added: x.Added.UTC().Format(time.RFC3339), AddedText: x.Added.UTC().Format(MinuteLayout), Opened: m.Engagement.Opened, Rejected: x.Rejected, Pieces: []PieceRow{}}
	if x.Rejected {
		out.RejectedBy = x.RejectedUserEmail
		out.RejectedUser = x.RejectedUserID
		out.Why = x.Why
		out.RejectedAt = x.RejectedAt.UTC().Format(time.RFC3339)
		out.RejectedAtText = x.RejectedAt.UTC().Format(MinuteLayout)
	}
	for _, channel := range channels {
		row := PieceRow{Channel: channel}
		for _, p := range m.Pieces {
			if p.Piece.Channel != channel {
				continue
			}
			row.State = p.Piece.State
			row.Opens = p.Engagement.Opens
			if row.State == store.StateDelivered {
				row.Delivered = p.Piece.Delivered.UTC().Format(time.RFC3339)
				row.DeliveredText = p.Piece.Delivered.UTC().Format(MinuteLayout)
				row.DeliveredBy = p.Piece.DeliveredUserEmail
				row.DeliveredUser = p.Piece.DeliveredUserID
				row.How = p.Piece.How
			}
			if row.Opens > 0 {
				row.First = p.Engagement.First.UTC().Format(time.RFC3339)
				row.FirstText = p.Engagement.First.UTC().Format(MinuteLayout)
				row.Last = p.Engagement.Last.UTC().Format(time.RFC3339)
				row.LastText = p.Engagement.Last.UTC().Format(MinuteLayout)
			}
		}
		out.Pieces = append(out.Pieces, row)
	}
	return out
}
func callTool(t *testing.T, client *mcp.Client, name string, args any, out any) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), identity.Caller{UserID: "viewer", Email: "mgreenly+fake00@gmail.com"}, name, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() {
		t.Fatal(result)
	}
	encoded, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if err = json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(envelope.Structured, out); err != nil {
		t.Fatal(err)
	}
}

// R-WBG1-KFFC R-WCNX-Y761 R-WDVU-BYWQ R-WF3Q-PQNF R-WSIM-X7T2 R-WTQJ-AZJR R-WUYF-ORAG R-WW6C-2J15 R-WXE8-GARU
func TestWindowsAndSharedToolTokens(t *testing.T) {
	f := pageSetup(t)
	assertTemplate(t, f, "/", "landing", 200, LandingData{Banner: expectedBanner(f, "/", nil), Campaigns: []CampaignRow{}})
	var campaignsInOrder []store.CampaignRow
	for i := range 52 {
		c := f.create(t, i)
		campaignsInOrder = append(campaignsInOrder, store.CampaignRow{Campaign: c})
	}
	slices.Reverse(campaignsInOrder)
	c := campaignsInOrder[0].Campaign
	var members []store.MemberRow
	for i := range 52 {
		members = append(members, f.add(t, c.ID, i))
	}
	campaignsInOrder[0].Counts.Members = len(members)
	tools.Register(f.cfg.MCP, tools.Config{Store: f.st, Telemetry: f.writer})
	srv := httptest.NewServer(identity.Require(f.cfg.MCP))
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL})
	for _, kind := range []string{"campaigns", "campaign"} {
		path := "/"
		if kind == "campaign" {
			path = "/campaigns/" + c.ID
		}
		var previous []string
		token := ""
		for start := 0; start < 52; start += paging.DefaultLimit {
			end := min(start+paging.DefaultLimit, 52)
			next, prev := "", ""
			var firstAt, lastAt time.Time
			var firstID, lastID string
			if kind == "campaigns" {
				first, last := campaignsInOrder[start].Campaign, campaignsInOrder[end-1].Campaign
				firstAt, lastAt, firstID, lastID = first.Created, last.Created, first.ID, last.ID
			} else {
				first, last := members[start].Member, members[end-1].Member
				firstAt, lastAt, firstID, lastID = first.Added, last.Added, first.Candidate, last.Candidate
			}
			if end < 52 {
				next = (paging.Token{Tool: kind}).Encode(lastAt.Format(time.RFC3339), lastID)
			}
			if start > 0 {
				prev = (paging.Token{Tool: kind + "-before"}).Encode(firstAt.Format(time.RFC3339), firstID)
			}
			target := path
			if token != "" {
				target += "?page=" + token
			}
			if kind == "campaigns" {
				data := LandingData{Banner: expectedBanner(f, target, nil), Campaigns: expectedLanding(campaignsInOrder[start:end]), Page: token, Prev: prev, Next: next}
				assertTemplate(t, f, target, "landing", 200, data)
				var out tools.CampaignList
				callTool(t, client, kind, map[string]any{"page": token}, &out)
				if len(out.Campaigns) != len(data.Campaigns) {
					t.Fatal("tool window length")
				}
				for i, row := range out.Campaigns {
					if row.ID != data.Campaigns[i].ID {
						t.Fatal("tool ordering")
					}
				}
				if (out.Next == nil && next != "") || (out.Next != nil && *out.Next != next) {
					t.Fatal("tool next", out.Next, next)
				}
			} else {
				data := CampaignData{Banner: expectedBanner(f, target, []page.Level{{Name: c.ID, URL: path}}), Campaign: expectedCampaign(c, store.Counts{Members: 52}), Material: []MaterialRow{}, Members: []MemberRow{}, Page: token, Prev: prev, Next: next}
				for _, m := range members[start:end] {
					data.Members = append(data.Members, expectedMember(m, c.Channels, "", ""))
				}
				assertTemplate(t, f, target, "campaign", 200, data)
				var out tools.CampaignRead
				callTool(t, client, kind, map[string]any{"campaign": c.ID, "page": token}, &out)
				if len(out.Members) != len(data.Members) {
					t.Fatal("tool member length")
				}
				for i, row := range out.Members {
					if row.Candidate != data.Members[i].Candidate {
						t.Fatal("tool member order")
					}
				}
				if (out.Next == nil && next != "") || (out.Next != nil && *out.Next != next) {
					t.Fatal("tool member next")
				}
			}
			if prev != "" {
				previous = append(previous, prev)
			}
			token = next
		}
		// The second before-token precedes row 51; the first precedes row 26 and underflows.
		for i, prev := range previous {
			target := path + "?page=" + prev
			if i == 0 {
				assertBare(t, pageResponse(f, "GET", target, "viewer"), 303, http.Header{"Location": {path}})
				continue
			}
			start, end := 25, 50
			if kind == "campaigns" {
				rows := campaignsInOrder[start:end]
				first, last := rows[0].Campaign, rows[len(rows)-1].Campaign
				data := LandingData{Banner: expectedBanner(f, target, nil), Campaigns: expectedLanding(rows), Page: prev, Prev: (paging.Token{Tool: kind + "-before"}).Encode(first.Created.Format(time.RFC3339), first.ID), Next: (paging.Token{Tool: kind}).Encode(last.Created.Format(time.RFC3339), last.ID)}
				assertTemplate(t, f, target, "landing", 200, data)
			} else {
				rows := members[start:end]
				first, last := rows[0].Member, rows[len(rows)-1].Member
				data := CampaignData{Banner: expectedBanner(f, target, []page.Level{{Name: c.ID, URL: path}}), Campaign: expectedCampaign(c, store.Counts{Members: 52}), Material: []MaterialRow{}, Members: []MemberRow{}, Page: prev, Prev: (paging.Token{Tool: kind + "-before"}).Encode(first.Added.Format(time.RFC3339), first.Candidate), Next: (paging.Token{Tool: kind}).Encode(last.Added.Format(time.RFC3339), last.Candidate)}
				for _, m := range rows {
					data.Members = append(data.Members, expectedMember(m, c.Channels, "", ""))
				}
				assertTemplate(t, f, target, "campaign", 200, data)
			}
		}
		target := path + "?page=last"
		if kind == "campaigns" {
			rows := campaignsInOrder[27:]
			first := rows[0].Campaign
			assertTemplate(t, f, target, "landing", 200, LandingData{Banner: expectedBanner(f, target, nil), Campaigns: expectedLanding(rows), Page: "last", Prev: (paging.Token{Tool: kind + "-before"}).Encode(first.Created.Format(time.RFC3339), first.ID)})
		} else {
			rows := members[27:]
			first := rows[0].Member
			data := CampaignData{Banner: expectedBanner(f, target, []page.Level{{Name: c.ID, URL: path}}), Campaign: expectedCampaign(c, store.Counts{Members: 52}), Material: []MaterialRow{}, Members: []MemberRow{}, Page: "last", Prev: (paging.Token{Tool: kind + "-before"}).Encode(first.Added.Format(time.RFC3339), first.Candidate)}
			for _, m := range rows {
				data.Members = append(data.Members, expectedMember(m, c.Channels, "", ""))
			}
			assertTemplate(t, f, target, "campaign", 200, data)
		}
	}
	empty := f.create(t, 100)
	path := "/campaigns/" + empty.ID
	assertTemplate(t, f, path, "campaign", 200, CampaignData{Banner: expectedBanner(f, path, []page.Level{{Name: empty.ID, URL: path}}), Campaign: expectedCampaign(empty, store.Counts{}), Material: []MaterialRow{}, Members: []MemberRow{}})
	for _, method := range []string{"GET", "HEAD"} {
		assertBare(t, pageResponse(f, method, path+"?page=last", "viewer"), 303, http.Header{"Location": {path}})
	}
}

// R-W908-SVXY R-WIRF-V1VI R-WJZC-8TM7
func TestRecordFormatting(t *testing.T) {
	f := pageSetup(t)
	r := pageRequest(f, "GET", "/", "viewer")
	at := time.Date(2026, 10, 5, 10, 0, 7, 0, time.FixedZone("offset", 3600))
	m := store.MemberRow{Member: store.Member{Candidate: "candidate-chosen", Added: at}, Pieces: []store.PieceRow{{Piece: store.Piece{Channel: store.ChannelEmail, State: store.StatePlanned, Delivered: at, DeliveredUserID: "ignore", DeliveredUserEmail: "mgreenly+fake01@gmail.com", How: store.HowHand}, Engagement: store.PieceEngagement{First: at, Last: at}}}}
	want := MemberRow{Candidate: m.Member.Candidate, Added: "2026-10-05T09:00:07Z", AddedText: "2026-10-05 09:00", Pieces: []PieceRow{{Channel: store.ChannelMail}, {Channel: store.ChannelEmail, State: store.StatePlanned}}}
	if got := memberRow(f.cfg, r, []string{store.ChannelMail, store.ChannelEmail}, m); !reflect.DeepEqual(got, want) {
		t.Fatalf("member %+v want %+v", got, want)
	}
	for _, state := range []string{store.StateDelivered, store.StateDropped} {
		m.Pieces[0].Piece.State = state
		m.Pieces[0].Engagement = store.PieceEngagement{Opens: 2, First: at, Last: at.Add(5 * time.Minute)}
		p := pieceRow(m.Pieces[0])
		if p.First != "2026-10-05T09:00:07Z" || p.FirstText != "2026-10-05 09:00" || p.LastText != "2026-10-05 09:05" || p.Opens != 2 {
			t.Fatal(p)
		}
		if state == store.StateDelivered {
			if p.Delivered != "2026-10-05T09:00:07Z" || p.DeliveredText != "2026-10-05 09:00" || p.DeliveredUser != "ignore" || p.How != store.HowHand {
				t.Fatal(p)
			}
		} else if p.Delivered != "" || p.DeliveredText != "" || p.DeliveredBy != "" || p.DeliveredUser != "" || p.How != "" {
			t.Fatal(p)
		}
	}
	for _, layout := range []string{MinuteLayout, SecondLayout} {
		a, b := stamp(time.Time{}, layout)
		if a != "" || b != "" {
			t.Fatal(a, b)
		}
	}
}

type candidateProbe struct {
	mu       sync.Mutex
	mode     string
	ids      []string
	users    []identity.Caller
	business map[string]any
}

func candidateFake(t *testing.T, f *pageFixture) (*candidateProbe, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "candidate.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	probe := &candidateProbe{business: map[string]any{"id": "cand_0000000000000001", "status": "active", "name": "Display name chosen", "names": []string{"private-other-name"}, "street": "private-street", "city": "private-city", "state": "private-state", "zip": "private-zip", "county": "private-county", "phone": "private-phone", "email": "mgreenly+fake99@gmail.com", "people": []map[string]any{{"name": "private-person", "role": "private-role"}}}}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rpc struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
			t.Error(err)
			return
		}
		probe.mu.Lock()
		defer probe.mu.Unlock()
		probe.ids = append(probe.ids, rpc.Params.Arguments["candidate"])
		probe.users = append(probe.users, identity.Caller{UserID: r.Header.Get("X-User-Id"), Email: r.Header.Get("X-User-Email")})
		if rpc.Method != "tools/call" || rpc.Params.Name != "candidate" {
			t.Errorf("unexpected sibling call %+v", rpc)
		}
		if probe.mode == "500" {
			w.WriteHeader(500)
			return
		}
		if probe.mode == "disconnect" {
			h, ok := w.(http.Hijacker)
			if !ok {
				t.Error("hijack unavailable")
				return
			}
			conn, _, err := h.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		result := map[string]any{"content": []any{}, "structuredContent": map[string]any{"candidate": probe.business}}
		if probe.mode == "refusal" {
			result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "not_found: fixture"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result}); err != nil {
			t.Error(err)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	f.cfg.Candidates = candidates.NewClient(candidates.ClientConfig{Service: services.Entry{Name: "candidates", Socket: socket}, Telemetry: f.writer})
	return probe, socket
}
func (p *candidateProbe) reset(mode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = mode
	p.ids = nil
	p.users = nil
}
func (p *candidateProbe) recorded() ([]string, []identity.Caller) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.ids), slices.Clone(p.users)
}
func writeServices(t *testing.T, path, candidateURL, artifactURL string) {
	t.Helper()
	entries := []map[string]any{}
	for name, u := range map[string]string{"auth": "https://accounts.example.test", "candidates": candidateURL, "artifacts": artifactURL, "gmail": "https://gmail.example.test", "connections": "https://connections.example.test"} {
		entries = append(entries, map[string]any{"name": name, "url": u, "description": "fixture", "socket": "/unused/fixture.sock", "enabled": true, "mcp": true})
	}
	raw, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func snapshot(t *testing.T, f *pageFixture, id, piece string) string {
	t.Helper()
	ctx := context.Background()
	cs, e1 := f.st.ListCampaigns(ctx, "", "", 500)
	c, e2 := f.st.GetCampaign(ctx, id)
	counts, e3 := f.st.Counts(ctx, id)
	ms, e4 := f.st.Materials(ctx, id)
	members, e5 := f.st.ListMembers(ctx, id, "", "", 500)
	p, e6 := f.st.GetPiece(ctx, piece)
	vs, e7 := f.st.ListVisits(ctx, piece, 0, 500)
	for _, err := range []error{e1, e2, e3, e4, e5, e6, e7} {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal([]any{cs, c, counts, ms, members, p, vs})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// R-WGBN-3IE4 R-WIRF-V1VI R-WJZC-8TM7 R-D1SN-LVRE R-DFW6-SFBA R-D30J-ZNI3 R-X8DB-W8G3
func TestCandidateNamesPrivacyAndReadOnly(t *testing.T) {
	f := pageSetup(t)
	c := f.create(t, 0)
	member := f.add(t, c.ID, 0)
	piece := member.Pieces[0].Piece
	f.now = f.now.Add(time.Minute)
	if _, _, err := f.st.DeliverPiece(context.Background(), store.PieceAction{Piece: piece.ID, UserID: "deliverer", UserEmail: "mgreenly+fake01@gmail.com"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.RejectMember(context.Background(), store.Rejection{Campaign: c.ID, Candidate: member.Member.Candidate, Why: "reason chosen", UserID: "rejecter"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		f.now = f.now.Add(time.Minute)
		if _, err := f.st.Visit(context.Background(), piece.Token, store.VisitInput{UserAgent: "agent-chosen"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 2 {
		if _, _, err := f.st.AddMaterial(context.Background(), store.Material{Campaign: c.ID, Path: fmt.Sprintf("material-%d", i), Label: fmt.Sprintf("label-%d", i), Artifact: fmt.Sprintf("art_%016x", i+1), AddedUserID: "material-adder"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.st.AddMaterial(context.Background(), store.Material{Campaign: c.ID, Path: "material-0", Label: "replacement label", Artifact: "art_0000000000000003", AddedUserID: "material-adder"}); err != nil {
		t.Fatal(err)
	}
	probe, socket := candidateFake(t, f)
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	f.cfg.ServicesPath = servicesPath
	path := "/campaigns/" + c.ID
	before := snapshot(t, f, c.ID, piece.ID)
	for _, mode := range []string{"", "refusal", "500", "disconnect", "absent", "nil"} {
		probe.reset(mode)
		writeServices(t, servicesPath, "https://candidates.example.test", "https://artifacts.example.test")
		candidateClient := f.cfg.Candidates
		if mode == "nil" {
			f.cfg.Candidates = nil
		}
		if mode == "absent" {
			f.cfg.Candidates = candidates.NewClient(candidates.ClientConfig{Service: services.Entry{Name: "candidates", Socket: socket + "-missing"}, Telemetry: f.writer})
		}
		rows, err := f.st.ListMembers(context.Background(), c.ID, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		counts, err := f.st.Counts(context.Background(), c.ID)
		if err != nil {
			t.Fatal(err)
		}
		material, err := f.st.Materials(context.Background(), c.ID)
		if err != nil {
			t.Fatal(err)
		}
		name := ""
		if mode == "" {
			name = "Display name chosen"
		}
		data := CampaignData{Banner: expectedBanner(f, path, []page.Level{{Name: c.ID, URL: path}}), Campaign: expectedCampaign(c, counts), Material: []MaterialRow{}, Members: []MemberRow{expectedMember(rows[0], c.Channels, "https://candidates.example.test", name)}}
		for _, m := range material {
			data.Material = append(data.Material, MaterialRow{Label: m.Label, Path: m.Path, ArtifactPage: urls.ArtifactPage("https://artifacts.example.test", m.Artifact)})
		}
		assertTemplate(t, f, path, "campaign", 200, data)
		ids, users := probe.recorded()
		if mode != "nil" && mode != "absent" {
			if !reflect.DeepEqual(ids, []string{member.Member.Candidate, member.Member.Candidate}) {
				t.Fatal("candidate requests", ids)
			}
			for _, u := range users {
				if u.UserID != "viewer" || u.Email != "mgreenly+fake00@gmail.com" {
					t.Fatal("forwarded caller", u)
				}
			}
		} else if len(ids) != 0 {
			t.Fatal(ids)
		}
		probe.reset(mode)
		a := pageResponse(f, "GET", path, "viewer")
		b := pageResponse(f, "GET", path, "other-viewer")
		if a.Code != b.Code || a.Body.String() != b.Body.String() || !reflect.DeepEqual(a.Header(), b.Header()) {
			t.Fatal("user-scoped page")
		}
		hidden := []string{"private-other-name", "private-street", "private-city", "private-state", "private-zip", "private-county", "private-phone", "mgreenly+fake99@gmail.com", "private-person", "private-role", piece.Token}
		for _, target := range []string{path, "/", "/tools", "/about", "/nope"} {
			response := pageResponse(f, "GET", target, "viewer")
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatal("cookie")
			}
			for _, secret := range hidden {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("private field leaked: %s", secret)
				}
			}
		}
		if got := snapshot(t, f, c.ID, piece.ID); got != before {
			t.Fatal("page changed store")
		}
		probe.reset(mode)
		for _, target := range []string{"/", "/tools", "/about", "/nope", "/?page=bogus", "/campaigns/camp_ffffffffffffffff", "/campaigns/" + c.ID + "?page=last"} {
			_ = pageResponse(f, "GET", target, "viewer")
		}
		if ids, _ := probe.recorded(); len(ids) != 0 {
			t.Fatal("unneeded candidates call", ids)
		}
		f.cfg.Candidates = candidateClient
	}
	// Sibling addresses are read on each request, including their disappearance.
	writeServices(t, servicesPath, "https://new-candidates.example.test", "https://new-artifacts.example.test")
	f.cfg.Candidates = nil
	row, err := f.st.GetMember(context.Background(), c.ID, member.Member.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := f.st.Counts(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := f.st.Materials(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range []string{servicesPath, ""} {
		f.cfg.ServicesPath = sp
		candidateBase, artifactBase := "", ""
		if sp != "" {
			candidateBase = "https://new-candidates.example.test"
			artifactBase = "https://new-artifacts.example.test"
		}
		data := CampaignData{Banner: expectedBanner(f, path, []page.Level{{Name: c.ID, URL: path}}), Campaign: expectedCampaign(c, counts), Material: []MaterialRow{}, Members: []MemberRow{expectedMember(row, c.Channels, candidateBase, "")}}
		for _, m := range ms {
			data.Material = append(data.Material, MaterialRow{Label: m.Label, Path: m.Path, ArtifactPage: urls.ArtifactPage(artifactBase, m.Artifact)})
		}
		assertTemplate(t, f, path, "campaign", 200, data)
	}
}

func randomFixture(seed int) []byte {
	out := make([]byte, 0, 32768)
	for i := range 1024 {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", seed, i)))
		out = append(out, sum[:]...)
	}
	return out
}

// R-D1SN-LVRE R-DFW6-SFBA
func TestOnlyShownMembersReachCandidates(t *testing.T) {
	f := pageSetup(t)
	c := f.create(t, 0)
	for i := range 27 {
		f.add(t, c.ID, i)
	}
	probe, socket := candidateFake(t, f)
	// Every sibling address shares the probe, so any attempted sibling call is observable.
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	entries := []map[string]any{}
	for _, name := range []string{"auth", "telemetry", "events", "artifacts", "gmail", "connections", "repos", "scripts", "cron", "home", "webhooks", "prompts", "gdrive", "scrapes", "candidates"} {
		entries = append(entries, map[string]any{"name": name, "url": "https://" + name + ".example.test", "description": "fixture", "socket": socket, "enabled": true, "mcp": true})
	}
	raw, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(servicesPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.cfg.ServicesPath = servicesPath
	rows, err := f.st.ListMembers(context.Background(), c.ID, "", "", 27)
	if err != nil {
		t.Fatal(err)
	}
	token := (paging.Token{Tool: "campaign"}).Encode(rows[24].Member.Added.UTC().Format(time.RFC3339), rows[24].Member.Candidate)
	for _, tc := range []struct {
		target     string
		start, end int
	}{{"/campaigns/" + c.ID, 0, 25}, {"/campaigns/" + c.ID + "?page=" + token, 25, 27}} {
		probe.reset("")
		response := pageResponse(f, "GET", tc.target, "viewer")
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
		ids, users := probe.recorded()
		want := []string{}
		for _, m := range rows[tc.start:tc.end] {
			want = append(want, m.Member.Candidate)
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatal("shown requests", ids, want)
		}
		for _, u := range users {
			if u.UserID != "viewer" || u.Email != "mgreenly+fake00@gmail.com" {
				t.Fatal(u)
			}
		}
	}
	probe.reset("")
	for _, target := range []string{"/", "/tools", "/about", "/nope"} {
		if pageResponse(f, "GET", target, "viewer").Code == 500 {
			t.Fatal(target)
		}
	}
	if ids, _ := probe.recorded(); len(ids) != 0 {
		t.Fatal("unexpected sibling request", ids)
	}
}

// R-WSIM-X7T2 R-WUYF-ORAG
func TestCampaignWindowBoundariesAndDeletedRows(t *testing.T) {
	f := pageSetup(t)
	var ordered []store.CampaignRow
	for i := range paging.DefaultLimit {
		ordered = append(ordered, store.CampaignRow{Campaign: f.create(t, i)})
	}
	slices.Reverse(ordered)
	assertTemplate(t, f, "/", "landing", 200, LandingData{Banner: expectedBanner(f, "/", nil), Campaigns: expectedLanding(ordered)})
	for _, method := range []string{"GET", "HEAD"} {
		assertBare(t, pageResponse(f, method, "/?page=last", "viewer"), 303, http.Header{"Location": {"/"}})
	}
	newest := f.create(t, 25)
	ordered = append([]store.CampaignRow{{Campaign: newest}}, ordered...)
	last := ordered[24].Campaign
	next := (paging.Token{Tool: "campaigns"}).Encode(last.Created.Format(time.RFC3339), last.ID)
	assertTemplate(t, f, "/", "landing", 200, LandingData{Banner: expectedBanner(f, "/", nil), Campaigns: expectedLanding(ordered[:25]), Next: next})
	oldest := ordered[25].Campaign
	target := "/?page=" + next
	assertTemplate(t, f, target, "landing", 200, LandingData{Banner: expectedBanner(f, target, nil), Campaigns: expectedLanding(ordered[25:]), Page: next, Prev: (paging.Token{Tool: "campaigns-before"}).Encode(oldest.Created.Format(time.RFC3339), oldest.ID)})
	if _, _, err := f.st.DeleteCampaign(context.Background(), newest.ID); err != nil {
		t.Fatal(err)
	}
	assertTemplate(t, f, "/", "landing", 200, LandingData{Banner: expectedBanner(f, "/", nil), Campaigns: expectedLanding(ordered[1:])})
}
