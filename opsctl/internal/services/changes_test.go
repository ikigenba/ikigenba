package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// R-UKL2-EMYF
func TestWriteClassifiesEntryMemberDifferences(t *testing.T) {
	for _, test := range []struct {
		name     string
		change   func(map[string]any)
		disabled bool
		noIcon   bool
		want     Change
	}{
		{name: "same", want: Unchanged},
		{name: "unknown member", change: func(p map[string]any) { p["future"] = []any{1, "value"} }, want: Unchanged},
		{name: "url", change: func(p map[string]any) { p["url"] = "old" }, want: Updated},
		{name: "description", change: func(p map[string]any) { p["description"] = "old" }, want: Updated},
		{name: "socket", change: func(p map[string]any) { p["socket"] = "old" }, want: Updated},
		{name: "group changed", change: func(p map[string]any) { p["group"] = "core" }, want: Updated},
		{name: "group absent before upgrade", change: func(p map[string]any) { delete(p, "group") }, want: Updated},
		{name: "group number", change: func(p map[string]any) { p["group"] = 17 }, want: Updated},
		{name: "mcp", change: func(p map[string]any) { p["mcp"] = true }, want: Updated},
		{name: "icon", change: func(p map[string]any) { p["icon"] = "old" }, want: Updated},
		{name: "icon gained", change: func(p map[string]any) { delete(p, "icon") }, want: Updated},
		{name: "icon lost", noIcon: true, want: Updated},
		{name: "nonstring icon absent", noIcon: true, change: func(p map[string]any) { p["icon"] = []any{1} }, want: Unchanged},
		{name: "nonstring icon gained", change: func(p map[string]any) { p["icon"] = false }, want: Updated},
		{name: "enabled invalid", change: func(p map[string]any) { p["enabled"] = "true" }, want: Updated},
		{name: "disabled overrides changes", disabled: true, change: func(p map[string]any) { p["description"] = "old"; p["mcp"] = true; delete(p, "group") }, want: Disabled},
		{name: "enabled overrides changes", change: func(p map[string]any) { p["enabled"] = false; p["socket"] = "old"; delete(p, "group") }, want: Enabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := perAppRoot(t)
			icon := renderFixture(t, root, "running", "app = \"running\"\n", true, []byte("<svg/>\n"))
			if test.noIcon {
				if err := os.Remove(icon); err != nil {
					t.Fatal(err)
				}
			}
			prior := map[string]any{"name": "running", "url": "https://running.example.test", "description": "", "socket": "/run/ikigenba/running.sock", "enabled": true, "mcp": false, "icon": "<svg/>\n", "group": "application"}
			if test.change != nil {
				test.change(prior)
			}
			data, err := json.Marshal(map[string]any{"services": []any{prior}})
			if err != nil {
				t.Fatal(err)
			}
			file := servicesFile(root)
			if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, data, 0o600); err != nil {
				t.Fatal(err)
			}
			changes, err := Write(context.Background(), renderWriteEnv(t, root, map[string]bool{"running": test.disabled}, nil), "example.test")
			want := Changes{}
			if test.want != Unchanged {
				want["running"] = test.want
			}
			if err != nil || !reflect.DeepEqual(changes, want) {
				t.Fatalf("changes=%v error=%v, want %v", changes, err, want)
			}
		})
	}
	for _, member := range []string{"url", "description", "socket", "enabled", "mcp", "group"} {
		for _, value := range []string{"missing", "null", "object"} {
			t.Run(member+" "+value, func(t *testing.T) {
				root := perAppRoot(t)
				renderFixture(t, root, "running", "app = \"running\"\n", true, nil)
				prior := map[string]any{"name": "running", "url": "https://running.example.test", "description": "", "socket": "/run/ikigenba/running.sock", "enabled": true, "mcp": false, "group": "application"}
				switch value {
				case "missing":
					delete(prior, member)
				case "null":
					prior[member] = nil
				case "object":
					prior[member] = map[string]any{"x": true}
				}
				data, err := json.Marshal(map[string]any{"services": []any{prior}})
				if err != nil {
					t.Fatal(err)
				}
				file := servicesFile(root)
				if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, data, 0o600); err != nil {
					t.Fatal(err)
				}
				changes, err := Write(context.Background(), renderWriteEnv(t, root, nil, nil), "example.test")
				if err != nil || !reflect.DeepEqual(changes, Changes{"running": Updated}) {
					t.Fatalf("changes=%v error=%v", changes, err)
				}
			})
		}
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
