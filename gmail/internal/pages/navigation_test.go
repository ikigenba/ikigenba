package pages

import (
	"bytes"
	"context"
	"encoding/binary"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
)

func navigationPath(d, x string) string {
	return "/?" + url.Values{"drafts_page": {d}, "transfers_page": {x}}.Encode()
}

// R-GW9F-UM50 R-GYP8-M5ME R-GZX4-ZXD3 R-H151-DP3S R-H2CX-RGUH
// R-H60M-WS2K R-H9OC-23AN R-HC44-TMS1 R-HDC1-7EIQ
func TestLandingReverseAndLast(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 3} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			_, database, clock := fixture(t)
			seq := make([]byte, (2*count+4)*8)
			for i := 0; i < len(seq)/8; i++ {
				binary.BigEndian.PutUint64(seq[i*8:], uint64(i+1))
			}
			now := clock()
			st := store.New(database, store.Config{Now: func() time.Time { return now }, Rand: bytes.NewReader(seq)})
			ctx := context.Background()
			for _, user := range []string{"owner", "other"} {
				n := count
				if user == "other" {
					n = 1
				}
				for i := 0; i < n; i++ {
					now = clock().Add(time.Duration(i/2) * time.Second)
					if _, err := st.AddDraft(ctx, store.Draft{UserID: user, GmailDraft: user + strconv.Itoa(i), Message: "message", Subject: user + "subject" + strconv.Itoa(i)}); err != nil {
						t.Fatal(err)
					}
					if _, err := st.AddTransfer(ctx, store.Transfer{UserID: user, Message: "message", Path: user + "/" + strconv.Itoa(i), Name: user + "name" + strconv.Itoa(i), Direction: store.DirectionPull, Status: store.StatusQueued}); err != nil {
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
			var captured LandingData
			tpl := template.Must(template.New("landing").Funcs(template.FuncMap{"capture": func(d LandingData) string { captured = d; return "" }}).Parse("{{capture .}}"))
			h := identity.Require(Handler(Config{Banner: banner, Pages: load(t), Store: st}))
			capHandler := identity.Require(Handler(Config{Banner: banner, Pages: &Set{templates: tpl}, Store: st}))
			get := func(d, x string) LandingData {
				t.Helper()
				p := navigationPath(d, x)
				if w := request(capHandler, http.MethodGet, p, "owner"); w.Code != 200 {
					t.Fatalf("get %s status %d", p, w.Code)
				}
				check(t, request(h, http.MethodGet, p, "owner"), 200, "landing", captured)
				head := request(h, http.MethodHead, p, "owner")
				if head.Code != 200 || head.Body.Len() != 0 {
					t.Fatal("HEAD window", head.Code)
				}
				return captured
			}
			redirect := func(d, x, location string) {
				t.Helper()
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					w := request(h, method, navigationPath(d, x)+"&page=bogus&limit=1", "owner")
					if w.Code != 303 || !reflect.DeepEqual(w.Header().Values("Location"), []string{location}) || w.Body.Len() != 0 {
						t.Fatalf("redirect %s %s got %d %v %q", d, x, w.Code, w.Header(), w.Body.String())
					}
				}
			}
			keyD := func(i int, name string) string {
				return (paging.Token{Tool: name}).Encode(ds[i].Created.UTC().Format(time.RFC3339), ds[i].ID)
			}
			keyX := func(i int, name string) string {
				return (paging.Token{Tool: name}).Encode(xs[i].Started.UTC().Format(time.RFC3339), xs[i].ID)
			}
			rows := func(lo, hi int) LandingData {
				b := expectedBanner()
				b.Trail = nil
				want := LandingData{Banner: b, Drafts: []DraftRow{}, Transfers: []TransferRow{}}
				for _, d := range ds[lo:hi] {
					want.Drafts = append(want.Drafts, DraftRow{ID: d.ID, Status: d.Status, Subject: d.Subject, To: d.To, Cc: d.Cc, Bcc: d.Bcc, AttachmentCount: len(d.Attachments), Link: d.Link, Created: d.Created.UTC().Format(time.RFC3339), CreatedText: d.Created.UTC().Format(MinuteLayout)})
				}
				for _, x := range xs[lo:hi] {
					want.Transfers = append(want.Transfers, TransferRow{ID: x.ID, Direction: x.Direction, Status: x.Status, Name: x.Name, Path: x.Path, Started: x.Started.UTC().Format(time.RFC3339), StartedText: x.Started.UTC().Format(MinuteLayout)})
				}
				return want
			}
			if count <= paging.DefaultLimit {
				redirect("last", "last", "/")
				redirect("last", "", "/")
				redirect("", "last", "/")
				return
			}
			var forward []LandingData
			dp, xp := "", ""
			for {
				data := get(dp, xp)
				forward = append(forward, data)
				if data.DraftsNext == "" {
					break
				}
				dp, xp = data.DraftsNext, data.TransfersNext
			}
			// Each list walks backward while retaining the other list's after-token.
			for _, drafts := range []bool{true, false} {
				other := xp
				current := dp
				if !drafts {
					other = dp
					current = xp
				}
				for i := len(forward) - 1; i > 0; i-- {
					d, x := current, other
					if !drafts {
						d, x = other, current
					}
					got := get(d, x)
					if drafts {
						if !reflect.DeepEqual(got.Drafts, forward[i].Drafts) || !reflect.DeepEqual(got.Transfers, forward[len(forward)-1].Transfers) {
							t.Fatal("backward draft windows", i)
						}
						current = got.DraftsPrev
					} else {
						if !reflect.DeepEqual(got.Transfers, forward[i].Transfers) || !reflect.DeepEqual(got.Drafts, forward[len(forward)-1].Drafts) {
							t.Fatal("backward transfer windows", i)
						}
						current = got.TransfersPrev
					}
				}
				if drafts {
					redirect(current, other, "/?transfers_page="+other)
				} else {
					redirect(other, current, "/?drafts_page="+other)
				}
			}
			// Last is aligned to the oldest records even when the Next walk ends short.
			last := get("last", "last")
			want := rows(count-paging.DefaultLimit, count)
			want.DraftsPage, want.TransfersPage = "last", "last"
			want.DraftsPrev = keyD(count-paging.DefaultLimit, "drafts-before")
			want.TransfersPrev = keyX(count-paging.DefaultLimit, "transfers-before")
			if !reflect.DeepEqual(last, want) {
				t.Fatalf("last window got %+v want %+v", last, want)
			}
			// All four accepted modes preserve the other list's full data and nav tokens.
			for _, values := range [][2]string{{"", xp}, {dp, xp}, {"last", xp}, {"", xp}, {dp, ""}, {dp, "last"}} {
				got := get(values[0], values[1])
				fixed := forward[len(forward)-1]
				if values[1] == xp && (!reflect.DeepEqual(got.Transfers, fixed.Transfers) || got.TransfersPage != fixed.TransfersPage || got.TransfersPrev != fixed.TransfersPrev || got.TransfersNext != fixed.TransfersNext) {
					t.Fatal("changed independent transfers")
				}
				if values[0] == dp && (!reflect.DeepEqual(got.Drafts, fixed.Drafts) || got.DraftsPage != fixed.DraftsPage || got.DraftsPrev != fixed.DraftsPrev || got.DraftsNext != fixed.DraftsNext) {
					t.Fatal("changed independent drafts")
				}
			}
			// Synthetic positions prove a before-token does not need an extant record.
			if count > paging.DefaultLimit+1 {
				pos := count - 1
				d := (paging.Token{Tool: "drafts-before"}).Encode(ds[pos].Created.UTC().Format(time.RFC3339), ds[pos].ID+"!")
				x := (paging.Token{Tool: "transfers-before"}).Encode(xs[pos].Started.UTC().Format(time.RFC3339), xs[pos].ID+"!")
				got := get(d, x)
				want = rows(pos-paging.DefaultLimit, pos)
				want.DraftsPage, want.TransfersPage = d, x
				want.DraftsPrev = keyD(pos-paging.DefaultLimit, "drafts-before")
				want.TransfersPrev = keyX(pos-paging.DefaultLimit, "transfers-before")
				want.DraftsNext = keyD(pos-1, "drafts")
				want.TransfersNext = keyX(pos-1, "transfers")
				if !reflect.DeepEqual(got, want) {
					t.Fatal("synthetic before position", got, want)
				}
				dOnly := get(d, xp)
				xOnly := get(dp, x)
				fixed := forward[len(forward)-1]
				if !reflect.DeepEqual(dOnly.Drafts, want.Drafts) || dOnly.DraftsPage != want.DraftsPage || dOnly.DraftsPrev != want.DraftsPrev || dOnly.DraftsNext != want.DraftsNext || !reflect.DeepEqual(dOnly.Transfers, fixed.Transfers) || dOnly.TransfersPage != fixed.TransfersPage || dOnly.TransfersPrev != fixed.TransfersPrev || dOnly.TransfersNext != fixed.TransfersNext {
					t.Fatal("independent before draft window")
				}
				if !reflect.DeepEqual(xOnly.Transfers, want.Transfers) || xOnly.TransfersPage != want.TransfersPage || xOnly.TransfersPrev != want.TransfersPrev || xOnly.TransfersNext != want.TransfersNext || !reflect.DeepEqual(xOnly.Drafts, fixed.Drafts) || xOnly.DraftsPage != fixed.DraftsPage || xOnly.DraftsPrev != fixed.DraftsPrev || xOnly.DraftsNext != fixed.DraftsNext {
					t.Fatal("independent before transfer window")
				}
			}
			firstD, firstX := keyD(0, "drafts-before"), keyX(0, "transfers-before")
			redirect(firstD, firstX, "/")
			redirect(firstD, "last", "/?transfers_page=last")
			redirect("last", firstX, "/?drafts_page=last")
			// An empty after-window is still a page, never an underflow redirect.
			empty := get(keyD(count-1, "drafts"), keyX(count-1, "transfers"))
			if empty.Drafts == nil || empty.Transfers == nil || len(empty.Drafts) != 0 || len(empty.Transfers) != 0 || empty.DraftsPrev != "" || empty.TransfersPrev != "" || empty.DraftsNext != "" || empty.TransfersNext != "" {
				t.Fatal("empty after nav", empty)
			}
		})
	}
}

