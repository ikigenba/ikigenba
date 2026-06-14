package config

// The call-site registry (plan P2, eval-engine enablement obligation 1): the
// documented, single-source list of the TEN inference sites the eval harness
// (Part II) scores. It is a SUPERSET of the eight config-injected LLM sites in
// LLM (config.go):
//
//   - The eight config-injected sites (extract, match, merge, compile, ask, and
//     the three lint calls) each name their own injected-config triple
//     (Injected=true) and a callable entry point that resolves to a
//     config.CallSite. Going through the internal/llm wrapper with that triple is
//     what makes the site harness-callable (obligation 1).
//   - The canonical-name pick rides the dup-judge call's triple and entry point
//     (it is a field of the dup-judge output, design §6) — scoreable, no triple
//     of its own (Injected=false).
//   - The three retrieval lanes (candidates, search, sweep) are zero-LLM —
//     scoreable as retrieval quality, but carry no config-injected triple
//     (Injected=false).
//
// Sites are added to (filled out with their real entry point + landing phase) as
// their phases land; P2 establishes the registry and the convention so "every
// site is harness-callable" is a checklist by the end, not a retrofit. This list
// is the authoritative enumeration both the harness and the per-site liveness
// checks are checked against.

// Site is one entry in the call-site registry.
type Site struct {
	// Name is the stable site identifier; for an Injected site it matches the
	// config.CallSite.Name and the accounting log's call_site field.
	Name string
	// Injected reports whether the site carries its own config-injected
	// {prompt, model, effort} triple. The eight LLM sites are true; the
	// canonical-name pick and the three retrieval lanes are false.
	Injected bool
	// RidesOn names the Injected site whose triple/entry point this site shares
	// (empty unless this is a non-injected LLM-bearing site). canonical-name pick
	// rides "lint_dup_judge".
	RidesOn string
	// Phase is the plan phase that lands this site's real entry point.
	Phase string
	// Notes is a short description of what the site scores.
	Notes string
}

// Registry is the canonical ten-site enumeration (plan P2 / research §"inference
// inventory"). Order is stable for iteration and reporting.
var Registry = []Site{
	{Name: "extract", Injected: true, Phase: "P6a", Notes: "Extract subjects/claims from an inbox row."},
	{Name: "match", Injected: true, Phase: "P6b2", Notes: "Decide whether an extracted subject matches an existing one; emits the dup_pairs side channel."},
	{Name: "compile", Injected: true, Phase: "P8", Notes: "Compile a digest entry into per-claim cites + occurred_at."},
	{Name: "merge", Injected: true, Phase: "P7a2", Notes: "Merge a subject's claims into its page prose."},
	{Name: "lint_dup_judge", Injected: true, Phase: "P9a", Notes: "Judge a duplicate-flag pair: merge | dismiss | can't-tell-yet; also emits the canonical-name pick."},
	{Name: "canonical_name_pick", Injected: false, RidesOn: "lint_dup_judge", Phase: "P9a", Notes: "The surviving canonical name — a field of the dup-judge output (design §6), not a separate call."},
	{Name: "lint_fold", Injected: true, Phase: "P9a", Notes: "Fold/merge two pages during lint."},
	{Name: "lint_stale", Injected: true, Phase: "P9c", Notes: "Repair/annotate a stale note."},
	{Name: "ask", Injected: true, Phase: "P10", Notes: "Answer a question over the wiki with citations (RAG agent)."},
	{Name: "candidates", Injected: false, Phase: "P6b", Notes: "Retrieval lane — resolve candidate subjects (zero-LLM, FTS5)."},
	{Name: "search", Injected: false, Phase: "P10", Notes: "Retrieval lane — the search verb / ask's search tool (zero-LLM)."},
	{Name: "sweep", Injected: false, Phase: "P9b", Notes: "Retrieval lane — lint-sweep candidate retrieval (zero-LLM)."},
}

// InjectedSites returns the names of the eight config-injected LLM sites — the
// ones LLM must carry a triple for. The registry and the LLM struct cannot drift:
// the P2 test asserts these names exactly match LLM's fields.
func InjectedSites() []string {
	var out []string
	for _, s := range Registry {
		if s.Injected {
			out = append(out, s.Name)
		}
	}
	return out
}
