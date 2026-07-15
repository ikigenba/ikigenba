package script

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunContentHandlerServesFileMetadataAndBytes(t *testing.T) {
	// R-IIQS-PDUK
	runsDir := t.TempDir()
	svc := NewService(nil, runsDir, nil)
	runID := "run-123"
	rel := "reports/out.pdf"
	body := []byte("%PDF-1.7\nphase 22\n")
	path := filepath.Join(runsDir, runID, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	query := url.Values{"run_id": {runID}, "path": {rel}}
	req := httptest.NewRequest(http.MethodGet, "/run-content?"+query.Encode(), nil)
	rr := httptest.NewRecorder()
	svc.RunContentHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
	}
	if got := rr.Body.Bytes(); string(got) != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if got := rr.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length = %q, want %d", got, len(body))
	}
	if got := rr.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", got)
	}
}

func TestRunContentHandlerMapsInvalidAndMissingTargetsToBareNotFound(t *testing.T) {
	// R-IJYP-35L9
	runsDir := t.TempDir()
	svc := NewService(nil, runsDir, nil)
	runID := "run-123"
	dir := filepath.Join(runsDir, runID, "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(runsDir, "secrets")
	if err := os.WriteFile(secret, []byte("must-not-leak"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		runID string
		path  string
	}{
		{name: "unknown run", runID: "unknown", path: "out.pdf"},
		{name: "run id separator", runID: "nested/run", path: "out.pdf"},
		{name: "absent path", runID: runID},
		{name: "directory", runID: runID, path: "reports"},
		{name: "escape", runID: runID, path: "../../secrets"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := url.Values{"run_id": {tc.runID}, "path": {tc.path}}
			rr := httptest.NewRecorder()
			svc.RunContentHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/run-content?"+query.Encode(), nil))
			response, err := io.ReadAll(rr.Result().Body)
			if err != nil {
				t.Fatal(err)
			}
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, body = %q", rr.Code, response)
			}
			if strings.Contains(string(response), runsDir) || strings.Contains(string(response), "must-not-leak") {
				t.Fatalf("404 body leaks resolved path or file bytes: %q", response)
			}
		})
	}
}
