package server

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func profileFixtureOrder(tokens []store.Token) {
	sort.Slice(tokens, func(i, j int) bool {
		a, b := tokens[i], tokens[j]
		if a.LastUsedAt == nil && b.LastUsedAt != nil {
			return false
		}
		if a.LastUsedAt != nil && b.LastUsedAt == nil {
			return true
		}
		if a.LastUsedAt != nil && !a.LastUsedAt.Equal(*b.LastUsedAt) {
			return a.LastUsedAt.After(*b.LastUsedAt)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID < b.ID
	})
}

func expectedProfileClient(token store.Token, draw time.Time) mcpClientData {
	row := mcpClientData{ID: token.ID, Name: token.Name, Approved: token.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), ApprovedText: token.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), Expires: token.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"), ExpiresText: token.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"), Expired: !token.ExpiresAt.After(draw)}
	if token.LastUsedAt != nil {
		row.LastUsed = token.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
		row.LastUsedTitle = token.LastUsedAt.UTC().Format("2006-01-02 15:04 UTC")
		row.LastUsedElapsed = expectedElapsed(draw.Sub(*token.LastUsedAt))
	}
	return row
}

func TestProfileFirstPageBoundaries(t *testing.T) {
	// R-9PA4-5RO4 R-9SXT-B2W7 R-9QI0-JJET: independent per-kind first pages, complete data and exact template output.
	counts := []int{0, paging.DefaultLimit - 1, paging.DefaultLimit, paging.DefaultLimit + 1, 2 * paging.DefaultLimit}
	for _, personalCount := range counts {
		for _, clientCount := range counts {
			t.Run(fmt.Sprintf("personal=%d/client=%d", personalCount, clientCount), func(t *testing.T) {
				st := openTokenTestStore(t)
				user, session := tokenTestIdentity(t, st, "first-page")
				other, _ := tokenTestIdentity(t, st, "foreign-first-page")
				foreignPersonal, _, err := st.CreateToken(other.ID, "foreign personal", store.ExpiryNever, tokenTestNow)
				if err != nil {
					t.Fatal(err)
				}
				foreignClient, _ := mintProfileClient(t, st, other.ID, "foreign client", tokenTestNow)
				var personal, clients []store.Token
				for _, kind := range []store.TokenKind{store.TokenPersonal, store.TokenClient} {
					count := personalCount
					if kind == store.TokenClient {
						count = clientCount
					}
					for i := range count {
						created := tokenTestNow.Add(-time.Duration(i%4) * time.Hour)
						var token store.Token
						var secret string
						if kind == store.TokenPersonal {
							token, secret, err = st.CreateToken(user.ID, fmt.Sprintf("personal fixture %d", i), store.Expiry90d, created)
							if err != nil {
								t.Fatal(err)
							}
							if i%3 == 0 {
								if err = st.SetTokenEnabled(user.ID, token.ID, false); err != nil {
									t.Fatal(err)
								}
								token.Enabled = false
							}
						} else {
							token, secret = mintProfileClient(t, st, user.ID, fmt.Sprintf("client fixture %d", i), created)
						}
						if i%3 != 0 {
							used := tokenTestNow.Add(-time.Duration(i%5) * time.Minute)
							if _, err = st.TouchTokenIdentity(secret, "mcp.green.example", used); err != nil {
								t.Fatal(err)
							}
							token.LastUsedAt = &used
						}
						if kind == store.TokenPersonal {
							personal = append(personal, token)
						} else {
							clients = append(clients, token)
						}
					}
				}
				profileFixtureOrder(personal)
				profileFixtureOrder(clients)
				expected := profilePageData{Apex: "localhost", Email: user.Email, Workspace: "green.example", Rows: make([]tokenRowData, 0), Next: personalCount > paging.DefaultLimit, Create: tokenCreateData{Expiry: "90d"}, Clients: mcpClientsData{Clients: make([]mcpClientData, 0), ClientsNext: clientCount > paging.DefaultLimit}}
				for _, token := range personal[:min(len(personal), paging.DefaultLimit)] {
					expected.Rows = append(expected.Rows, expectedTokenRow(token, tokenTestNow))
				}
				for _, token := range clients[:min(len(clients), paging.DefaultLimit)] {
					expected.Clients.Clients = append(expected.Clients.Clients, expectedProfileClient(token, tokenTestNow))
				}
				srv := tokenTestServer(t, st)
				expected.Workspace = srv.cfg.WorkspaceDomain
				rows, next, err := srv.tokenRows(user.ID, tokenTestNow)
				if err != nil || next != expected.Next || !reflect.DeepEqual(rows, expected.Rows) {
					t.Fatalf("personal page=%#v next=%v err=%v", rows, next, err)
				}
				clientData, err := srv.profileClients(user.ID, tokenTestNow)
				if err != nil || !reflect.DeepEqual(clientData, expected.Clients) {
					t.Fatalf("client page=%#v err=%v", clientData, err)
				}
				w := httptest.NewRecorder()
				r := tokenProfileRequest(session.ID)
				r.URL.RawQuery = "page=untrusted&limit=1"
				srv.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("profile status=%d", w.Code)
				}
				assertAuthTemplate(t, w.Body.String(), "page", authPageData{Banner: testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}), Profile: &expected})
				excluded := []store.Token{foreignPersonal, foreignClient}
				excluded = append(excluded, personal[min(len(personal), paging.DefaultLimit):]...)
				excluded = append(excluded, clients[min(len(clients), paging.DefaultLimit):]...)
				for _, token := range excluded {
					if strings.Contains(w.Body.String(), token.ID) {
						t.Fatalf("profile exposed excluded token %s", token.ID)
					}
				}
			})
		}
	}
}

func TestTokenActionsBeyondProfileFirstPage(t *testing.T) {
	// R-5HLP-AIJE R-A6CP-IK1U R-9U5P-OUMW: hidden tokens remain directly actionable; toggle attribution uses the target's state.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "hidden-action")
	target, secret, err := st.CreateToken(user.ID, "old hidden target", store.ExpiryNever, tokenTestNow.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i := range paging.DefaultLimit + 1 {
		if _, _, err := st.CreateToken(user.ID, fmt.Sprintf("new visible %d", i), store.ExpiryNever, tokenTestNow); err != nil {
			t.Fatal(err)
		}
	}
	f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
	profile, _ := f.request(t, tokenProfileRequest(session.ID))
	if profile.Code != 200 || strings.Contains(profile.Body.String(), target.ID) {
		t.Fatal("target must lie beyond profile first page")
	}
	for _, tc := range []struct{ action, event string }{{"enable", ""}, {"disable", "token.disabled"}, {"disable", ""}, {"enable", "token.enabled"}, {"delete", "token.deleted"}} {
		w, events := f.request(t, tokenActionRequest(session.ID, target.ID, tc.action))
		if w.Code != 302 || w.Header().Get("Location") != "/" {
			t.Fatalf("hidden %s status=%d", tc.action, w.Code)
		}
		assertTokenDomainTrail(t, events, user.ID, tc.event, target.ID)
		got, err := st.FindToken(user.ID, target.ID)
		if tc.action == "delete" {
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("deleted target=%#v %v", got, err)
			}
		} else if err != nil || got.Enabled != (tc.action == "enable") {
			t.Fatalf("target state=%#v %v", got, err)
		}
	}
	if _, err := st.LookupTokenIdentity(secret, "mcp.green.example", tokenTestNow); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted secret identity=%v", err)
	}
}
