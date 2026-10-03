package apps_test

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-7P5I-XTXZ
func TestResourcesFields(t *testing.T) {
	resources := apps.Resources{CPUWeight: 100, MemoryMax: 536870912, IOWeight: 200}
	var cpu, io int
	var memory int64
	cpu, memory, io = resources.CPUWeight, resources.MemoryMax, resources.IOWeight
	if cpu != 100 || memory != 536870912 || io != 200 {
		t.Fatalf("Resources = %#v", resources)
	}
	var absent apps.Resources
	if absent.CPUWeight != 0 || absent.MemoryMax != 0 || absent.IOWeight != 0 {
		t.Fatalf("absent Resources = %#v", absent)
	}
}

// R-RHJO-G1CM
func TestParseManifestPartialResourcesHaveZeroDefaults(t *testing.T) {
	for _, test := range []struct {
		data string
		want apps.Resources
	}{
		{"", apps.Resources{}},
		{"[resources]", apps.Resources{}},
		{"resources.cpu_weight = 1", apps.Resources{CPUWeight: 1}},
		{"[resources]\nmemory_max = '512M'", apps.Resources{MemoryMax: 536870912}},
		{"resources = { io_weight = 10000 }", apps.Resources{IOWeight: 10000}},
	} {
		manifest, err := apps.ParseManifest([]byte(test.data))
		if err != nil || manifest.Resources != test.want {
			t.Errorf("ParseManifest(%q) Resources = %#v, %v; want %#v", test.data, manifest.Resources, err, test.want)
		}
	}
}

// R-7V90-UONG R-XSFU-AXFW
func TestParseManifestResourceWeights(t *testing.T) {
	for _, key := range []string{"cpu_weight", "io_weight"} {
		wantError := fmt.Sprintf("'resources.%s' must be a whole number from 1 to 10000", key)
		for _, value := range []string{"0", "10001", "-1", "50.0", `"50"`, "true", "[]", "{}"} {
			manifest, err := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\n" + key + " = " + value))
			assertEmptyResourceFailure(t, manifest)
			if err == nil || err.Error() != wantError {
				t.Errorf("%s = %s: error = %v; want %q", key, value, err, wantError)
			}
		}
		for _, value := range []int{1, 100, 10000} {
			manifest, err := apps.ParseManifest([]byte(fmt.Sprintf("[resources]\n%s = %d", key, value)))
			actual := manifest.Resources.CPUWeight
			if key == "io_weight" {
				actual = manifest.Resources.IOWeight
			}
			if err != nil || actual != value {
				t.Errorf("%s = %d: got %d, %v", key, value, actual, err)
			}
		}
	}
}

// R-7WGX-8GE5 R-XSFU-AXFW
func TestParseManifestMemoryBytes(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int64
	}{
		{"1", 1}, {"512M", 536870912}, {"1048576", 1048576}, {"2K", 2048}, {"2G", 2147483648},
		{"9223372036854775807", math.MaxInt64}, {"8589934591G", 9223372035781033984},
		{"000512M", 536870912}, {strings.Repeat("0", 100) + "1", 1},
	} {
		manifest, err := apps.ParseManifest([]byte(fmt.Sprintf("[resources]\nmemory_max = %q", test.value)))
		if err != nil || manifest.Resources.MemoryMax != test.want {
			t.Errorf("memory_max = %q: got %d, %v; want %d", test.value, manifest.Resources.MemoryMax, err, test.want)
		}
	}
	for _, value := range []string{
		`"0"`, `"0M"`, `"512MB"`, `"512m"`, `"1.5G"`, `"50%"`, `""`, `"infinity"`, "536870912",
		`"17179869185G"`, `"9223372036854775808"`, `"8589934592G"`, `"9007199254740992K"`,
		`"8796093022208M"`, `"-1"`, `"+1"`, `" 1"`, `"1 "`, `"１"`, `"1_000"`, `"K"`,
		"true", "[]", "{}",
	} {
		manifest, err := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\nmemory_max = " + value))
		assertEmptyResourceFailure(t, manifest)
		const want = "'resources.memory_max' must be a whole number of bytes, optionally followed by K, M, or G"
		if err == nil || err.Error() != want {
			t.Errorf("memory_max = %s: error = %v; want %q", value, err, want)
		}
	}
}

