package web_test

import (
	"bytes"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	assets "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

// R-RXNM-K6Y8 R-DLOQ-H9RT R-S03F-BQFM
func TestPageTemplateSet(t *testing.T) {
	templates, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	b := page.Banner{Service: "chosen-service", Release: "chosen-release", Commit: "chosen-commit"}
	for _, name := range []string{"landing", "about", "tools"} {
		if templates.Lookup(name) == nil {
			t.Fatal("missing template", name)
		}
		var data any = struct{ Banner page.Banner }{b}
		if name == "about" {
			data = struct {
				Banner      page.Banner
				Description string
			}{b, web.Description}
		}
		if name == "tools" {
			// R-DMWM-V1II R-DO4J-8T97
			tool := web.Tool{"supplied-name", "supplied-description"}
			if tool.Name != "supplied-name" || tool.Description != "supplied-description" {
				t.Fatal("positional tool fields", tool)
			}
			data = web.ToolsData{b, []web.Tool{tool}}
		}
		var output bytes.Buffer
		if err := templates.ExecuteTemplate(&output, name, data); err != nil {
			t.Fatal(err)
		}
		if name == "tools" {
			for _, value := range []string{"supplied-name", "supplied-description"} {
				if !bytes.Contains(output.Bytes(), []byte(value)) {
					t.Fatal("missing supplied tool value", value)
				}
			}
		}
		if output.Len() == 0 {
			t.Fatal("empty template", name)
		}
	}
}
