package consumer

// Subscription declares one thing this consumer listens to. Filter is a routing
// glob over the canonical routing key; Handler runs the effect for a match while
// the engine commits the cursor for every event regardless (§7.3).
// The handler matches against its declared Subscription instead of a hardcoded
// literal, so the runtime filter and what reflection reports cannot drift.
type Subscription struct {
	Source      string  // upstream service name, e.g. "crm" (must be ⊆ Spec.Consumes)
	Filter      string  // canonical-key glob, e.g. "dropbox:create/bills/**/*.pdf"
	Description string  // what this service does in reaction
	Handler     Handler // effect; omitted from reflection output
}
