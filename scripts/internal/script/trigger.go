package script

import (
	"fmt"
	"sort"
	"strings"

	"eventplane/outbox"
)

// knownFamilies is the static catalogue of event kinds scripts consumes. Event
// subjects deliberately remain open: producers own their subject vocabulary.
var knownFamilies = map[string][]string{
	"cron":    {"tick"},
	"crm":     {"contact.created", "contact.updated", "contact.tagged", "contact.untagged"},
	"ledger":  {"recorded"},
	"dropbox": {"create", "modify", "delete"},
	"prompts": {"run.succeeded", "run.failed"},
}

func triggerSources() []string {
	out := make([]string, 0, len(knownFamilies))
	for source := range knownFamilies {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

// validateTrigger accepts a canonical routing-key filter. Its source segment
// must be a literal source because consumption is wired one upstream at a time.
func validateTrigger(filter string) (string, error) {
	i := strings.IndexByte(filter, ':')
	if i < 1 {
		return "", fmt.Errorf("%w: trigger filter must begin with a literal source followed by :", ErrValidation)
	}
	source := filter[:i]
	if strings.ContainsAny(source, "*?[") {
		return "", fmt.Errorf("%w: trigger source %q must be literal", ErrValidation, source)
	}
	kinds, ok := knownFamilies[source]
	if !ok {
		return "", fmt.Errorf("%w: unknown trigger source %q (known: %v)", ErrValidation, source, triggerSources())
	}
	registry := make(outbox.Registry, 0, len(kinds))
	for _, kind := range kinds {
		registry = append(registry, outbox.Family{Kind: kind})
	}
	ok, err := registry.CouldMatch(source, filter)
	if err != nil {
		return "", fmt.Errorf("%w: invalid trigger filter %q: %v", ErrValidation, filter, err)
	}
	if !ok {
		return "", fmt.Errorf("%w: trigger filter %q matches no %s event family", ErrValidation, filter, source)
	}
	return source, nil
}
