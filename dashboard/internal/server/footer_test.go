package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestServedSurfacesOmitSiteFooter(t *testing.T) {
	srv, deps := patTestServer(t)
	const owner = "owner@int.ikigenba.com"
	cookie := mintSession(t, deps, owner)
	signedIn := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	responses := []struct {
		name string
		rec  *httptestResponse
	}{
		{
			name: "logged-out index",
			rec:  responseFromRecorder(do(t, srv, "GET", "https://int.ikigenba.com/", nil)),
		},
		{
			name: "logged-in index",
			rec:  responseFromRecorder(do(t, srv, "GET", "https://int.ikigenba.com/", signedIn)),
		},
		{
			name: "profile",
			rec:  responseFromRecorder(do(t, srv, "GET", "https://int.ikigenba.com/profile", signedIn)),
		},
		{
			name: "PAT created",
			rec: responseFromRecorder(doForm(t, srv, "https://int.ikigenba.com/pat",
				url.Values{"label": {"Codex on laptop"}},
				map[string]string{
					"Cookie": cookie.Name + "=" + cookie.Value,
					"Origin": "https://int.ikigenba.com",
				})),
		},
		{
			name: "stylesheet",
			rec:  responseFromRecorder(do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)),
		},
	}

	for _, resp := range responses {
		t.Run(resp.name, func(t *testing.T) {
			// R-EFJZ-FRQ1
			if resp.rec.code != http.StatusOK {
				t.Fatalf("status = %d, want 200\n%s", resp.rec.code, resp.rec.body)
			}
			if strings.Contains(resp.rec.body, "<footer") {
				t.Errorf("%s still renders footer markup:\n%s", resp.name, resp.rec.body)
			}
			if strings.Contains(resp.rec.body, "site-footer") {
				t.Errorf("%s still references site-footer:\n%s", resp.name, resp.rec.body)
			}
			if resp.name == "stylesheet" && strings.Contains(resp.rec.body, ".site-footer") {
				t.Errorf("stylesheet still contains .site-footer selector:\n%s", resp.rec.body)
			}
		})
	}
}

type httptestResponse struct {
	code int
	body string
}

func responseFromRecorder(rec *httptest.ResponseRecorder) *httptestResponse {
	return &httptestResponse{code: rec.Code, body: rec.Body.String()}
}
