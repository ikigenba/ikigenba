package server

import (
	"bytes"
	"dashboard/ui"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"testing"
)

func TestEveryServedDocumentCarriesBrandIcons(t *testing.T) {
	documents, err := fs.Glob(ui.Files, "html/*.html")
	if err != nil {
		t.Fatalf("enumerate embedded HTML documents: %v", err)
	}
	if len(documents) == 0 {
		t.Fatal("embedded UI contains no HTML documents")
	}
	sort.Strings(documents)

	srv := testServer(t)
	session := liveSession(t, srv)
	signedIn := map[string]string{"Cookie": session.Name + "=" + session.Value}

	// R-RYDN-YNR5
	for _, document := range documents {
		t.Run(path.Base(document), func(t *testing.T) {
			body := renderDocumentThroughRouter(t, path.Base(document), srv, signedIn)
			head := documentHead(t, body)
			for _, link := range []string{
				`<link rel="icon" href="/static/favicon.ico" sizes="any">`,
				`<link rel="icon" type="image/png" href="/static/favicon-32.png">`,
				`<link rel="apple-touch-icon" href="/static/apple-touch-icon.png">`,
			} {
				if !strings.Contains(head, link) {
					t.Errorf("served %s head missing %q:\n%s", document, link, head)
				}
			}
		})
	}
}

func renderDocumentThroughRouter(t *testing.T, document string, srv *http.Server, signedIn map[string]string) string {
	t.Helper()

	var target string
	switch document {
	case "index.html":
		target = "/"
	case "install.html":
		target = "/install"
	case "metrics.html":
		target = "/metrics"
	case "profile.html":
		target = "/profile"
	case "oauth_authorize.html":
		ts, _, client := newOAuthTest(t)
		clientID := registerClient(t, ts, client)
		resp, err := client.Get(authorizeURL(ts, clientID, nil))
		if err != nil {
			t.Fatalf("GET authorize chooser: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read authorize chooser: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /oauth/authorize status = %d, want 200\n%s", resp.StatusCode, body)
		}
		return string(body)
	default:
		t.Fatalf("committed HTML document %q has no assembled route assertion", document)
	}

	rec := do(t, srv, http.MethodGet, "https://int.ikigenba.com"+target, signedIn)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s for %s status = %d, want 200\n%s", target, document, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestAssembledStaticRoutesServeBrandIcons(t *testing.T) {
	srv := testServer(t)

	// R-RZLK-CFHU
	for _, asset := range []struct {
		name        string
		contentType string
	}{
		{name: "favicon.ico", contentType: "image/x-icon"},
		{name: "favicon-32.png", contentType: "image/png"},
		{name: "apple-touch-icon.png", contentType: "image/png"},
	} {
		t.Run(asset.name, func(t *testing.T) {
			rec := do(t, srv, http.MethodGet, "https://int.ikigenba.com/static/"+asset.name, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Fatal("response body is empty")
			}
			if got := rec.Header().Get("Content-Type"); got != asset.contentType {
				t.Errorf("Content-Type = %q, want %q", got, asset.contentType)
			}
		})
	}
}

func TestRootFaviconMatchesAssembledStaticRoute(t *testing.T) {
	srv := testServer(t)

	// R-8MFA-HUNC
	root := do(t, srv, http.MethodGet, "https://int.ikigenba.com/favicon.ico", nil)
	if root.Code != http.StatusOK {
		t.Fatalf("GET /favicon.ico status = %d, want 200", root.Code)
	}
	if got := root.Header().Get("Content-Type"); got != "image/x-icon" {
		t.Errorf("GET /favicon.ico Content-Type = %q, want %q", got, "image/x-icon")
	}

	static := do(t, srv, http.MethodGet, "https://int.ikigenba.com/static/favicon.ico", nil)
	if static.Code != http.StatusOK {
		t.Fatalf("GET /static/favicon.ico status = %d, want 200", static.Code)
	}
	if !bytes.Equal(root.Body.Bytes(), static.Body.Bytes()) {
		t.Error("GET /favicon.ico body differs from GET /static/favicon.ico")
	}
}
