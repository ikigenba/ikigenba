package main

import (
	"appkit/web"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	headPattern      = regexp.MustCompile(`(?is)<head\b[^>]*>(.*?)</head>`)
	linkPattern      = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attributePattern = regexp.MustCompile(`([[:alnum:]-]+)\s*=\s*"([^"]*)"`)
)

// R-RYDN-YNR5
func TestEveryServedDocumentCarriesBrandIconLinks(t *testing.T) {
	root := repoShareWWWPath(t)
	site, err := web.Load(root)
	if err != nil {
		t.Fatalf("load committed web surface: %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("enumerate committed web surface: %v", err)
	}
	documents := 0
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".html") && !strings.HasSuffix(entry.Name(), ".tmpl")) {
			continue
		}
		documents++
		recorder := httptest.NewRecorder()
		if err := site.Render(recorder, entry.Name(), struct{ Service, Version string }{"webhooks", "test"}); err != nil {
			t.Fatalf("render %s: %v", entry.Name(), err)
		}
		if recorder.Code != http.StatusOK {
			t.Errorf("render %s status = %d, want 200", entry.Name(), recorder.Code)
		}
		assertBrandIconLinks(t, entry.Name(), recorder.Body.String())
	}
	if documents == 0 {
		t.Fatal("committed web surface contains no renderable documents")
	}
}

func assertBrandIconLinks(t *testing.T, document, body string) {
	t.Helper()
	head := headPattern.FindStringSubmatch(body)
	if head == nil {
		t.Fatalf("rendered %s has no head element", document)
	}

	wants := []map[string]string{
		{"rel": "icon", "sizes": "any", "href": "static/favicon.ico"},
		{"rel": "icon", "type": "image/png", "href": "static/favicon-32.png"},
		{"rel": "apple-touch-icon", "href": "static/apple-touch-icon.png"},
	}
	links := linkPattern.FindAllString(head[1], -1)
	for _, want := range wants {
		found := false
		for _, link := range links {
			attrs := make(map[string]string)
			for _, match := range attributePattern.FindAllStringSubmatch(link, -1) {
				attrs[strings.ToLower(match[1])] = match[2]
			}
			matches := true
			for key, value := range want {
				if attrs[key] != value {
					matches = false
					break
				}
			}
			if matches {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rendered %s head lacks link attributes %v", document, want)
		}
	}
}

// R-RZLK-CFHU
func TestAssembledServiceServesBrandIconAssets(t *testing.T) {
	bin := buildBinary(t)
	port := freeTCPPort(t)
	ctx, cancel := context.WithCancel(context.Background())

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = testEnv(map[string]string{
		"WEBHOOKS_DB_PATH":     filepath.Join(t.TempDir(), "webhooks.db"),
		"WEBHOOKS_WWW_PATH":    repoShareWWWPath(t),
		"WEBHOOKS_PORT":        strconv.Itoa(port),
		"WEBHOOKS_RESOURCE_ID": "http://127.0.0.1/srv/webhooks/mcp",
		"WEBHOOKS_AUTH_SERVER": "http://127.0.0.1",
	})
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start serve: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer stopProcess(cancel, done)

	waitForHealth(t, port, done, &stdout, &stderr)
	client := http.Client{}
	assets := []struct {
		name        string
		contentType string
	}{
		{name: "favicon.ico", contentType: "image/x-icon"},
		{name: "favicon-32.png", contentType: "image/png"},
		{name: "apple-touch-icon.png", contentType: "image/png"},
	}
	for _, asset := range assets {
		t.Run(asset.name, func(t *testing.T) {
			url := fmt.Sprintf("http://127.0.0.1:%d/static/%s", port, asset.name)
			resp, err := client.Get(url)
			if err != nil {
				t.Fatalf("GET %s: %v", url, err)
			}
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if readErr != nil {
				t.Fatalf("read GET %s response: %v", url, readErr)
			}
			if closeErr != nil {
				t.Fatalf("close GET %s response: %v", url, closeErr)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s status = %d, want 200", url, resp.StatusCode)
			}
			if len(body) == 0 {
				t.Errorf("GET %s returned an empty body", url)
			}
			if got := resp.Header.Get("Content-Type"); got != asset.contentType {
				t.Errorf("GET %s Content-Type = %q, want %q", url, got, asset.contentType)
			}
		})
	}
}
