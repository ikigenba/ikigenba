package services

import (
	"reflect"
	"testing"
)

// R-LY06-CROI
func TestClassifyChanges(t *testing.T) {
	previous := []byte(`{
  "services": [
    {"name":"same","url":"https://same","icon":"<svg/>","enabled":true,"ignored":1},
    {"name":"removed","url":"old","icon":"old","enabled":true},
    {"name":"disabled","url":"old","icon":"old","enabled":true},
    {"name":"enabled","url":"old","icon":"old","enabled":false},
    {"name":"url","url":"old","icon":"old","enabled":true},
    {"name":"icon","url":"old","icon":"old","enabled":true},
    {"name":"other","url":"old","icon":"old","enabled":true}
  ]
}`)
	current := []entry{
		{Name: "same", URL: "https://same", Icon: "<svg/>", Enabled: true},
		{Name: "added", URL: "new", Icon: "new", Enabled: true},
		{Name: "disabled", URL: "changed", Icon: "old", Enabled: false},
		{Name: "enabled", URL: "old", Icon: "changed", Enabled: true},
		{Name: "url", URL: "new", Icon: "old", Enabled: true},
		{Name: "icon", URL: "old", Icon: "new", Enabled: true},
		{Name: "other", URL: "old", Icon: "old", Enabled: true},
	}
	want := Changes{
		"added": Added, "removed": Removed, "disabled": Disabled,
		"enabled": Enabled, "url": Updated, "icon": Updated,
	}
	if got := classify(previous, current); !reflect.DeepEqual(got, want) {
		t.Fatalf("classify() = %v, want %v", got, want)
	}
}

// R-8QFT-9FXP
func TestClassifyPreviousFileTolerance(t *testing.T) {
	current := []entry{{Name: "new", URL: "new", Icon: "new", Enabled: true}}
	for name, previous := range map[string][]byte{
		"absent or unreadable": nil,
		"malformed":            []byte(`{"services": [`),
		"nonobject":            []byte(`[]`),
		"missing services":     []byte(`{}`),
		"nonarray services":    []byte(`{"services": {}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if got, want := classify(previous, current), (Changes{"new": Added}); !reflect.DeepEqual(got, want) {
				t.Fatalf("classify() = %v, want %v", got, want)
			}
		})
	}

	previous := []byte(`{"services":[null,17,[],{"url":"orphan"},{"name":false},` +
		`{"name":"first","url":"one","icon":"i","enabled":true},` +
		`{"name":"first","url":"two","icon":"i","enabled":true}]}`)
	if got, want := classify(previous, []entry{{Name: "first", URL: "two", Icon: "i", Enabled: true}}),
		(Changes{"first": Updated}); !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate classification = %v, want %v", got, want)
	}
}
