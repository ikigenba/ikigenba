package urls_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
)

func TestAddresses(t *testing.T) {
	// R-RARI-EG64 R-RD7B-5ZNI R-REF7-JRE7 R-RFN3-XJ4W R-RGV0-BAVL
	if urls.Service != "connections" || urls.CrossOrigin == "" || strings.Contains(urls.CrossOrigin, "\n") {
		t.Fatal("constants")
	}
	path := filepath.Join(t.TempDir(), "services.json")
	b, err := json.Marshal(map[string]any{"services": []services.Entry{{Name: urls.Service, URL: "https://broker.example/"}, {Name: "auth", URL: "https://login.example/"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Service entries use the services file's lowercase contract fields.
	b = []byte(strings.NewReplacer("\"Name\"", "\"name\"", "\"URL\"", "\"url\"", "\"Description\"", "\"description\"", "\"Socket\"", "\"socket\"", "\"Enabled\"", "\"enabled\"", "\"MCP\"", "\"mcp\"").Replace(string(b)))
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "https://Connections.demo.test:443/", nil)
	for _, v := range []string{"http", "HTTP", "https", "HTTPS", "bogus", ""} {
		r.Header.Set("X-Forwarded-Proto", v)
		want := "https"
		if strings.EqualFold(v, "http") {
			want = "http"
		}
		if urls.Scheme(r) != want {
			t.Fatal(v)
		}
	}
	if urls.Configured(path) != "https://broker.example" || urls.Configured("") != "" || urls.Callback("override", path) != "override" || urls.Callback("", path) != urls.Configured(path) || urls.Callback("", "") != "" {
		t.Fatal("callback/base")
	}
	if urls.Connect("", "google") != "/google/connect" || urls.Connect("https://broker.example", "google") != "https://broker.example/google/connect" {
		t.Fatal("connect")
	}
	if urls.Auth(r, path) != "https://login.example" || urls.Auth(r, "") != "https://auth.demo.test" {
		t.Fatal("auth")
	}
}
func TestSameOrigin(t *testing.T) {
	// R-RI2W-P2MA
	r := httptest.NewRequest("POST", "http://connections.demo.test:7400/", nil)
	r.Header.Set("X-Forwarded-Proto", "http")
	for _, v := range []string{"", "null", "https://connections.demo.test:7400", "http://connections.demo.test:7400/", "http://Connections.demo.test:7400", "http://auth.demo.test:7400", "http://connections.demo.test:7400"} {
		r.Header.Del("Origin")
		if v != "" {
			r.Header.Set("Origin", v)
		}
		if urls.SameOrigin(r) != (v == "http://connections.demo.test:7400") {
			t.Fatal(v)
		}
	}
	r.Header.Add("Origin", "http://connections.demo.test:7400")
	if urls.SameOrigin(r) {
		t.Fatal("repeated origin")
	}
}

func TestAddressEdges(t *testing.T) {
	// R-RD7B-5ZNI R-REF7-JRE7 R-RFN3-XJ4W R-RGV0-BAVL
	r := httptest.NewRequest("GET", "https://CONNECTIONS.demo.test:999999/", nil)
	r.Header["X-Forwarded-Proto"] = []string{"HTTP", "https"}
	if urls.Scheme(r) != "http" || urls.Auth(r, "") != "http://auth.demo.test" {
		t.Fatal("first scheme/decimal port")
	}
	path := filepath.Join(t.TempDir(), "services.json")
	for _, body := range []string{`{"services":[]}`, `{broken`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if urls.Configured(path) != "" || urls.Callback("", path) != "" {
			t.Fatal("invalid/absent")
		}
	}
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"connections","url":"https://registered.example//","description":"","socket":"","enabled":true,"mcp":true},{"name":"auth","url":"","description":"","socket":"","enabled":true,"mcp":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if urls.Configured(path) != "https://registered.example/" || urls.Auth(r, path) != "" {
		t.Fatal("trim/empty entry")
	}
}
