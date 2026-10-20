package links_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/links"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

type fixture struct {
	d        *db.DB
	st       *store.Store
	h        http.Handler
	capture  *telemetry.Capture
	writer   *telemetry.Writer
	campaign store.Campaign
	piece    store.Piece
	now      time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 10, 5, 9, 0, 7, 555, time.FixedZone("offset", 3600))}
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "campaigns.db"), Migrations: campaigns.Migrations(), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.Close() })
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(randomFixture(7))})
	f.campaign, err = f.st.CreateCampaign(context.Background(), store.Campaign{Name: "a campaign", CreatedUserID: "creator", Channels: []string{store.ChannelMail}, Destination: "https://destination.example/"})
	if err != nil {
		t.Fatal(err)
	}
	member, _, err := f.st.AddMember(context.Background(), store.MemberInput{UserID: "actor", Campaign: f.campaign.ID, Candidate: "cand_1111111111111111", Channels: []string{store.ChannelMail}})
	if err != nil {
		t.Fatal(err)
	}
	f.piece = member.Pieces[0].Piece
	f.capture = &telemetry.Capture{}
	f.writer = telemetry.New(telemetry.Config{Service: "campaigns", Sink: f.capture, Now: func() time.Time { return f.now }, Rand: bytes.NewReader(randomFixture(8)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { f.writer.Shutdown(context.Background(), "test") })
	f.h = links.Handler(links.Config{Store: f.st, Telemetry: f.writer, DefaultDestination: "https://fallback.example/"})
	return f
}
func request(t *testing.T, f *fixture, method, path string, headers http.Header, caller *identity.Caller) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.Header = headers
	r.RemoteAddr = "192.0.2.1:5000"
	if caller != nil {
		r = r.WithContext(identity.NewContext(r.Context(), *caller))
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func answer(t *testing.T, w *httptest.ResponseRecorder, destination string) {
	t.Helper()
	want := http.Header{"Location": {destination}, "Cache-Control": {"no-store"}, "Content-Length": {"0"}}
	if w.Code != 302 || !reflect.DeepEqual(w.Header(), want) || w.Body.Len() != 0 {
		t.Fatalf("answer = %d %v %q", w.Code, w.Header(), w.Body.String())
	}
}
func visits(t *testing.T, f *fixture) []store.Visit {
	t.Helper()
	v, err := f.st.ListVisits(context.Background(), f.piece.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func flush(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// R-ZG45-OPXW R-ZIJY-G9FA R-ZJRU-U15Z R-ZONG-D44R R-ZKZR-7SWO R-ZM7N-LKND
func TestVisits(t *testing.T) {
	f := setup(t)
	for i, method := range []string{"GET", "GET", "HEAD"} {
		f.now = f.now.Add(time.Minute)
		token := f.piece.Token
		if i == 2 {
			token = strings.ToUpper(token)
		}
		headers := http.Header{"User-Agent": {"first agent", "second agent"}, "X-Real-Ip": {"203.0.113.7", "203.0.113.9"}, "Referer": {"https://header.example/", "https://ignored.example/"}}
		query := "?r=https%3A%2F%2Fquery.example%2F&r=ignored"
		if i == 1 {
			query = "?r="
		}
		if i == 2 {
			headers = nil
			query = ""
		}
		answer(t, request(t, f, method, "/t/"+token+query, headers, nil), f.campaign.Destination)
		got := visits(t, f)
		if len(got) != i+1 {
			t.Fatalf("visits %d", len(got))
		}
		want := store.Visit{Seq: got[0].Seq, Piece: f.piece.ID, At: f.now.UTC().Truncate(time.Second)}
		if i < 2 {
			want.UserAgent = "first agent"
			want.Address = "203.0.113.7"
			want.Referrer = "https://query.example/"
			if i == 1 {
				want.Referrer = "https://header.example/"
			}
		}
		if !reflect.DeepEqual(got[0], want) {
			t.Fatalf("visit %+v want %+v", got[0], want)
		}
	}
	destination := "https://changed.example/"
	if _, _, err := f.st.EditCampaign(context.Background(), store.CampaignEdit{ID: f.campaign.ID, Destination: &destination}); err != nil {
		t.Fatal(err)
	}
	answer(t, request(t, f, "GET", "/t/"+f.piece.Token, nil, nil), destination)
	if len(visits(t, f)) != 4 {
		t.Fatal("changed destination lost visit")
	}
}

// R-29O9-GYYQ
func TestPieceStatesAndRejectionRemainVisitable(t *testing.T) {
	for _, state := range []string{store.StatePlanned, store.StateDelivered, store.StateDropped} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			var err error
			switch state {
			case store.StateDelivered:
				_, _, err = f.st.DeliverPiece(context.Background(), store.PieceAction{UserID: "actor", Piece: f.piece.ID})
			case store.StateDropped:
				_, _, err = f.st.DropPiece(context.Background(), store.PieceAction{UserID: "actor", Piece: f.piece.ID})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = f.st.RejectMember(context.Background(), store.Rejection{UserID: "actor", Campaign: f.campaign.ID, Candidate: f.piece.Candidate, Why: "test rejection"}); err != nil {
				t.Fatal(err)
			}
			answer(t, request(t, f, "GET", "/t/"+f.piece.Token, nil, nil), f.campaign.Destination)
			if len(visits(t, f)) != 1 {
				t.Fatal("visit missing")
			}
		})
	}
}

// R-ZPVC-QVVG R-ZR39-4NM5 R-ZSB5-IFCU
func TestNonVisitsAndDatabaseFailure(t *testing.T) {
	f := setup(t)
	paths := []string{"/t/", "/t/" + f.piece.Token + "/", "/t/" + f.piece.Token + "/x", "/t/" + f.piece.Token[:25], "/t/" + f.piece.Token + "a", "/t/a%2F" + strings.Repeat("a", 24), "/x/" + f.piece.Token, "/t/" + strings.Repeat("a", 26)}
	for _, bad := range []string{"0", "1", "8", "9", "-", "."} {
		paths = append(paths, "/t/"+bad+strings.Repeat("a", 25))
	}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD"} {
			answer(t, request(t, f, method, path, nil, nil), "https://fallback.example/")
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE"} {
		answer(t, request(t, f, method, "/t/"+f.piece.Token, nil, nil), "https://fallback.example/")
	}
	if len(visits(t, f)) != 0 {
		t.Fatal("non-visit recorded")
	}
	flush(t, f)
	if len(f.capture.Events()) != 0 {
		t.Fatal("non-visit emitted")
	}
	if _, _, err := f.st.DeleteCampaign(context.Background(), f.campaign.ID); err != nil {
		t.Fatal(err)
	}
	answer(t, request(t, f, "GET", "/t/"+f.piece.Token, nil, nil), "https://fallback.example/")
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range append(paths, "/t/"+f.piece.Token) {
		answer(t, request(t, f, "GET", path, nil, nil), "https://fallback.example/")
	}
	flush(t, f)
	if len(f.capture.Events()) != 0 {
		t.Fatal("failed visit emitted")
	}
}

// R-ZTJ1-W73J R-2AW5-UQPF
func TestIdentityIgnoredAndVisitTrail(t *testing.T) {
	f := setup(t)
	caller := identity.Caller{UserID: "owner", Email: "mgreenly+fake00@gmail.com", RequestID: "request-chosen"}
	for i := range 3 {
		headers := http.Header{"User-Agent": {"same agent"}, "Referer": {"same referrer"}, "X-Real-Ip": {"same address"}}
		var c *identity.Caller
		if i == 0 {
			headers.Set("X-User-Id", caller.UserID)
			headers.Set("X-User-Email", caller.Email)
			headers.Set("Authorization", "Bearer fixture")
			headers.Set("Cookie", "fixture=value")
		}
		if i == 1 {
			c = &caller
		}
		answer(t, request(t, f, "GET", "/t/"+f.piece.Token, headers, c), f.campaign.Destination)
	}
	got := visits(t, f)
	if len(got) != 3 {
		t.Fatal(got)
	}
	for _, v := range got {
		if v.UserAgent != "same agent" || v.Referrer != "same referrer" || v.Address != "same address" {
			t.Fatal(v)
		}
	}
	flush(t, f)
	events := f.capture.Events()
	if len(events) != 3 {
		t.Fatal(events)
	}
	for i, e := range events {
		q := ""
		if i == 1 {
			q = caller.RequestID
		}
		if e.Name != trail.EventVisited || e.User != "" || e.RequestID != q || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"campaign": f.campaign.ID, "piece": f.piece.ID}) {
			t.Fatalf("event %+v", e)
		}
	}
}

