package server

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func TestProfileNavigationDataShape(_ *testing.T) {
	// R-V6G4-D6NQ
	var _ struct {
		Apex, Email, Workspace string
		Rows                   []tokenRowData
		Page, Prev, Next       string
		Create                 tokenCreateData
		Clients                mcpClientsData
	} = profilePageData{}
}

func fixturePageValue(token store.Token, tool string) string {
	var used, at int64
	if token.LastUsedAt != nil {
		used, at = 1, token.LastUsedAt.UnixNano()
	}
	return (paging.Token{Tool: tool}).Encode(used, at, token.CreatedAt.UnixNano(), token.ID)
}

func navigationTokens(t *testing.T, st *store.Store, userID string, kind store.TokenKind, count int) []store.Token {
	t.Helper()
	tokens := make([]store.Token, 0, count)
	for i := range count {
		created := tokenTestNow.Add(-time.Duration(i%4) * time.Hour)
		var token store.Token
		var secret string
		if kind == store.TokenPersonal {
			var err error
			token, secret, err = st.CreateToken(userID, fmt.Sprintf("walk personal %d", i), store.Expiry90d, created)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			token, secret = mintProfileClient(t, st, userID, fmt.Sprintf("walk client %d", i), created)
		}
		if i%3 != 0 {
			used := tokenTestNow.Add(-time.Duration(i%5) * time.Minute)
			if _, err := st.TouchTokenIdentity(secret, "mcp.green.example", used); err != nil {
				t.Fatal(err)
			}
			token.LastUsedAt = &used
		}
		tokens = append(tokens, token)
	}
	profileFixtureOrder(tokens)
	return tokens
}

type navigationWindow struct {
	tokens            []store.Token
	value, prev, next string
	underflows        bool
}

func navigationExpected(tokens []store.Token, tool, value string, start, end int, underflows bool) navigationWindow {
	expected := navigationWindow{tokens: tokens[start:end], value: value, underflows: underflows}
	if start != end {
		if value != "" && !underflows {
			expected.prev = fixturePageValue(tokens[start], tool+"-before")
		}
		var used, last, created int64
		var id string
		before := (paging.Token{Tool: tool + "-before"}).Decode(value, &used, &last, &created, &id) == nil
		if value != "last" && (before || end < len(tokens)) {
			expected.next = fixturePageValue(tokens[end-1], tool)
		}
	}
	return expected
}

func assertNavigationResponse(t *testing.T, srv *Server, sessionID, email string, personal, clients navigationWindow) {
	t.Helper()
	request := tokenProfileRequest(sessionID)
	query := url.Values{"tokens_page": {personal.value}, "clients_page": {clients.value}, "return": {"ignored"}, "limit": {"1"}}
	request.URL.RawQuery = query.Encode()
	response := httptest.NewRecorder()
	srv.ServeHTTP(response, request)
	if personal.underflows || clients.underflows {
		target := "/"
		if personal.value != "" && !personal.underflows {
			target += "?tokens_page=" + personal.value
		}
		if clients.value != "" && !clients.underflows {
			target += "?clients_page=" + clients.value
		}
		if response.Code != 303 || response.Header().Get("Location") != target {
			t.Fatalf("pages %q/%q redirect=%d %q want %q", personal.value, clients.value, response.Code, response.Header().Get("Location"), target)
		}
		return
	}
	if response.Code != 200 || response.Header().Get("Content-Type") != signInHTMLContentType {
		t.Fatalf("profile=%d %v", response.Code, response.Header())
	}
	expected := profilePageData{Apex: "localhost", Email: email, Workspace: srv.cfg.WorkspaceDomain, Rows: []tokenRowData{}, Page: personal.value, Prev: personal.prev, Next: personal.next, Create: tokenCreateData{Expiry: "90d"}, Clients: mcpClientsData{Clients: []mcpClientData{}, ClientsPage: clients.value, ClientsPrev: clients.prev, ClientsNext: clients.next, TokensPage: personal.value}}
	for _, token := range personal.tokens {
		expected.Rows = append(expected.Rows, expectedTokenRow(token, tokenTestNow))
	}
	for _, token := range clients.tokens {
		expected.Clients.Clients = append(expected.Clients.Clients, expectedProfileClient(token, tokenTestNow))
	}
	assertAuthTemplate(t, response.Body.String(), "page", authPageData{Banner: testPageBanner(page.User{Email: email, ProfileURL: "/", LogoutURL: "/logout"}), Profile: &expected})
}

