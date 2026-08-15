package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"prompts/internal/prompt"
	"strings"
	"testing"
)

func TestBrandIconAssetsMatchSuiteReference(t *testing.T) {
	for _, name := range []string{"favicon.ico", "favicon-32.png", "apple-touch-icon.png"} {
		got, err := os.ReadFile(filepath.Join(promptsWWWPath(), "static", name))
		if err != nil {
			t.Fatalf("read shipped brand icon %s: %v", name, err)
		}
		want, err := os.ReadFile(filepath.Join("..", "..", "..", "design", "brand", name))
		if err != nil {
			t.Fatalf("read suite brand icon %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("shipped brand icon %s differs from design/brand reference", name)
		}
	}
}

func TestEveryServedDocumentCarriesBrandIconLinks(t *testing.T) {
	// R-RYDN-YNR5
	promptRow := prompt.Prompt{
		ID: "brand-icon-prompt", Name: "Brand icon prompt", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-08-11T00:00:00Z", UpdatedAt: "2026-08-11T00:00:00Z",
	}
	runRow := prompt.Run{
		ID: "brand-icon-run", PromptID: promptRow.ID, PromptName: promptRow.Name,
		OwnerEmail: promptRow.OwnerEmail, Status: prompt.RunSucceeded,
		StartedAt: "2026-08-11T00:00:00Z", EndedAt: "2026-08-11T00:01:00Z",
	}
	handler := newUIHandler(t, []prompt.Prompt{promptRow}, []prompt.Run{runRow})
	renders := map[string]struct {
		path   string
		status int
	}{
		"ui-404.html":     {path: "/ui/prompts/missing-brand-icon-prompt", status: http.StatusNotFound},
		"ui-prompt.html":  {path: "/ui/prompts/" + promptRow.ID, status: http.StatusOK},
		"ui-prompts.html": {path: "/ui/", status: http.StatusOK},
		"ui-run.html":     {path: "/ui/runs/" + runRow.ID, status: http.StatusOK},
		"ui-runs.html":    {path: "/ui/runs", status: http.StatusOK},
	}

	documents, err := filepath.Glob(filepath.Join(promptsWWWPath(), "ui-*.html"))
	if err != nil {
		t.Fatalf("enumerate committed UI documents: %v", err)
	}
	if len(documents) == 0 {
		t.Fatal("no committed UI documents found")
	}
	for _, document := range documents {
		name := filepath.Base(document)
		render, ok := renders[name]
		if !ok {
			t.Errorf("committed document %s has no real render-path request", name)
			continue
		}
		delete(renders, name)

		rec := serveUI(t, handler, render.path)
		if rec.Code != render.status {
			t.Errorf("render %s through %s: status = %d, want %d", name, render.path, rec.Code, render.status)
			continue
		}
		head := renderedHead(t, rec.Body.String())
		for _, want := range []string{
			`<link rel="icon" href="/srv/prompts/static/favicon.ico" sizes="any">`,
			`<link rel="icon" type="image/png" href="/srv/prompts/static/favicon-32.png">`,
			`<link rel="apple-touch-icon" href="/srv/prompts/static/apple-touch-icon.png">`,
		} {
			if !strings.Contains(head, want) {
				t.Errorf("rendered %s head missing %q:\n%s", name, want, head)
			}
		}
	}
	for name := range renders {
		t.Errorf("render-path mapping %s has no committed UI document", name)
	}
}

func TestAssembledRouterServesBrandIconAssetsWithPinnedTypes(t *testing.T) {
	// R-RZLK-CFHU
	handler := newUIHandler(t, nil, nil)
	assets := []struct {
		name        string
		contentType string
	}{
		{name: "favicon.ico", contentType: "image/x-icon"},
		{name: "favicon-32.png", contentType: "image/png"},
		{name: "apple-touch-icon.png", contentType: "image/png"},
	}
	for _, asset := range assets {
		rec := serveUI(t, handler, "/static/"+asset.name)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /static/%s status = %d, want %d", asset.name, rec.Code, http.StatusOK)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET /static/%s returned an empty body", asset.name)
		}
		if got := rec.Header().Get("Content-Type"); got != asset.contentType {
			t.Errorf("GET /static/%s Content-Type = %q, want %q", asset.name, got, asset.contentType)
		}
	}
}