// R-28GD-3781
func TestWireAnswersHaveOneShape(t *testing.T) {
	f := setup(t)
	f.h = links.Handler(links.Config{Store: f.st, Telemetry: f.writer, DefaultDestination: f.campaign.Destination})
	server := httptest.NewServer(f.h)
	defer server.Close()
	var baseline string
	for _, item := range []struct{ method, path string }{{"GET", "/t/" + f.piece.Token}, {"HEAD", "/t/" + strings.Repeat("a", 26)}, {"POST", "/t/" + strings.Repeat("a", 26)}, {"GET", "/different"}} {
		conn, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: example.test\r\n\r\n", item.method, item.path); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(conn)
		var wire strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(line, "Date:") {
				wire.WriteString(line)
			}
			if line == "\r\n" {
				break
			}
		}
		if err = conn.Close(); err != nil {
			t.Fatal(err)
		}
		if baseline == "" {
			baseline = wire.String()
		} else if baseline != wire.String() {
			t.Fatalf("wire\n%q\nwant\n%q", wire.String(), baseline)
		}
	}
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Get(server.URL + "/t/" + f.piece.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 302 || len(body) != 0 || len(resp.Header) != 4 || resp.Header.Get("Location") != f.campaign.Destination || resp.Header.Get("Content-Length") != "0" || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Date") == "" {
		t.Fatalf("wire response %d %v %q", resp.StatusCode, resp.Header, body)
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
