package apps_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-93Z5-D770
func TestIconAndServicesConstants(t *testing.T) {
	const icon, path, environment = apps.IconPath, apps.ServicesPath, apps.ServicesEnv
	if icon != "share/icon.svg" || path != "/run/ikigenba/services.json" || apps.PerAppServicesPath != "/var/lib/ikigenba/services.json" || environment != "IKIGENBA_SERVICES" {
		t.Fatalf("icon and services constants: %q, %q, %q", icon, path, environment)
	}
}
