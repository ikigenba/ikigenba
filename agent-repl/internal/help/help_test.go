package help_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-repl/internal/help"
	"github.com/ikigenba/ikigenba/agentkit"
)

// R-VF9J-M2PO
func TestExportedCatalogRenderersReturnText(t *testing.T) {
	if help.Providers() == "" || help.Models() == "" {
		t.Fatal("Providers or Models returned empty text")
	}
}

// R-VIX8-RDXR
func TestProvidersListsHostsInOrderWithAPIKeyFormatting(t *testing.T) {
	got := strings.Split(help.Providers(), "\n")
	wantHosts := hosts()
	var apiKeyLines []string
	for _, line := range got {
		if !strings.Contains(line, "auth=oauth") {
			apiKeyLines = append(apiKeyLines, line)
		}
	}
	if len(apiKeyLines) != len(wantHosts) {
		t.Fatalf("API-key line count = %d, want %d", len(apiKeyLines), len(wantHosts))
	}
	for i, host := range wantHosts {
		want := fmt.Sprintf("  %-13s%-14s(%s_API_KEY)", host, "auth=api_key", strings.ToUpper(string(host)))
		if apiKeyLines[i] != want {
			t.Errorf("API-key line %d = %q, want %q", i, apiKeyLines[i], want)
		}
	}
}

// R-B9R3-FFZ7
func TestProvidersDerivesOAuthLinesFromOfferingEndpoints(t *testing.T) {
	lines := strings.Split(help.Providers(), "\n")
	claimed := make(map[int]bool)
	for _, host := range hosts() {
		apiKey := fmt.Sprintf("  %-13s", host)
		apiKeyIndex := lineWithPrefix(t, lines, apiKey)
		claimed[apiKeyIndex] = true

		oauthIndex := apiKeyIndex + 1
		hasOAuth := oauthIndex < len(lines) && strings.Contains(lines[oauthIndex], "auth=oauth")
		if !hostSupportsOAuth(host) {
			if hasOAuth {
				t.Errorf("unsupported host %q has OAuth line %q", host, lines[oauthIndex])
			}
			continue
		}

		want := fmt.Sprintf("%15s%-14s(auth_file=~/.agent-repl/%s-auth.json)", "", "auth=oauth", host)
		if !hasOAuth {
			t.Errorf("supporting host %q has no OAuth line immediately after its API-key line", host)
			continue
		}
		if lines[oauthIndex] != want {
			t.Errorf("OAuth line for %q = %q, want %q", host, lines[oauthIndex], want)
		}
		claimed[oauthIndex] = true
	}
	if len(claimed) != len(lines) {
		var extra []string
		for i, line := range lines {
			if !claimed[i] {
				extra = append(extra, line)
			}
		}
		t.Fatalf("unexpected provider lines: %q", extra)
	}
}

// R-VMKX-WP5U
func TestModelsGroupsEveryCatalogModelByOrderedHost(t *testing.T) {
	output := help.Models()
	wantHosts := hosts()
	if strings.HasPrefix(output, "\n") || strings.HasSuffix(output, "\n") {
		t.Fatalf("Models has a leading or trailing blank-line artifact: %q", output)
	}
	if strings.Contains(output, "\n\n\n") {
		t.Fatalf("Models has more than one blank line between sections: %q", output)
	}
	if got, want := strings.Count(output, "\n\n"), len(wantHosts)-1; got != want {
		t.Fatalf("blank-line separator count = %d, want %d", got, want)
	}

	sections := strings.Split(output, "\n\n")
	if len(sections) != len(wantHosts) {
		t.Fatalf("section count = %d, want %d", len(sections), len(wantHosts))
	}

	wantModels := catalogModelsByHost()
	for i, section := range sections {
		lines := strings.Split(section, "\n")
		if got, want := lines[0], string(wantHosts[i]); got != want {
			t.Errorf("section %d heading = %q, want bare host name %q", i, got, want)
			continue
		}

		gotModels := make([]string, 0, len(lines)-1)
		for rowIndex, row := range lines[1:] {
			fields := strings.Fields(row)
			if len(fields) == 0 {
				t.Fatalf("host %q row %d is empty", wantHosts[i], rowIndex)
			}
			gotModels = append(gotModels, fields[0])
		}
		want := wantModels[wantHosts[i]]
		if got := strings.Join(gotModels, "\n"); got != strings.Join(want, "\n") {
			t.Errorf("models under host %q = %q, want catalog order %q", wantHosts[i], gotModels, want)
		}
	}
}

// R-VNSU-AGWJ
func TestModelRowsUseFirstHostOfferingAndReasoningShape(t *testing.T) {
	rows := renderedRows(t)
	for _, entry := range agentkit.Catalog() {
		for _, host := range hosts() {
			offering, ok := firstOffering(entry, host)
			if !ok {
				continue
			}
			got := rows[string(host)][entry.Model]
			if offering.Reasoning.Kind == agentkit.ReasoningKindNone {
				if got != "  "+entry.Model {
					t.Errorf("none row = %q, want %q", got, "  "+entry.Model)
				}
				continue
			}
			prefix := fmt.Sprintf("  %-26s%s={", entry.Model, offering.Reasoning.Term)
			if !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, "}") {
				t.Errorf("row for %s/%s = %q, want prefix %q and closing brace", host, entry.Model, got, prefix)
			}
		}
	}
}

