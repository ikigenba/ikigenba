package apps_test

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-Y1RD-E8L4
func TestResourcesFields(t *testing.T) {
	resources := apps.Resources{Slice: "core", MemoryMax: 536870912, GoMemoryLimit: 268435456, CPUWeight: 100, Delegate: true, OOMPolicy: "continue"}
	var slice, oom string
	var memory, goMemory int64
	var cpu int
	var delegate bool
	slice, memory, goMemory, cpu, delegate, oom = resources.Slice, resources.MemoryMax, resources.GoMemoryLimit, resources.CPUWeight, resources.Delegate, resources.OOMPolicy
	if slice != "core" || memory != 536870912 || goMemory != 268435456 || cpu != 100 || !delegate || oom != "continue" {
		t.Fatalf("Resources = %#v", resources)
	}
}

// R-ZATO-AEFN
func TestParseManifestPartialResourcesHaveDefaults(t *testing.T) {
	defaults := apps.Resources{Slice: "apps", MemoryMax: 134217728, GoMemoryLimit: 100663296, CPUWeight: 100}
	for _, test := range []struct {
		data string
		want apps.Resources
	}{
		{"", defaults}, {"[resources]", defaults},
		{"resources.cpu_weight = 1", apps.Resources{Slice: "apps", MemoryMax: 134217728, GoMemoryLimit: 100663296, CPUWeight: 1}},
		{"[resources]\nmemory_max = '256M'", apps.Resources{Slice: "apps", MemoryMax: 268435456, GoMemoryLimit: 201326592, CPUWeight: 100}},
		{"[resources]\nmemory_max = '9223372036854775807'", apps.Resources{Slice: "apps", MemoryMax: math.MaxInt64, GoMemoryLimit: 6917529027641081855, CPUWeight: 100}},
		{"[resources]\nmemory_max = '7'", apps.Resources{Slice: "apps", MemoryMax: 7, GoMemoryLimit: 5, CPUWeight: 100}},
		{"resources = { delegate = true }", apps.Resources{Slice: "apps", MemoryMax: 134217728, GoMemoryLimit: 100663296, CPUWeight: 100, Delegate: true}},
	} {
		manifest, err := apps.ParseManifest([]byte(test.data))
		if err != nil || manifest.Resources != test.want {
			t.Errorf("ParseManifest(%q) = %#v, %v; want %#v", test.data, manifest.Resources, err, test.want)
		}
	}
}

// R-ZC1K-O66C R-ZD9H-1XX1 R-ZFP9-THEF R-ZGX6-7954 R-ZI52-L0VT R-XSFU-AXFW
func TestParseManifestResourceValues(t *testing.T) {
	for _, test := range []struct {
		key, wantError     string
		rejected, accepted []string
	}{
		{"slice", "'resources.slice' must be \"core\" or \"apps\"", []string{`"edge"`, `"Core"`, `""`, "1", "true", "[]", "{}"}, []string{`"core"`, `"apps"`}},
		{"go_memory_limit", "'resources.go_memory_limit' must be a whole number of bytes, optionally followed by K, M, or G", []string{`"0"`, `"128MB"`, `"128m"`, `"1.5G"`, `""`, "134217728", `"9223372036854775808"`, `"8589934592G"`, `"17179869185G"`, `"-1"`, `"+1"`, `" 1"`, `"1 "`, `"１"`, `"1_000"`, `"K"`, "true", "[]", "{}"}, []string{`"1"`, `"128M"`, `"2K"`, `"0001"`}},
		{"cpu_weight", "'resources.cpu_weight' must be a whole number from 1 to 10000", []string{"0", "10001", "20000", "-1", "50.0", `"50"`, "true", "[]", "{}"}, []string{"1", "100", "10000"}},
		{"delegate", "'resources.delegate' must be true or false", []string{`"yes"`, `"true"`, "1", "[]", "{}"}, []string{"true", "false"}},
		{"oom_policy", "'resources.oom_policy' must be \"continue\"", []string{`"stop"`, `"kill"`, `"Continue"`, `""`, "true", "1", "[]", "{}"}, []string{`"continue"`}},
	} {
		for _, value := range test.rejected {
			model, err := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\n" + test.key + " = " + value))
			assertEmptyResourceFailure(t, model)
			if err == nil || err.Error() != test.wantError {
				t.Errorf("%s = %s: %v; want %s", test.key, value, err, test.wantError)
			}
		}
		for _, value := range test.accepted {
			model, err := apps.ParseManifest([]byte("[resources]\n" + test.key + " = " + value))
			if err != nil {
				t.Errorf("%s = %s: %v", test.key, value, err)
				continue
			}
			switch test.key {
			case "slice":
				if model.Resources.Slice != strings.Trim(value, "\"") {
					t.Errorf("slice: %#v", model.Resources)
				}
			case "cpu_weight":
				if fmt.Sprint(model.Resources.CPUWeight) != value {
					t.Errorf("cpu: %#v", model.Resources)
				}
			case "delegate":
				if fmt.Sprint(model.Resources.Delegate) != value {
					t.Errorf("delegate: %#v", model.Resources)
				}
			case "oom_policy":
				if model.Resources.OOMPolicy != "continue" {
					t.Errorf("oom: %#v", model.Resources)
				}
			case "go_memory_limit":
				want := map[string]int64{`"1"`: 1, `"128M"`: 134217728, `"2K"`: 2048, `"0001"`: 1}[value]
				if model.Resources.GoMemoryLimit != want {
					t.Errorf("Go memory: %#v", model.Resources)
				}
			}
		}
	}
}

