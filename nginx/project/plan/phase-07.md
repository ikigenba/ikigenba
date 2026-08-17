# Phase 7 — The committed `michaelgreenly.dev` vhost server file

*Realizes design Decision 5 (the `michaelgreenly.dev` vhost).*

Create the one committed artifact D5 owns: `michaelgreenly.dev/nginx.conf` (from
the tree root `nginx/`), containing exactly the two-block server file in D5's
Decision — the `:80` block with the shared-webroot ACME location and the HTTPS
redirect, and the `:443` block with the domain's own certificate lineage paths,
HSTS, and the `location /` proxy onto the sites public tier for the
`michaelgreenly-dev` slug. Committed source, not a template: no placeholder, no
per-box substitution. Nothing else changes — no edit to `nginx.conf`, `run`,
the `parked/` files, or any generated fragment. Installation is not this
phase's work; it is the operator runbook step D5 cites into the repo-root
`deploy.md`.

**Done when** (run from the tree root `nginx/`):

- `test -f michaelgreenly.dev/nginx.conf` exits 0.
- `grep -c 'server_name michaelgreenly.dev;' michaelgreenly.dev/nginx.conf`
  prints exactly `2` (one per block).
- `grep -c 'proxy_pass http://127.0.0.1:3004/public/michaelgreenly-dev/;' michaelgreenly.dev/nginx.conf`
  prints exactly `1`.
- `grep -c '/etc/letsencrypt/live/michaelgreenly.dev/' michaelgreenly.dev/nginx.conf`
  prints exactly `2` (certificate and key).
- `grep -c 'default_server' michaelgreenly.dev/nginx.conf` prints exactly `0`
  (selection is by name; D3's catch-all is untouched).
- The tree is green per Conventions: `bash -n run` exits 0 and
  `mkdir -p tmp && nginx -p . -c nginx.conf -t` exits 0 (the new file is a
  server-context fragment outside the dev front door's config, so the dev gate
  must be unaffected).
