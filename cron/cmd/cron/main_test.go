package main

import (
	"os/exec"
	"strings"
	"testing"

	"appkit/manifest"
)

// R-4LKF-FB23
func TestManifestDeclaresCronPathRoutedProducer(t *testing.T) {
	got := manifest.Emit(manifest.Fields{
		App:   "cron",
		Mount: "/srv/cron/",
		Port:  3007,
		MCP:   true,
		Feed:  "/feed",
		Extras: []manifest.KV{
			{Key: "OUTBOX_RETENTION_DAYS", Value: "7"},
			{Key: "OUTBOX_RETENTION_MAX_ROWS", Value: "1000000"},
		},
	})
	for _, want := range []string{
		"APP=cron\n",
		"MOUNT=/srv/cron/\n",
		"PORT=3007\n",
		"MCP=true\n",
		"FEED=/feed\n",
		"OUTBOX_RETENTION_DAYS=7\n",
		"OUTBOX_RETENTION_MAX_ROWS=1000000\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cron manifest missing %q\n--- manifest ---\n%s", want, got)
		}
	}
}

func TestManifestVerbRejectedByReducedAppkitDispatch(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "manifest")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("cron manifest command succeeded, want reduced appkit dispatch rejection\n%s", out)
	}
	got := string(out)
	for _, want := range []string{
		`cron: unknown command "manifest"`,
		"serve|version|migrate|schema",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cron manifest rejection missing %q\n--- output ---\n%s", want, got)
		}
	}
}
