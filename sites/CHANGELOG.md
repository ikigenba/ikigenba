# Changelog

## v0.34.0 — 2026-08-19

- New `file_delete` and `rmdir` tools remove a single file or a whole directory subtree from a site in one operation.
- A directory URL requested without a trailing slash now redirects to a relative location (`blog/` rather than an absolute `/public/<slug>/blog/`), so the redirect survives serving behind a custom domain or path prefix.
- Writing or editing a file onto a path that is already a directory is now refused with a clear validation error instead of failing confusingly; use `rmdir` to remove the directory first.
- Publishing through the authoritative doors (`set_path`, repos push, Dropbox `sync`) now safely handles a path changing between a file and a directory, instead of erroring mid-reconcile and leaving partial state.
- Dropbox `sync` now skips directory entries in the mirror listing, so importing a folder with subfolders no longer produces spurious fetch failures.

## v0.33.0 — 2026-08-17

- Adopt the per-service customer-data and dev-config env manifests: `env.list` now authors the shipped `manifest.env`. Redeployed to verify manifest and secret handling end to end. No API, schema, or data changes.

## v0.32.0 — 2026-08-15

- Adopted the suite-wide LLM-lint semantic gate (D31); the existing test suite
  already satisfied it, so no code or test changes were required. Bumped as part
  of the suite-wide gate-adoption release. No user-facing behavior, API, schema,
  or data changes; the shipped binary is functionally unchanged.

## v0.31.0 — 2026-08-15

- Internal code-quality hardening: the service now conforms to the suite's strict mechanical lint tier (formatting, complexity, and style rules enforced by the shared lint gate). No user-facing behavior, API, or data changes.

## v0.30.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.
- This covers the sites service's own page only. Websites you host through sites are untouched: their markup is yours, and a site with no icon of its own still has none.

## v0.29.0 — 2026-08-09

- baseline; changes before this version are recorded only in git history
