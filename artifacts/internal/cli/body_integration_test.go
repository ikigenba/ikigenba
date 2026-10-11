package cli_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/artifacts/internal/cli"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
)

// R-OJS9-D6KZ R-P35L-1BJU R-OIKC-ZEUA R-0IFN-0VUT
func TestRunMarkedBodiesAndSilentStreams(t *testing.T) {
	bodies := []string{
		"GhjKlmNopQrStuVwxYz01234", "ZyxWvuTsrQpoNmlKjhG98765",
		"PqrStuVwxYzGhjKlmNop6789", "UvwXyzGhiJklMnoPqrStu012",
		"MnoPqrStuVwxYzGhjKlm3456", "KlmNopQrsTuvWxyZghIj7890",
		"XyzGhjKlmNopQrsTuvWxy456",
	}
	bodies[2] += bodies[2]
	bodies[5] += bodies[5]
	servicesPath, url := serveSibling(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.RawQuery {
		case "over":
			_, _ = io.WriteString(w, bodies[5])
		case "cut":
			w.Header().Set("Content-Length", strconv.Itoa(len(bodies[6])+10))
			_, _ = io.WriteString(w, bodies[6])
		default:
			_, _ = io.WriteString(w, bodies[4])
		}
	}))
	r := newRun(t, t.TempDir())
	r.p.Rand = &exclusiveRandom{}
	r.env[services.Variable] = servicesPath
	r.env["ARTIFACT_MAX_BYTES"] = "32"
	serviceDoc, err := os.ReadFile(filepath.Clean(servicesPath))
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := json.Marshal([]any{r.env, r.p.Dir, r.p.Version, string(serviceDoc), url, url + "?over", url + "?cut", "item.bin", "import.bin", "over.bin", "cut.bin", "failed.bin", "owner", "other", "owner@example.test", "other@example.test", "caller-request", "https", "artifacts", "bytes=2-12", r.p.Now().Add(-time.Second).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range bodies {
		if len(body) < 16 {
			t.Fatal("short marked body")
		}
		for _, c := range body {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				t.Fatal("non-alphanumeric marked body")
			}
		}
		for i := 0; i+8 <= len(body); i++ {
			if !strings.ContainsAny(body[i:i+8], "ghijklmnopqrstuvwxyzGHIJKLMNOPQRSTUVWXYZ") {
				t.Fatal("hex-only marked window")
			}
		}
		if strings.Contains(string(inputs), body) {
			t.Fatal("marked body appears in another input")
		}
	}
	r.start()
	var artifactID string
	for i, want := range []int{201, 200} {
		status, reply := r.request("PUT", "/files/item.bin", "owner", strings.NewReader(bodies[i]), nil)
		expectStatus(t, status, want)
		var metadata struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(reply, &metadata); err != nil {
			t.Fatal(err)
		}
		artifactID = metadata.ID
	}
	for _, c := range []struct {
		method  string
		headers map[string]string
		status  int
	}{
		{"GET", nil, 200}, {"GET", map[string]string{"Range": "bytes=2-12"}, 206}, {"HEAD", nil, 200},
	} {
		status, _ := r.request(c.method, "/files/item.bin", "owner", nil, c.headers)
		expectStatus(t, status, c.status)
	}
	for _, c := range []struct {
		path, user, body string
		headers          map[string]string
		status           int
	}{
		{"over.bin", "owner", bodies[2], nil, 413},
		{"item.bin", "other", bodies[3], nil, 403},
		{"past.bin", "owner", bodies[3], map[string]string{"X-Artifact-Expires": r.p.Now().Add(-time.Second).Format(time.RFC3339)}, 400},
	} {
		status, _ := r.request("PUT", "/files/"+c.path, c.user, strings.NewReader(c.body), c.headers)
		expectStatus(t, status, c.status)
	}
	var results []any
	for _, c := range []struct {
		path, query string
		fails       bool
	}{{"import.bin", "", false}, {"over.bin", "?over", true}, {"cut.bin", "?cut", true}} {
		result := r.call("import", map[string]any{"path": c.path, "url": url + c.query})
		if result.IsError() != c.fails {
			t.Fatalf("import %s error=%v", c.path, result.IsError())
		}
		if c.query == "?over" && resultText(t, result) != tools.TooLarge(32) {
			t.Fatal("import was not refused for its cap", resultText(t, result))
		}
		if c.query == "?cut" && resultText(t, result) != tools.FetchCutOff(url+c.query) {
			t.Fatal("import was not refused for its cut-off body", resultText(t, result))
		}
		results = append(results, result)
	}
	for _, c := range []struct {
		name string
		args any
	}{{"list", map[string]any{}}, {"show", map[string]any{"path": "item.bin"}}} {
		result := r.call(c.name, c.args)
		if result.IsError() {
			t.Fatalf("%s failed", c.name)
		}
		results = append(results, result)
	}
	var pages [][]byte
	for _, path := range []string{"/", "/" + artifactID, "/tools", "/about"} {
		status, body := r.request("GET", path, "owner", nil, nil)
		expectStatus(t, status, 200)
		pages = append(pages, body)
	}
	status, _ := r.request("DELETE", "/files/item.bin", "owner", nil, nil)
	expectStatus(t, status, 204)
	result := r.call("delete", map[string]any{"path": "import.bin"})
	if result.IsError() {
		t.Fatal("delete failed")
	}
	results = append(results, result)
	// A prematurely ended body reaches the server as a real read failure.
	conn, err := net.Dial("unix", r.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(conn, "PUT /files/failed.bin HTTP/1.1\r\nHost: artifacts\r\nX-User-Id: owner\r\nContent-Length: %d\r\n\r\n%s", len(bodies[0])+1, bodies[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.(*net.UnixConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "PUT"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, response.StatusCode, 400)
	if r.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	if r.stdout.Writes() != 0 || r.stderr.Writes() != 0 {
		t.Fatalf("serving writes stdout=%q stderr=%q", r.stdout.String(), r.stderr.String())
	}
	for _, value := range []any{r.trail.Events(), r.bus.Events(), r.stderr.String(), results} {
		assertNoMarkedJSON(t, value, bodies)
	}
	for _, body := range pages {
		assertNoMarkedText(t, string(body), bodies)
	}
}

func assertNoMarkedText(t *testing.T, text string, bodies []string) {
	t.Helper()
	text = strings.ToLower(text)
	for _, body := range bodies {
		body = strings.ToLower(body)
		for i := 0; i+8 <= len(body); i++ {
			if strings.Contains(text, body[i:i+8]) {
				t.Fatalf("marked window %q leaked", body[i:i+8])
			}
		}
	}
}

func assertNoMarkedJSON(t *testing.T, value any, bodies []string) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			assertNoMarkedText(t, v, bodies)
		case []any:
			for _, member := range v {
				walk(member)
			}
		case map[string]any:
			for key, member := range v {
				assertNoMarkedText(t, key, bodies)
				walk(member)
			}
		}
	}
	walk(decoded)
}
