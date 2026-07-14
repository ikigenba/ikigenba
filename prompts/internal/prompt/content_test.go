package prompt

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"appkit/server"
)

func TestRunContentHandlerServesSandboxFile(t *testing.T) {
	// R-6C2D-19HN
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	svc, _, sb, _ := newTestService(t)
	p := mustCreate(t, svc, ownerA)
	run, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data := []byte("%PDF-1.7\ncontent\n")
	if err := os.MkdirAll(filepath.Join(sb.Root(run.ID), "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sb.Root(run.ID), "reports", "out.pdf"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	query := url.Values{"run_id": {run.ID}, "path": {"reports/out.pdf"}}
	req := httptest.NewRequest(http.MethodGet, "/run-content?"+query.Encode(), nil)
	rr := httptest.NewRecorder()
	svc.RunContentHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rr.Code, rr.Body.String())
	}
	if !bytes.Equal(rr.Body.Bytes(), data) {
		t.Fatalf("body = %q, want %q", rr.Body.Bytes(), data)
	}
	if got, want := rr.Header().Get("Content-Length"), "17"; got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/pdf") {
		t.Fatalf("Content-Type = %q, want application/pdf", got)
	}
}

func TestRunContentRouteUsesChassisLoopbackGuard(t *testing.T) {
	// R-6DA9-F18C
	// R-BI5J-4GM6
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	svc, _, sb, _ := newTestService(t)
	p := mustCreate(t, svc, ownerA)
	run, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	secret := []byte("do not expose")
	if err := os.WriteFile(filepath.Join(sb.Root(run.ID), "secret.txt"), secret, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		header     string
		wantStatus int
		wantBody   bool
	}{
		{name: "front door", header: "X-Forwarded-Proto", wantStatus: http.StatusNotFound},
		{name: "loopback identity", header: "X-Owner-Email", wantStatus: http.StatusOK, wantBody: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/run-content?run_id="+url.QueryEscape(run.ID)+"&path=secret.txt", nil)
			req.Header.Set(tc.header, "https")
			rr := httptest.NewRecorder()
			server.LoopbackOnly(svc.RunContentHandler()).ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
			if got := bytes.Contains(rr.Body.Bytes(), secret); got != tc.wantBody {
				t.Fatalf("body contains file = %v, want %v: %q", got, tc.wantBody, rr.Body.Bytes())
			}
		})
	}
}

func TestRunContentHandlerMapsInvalidTargetsToNotFound(t *testing.T) {
	// R-6EI5-SSZ1
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	svc, _, sb, _ := newTestService(t)
	p := mustCreate(t, svc, ownerA)
	run, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := os.Mkdir(filepath.Join(sb.Root(run.ID), "directory"), 0o755); err != nil {
		t.Fatal(err)
	}

	for name, target := range map[string]string{
		"unknown run": "/run-content?run_id=unknown&path=file.txt",
		"absent path": "/run-content?run_id=" + url.QueryEscape(run.ID),
		"directory":   "/run-content?run_id=" + url.QueryEscape(run.ID) + "&path=directory",
		"escape":      "/run-content?run_id=" + url.QueryEscape(run.ID) + "&path=../../secrets",
	} {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			svc.RunContentHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body=%q", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), sb.Root(run.ID)) {
				t.Fatalf("response leaked resolved path: %q", rr.Body.String())
			}
		})
	}
}