// R-ZEHD-FPNQ R-ZD9H-1XX1
func TestGoMemoryLimitComparedWithMemoryMax(t *testing.T) {
	for _, test := range []struct {
		data     string
		want     int64
		rejected bool
	}{
		{`memory_max = "256M"` + "\n" + `go_memory_limit = "512M"`, 0, true},
		{`go_memory_limit = "129M"`, 0, true},
		{`memory_max = "256M"` + "\n" + `go_memory_limit = "256M"`, 268435456, false},
		{`go_memory_limit = "128M"`, 134217728, false},
		{`memory_max = "9223372036854775807"` + "\n" + `go_memory_limit = "9223372036854775807"`, math.MaxInt64, false},
		{`memory_max = "2G"` + "\n" + `go_memory_limit = "2G"`, 2147483648, false},
	} {
		model, err := apps.ParseManifest([]byte("[resources]\n" + test.data))
		if test.rejected {
			assertEmptyResourceFailure(t, model)
			if err == nil || err.Error() != "'resources.go_memory_limit' must not be larger than 'resources.memory_max'" {
				t.Errorf("%s: %v", test.data, err)
			}
		} else if err != nil || model.Resources.GoMemoryLimit != test.want {
			t.Errorf("%s: %#v, %v", test.data, model.Resources, err)
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

// R-ZJCY-YSMI R-XSFU-AXFW
func TestParseManifestUnknownResources(t *testing.T) {
	for _, value := range []string{"50", `"50"`, "true", "[]", "{}", "{ nested = 1 }"} {
		manifest, err := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\ncpu_quota = " + value))
		assertEmptyResourceFailure(t, manifest)
		const want = "'resources.cpu_quota' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy"
		if err == nil || err.Error() != want {
			t.Errorf("cpu_quota = %s: error = %v; want %q", value, err, want)
		}
	}
	for _, data := range []string{"[resources]\nio_weight = 50", "[resources.unknown]", "resources.unknown.nested = 1", "[resources]\n'Unknown Key' = 1"} {
		key := "unknown"
		if strings.Contains(data, "io_weight") {
			key = "io_weight"
		}
		if strings.Contains(data, "Unknown Key") {
			key = "Unknown Key"
		}
		manifest, err := apps.ParseManifest([]byte(resourceCompanionFields + data))
		assertEmptyResourceFailure(t, manifest)
		want := fmt.Sprintf("'resources.%s' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy", key)
		if err == nil || err.Error() != want {
			t.Errorf("ParseManifest(%q) = %v; want %q", data, err, want)
		}
	}
}

// R-ZKKV-CKD7 R-XSFU-AXFW
func TestParseManifestResourceErrorOrder(t *testing.T) {
	for _, test := range []struct{ data, offending string }{
		{"slice = 'edge'\nmemory_max = '512MB'", "slice"},
		{"memory_max = '512MB'\ngo_memory_limit = '1.5G'", "memory_max"},
		{"go_memory_limit = '1.5G'\ncpu_weight = 0", "go_memory_limit"},
		{"memory_max = '256M'\ngo_memory_limit = '512M'\ncpu_weight = 0", "comparison"},
		{"cpu_weight = 0\ndelegate = 1", "cpu_weight"},
		{"delegate = 1\noom_policy = 'stop'", "delegate"},
		{"oom_policy = 'stop'\nio_weight = 1", "oom_policy"},
		{"zz = 1\nio_weight = 1", "io_weight"},
		{"zz = 1\n'Z' = 1\n'a' = 1", "Z"},
	} {
		baselineModel, baseline := apps.ParseManifest([]byte(resourceCompanionFields + "[resources]\n" + map[string]string{"slice": "slice = 'edge'", "cpu_weight": "cpu_weight = 0", "memory_max": "memory_max = '512MB'", "go_memory_limit": "go_memory_limit = '1.5G'", "comparison": "memory_max = '256M'\ngo_memory_limit = '512M'", "delegate": "delegate = 1", "oom_policy": "oom_policy = 'stop'", "io_weight": "io_weight = 1", "Z": "Z = 1"}[test.offending]))
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
