package apps

import "testing"

func TestManifestDisownsUsesTOMLIdentityDespiteSchemaFaults(t *testing.T) {
	// R-20O1-EI5C
	for _, test := range []struct {
		name, data string
		disowns    bool
	}{
		{"matching", "app = 'notes'\n", false},
		{"absent", "[env]\nKEY = 'plain'\n", true},
		{"other", "app = 'other'\n", true},
		{"integer", "app = 42\n", true},
		{"array", "app = ['notes']\n", true},
		{"table", "[app]\nname = 'notes'\n", true},
		{"matching schema fault", "app = 'notes'\n[resources]\nio_weight = 50\n", false},
		{"different schema fault", "app = 'other'\n[resources]\nio_weight = 50\n", true},
		{"different array table", "app = 'other'\n[[database]]\npath = 'state/db'\n", true},
		{"bad syntax", "app = \n", false},
		{"duplicate key", "app = 'notes'\napp = 'other'\n", false},
		{"bad syntax after different identity", "app = 'other'\n[broken\n", false},
		{"invalid utf8", "app = 'other'\n#\xff", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ManifestDisowns([]byte(test.data), "notes"); got != test.disowns {
				t.Fatalf("ManifestDisowns = %v, want %v", got, test.disowns)
			}
		})
	}
}
