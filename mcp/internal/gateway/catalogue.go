package gateway

import (
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

func mcpNames(entries services.List) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Name] {
			continue
		}
		seen[e.Name] = true
		first, ok := entries.Find(e.Name)
		if ok && e.Name != ServiceName && first.MCP {
			names = append(names, e.Name)
		}
	}
	sort.Strings(names)
	return names
}
func scopeNames(scope string) ([]string, bool) {
	names := strings.Split(scope, ",")
	seen := map[string]bool{}
	for _, n := range names {
		if len(n) < 1 || len(n) > 63 || seen[n] {
			return nil, false
		}
		seen[n] = true
		for i := 0; i < len(n); i++ {
			c := n[i]
			alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
			if !alnum && (c != '-' || i == 0 || i == len(n)-1) {
				return nil, false
			}
		}
	}
	sort.Strings(names)
	return names, true
}
func serviceStatus(entries services.List, name string) (string, bool, *string) {
	e, ok := entries.Find(name)
	reason := "not installed"
	if ok && name != ServiceName && e.MCP {
		if e.Enabled {
			return e.Description, true, nil
		}
		reason = "disabled"
		return e.Description, false, &reason
	}
	return "", false, &reason
}
