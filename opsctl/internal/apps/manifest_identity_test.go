package apps_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-91JC-LNPM R-90BG-7VYX
func TestDiscoveredManifestIdentityAndFaultsStayPerService(t *testing.T) {
	for _, fixture := range []struct {
		name, data string
		fault      bool
	}{
		{"matching", "app = 'notes'\n", false},
		{"absent", "[env]\nKEY = 'plain'\n", false},
		{"other", "app = 'other'\n", true},
		{"integer", "app = 42\n", true},
		{"array", "app = ['notes']\n", true},
		{"table", "[app]\nname = 'notes'\n", true},
		{"matching schema fault", "app = 'notes'\n[resources]\nio_weight = 50\n", true},
		{"different schema fault", "app = 'other'\n[resources]\nio_weight = 50\n", true},
		{"different array table", "app = 'other'\n[[database]]\npath = 'state/db'\n", true},
		{"bad syntax", "app = \n", true},
		{"duplicate key", "app = 'notes'\napp = 'other'\n", true},
		{"bad syntax after different identity", "app = 'other'\n[broken\n", true},
		{"invalid utf8", "app = 'other'\n#\xff", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeManifest(t, root, "notes", fixture.data)
			writeManifest(t, root, "unaffected", "app = 'unaffected'\n")
			services, err := apps.Discover(root)
			if err != nil || len(services) != 2 {
				t.Fatalf("Discover=%#v,%v", services, err)
			}
			notes := services[0]
			if notes.Name != "notes" || (notes.ManifestError != nil) != fixture.fault || (notes.Manifest == nil) != fixture.fault {
				t.Fatalf("notes=%#v; fault=%v", notes, fixture.fault)
			}
			if !fixture.fault && notes.Manifest.App != "" && notes.Manifest.App != "notes" {
				t.Fatalf("accepted mismatching app: %#v", notes.Manifest)
			}
			other := services[1]
			if other.Name != "unaffected" || other.Manifest == nil || other.Manifest.App != "unaffected" || other.ManifestError != nil {
				t.Fatalf("unaffected=%#v", other)
			}
		})
	}
}
