package smarthttp_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
)

// R-U4IX-S21H
func TestRefusalCopyConstants(t *testing.T) {
	const notFound string = smarthttp.NotFound
	const repoNotFound string = smarthttp.RepoNotFound
	const unreachable string = smarthttp.Unreachable
	const unavailable string = smarthttp.Unavailable
	const tooBusy string = smarthttp.TooBusy
	const stopping string = smarthttp.Stopping
	const atSizeLimit string = smarthttp.AtSizeLimit
	for _, value := range []string{notFound, repoNotFound, unreachable, unavailable, tooBusy, stopping, atSizeLimit} {
		if value == "" || strings.Contains(value, "\n") {
			t.Fatalf("invalid response copy constant: %q", value)
		}
	}
	if strings.Count(atSizeLimit, "%d") != 1 || strings.Contains(strings.ReplaceAll(atSizeLimit, "%d", ""), "%") {
		t.Fatal("size-limit format must have exactly one integer verb")
	}
}
