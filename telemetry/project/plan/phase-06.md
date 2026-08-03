# Phase 6 — The shipped nginx location fragment and its drift guards

*Realizes design Decision 6 (nginx location fragment). Depends on Phase 05.*

`telemetry/etc/nginx.conf` is authored as a pure location fragment — shipped
verbatim in the deploy bundle, symlinked live by `opsctl setup`, and therefore
directly loadable by nginx with no substitution step. It carries exactly the
three tiers D6 states:

- an exact-match ungated PRM location that clears `X-Correlation-Id`;
- an exact-match `auth_request /_authn`-gated `/srv/telemetry/mcp` that sets
  each of the five identity headers exactly once from the auth subrequest,
  captures the introspection-minted correlation id via `auth_request_set` and
  overwrites `X-Correlation-Id` with it, and carries the rate-limit fidelity
  shim through a named `@telemetry_authn_500` location;
- a prefix catch-all `return 404;`.

No `/ingest` location, no `/feed` location, no session-gated mount root, no
`/static/` tier.

The guard tests live in `cmd/telemetry` alongside the composition root (the
sibling-service idiom) and parse the committed file — they do not stand up
nginx, which is the repo-root front door's concern.

**Done when:**

- Every id below is covered by a clearly-named, id-tagged test:
  - R-W60I-CYGD — the file contains no `server`/`listen`/`ssl_certificate`/
    `http` block, every `proxy_pass` targets `127.0.0.1:registry.MustPort("telemetry")`,
    and every `location` path is under `/srv/telemetry/`.
  - R-W78E-QQ72 — the gated MCP location is exact-match, carries
    `auth_request /_authn`, and sets each of the five identity headers exactly
    once from an `auth_request_set` variable.
  - R-W8GB-4HXR — the file contains the substring `ingest` zero times, and the
    only prefix `location /srv/telemetry/` is a `return 404;` with no
    `proxy_pass`.
  - R-W9O7-I9OG — the PRM location is exact-match, has no `auth_request`, and
    proxies to the upstream `/.well-known/oauth-protected-resource`.
  - R-WAW3-W1F5 — the gated location captures the correlation id via
    `auth_request_set` and sets `X-Correlation-Id` from that variable, while
    the PRM location sets it to the empty string.
- `grep -c ingest telemetry/etc/nginx.conf` outputs `0`.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  `go test ./...` all exit 0 in `telemetry/`.
