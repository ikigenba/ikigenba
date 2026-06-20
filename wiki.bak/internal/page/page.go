// Package page is the knowledge layer's registry primitives (design §4.1): the
// subjects + aliases row types and the pure `normalize` function the resolution
// step (P6b) keys on. Pages themselves (the prose bodies + FTS5) and the
// resolution lookup land in later phases; P6a establishes only the registry
// shapes and the normalization rule every alias key is built through.
//
// `normalize` is the load-bearing primitive: the extracted name is a lookup key,
// never an address (design §4.3). Per subject the resolver builds the key set
// normalize(name) ∪ normalize(aliases) and queries aliases by it. Because the
// rule is pure, versioned, and rebuildable, the whole alias index can be rebuilt
// from the original names if the rule ever changes — nothing in the database
// depends on a normalization that can't be reproduced from source.
package page

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Type is the closed three-type subject taxonomy (design §4.1). It mirrors the
// integrate.SubjectType values; the registry stores it on both subjects.type and
// aliases.type (the latter is required by the UNIQUE(type, norm) lookup key).
type Type = string

const (
	TypeEntity  Type = "entity"
	TypeEvent   Type = "event"
	TypeConcept Type = "concept"
)

// Subject is one row of the registry's `subjects` table (design §4.1): every
// subject ever minted. Pages hang off Subject by id; aliases point back to it.
type Subject struct {
	// ID is the subject's ULID — the durable address resolution returns.
	ID string
	// Type is the closed-set type (entity | event | concept).
	Type Type
	// Kind is the freeform, prompt-anchored subtype (e.g. "person", "org").
	Kind string
	// CanonicalName is the chosen display name (lint may rewrite it on merge).
	CanonicalName string
	// CreatedByRun is the run id that minted this subject (→ runs.id).
	CreatedByRun string
	// OccurredAt is the nullable ISO-8601 prefix world-time, type=event only,
	// first-writer-wins. Empty for non-event subjects.
	OccurredAt string
}

// Alias is one row of the registry's `aliases` table (design §4.3): every name a
// subject has been known by, stored as its normalized key. UNIQUE(type, norm) is
// both the duplicate-mint guard and the per-name lookup key.
type Alias struct {
	// Type is the subject's type, denormalized onto the alias for the lookup +
	// the UNIQUE constraint.
	Type Type
	// Norm is normalize(name) — the key resolution matches against.
	Norm string
	// SubjectID is the subject this alias resolves to (→ subjects.id).
	SubjectID string
}

// normalizer strips combining marks (diacritics) after NFKD decomposition. It is
// built once (it is stateless) and reused; the transform.Chain is not safe for
// concurrent use, so Normalize constructs a fresh string per call via
// transform.String rather than sharing a Transformer.
var stripMarks = runes.Remove(runes.In(unicode.Mn))

// Normalize maps a surface name to its alias key (design §4.3): NFKC fold,
// casefold (lowercase), trim, collapse internal whitespace to single spaces, and
// strip diacritics. The rule is pure and deterministic — the same input always
// yields the same key — so the alias index is rebuildable from original names.
//
// Order matters: decompose (NFKD) so diacritic marks become separable, strip the
// marks, then recompose to NFKC for compatibility folding (e.g. ﬁ → fi, full-width
// → half-width), lowercase, and finally collapse whitespace. Casefolding via
// strings.ToLower is sufficient for the lookup-key purpose here.
func Normalize(s string) string {
	// Decompose, strip combining marks, recompose under NFKC.
	t := transform.Chain(norm.NFD, stripMarks, norm.NFKC)
	out, _, err := transform.String(t, s)
	if err != nil {
		// transform.String only errors on a transformer that rejects input;
		// these never do, so fall back to the raw string rather than panic.
		out = s
	}
	out = strings.ToLower(out)
	out = collapseWhitespace(out)
	return out
}

// collapseWhitespace trims leading/trailing whitespace and collapses every run of
// internal Unicode whitespace to a single ASCII space.
func collapseWhitespace(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// KeySet builds the deduplicated normalized key set for a subject's surface forms
// (design §4.3: normalize(name) ∪ normalize(aliases)). Empty keys are dropped so
// a blank alias never collapses distinct subjects. Order is stable (insertion
// order of first appearance) for deterministic queries and tests.
func KeySet(name string, aliases []string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(raw string) {
		k := Normalize(raw)
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	add(name)
	for _, a := range aliases {
		add(a)
	}
	return out
}
