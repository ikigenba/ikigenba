package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func TestDescriptionAndAboutData(t *testing.T) {
	// R-RHHG-K4QK R-RIPC-XWH9: constant use and positional construction prove the public seam.
	const description string = Description
	if description == "" || strings.ContainsAny(description, "\"\\") || strings.ContainsFunc(description, unicode.IsControl) {
		t.Fatalf("description is not a TOML basic-string value: %q", description)
	}
	banner := page.Banner{Service: "provided service", Release: "provided release", Commit: "provided commit"}
	data := AboutData{banner, description}
	if !reflect.DeepEqual(data.Banner, banner) || data.Description != description {
		t.Fatalf("about data=%+v", data)
	}
}

func aboutState(t *testing.T, st *store.Store) string {
	t.Helper()
	var out strings.Builder
	err := serverStoreDB(t, st).Read(t.Context(), func(tx *sql.Tx) error {
		for _, table := range []struct{ name, query string }{
			{"users", "SELECT * FROM users ORDER BY 1"},
			{"sessions", "SELECT * FROM sessions ORDER BY 1"},
			{"login_states", "SELECT * FROM login_states ORDER BY 1"},
			{"tokens", "SELECT * FROM tokens ORDER BY 1"},
			{"clients", "SELECT * FROM clients ORDER BY 1"},
			{"auth_codes", "SELECT * FROM auth_codes ORDER BY 1"},
			{"schema_migrations", "SELECT * FROM schema_migrations ORDER BY 1"},
		} {
			rows, err := tx.QueryContext(t.Context(), table.query)
			if err != nil {
				return err
			}
			columns, err := rows.Columns()
			if err != nil {
				_ = rows.Close()
				return err
			}
			fmt.Fprintln(&out, table.name)
			for rows.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					_ = rows.Close()
					return err
				}
				fmt.Fprintf(&out, "%#v\n", values)
			}
			err = rows.Err()
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestAboutLiveSession(t *testing.T) {
	// R-RJX9-BO7Y R-RQ0R-8IXF R-RR8N-MAO4 R-RL55-PFYN R-RNKY-GZG1.
	f := newOAuthFixture(t, "")
	client := f.client(t, "persisted client", oauthCallback)
	if _, err := f.st.CreateAuthCode(client.ID, f.user.ID, oauthCallback, oauthChallenge, "", f.now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.CreateToken(f.user.ID, "persisted token", store.Expiry90d, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateLoginState("provided verifier", "provided return"); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(10 * time.Minute)
	returned := page.Banner{Service: "provided <& service", Release: "provided <& release", Commit: "provided <& commit", Home: "https://home.example/", Tools: true, Email: "returned@work.example", ProfileURL: "/provided-profile", LogoutURL: "/provided-logout", Trail: []page.Level{{Name: "incoming", URL: "/incoming"}}}
	original := returned.Trail[0]
	calls := []page.User{}
	f.trail.server.cfg.Banner = func(u page.User) page.Banner { calls = append(calls, u); return returned }
	banner := returned
	banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
	var want strings.Builder
	if err := authTemplates.ExecuteTemplate(&want, "about", AboutData{banner, Description}); err != nil {
		t.Fatal(err)
	}
	before := aboutState(t, f.st)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, query := range []string{"", "?return=https%3A%2F%2Felsewhere.example", "?return=%", "?return=first&return=second"} {
			calls = nil
			w, events := f.request(t, method, "/about"+query, "", f.session.ID)
			if w.Code != 200 || w.Header().Get("Content-Type") != signInHTMLContentType || w.Body.String() != want.String() {
				t.Fatalf("%s %s response=%d %v %q", method, query, w.Code, w.Header(), w.Body.String())
			}
			if !reflect.DeepEqual(calls, []page.User{{Email: f.user.Email, ProfileURL: "/", LogoutURL: "/logout"}}) {
				t.Fatalf("banner calls=%v", calls)
			}
			assertPageBanner(t, w.Body.String(), banner)
			if returned.Trail[0] != original {
				t.Fatal("incoming banner trail mutated")
			}
			if got := aboutState(t, f.st); got != before {
				t.Fatalf("about changed state:\n%s", got)
			}
			if len(events) != 2 {
				t.Fatalf("about events=%v", events)
			}
		}
	}
}

func TestAboutNoLiveSessionMatchesRoot(t *testing.T) {
	// R-RSGK-02ET: absent, unknown, idle and capped credentials render the same sign-in response as root.
	for _, age := range []time.Duration{0, 16 * time.Minute, 19 * time.Hour} {
		f := newOAuthFixture(t, "")
		f.now = f.now.Add(age)
		if age == 19*time.Hour {
			// Keep the idle window live so only the absolute cap expires this session.
			if err := serverStoreDB(t, f.st).Write(t.Context(), func(tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), "UPDATE sessions SET last_used_at = ? WHERE id = ?", f.now.Add(-time.Minute).UnixNano(), f.session.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		for _, cookie := range []string{"", "unknown", f.session.ID} {
			if age == 0 && cookie == f.session.ID {
				continue
			}
			before := aboutState(t, f.st)
			for _, query := range []string{"", "?return=https%3A%2F%2Fapp.green.example%2Fwork", "?return=%", "?return=first&return=second"} {
				root, _ := f.request(t, http.MethodGet, "/"+query, "", cookie)
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					w, _ := f.request(t, method, "/about"+query, "", cookie)
					if w.Code != root.Code || w.Header().Get("Content-Type") != root.Header().Get("Content-Type") || w.Body.String() != root.Body.String() {
						t.Fatalf("age=%v cookie=%q %s differs from root", age, cookie, method)
					}
					if got := aboutState(t, f.st); got != before {
						t.Fatal("unsigned about changed state")
					}
				}
			}
		}
	}
}

func TestAboutMethodAndExactPath(t *testing.T) {
	// R-RJX9-BO7Y R-RTOG-DU5I: routing is exact, and unsupported methods reject before consulting cookies or storage.
	f := newOAuthFixture(t, "")
	before := aboutState(t, f.st)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, "CUSTOM"} {
		for _, cookie := range []string{"", "unknown", f.session.ID} {
			w, _ := f.request(t, method, "/about", "", cookie)
			if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) {
				t.Fatalf("%s response=%d %v", method, w.Code, w.Header())
			}
			if got := aboutState(t, f.st); got != before {
				t.Fatal("rejected method changed state")
			}
		}
	}
	for _, path := range []string{"/about/", "/about/child", "/About", "/about-other"} {
		w, _ := f.request(t, http.MethodGet, path, "", f.session.ID)
		if w.Code != 404 {
			t.Fatalf("%s=%d", path, w.Code)
		}
	}
	failServerStore(t, f.st)
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		w, _ := f.request(t, method, "/about", "", f.session.ID)
		if w.Code != 405 {
			t.Fatalf("failed store influenced method rejection: %d", w.Code)
		}
	}
}

func TestAboutStoreFailure(t *testing.T) {
	f := newOAuthFixture(t, "")
	failServerStore(t, f.st)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://auth.green.example/about", nil)
	r.AddCookie(cookieForHost(r.Host, f.session.ID, false))
	w, events := f.trail.request(t, r)
	if w.Code != 500 {
		t.Fatalf("store failure=%d", w.Code)
	}
	assertTrail(t, events, r, 500)
}
