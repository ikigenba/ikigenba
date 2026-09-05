// Package help renders the provider and model portions of the help catalog.
package help

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Providers returns the authentication choices for every catalog host.
func Providers() string {
	catalog := agentkit.Catalog()
	hosts := catalogHosts(catalog)
	var lines []string

	for _, host := range hosts {
		lines = append(lines, fmt.Sprintf("  %-13s%-14s(%s_API_KEY)", host, "auth=api_key", strings.ToUpper(string(host))))
		if supportsOAuth(catalog, host) {
			lines = append(lines, fmt.Sprintf("%15s%-14s(auth_file=~/.agent-repl/%s-auth.json)", "", "auth=oauth", host))
		}
	}

	return strings.Join(lines, "\n")
}

// Models returns the catalog models grouped by host.
func Models() string {
	catalog := agentkit.Catalog()
	hosts := catalogHosts(catalog)
	sections := make([]string, 0, len(hosts))

	for _, host := range hosts {
		lines := []string{string(host)}
		for _, entry := range catalog {
			if offering, ok := firstOffering(entry, host); ok {
				lines = append(lines, modelRow(entry.Model, offering.Reasoning))
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	return strings.Join(sections, "\n\n")
}

func catalogHosts(catalog []agentkit.CatalogEntry) []agentkit.Host {
	set := make(map[agentkit.Host]struct{})
	for _, entry := range catalog {
		for _, offering := range entry.Offerings {
			set[offering.Host] = struct{}{}
		}
	}

	hosts := make([]agentkit.Host, 0, len(set))
	for host := range set {
		hosts = append(hosts, host)
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i] < hosts[j] })
	return hosts
}

func supportsOAuth(catalog []agentkit.CatalogEntry, host agentkit.Host) bool {
	for _, entry := range catalog {
		for _, offering := range entry.Offerings {
			if offering.Host != host {
				continue
			}
			for _, mode := range offering.AuthModes {
				if mode == agentkit.AuthModeOAuth {
					return true
				}
			}
		}
	}
	return false
}

func firstOffering(entry agentkit.CatalogEntry, host agentkit.Host) (agentkit.Offering, bool) {
	for _, offering := range entry.Offerings {
		if offering.Host == host {
			return offering, true
		}
	}
	return agentkit.Offering{}, false
}

func modelRow(model string, spec agentkit.ReasoningSpec) string {
	if spec.Kind == agentkit.ReasoningKindNone {
		return "  " + model
	}

	entries := reasoningVocabulary(spec)
	return fmt.Sprintf("  %-26s%s={%s}", model, spec.Term, strings.Join(entries, "|"))
}

func reasoningVocabulary(spec agentkit.ReasoningSpec) []string {
	var entries []string
	if spec.Default.Mode == agentkit.ReasoningDefault {
		entries = append(entries, "*dynamic")
	}

	switch spec.Kind {
	case agentkit.ReasoningKindEffort:
		for _, level := range spec.Levels {
			entries = append(entries, markDefault(level.String(), spec.Default.Mode == agentkit.ReasoningEffort && spec.Default.Effort == level))
		}
		if spec.CanDisable {
			entries = append(entries, markDefault("off", spec.Default.Mode == agentkit.ReasoningOff))
		}
	case agentkit.ReasoningKindBudget:
		if spec.CanDisable {
			entries = append(entries, markDefault("off", spec.Default.Mode == agentkit.ReasoningOff))
		}
		budget := fmt.Sprintf("%d–%d", spec.MinBudget, spec.MaxBudget)
		entries = append(entries, markDefault(budget, spec.Default.Mode == agentkit.ReasoningBudget))
	case agentkit.ReasoningKindToggle:
		if spec.CanEnable {
			entries = append(entries, markDefault("on", spec.Default.Mode == agentkit.ReasoningOn))
		}
		if spec.CanDisable {
			entries = append(entries, markDefault("off", spec.Default.Mode == agentkit.ReasoningOff))
		}
	}
	return entries
}

func markDefault(value string, isDefault bool) string {
	if isDefault {
		return "*" + value
	}
	return value
}
