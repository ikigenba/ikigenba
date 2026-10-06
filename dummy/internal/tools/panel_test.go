package tools_test

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func panelClient(t *testing.T, s *widget.Store) (*mcp.Client, http.Handler) {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _, _ := capturingWriter(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer})
	handler := panel.Handler(s, func(page.User) page.Banner { return page.Banner{} }, srv, writer)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client()}), handler
}

func panelRequest(h http.Handler, method, path, body, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-User-Id", caller.UserID)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	return response
}

var tablePattern = regexp.MustCompile(`(?is)<table(?:[^a-z0-9>][^>]*|)>.*?</table(?:[^a-z0-9>][^>]*|)>`)
var rowPattern = regexp.MustCompile(`(?is)<tr(?:[^a-z0-9>][^>]*|)>(.*?)</tr(?:[^a-z0-9>][^>]*|)>`)
var cellPattern = regexp.MustCompile(`(?is)(<td(?:[^a-z0-9>][^>]*|)>)(.*?)</td(?:[^a-z0-9>][^>]*|)>`)
var spanPattern = regexp.MustCompile(`(?is)^(<span(?:[^a-z0-9>][^>]*|)>)(.*?)</span(?:[^a-z0-9>][^>]*|)>$`)
var tagPattern = regexp.MustCompile(`<[^>]*>`)
var attributePattern = regexp.MustCompile(`^[\t\n\v\f\r ]+([^\t\n\v\f\r "'<>/=]+)(?:="([^"]*)")?`)

// readAttribute follows the design's left-to-right attribute grammar. A bare
// attribute is not an occurrence, and a second named occurrence is refused.
func readAttribute(attributes, name string) (string, bool) {
	value := ""
	found := false
	for {
		indices := attributePattern.FindStringSubmatchIndex(attributes)
		if indices == nil {
			return value, found
		}
		key := attributes[indices[2]:indices[3]]
		if indices[4] >= 0 && strings.EqualFold(key, name) {
			if found {
				return "", false
			}
			value = html.UnescapeString(attributes[indices[4]:indices[5]])
			found = true
		}
		attributes = attributes[indices[1]:]
	}
}

// tagAttributes skips the whole ASCII letter/digit/hyphen tag-name run,
// including any suffix permitted by the design's lexical tag definition.
func tagAttributes(tag string) string {
	end := strings.IndexByte(tag, '>')
	start := 1
	for start < end {
		c := tag[start]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
			break
		}
		start++
	}
	return tag[start:end]
}

func widgetsTable(body string) string {
	for _, table := range tablePattern.FindAllString(body, -1) {
		if value, ok := readAttribute(tagAttributes(table), "id"); ok && value == "widgets-table" {
			return table
		}
	}
	return ""
}

func assertLastRow(t *testing.T, body string, w widget.Widget) {
	t.Helper()
	table := widgetsTable(body)
	rows := rowPattern.FindAllStringSubmatch(table, -1)
	var last [][]string
	for _, row := range rows {
		if cells := cellPattern.FindAllStringSubmatch(row[1], -1); len(cells) > 0 {
			last = cells
		}
	}
	if len(last) < 3 {
		t.Fatalf("last row missing: %s", table)
	}
	want := []string{strings.Join(strings.Fields(w.Name), " "), strconv.Itoa(w.Count), string(w.Status)}
	for i := range 3 {
		text := strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(last[i][2], ""))), " ")
		if text != want[i] {
			t.Fatalf("cell %d = %q; want %q", i, text, want[i])
		}
	}
	if value, ok := readAttribute(tagAttributes(last[1][1]), "class"); !ok || value != "num" {
		t.Fatal("count cell lacks num hook")
	}
	span := spanPattern.FindStringSubmatch(strings.Trim(last[2][2], "\t\n\v\f\r "))
	if len(span) != 3 || span[2] != string(w.Status) {
		t.Fatalf("status cell = %s", last[2][2])
	}
	if value, ok := readAttribute(tagAttributes(span[1]), "class"); !ok || value != "status" {
		t.Fatal("status span lacks status class")
	}
	if value, ok := readAttribute(tagAttributes(span[1]), "data-status"); !ok || value != string(w.Status) {
		t.Fatal("status span lacks matching data-status")
	}
}

func TestToolCreationAppearsInPanel(t *testing.T) {
	s := toolsTestStore(t)
	c, h := panelClient(t, s)
	before := panelRequest(h, http.MethodGet, "/widgets/table", "", "")
	if before.Code != http.StatusOK || before.Header().Get("ETag") == "" {
		t.Fatal("initial table has no validator")
	}
	result := call(t, c, "create_widget", json.RawMessage(`{"name":"from MCP & panel","count":23,"status":"paused"}`))
	if result.IsError() {
		t.Fatal("creation failed")
	}
	w := widget.Widget{Name: "from MCP & panel", Count: 23, Status: widget.StatusPaused}
	// R-FH26-7X1Q: both surfaces show the created last row; stale validator gets 200.
	for _, path := range []string{"/widgets", "/widgets/table"} {
		response := panelRequest(h, http.MethodGet, path, "", "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, response.Code)
		}
		assertLastRow(t, response.Body.String(), w)
	}
	conditional := panelRequest(h, http.MethodGet, "/widgets/table", "", before.Header().Get("ETag"))
	if conditional.Code != http.StatusOK {
		t.Fatalf("stale validator status = %d", conditional.Code)
	}
}

func TestFormCreationAppearsInToolList(t *testing.T) {
	s := toolsTestStore(t)
	c, h := panelClient(t, s)
	body := url.Values{"name": {" from form & tools "}, "count": {"9"}, "status": {"retired"}}.Encode()
	response := panelRequest(h, http.MethodPost, "/widgets", body, "")
	if response.Code != http.StatusSeeOther {
		t.Fatalf("form status = %d: %s", response.Code, response.Body.String())
	}
	result := call(t, c, "list_widgets", nil)
	// R-FJHY-ZGJ4: the form-created widget is the final list object.
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		StructuredContent struct {
			Widgets []json.RawMessage `json:"widgets"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatal(err)
	}
	widgets := answer.StructuredContent.Widgets
	if len(widgets) == 0 {
		t.Fatal("empty widget list")
	}
	all := toolsStoreAll(t, s)
	want := widgetJSON(t, widget.Widget{ID: all[len(all)-1].ID, Name: "from form & tools", Count: 9, Status: widget.StatusRetired})
	jsonEqual(t, widgets[len(widgets)-1], want)
}
