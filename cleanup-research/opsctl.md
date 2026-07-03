# opsctl — cleanup findings

## High-priority (named migrations)
- opsctl/project/research/deploy-nginx-fragment-research.md:45 — states as present fact "`bin/ship` scps **only the single static binary** to `/tmp`" — the OLD flat-bin ship model. Ship now bundles a versioned tar.gz (binary + nginx.conf + manifest.env + share/) per deploy.md.
- opsctl/project/research/deploy-nginx-fragment-research.md:44 — "The box keeps no fragment source. `/opt/<app>/etc/` holds only `manifest.env`." Stale under the versioned-slot model: the bundle now carries `nginx.conf` and deploy reloads the fragment through `etc/current`.
- opsctl/project/research/deploy-nginx-fragment-research.md:14-20,34-38 — present-tense claim that "`opsctl deploy` swaps the binary + regenerates `manifest.env` but **never touches nginx**" / "Zero nginx calls." Contradicts current deploy (three-symlink swap + nginx fragment reload through `etc/current`; apex re-render for DEFAULT app). NOTE: this is a dated (2026-06-30) research note describing the pre-change problem it motivated; the design it fed (D04) shipped, so the "Today…" framing is now historical, not current.

## Other stale info
- opsctl/cmd/opsctl/main.go:79 — rollback help synopsis `opsctl rollback <app> [version]`; explicit target version is no longer supported (rollback.go:100 rejects it: "use -N snapshot recency"). Should read `[-N]`. (superseded verb args)
- opsctl/cmd/opsctl/main.go:278 — same stale `usage: opsctl rollback <app> [version]` error string; recovery model is now S3-snapshot recency (`-N`), not explicit version. (superseded verb args)
- opsctl/project/README.md:15 — "End-user documentation for this service lives in `opsctl/docs/`" but no `opsctl/docs/` directory exists. (dead path)

## Notes
- Registry migration: opsctl code references to `registry`/`bin/registry` (initbox.go:16, templates.go:37, manifest.go:10, setup.go:19/279, deploy.go:420) all treat the top-level registry as source of truth for ports/inventory — these are CURRENT, not stale.
- `convert.go` and the `convert` verb are legacy-layout migration tooling that is intentionally present (a live feature), not stale docs — did not flag.
- Design docs (D02 stage, INDEX) and main.go package comment correctly describe the versioned release-dir + three-symlink tar.gz model — current.
</content>
</invoke>