// R-VP0Q-O8N8
func TestReasoningVocabularyHasRequiredValuesAndOrder(t *testing.T) {
	for host, rows := range renderedRows(t) {
		for model, row := range rows {
			offering := lookupOffering(t, model, agentkit.Host(host))
			if offering.Reasoning.Kind == agentkit.ReasoningKindNone {
				continue
			}
			got := stripMarkers(vocabulary(t, row))
			want := expectedVocabulary(offering.Reasoning)
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("%s/%s vocabulary = %q, want %q", host, model, got, want)
			}
		}
	}
}

// R-VQ8N-20DX
func TestEveryReasoningVocabularyMarksExactlyItsDefault(t *testing.T) {
	for host, rows := range renderedRows(t) {
		for model, row := range rows {
			spec := lookupOffering(t, model, agentkit.Host(host)).Reasoning
			if spec.Kind == agentkit.ReasoningKindNone {
				continue
			}
			entries := vocabulary(t, row)
			var marked []string
			for _, entry := range entries {
				if strings.HasPrefix(entry, "*") {
					marked = append(marked, strings.TrimPrefix(entry, "*"))
				}
			}
			want := defaultEntry(spec)
			if len(marked) != 1 || marked[0] != want {
				t.Errorf("%s/%s marked defaults = %q, want exactly %q", host, model, marked, want)
			}
		}
	}
}

// R-VRGJ-FS4M
func TestCatalogRegressionRowsUnderRequiredHosts(t *testing.T) {
	rows := renderedRows(t)
	wants := map[string]map[string]string{
		"anthropic":  {"claude-haiku-4-5": "  claude-haiku-4-5          thinking_budget={*off|1024–4096}"},
		"gemini":     {"gemini-2.5-flash": "  gemini-2.5-flash          thinking_budget={*dynamic|off|0–24576}"},
		"openrouter": {"grok-4.20": "  grok-4.20                 thinking={on|*off}"},
	}
	for host, models := range wants {
		for model, want := range models {
			if got := rows[host][model]; got != want {
				t.Errorf("%s/%s row = %q, want %q", host, model, got, want)
			}
		}
	}
}

func hosts() []agentkit.Host {
	set := make(map[agentkit.Host]bool)
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			set[offering.Host] = true
		}
	}
	result := make([]agentkit.Host, 0, len(set))
	for host := range set {
		result = append(result, host)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func catalogModelsByHost() map[agentkit.Host][]string {
	result := make(map[agentkit.Host][]string)
	for _, entry := range agentkit.Catalog() {
		seen := make(map[agentkit.Host]bool)
		for _, offering := range entry.Offerings {
			if seen[offering.Host] {
				continue
			}
			seen[offering.Host] = true
			result[offering.Host] = append(result[offering.Host], entry.Model)
		}
	}
	return result
}

func hostSupportsOAuth(host agentkit.Host) bool {
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if offering.Host == host {
				for _, endpoint := range offering.Endpoints {
					if endpoint.AuthMode == agentkit.AuthModeOAuth {
						return true
					}
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

func lineWithPrefix(t *testing.T, lines []string, prefix string) int {
	t.Helper()
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return i
		}
	}
	t.Fatalf("no provider line begins with %q in %q", prefix, lines)
	return -1
}

func renderedRows(t *testing.T) map[string]map[string]string {
	t.Helper()
	result := make(map[string]map[string]string)
	for _, section := range strings.Split(help.Models(), "\n\n") {
		lines := strings.Split(section, "\n")
		result[lines[0]] = make(map[string]string)
		for _, row := range lines[1:] {
			model := strings.Fields(row)[0]
			result[lines[0]][model] = row
		}
	}
	return result
}

func lookupOffering(t *testing.T, model string, host agentkit.Host) agentkit.Offering {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		if entry.Model == model {
			if offering, ok := firstOffering(entry, host); ok {
				return offering
			}
		}
	}
	t.Fatalf("missing offering for %s/%s", host, model)
	return agentkit.Offering{}
}

func vocabulary(t *testing.T, row string) []string {
	t.Helper()
	start := strings.Index(row, "={")
	if start < 0 || !strings.HasSuffix(row, "}") {
		t.Fatalf("row has no vocabulary: %q", row)
	}
	return strings.Split(row[start+2:len(row)-1], "|")
}

func stripMarkers(entries []string) []string {
	result := make([]string, len(entries))
	for i, entry := range entries {
		result[i] = strings.TrimPrefix(entry, "*")
	}
	return result
}

func expectedVocabulary(spec agentkit.ReasoningSpec) []string {
	var result []string
	if spec.Default.Mode == agentkit.ReasoningDefault {
		result = append(result, "dynamic")
	}
	switch spec.Kind {
	case agentkit.ReasoningKindEffort:
		for _, level := range spec.Levels {
			result = append(result, level.String())
		}
		if spec.CanDisable {
			result = append(result, "off")
		}
	case agentkit.ReasoningKindBudget:
		if spec.CanDisable {
			result = append(result, "off")
		}
		result = append(result, fmt.Sprintf("%d–%d", spec.MinBudget, spec.MaxBudget))
	case agentkit.ReasoningKindToggle:
		if spec.CanEnable {
			result = append(result, "on")
		}
		if spec.CanDisable {
			result = append(result, "off")
		}
	}
	return result
}

func defaultEntry(spec agentkit.ReasoningSpec) string {
	switch spec.Default.Mode {
	case agentkit.ReasoningDefault:
		return "dynamic"
	case agentkit.ReasoningOff:
		return "off"
	case agentkit.ReasoningOn:
		return "on"
	case agentkit.ReasoningEffort:
		return spec.Default.Effort.String()
	case agentkit.ReasoningBudget:
		return fmt.Sprintf("%d–%d", spec.MinBudget, spec.MaxBudget)
	default:
		return ""
	}
}