// R-H9OC-23AN R-H151-DP3S
func TestLandingAsymmetricLastUnderflow(t *testing.T) {
	for _, longDrafts := range []bool{true, false} {
		t.Run(strconv.FormatBool(longDrafts), func(t *testing.T) {
			_, database, clock := fixture(t)
			seq := make([]byte, (3*paging.DefaultLimit+4)*8)
			for i := 0; i < len(seq)/8; i++ {
				binary.BigEndian.PutUint64(seq[i*8:], uint64(i+1))
			}
			st := store.New(database, store.Config{Now: clock, Rand: bytes.NewReader(seq)})
			for i := 0; i <= paging.DefaultLimit; i++ {
				if longDrafts || i < paging.DefaultLimit {
					if _, err := st.AddDraft(context.Background(), store.Draft{UserID: "owner", GmailDraft: strconv.Itoa(i), Message: "message"}); err != nil {
						t.Fatal(err)
					}
				}
				if !longDrafts || i < paging.DefaultLimit {
					if _, err := st.AddTransfer(context.Background(), store.Transfer{UserID: "owner", Direction: store.DirectionPull, Status: store.StatusQueued, Message: "message", Path: "out/file"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			h := identity.Require(Handler(Config{Banner: banner, Pages: load(t), Store: st}))
			location := "/?transfers_page=last"
			if longDrafts {
				location = "/?drafts_page=last"
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := request(h, method, navigationPath("last", "last"), "owner")
				if w.Code != 303 || !reflect.DeepEqual(w.Header().Values("Location"), []string{location}) || w.Body.Len() != 0 {
					t.Fatal("asymmetric last", w.Code, w.Header(), w.Body.String())
				}
			}
		})
	}
}
