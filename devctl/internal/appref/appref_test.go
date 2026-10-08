package appref_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
)

func TestValidName(t *testing.T) {
	t.Parallel()

	// R-CVWR-UA16 R-S6EM-KPG8
	valid := []string{"a", "0", "crm", "crm-api", "a--9", "snapshot", "seeds", "seed-1", "golden", strings.Repeat("a", 63)}
	for _, name := range valid {
		if !appref.ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}
	invalid := []string{
		"", "-crm", "crm-", "CRM", "crm_api", "cr.m", "café", strings.Repeat("a", 64),
		"host", "deploy", "snapshots", "seed", "backup-host", "backup-services", "renew-certificate",
	}
	for _, name := range invalid {
		if appref.ValidName(name) {
			t.Errorf("ValidName(%q) = true, want false", name)
		}
	}
}