// R-7XOT-M84U R-XSFU-AXFW
func TestParseManifestUnknownResources(t *testing.T) {
	for _, value := range []string{"50", `"50"`, "true", "[]", "{}", "{ nested = 1 }"} {
		manifest, err := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\ncpu_quota = " + value))
		assertEmptyResourceFailure(t, manifest)
		const want = "'resources.cpu_quota' is not allowed; the resources are cpu_weight, memory_max, and io_weight"
		if err == nil || err.Error() != want {
			t.Errorf("cpu_quota = %s: error = %v; want %q", value, err, want)
		}
	}
	for _, data := range []string{"[resources.unknown]", "resources.unknown.nested = 1", "[resources]\n'Unknown Key' = 1"} {
		key := "unknown"
		if strings.Contains(data, "Unknown Key") {
			key = "Unknown Key"
		}
		manifest, err := apps.ParseManifest([]byte(resourceCompanionFields + data))
		assertEmptyResourceFailure(t, manifest)
		want := fmt.Sprintf("'resources.%s' is not allowed; the resources are cpu_weight, memory_max, and io_weight", key)
		if err == nil || err.Error() != want {
			t.Errorf("ParseManifest(%q) = %v; want %q", data, err, want)
		}
	}
}

// R-7YWP-ZZVJ R-XSFU-AXFW
func TestParseManifestResourceErrorOrder(t *testing.T) {
	for _, test := range []struct{ data, offending string }{
		{"zz = 1\nio_weight = 0\nmemory_max = '512MB'\ncpu_weight = 0", "cpu_weight"},
		{"zz = 1\nio_weight = 0\nmemory_max = '512MB'", "memory_max"},
		{"zz = 1\nio_weight = 0", "io_weight"},
		{"zz = 1\ncpu_quota = 1", "cpu_quota"},
		{"zz = 1\n'Z' = 1\n'a' = 1", "Z"},
	} {
		baselineModel, baseline := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\n" + test.offending + " = " + map[string]string{"cpu_weight": "0", "memory_max": "'512MB'", "io_weight": "0", "cpu_quota": "1", "Z": "1"}[test.offending]))
		combinedModel, combined := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\n" + test.data))
		assertEmptyResourceFailure(t, baselineModel)
		assertEmptyResourceFailure(t, combinedModel)
		if baseline == nil || combined == nil || baseline.Error() != combined.Error() {
			t.Errorf("resource priority: got %v; want %v", combined, baseline)
		}
	}
	for _, other := range []string{
		"port = 3000", "app = 'host'", "app = 3", "description = 3", `description = "\t"`,
		`description = "\n"`, "default = 'yes'", "mcp = 3", "secrets = [3]", "env = { MODE = 3 }",
		"database = { engine = 'other', path = 'state/app.db' }",
	} {
		companion := "app = 'notes'\ndescription = 'Offers tools'\n"
		if strings.HasPrefix(other, "app =") {
			companion = "description = 'Offers tools'\n"
		} else if strings.HasPrefix(other, "description =") {
			companion = "app = 'notes'\n"
		}
		baselineModel, baseline := apps.ParseManifest([]byte(companion + other + "\n"))
		assertEmptyResourceFailure(t, baselineModel)
		for _, data := range []string{other + "\n[resources]\ncpu_weight = 0", "resources = { cpu_weight = 0 }\n" + other + "\n"} {
			combinedModel, combined := apps.ParseManifest([]byte(companion + data))
			assertEmptyResourceFailure(t, combinedModel)
			if baseline == nil || combined == nil {
				t.Errorf("other rule priority for %q: got %v; want %v", data, combined, baseline)
				continue
			}
			baselineText, combinedText := baseline.Error(), combined.Error()
			if strings.HasPrefix(baselineText, "invalid manifest: line ") && strings.HasPrefix(combinedText, "invalid manifest: line ") {
				baselineText = strings.SplitN(baselineText, ": ", 3)[2]
				combinedText = strings.SplitN(combinedText, ": ", 3)[2]
			}
			if baselineText != combinedText {
				t.Errorf("other rule priority for %q: got %v; want %v", data, combined, baseline)
			}
		}
	}
	manifest, err := apps.ParseManifest([]byte("app = 'notes'\nenv = { MODE = 'production' }\nmcp = true\n[resources]\ncpu_weight = 0"))
	assertEmptyResourceFailure(t, manifest)
	if err == nil || err.Error() != "'resources.cpu_weight' must be a whole number from 1 to 10000" {
		t.Errorf("resources did not precede empty MCP description: %v", err)
	}
}

const resourceCompanionFields = "app = 'notes'\ndescription = 'Offers tools'\ndefault = true\nsecrets = ['TOKEN']\nenv = { MODE = 'production' }\n"

func assertEmptyResourceFailure(t *testing.T, manifest apps.Manifest) {
	t.Helper()
	if !reflect.DeepEqual(manifest, apps.Manifest{}) {
		t.Fatalf("failed ParseManifest returned partial model: %#v", manifest)
	}
}
