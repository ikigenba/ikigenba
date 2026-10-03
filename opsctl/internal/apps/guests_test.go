package apps_test

import (
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-RHJO-G1CM R-RIRK-TT3B
func TestManifestGuestsIsOptionalBoolean(t *testing.T) {
	for _, test := range []struct {
		data string
		want bool
	}{
		{"", false},
		{"guests = false", false},
		{"guests = true", true},
	} {
		manifest, err := apps.ParseManifest([]byte(test.data))
		if err != nil || manifest.Guests != test.want || manifest.App != "" {
			t.Fatalf("ParseManifest(%q) = %#v, %v", test.data, manifest, err)
		}
	}
	for _, value := range []string{"'true'", "1", "1.0", "[]", "{}", "1979-05-27", "07:32:00", "1979-05-27T07:32:00Z"} {
		manifest, err := apps.ParseManifest([]byte("guests = " + value))
		if err == nil || !reflect.DeepEqual(manifest, apps.Manifest{}) {
			t.Fatalf("non-Boolean guests %s = %#v, %v", value, manifest, err)
		}
	}
}
