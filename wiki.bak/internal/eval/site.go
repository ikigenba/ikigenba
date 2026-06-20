package eval

import (
	"context"
	"encoding/json"

	"wiki/internal/config"
)

// SiteAdapter is the rig's single seam onto a Part I call site. Each of the ten
// inference sites supplies one adapter; the runner is site-agnostic and drives
// every site through this interface. An adapter NEVER reimplements its site — it
// unmarshals the case input into the site's real input type, calls the REAL
// call-site function with the injected (prompt, model, effort) triple, and
// marshals the raw output back to JSON for the cache (the production-code-path
// principle). P13 ships the Match adapter; P14+ add the rest.
type SiteAdapter interface {
	// Name is the registry site name this adapter scores (e.g. "match").
	Name() string
	// Run invokes the real call site for one case input with the given triple,
	// returning the raw site output as JSON (the cache value's Raw). The triple's
	// prompt has already been resolved (from the bundle's prompt artifact, or the
	// site's config default). The captured cost/latency are the runner's job, not
	// the adapter's.
	Run(ctx context.Context, site config.CallSite, input json.RawMessage) (json.RawMessage, error)
}
