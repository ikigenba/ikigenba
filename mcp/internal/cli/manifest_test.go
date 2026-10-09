package cli_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/mcp/internal/cli"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

// R-ZZLQ-E99Q
func TestManifestDescription(t *testing.T) {
	const manifest string = cli.Manifest
	want := "description = \"" + gateway.Description + "\""
	if !slices.Contains(strings.Split(manifest, "\n"), want) {
		t.Fatalf("manifest description differs from gateway description")
	}
}
