package pages_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/appkit/page"
)

// R-D6CE-FEAB
func TestDescriptionCopy(t *testing.T) {
	const description string = pages.Description
	if description == "" || strings.Contains(description, "\n") {
		t.Fatalf("invalid Description %q", description)
	}
	if !strings.Contains(cli.Manifest, "description = \""+description+"\"\n") {
		t.Fatal("manifest does not carry Description")
	}
	f := setup(t)
	target := "/about"
	same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "about", 200, pages.AboutData{Banner: banner(f, target, []page.Level{pages.AboutLevel}), Description: description}))
}