func TestProfileNavigationWindowsAndWalks(t *testing.T) {
	// R-URTB-RXRE R-UT18-5PI3 R-UVH0-X8ZH R-UWOX-B0Q6 R-V1KI-U3OY R-V2SF-7VFN R-VEZF-1KUL R-V0CM-GBY9 R-V8VX-4Q54
	for _, count := range []int{0, paging.DefaultLimit - 1, paging.DefaultLimit, paging.DefaultLimit + 1, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			st := openTokenTestStore(t)
			user, session := tokenTestIdentity(t, st, "walk")
			personal := navigationTokens(t, st, user.ID, store.TokenPersonal, count)
			clients := navigationTokens(t, st, user.ID, store.TokenClient, count)
			srv := tokenTestServer(t, st)
			firstPersonal := navigationExpected(personal, "tokens", "", 0, min(count, paging.DefaultLimit), false)
			firstClients := navigationExpected(clients, "clients", "", 0, min(count, paging.DefaultLimit), false)
			before := profileStateSnapshot(t, st)
			lists := []struct {
				tokens []store.Token
				tool   string
				kind   store.TokenKind
			}{{personal, "tokens", store.TokenPersonal}, {clients, "clients", store.TokenClient}}
			for _, list := range lists {
				check := func(expected navigationWindow) tokenPageWindow {
					position, err := tokenPagePosition(expected.value, list.tool)
					if err != nil {
						t.Fatal(err)
					}
					got, err := srv.tokenWindow(user.ID, list.kind, position, list.tool)
					if err != nil || !reflect.DeepEqual(got.tokens, expected.tokens) && (len(got.tokens) != 0 || len(expected.tokens) != 0) || got.prev != expected.prev || got.next != expected.next || got.underflows != expected.underflows {
						t.Fatalf("%s page=%q got=%#v expected=%#v err=%v", list.tool, expected.value, got, expected, err)
					}
					if list.tool == "tokens" {
						assertNavigationResponse(t, srv, session.ID, user.Email, expected, firstClients)
					} else {
						assertNavigationResponse(t, srv, session.ID, user.Email, firstPersonal, expected)
					}
					return got
				}
				var forward []navigationWindow
				value := ""
				for start := 0; ; start += paging.DefaultLimit {
					end := min(start+paging.DefaultLimit, count)
					expected := navigationExpected(list.tokens, list.tool, value, start, end, false)
					got := check(expected)
					forward = append(forward, expected)
					if got.next == "" {
						break
					}
					value = got.next
				}
				current := forward[len(forward)-1]
				for i := len(forward) - 2; i >= 0; i-- {
					end := (i + 1) * paging.DefaultLimit
					expected := navigationExpected(list.tokens, list.tool, current.prev, i*paging.DefaultLimit, end, i == 0)
					got := check(expected)
					current = expected
					if i > 0 && got.prev == "" {
						t.Fatal("backward walk ended early")
					}
				}
				last := navigationExpected(list.tokens, list.tool, "last", max(0, count-paging.DefaultLimit), count, count <= paging.DefaultLimit)
				check(last)
				headTime := tokenTestNow.Add(24 * time.Hour)
				head := store.Token{ID: "synthetic head", CreatedAt: headTime, LastUsedAt: &headTime}
				tail := store.Token{ID: "synthetic tail", CreatedAt: tokenTestNow.Add(-48 * time.Hour)}
				check(navigationExpected(list.tokens, list.tool, fixturePageValue(head, list.tool+"-before"), 0, 0, true))
				check(navigationExpected(list.tokens, list.tool, fixturePageValue(tail, list.tool), count, count, false))
				check(navigationExpected(list.tokens, list.tool, fixturePageValue(tail, list.tool+"-before"), max(0, count-paging.DefaultLimit), count, count <= paging.DefaultLimit))

				if count > paging.DefaultLimit {
					// A boundary need not identify a stored token.
					anchor := list.tokens[count-1]
					anchor.ID = "" // immediately before the last tied-time row in profile order
					synthetic := fixturePageValue(anchor, list.tool+"-before")
					position, err := tokenPagePosition(synthetic, list.tool)
					if err != nil {
						t.Fatal(err)
					}
					nearest, err := st.ListTokensBefore(user.ID, list.kind, position.position, paging.DefaultLimit+1)
					if err != nil {
						t.Fatal(err)
					}
					// The direct store result fixes the number before this synthetic position;
					// the server still must reverse the selected nearest rows and encode ends.
					n := min(len(nearest), paging.DefaultLimit)
					want := append([]store.Token{}, nearest[:n]...)
					for i, j := 0, len(want)-1; i < j; i, j = i+1, j-1 {
						want[i], want[j] = want[j], want[i]
					}
					expected := navigationWindow{tokens: want, value: synthetic, underflows: len(nearest) <= paging.DefaultLimit}
					if len(want) > 0 {
						expected.next = fixturePageValue(want[len(want)-1], list.tool)
						if !expected.underflows {
							expected.prev = fixturePageValue(want[0], list.tool+"-before")
						}
					}
					check(expected)
				}
			}
			candidates := func(tokens []store.Token, tool string) []navigationWindow {
				windows := []navigationWindow{navigationExpected(tokens, tool, "", 0, min(count, paging.DefaultLimit), false), navigationExpected(tokens, tool, "last", max(0, count-paging.DefaultLimit), count, count <= paging.DefaultLimit)}
				if count > paging.DefaultLimit {
					windows = append(windows, navigationExpected(tokens, tool, fixturePageValue(tokens[paging.DefaultLimit-1], tool), paging.DefaultLimit, min(2*paging.DefaultLimit, count), false))
					windows = append(windows, navigationExpected(tokens, tool, fixturePageValue(tokens[count-1], tool+"-before"), max(0, count-1-paging.DefaultLimit), count-1, count-1 <= paging.DefaultLimit))
					windows = append(windows, navigationExpected(tokens, tool, fixturePageValue(tokens[paging.DefaultLimit], tool+"-before"), 0, paging.DefaultLimit, true))
				}
				return windows
			}
			for _, p := range candidates(personal, "tokens") {
				for _, c := range candidates(clients, "clients") {
					assertNavigationResponse(t, srv, session.ID, user.Email, p, c)
				}
			}

			if got := profileStateSnapshot(t, st); got != before {
				t.Fatal("navigation changed persisted state")
			}
		})
	}
}

func TestProfileLastUnderflowKeepsOtherLast(t *testing.T) {
	// R-VEZF-1KUL: canonicalize only the underflowing list, preserving the other's last value.
	for _, personalCount := range []int{0, paging.DefaultLimit + 1} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "unequal-last")
		clientCount := paging.DefaultLimit + 1 - personalCount
		personal := navigationTokens(t, st, user.ID, store.TokenPersonal, personalCount)
		clients := navigationTokens(t, st, user.ID, store.TokenClient, clientCount)
		srv := tokenTestServer(t, st)
		before := profileStateSnapshot(t, st)
		p := navigationExpected(personal, "tokens", "last", max(0, personalCount-paging.DefaultLimit), personalCount, personalCount <= paging.DefaultLimit)
		c := navigationExpected(clients, "clients", "last", max(0, clientCount-paging.DefaultLimit), clientCount, clientCount <= paging.DefaultLimit)
		assertNavigationResponse(t, srv, session.ID, user.Email, p, c)
		if got := profileStateSnapshot(t, st); got != before {
			t.Fatal("last underflow changed state")
		}
	}
}
