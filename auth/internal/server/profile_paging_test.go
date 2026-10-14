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
	// R-E8AD-H04E R-EGTO-5EB9 R-E9I9-URV3 R-EEDV-DUTV: independent per-kind first pages, complete data and exact template output.
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
				expected := profilePageData{Apex: "localhost", Email: user.Email, Workspace: "green.example", Rows: make([]tokenRowData, 0), Next: expectedNextToken("tokens", personal), Create: tokenCreateData{Expiry: "90d"}, Clients: mcpClientsData{Clients: make([]mcpClientData, 0), ClientsNext: expectedNextToken("clients", clients)}}
				for _, token := range personal[:min(len(personal), paging.DefaultLimit)] {
					expected.Rows = append(expected.Rows, expectedTokenRow(token, tokenTestNow))
				}
				for _, token := range clients[:min(len(clients), paging.DefaultLimit)] {
					expected.Clients.Clients = append(expected.Clients.Clients, expectedProfileClient(token, tokenTestNow))
				}
				srv := tokenTestServer(t, st)
				expected.Workspace = srv.cfg.WorkspaceDomain
				rows, next, err := srv.tokenRows(user.ID, tokenTestNow, nil)
				if err != nil || next != expected.Next || !reflect.DeepEqual(rows, expected.Rows) {
					t.Fatalf("personal page=%#v next=%v err=%v", rows, next, err)
				}
				clientData, err := srv.profileClients(user.ID, tokenTestNow, nil)
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

				for _, window := range []struct {
					tool    string
					kind    store.TokenKind
					ordered []store.Token
				}{{"tokens", store.TokenPersonal, personal}, {"clients", store.TokenClient, clients}} {
					pageValue := ""
					remaining := window.ordered
					for {
						position, err := tokenPagePosition(pageValue, window.tool)
						if err != nil {
							t.Fatal(err)
						}
						got, next, err := srv.tokenWindow(user.ID, window.kind, position, window.tool)
						length := min(len(remaining), paging.DefaultLimit)
						sameWindow := reflect.DeepEqual(got, remaining[:length]) || len(got) == 0 && length == 0
						if err != nil || !sameWindow || next != expectedNextToken(window.tool, remaining) {
							t.Fatalf("%s window: %#v next=%q err=%v", window.tool, got, next, err)
						}
						remaining = remaining[length:]
						if next == "" {
							if len(remaining) != 0 {
								t.Fatal("traversal ended early")
							}
							break
						}
						pageValue = next
					}
				}

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

func expectedNextToken(tool string, tokens []store.Token) string {
	if len(tokens) <= paging.DefaultLimit {
		return ""
	}
	last := tokens[paging.DefaultLimit-1]
	var used, at int64
	if last.LastUsedAt != nil {
		used, at = 1, last.LastUsedAt.UnixNano()
	}
	return (paging.Token{Tool: tool}).Encode(used, at, last.CreatedAt.UnixNano(), last.ID)
}

func TestTokenPagePositions(t *testing.T) {
	// R-ED5Z-0336: empty, malformed, scoped and typed cursor positions.
	for _, tool := range []string{"tokens", "clients"} {
		for _, used := range []int64{0, 1, -1} {
			value := (paging.Token{Tool: tool}).Encode(used, int64(-123456789), int64(987654321), "position-fixture")
			got, err := tokenPagePosition(value, tool)
			want := &store.TokenPosition{CreatedAt: time.Unix(0, 987654321), ID: "position-fixture"}
			if used != 0 {
				at := time.Unix(0, -123456789)
				want.LastUsedAt = &at
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%s position = %#v, %v want %#v", tool, got, err, want)
			}
		}
		if got, err := tokenPagePosition("", tool); got != nil || err != nil {
			t.Fatalf("empty = %#v, %v", got, err)
		}
		for _, value := range []string{"bogus", (paging.Token{Tool: tool}).Encode("wrong-type", int64(1), int64(2), "fixture")} {
			if _, err := tokenPagePosition(value, tool); err == nil {
				t.Fatalf("accepted malformed value %q", value)
			}
		}
	}
}

