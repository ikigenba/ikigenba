package consumer

import "path"

// Subscription declares one thing this consumer listens to. Filter is a glob
// (stdlib path.Match) over the dotted event type; Handler runs the effect for a
// match while the engine commits the cursor for every event regardless (§7.3).
// The handler matches against its declared Subscription instead of a hardcoded
// literal, so the runtime filter and what reflection reports cannot drift.
//
// Caveat: event types are dotted with no slashes, so path.Match treats them as a
// flat glob — "contact.*" matches "contact.created", and "*" matches every type
// (a "*" spans dots, since there is no separator).
type Subscription struct {
	Source      string  // upstream service name, e.g. "crm" (must be ⊆ Spec.Consumes)
	Filter      string  // glob: "contact.created", "contact.*", "*"
	Description string  // what this service does in reaction
	Handler     Handler // effect; omitted from reflection output
}

// Match reports whether eventType matches the subscription's Filter. A
// malformed pattern (path.ErrBadPattern) never matches.
func (s Subscription) Match(eventType string) bool {
	ok, err := path.Match(s.Filter, eventType)
	return err == nil && ok
}
