package web_test

import (
	"bytes"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	assets "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

// R-RXNM-K6Y8 R-RYVI-XYOX R-S03F-BQFM
func TestPageTemplateSet(t *testing.T) {
	templates, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	b := page.Banner{Service: "chosen-service", Version: "chosen-version"}
	for _, name := range []string{"landing", "about"} {
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
		var output bytes.Buffer
		if err := templates.ExecuteTemplate(&output, name, data); err != nil {
			t.Fatal(err)
		}
		if output.Len() == 0 {
			t.Fatal("empty template", name)
		}
	}
}