func TestProfileInvalidPages(t *testing.T) {
	// R-EAQ6-8JLS: reject either cursor without rendering or modifying state, and leave unsigned requests with sign-in.
	for _, count := range []int{0, paging.DefaultLimit + 1} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "invalid-pages")
		for i := range count {
			if _, _, err := st.CreateToken(user.ID, fmt.Sprintf("personal %d", i), store.ExpiryNever, tokenTestNow); err != nil {
				t.Fatal(err)
			}
			mintProfileClient(t, st, user.ID, fmt.Sprintf("client %d", i), tokenTestNow)
		}
		srv := tokenTestServer(t, st)
		validTokens := (paging.Token{Tool: "tokens"}).Encode(int64(0), int64(0), tokenTestNow.UnixNano(), "fixture")
		validClients := (paging.Token{Tool: "clients"}).Encode(int64(0), int64(0), tokenTestNow.UnixNano(), "fixture")
		queries := []string{"tokens_page=bogus", "clients_page=bogus", "tokens_page=bogus&clients_page=bogus", "tokens_page=" + validTokens + "&clients_page=bogus", "tokens_page=bogus&clients_page=" + validClients}
		if _, err := tokenPagePosition(validClients, "tokens"); err != nil {
			queries = append(queries, "tokens_page="+validClients)
		}
		if _, err := tokenPagePosition(validTokens, "clients"); err != nil {
			queries = append(queries, "clients_page="+validTokens)
		}
		before := profileStateSnapshot(t, st)
		for _, query := range queries {
			request := tokenProfileRequest(session.ID)
			request.URL.RawQuery = query + "&unrelated=value&return=ignored"
			baselineRequest := request.Clone(request.Context())
			baselineURL := *request.URL
			baselineRequest.URL = &baselineURL
			baselineRequest.URL.Path = "/assets/theme.css"
			baseline, response := httptest.NewRecorder(), httptest.NewRecorder()
			srv.ServeHTTP(baseline, baselineRequest)
			srv.ServeHTTP(response, request)
			if response.Code != 404 || response.Code != baseline.Code || !reflect.DeepEqual(response.Header(), baseline.Header()) || response.Body.String() != baseline.Body.String() {
				t.Fatalf("query %q differs from missing asset response", query)
			}
			if got := profileStateSnapshot(t, st); got != before {
				t.Fatalf("query %q changed persisted state", query)
			}
			unsigned := tokenProfileRequest("")
			unsigned.URL.RawQuery = query
			signIn := httptest.NewRecorder()
			srv.ServeHTTP(signIn, unsigned)
			if signIn.Code != 200 || signIn.Header().Get("Content-Type") != signInHTMLContentType {
				t.Fatalf("unsigned query %q returned %d", query, signIn.Code)
			}
			assertAuthTemplate(t, signIn.Body.String(), "page", authPageData{SignIn: &signInPageData{Apex: "localhost", Host: unsigned.Host, Workspace: srv.cfg.WorkspaceDomain}})
		}
	}
}

func TestProfileListsPageIndependently(t *testing.T) {
	// R-EI1K-J61Y: each list retains its window when only the other list's cursor changes.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "independent-pages")
	var personal, clients []store.Token
	for i := range paging.DefaultLimit + 1 {
		created := tokenTestNow.Add(-time.Duration(i) * time.Nanosecond)
		token, _, err := st.CreateToken(user.ID, fmt.Sprintf("personal %d", i), store.ExpiryNever, created)
		if err != nil {
			t.Fatal(err)
		}
		personal = append(personal, token)
		client, _ := mintProfileClient(t, st, user.ID, fmt.Sprintf("client %d", i), created)
		clients = append(clients, client)
	}
	profileFixtureOrder(personal)
	profileFixtureOrder(clients)
	srv := tokenTestServer(t, st)
	personalNext, clientsNext := expectedNextToken("tokens", personal), expectedNextToken("clients", clients)
	for _, personalPage := range []string{"", personalNext} {
		for _, clientsPage := range []string{"", clientsNext} {
			expected := profilePageData{Apex: "localhost", Email: user.Email, Workspace: srv.cfg.WorkspaceDomain, Page: personalPage, Create: tokenCreateData{Expiry: "90d"}, Rows: []tokenRowData{}, Clients: mcpClientsData{Clients: []mcpClientData{}, ClientsPage: clientsPage, TokensPage: personalPage}}
			personalWindow, clientsWindow := personal[:paging.DefaultLimit], clients[:paging.DefaultLimit]
			if personalPage == "" {
				expected.Next = personalNext
			} else {
				personalWindow = personal[paging.DefaultLimit:]
			}
			if clientsPage == "" {
				expected.Clients.ClientsNext = clientsNext
			} else {
				clientsWindow = clients[paging.DefaultLimit:]
			}
			for _, token := range personalWindow {
				expected.Rows = append(expected.Rows, expectedTokenRow(token, tokenTestNow))
			}
			for _, token := range clientsWindow {
				expected.Clients.Clients = append(expected.Clients.Clients, expectedProfileClient(token, tokenTestNow))
			}
			request := tokenProfileRequest(session.ID)
			request.URL.RawQuery = "tokens_page=" + personalPage + "&clients_page=" + clientsPage + "&limit=1&return=ignored"
			response := httptest.NewRecorder()
			srv.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatal(response.Code)
			}
			assertAuthTemplate(t, response.Body.String(), "page", authPageData{Banner: testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}), Profile: &expected})
		}
	}
}
