# bug: a custom-domain site 404'd after `set_path` to a non-empty subfolder

**Observed.** On the live custom-domain site `michaelgreenly.dev`, calling
`set_path` to point at a non-empty subfolder returned success (the row showed the
new `path`), but the custom-domain homepage and `/c/` then `404`'d even though the
served copy contained a byte-correct `index.html`. Recovering with
`set_path(path:"")` restored serving. Other sites use a non-empty publish root
fine, so the failure looked specific to how the **custom-domain proxy** reaches
the served directory after a subfolder `set_path`.

**What this session already fixed.** The reconcile half of the symptom — a
`set_path`/push reconcile hitting `open …/qr: is a directory` and leaving partial
state on a file/directory type change — is addressed by Decision 42 (type-safe
in-place reconcile, Phase 63) and Decision 38's citation of it. Once that lands,
`set_path`'s re-materialization is type-safe and atomic-enough that a partial
reconcile no longer explains a post-`set_path` `404`.

**Residual, out of `sites/project/` scope.** If serving still breaks after a
subfolder `set_path` once D42 is built, the remaining cause is the
**custom-domain proxy configuration** (how `michaelgreenly.dev` is mapped onto
`/public/<slug>/…`) — nginx config living in the `nginx` (and/or dashboard) tree,
not in the sites service. sites serves the correct bytes at
`SiteDir(v, slug)`; how a non-`/srv/sites` front door routes to it is that tree's
concern. A `sites/project/` Decision cannot reach it.

**Suggested next step (outside this session).** After D42 is built, re-test a
subfolder `set_path` on a custom-domain site directly against the sites process
(`/public/<slug>/`) to confirm sites serves it, then inspect the custom-domain
proxy config in the nginx tree for how it maps the domain to the served path.
