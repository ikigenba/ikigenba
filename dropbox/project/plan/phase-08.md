# Phase 8 — Prove no loopback-port literal survives, and guard the deploy artifacts against registry drift

*Realizes design Decision 10 (source-scan guard + deploy-artifact drift guard).
Depends on phase 7 (the `registry` adoption must already be wired, and `go.mod`
must already require `registry`). Covers `R-QLO8-2HE3`, `R-QMW4-G94S`. **Read D10
for the exact guard shapes and rationale.***

Phase 7 removed dropbox's Go-source loopback literals (the `events.go` example
origin and the two own-port int defaults); this phase **enforces** that they stay
gone and turns the two static deploy artifacts that must still carry the literal
port (`etc/manifest.env`, `etc/nginx.conf`) into drift-caught, not drift-prone —
their dropbox tests now derive the expected port from `registry`.

**What gets changed (tests only — all inside `dropbox/`):**

- **Source-scan guard (`R-QLO8-2HE3`)** — add a genuinely-asserting test (a small
  guard file under `cmd/dropbox` or a dedicated `internal/` guard package) tagged
  `// R-QLO8-2HE3` that:
  - walks every `*.go` file under the `dropbox/` module root and fails if any
    file's source contains a bare loopback-address literal of the form
    `127.0.0.1:30` + two digits;
  - **assembles the forbidden needle at runtime** (e.g. `"127.0.0.1:" + "30"`)
    rather than embedding the full literal, and **skips its own filename**, so the
    guard can never match itself;
  - passes cleanly after phase 7 + the nginx-test repoint below (zero such
    literals remain) and would go red if a hardcoded loopback URL like
    `"http://127.0.0.1:3200"` were reintroduced.
- **Manifest drift guard (`R-QMW4-G94S`, part 1)** — in
  `dropbox/cmd/dropbox/main_test.go`, change `TestManifestLibraryByteEqualsCommittedFile`'s
  emitted field from `Port: 3200` to `Port: registry.MustPort("dropbox")`. Keep
  every other assertion and every other `Fields` value unchanged (`Feed:"/feed"`,
  the `OUTBOX_RETENTION_*` extras); the emitted `PORT=3200` still byte-matches the
  committed `etc/manifest.env` today. This test carries existing id R-8IAN-FB87;
  keep that tag and add `// R-QMW4-G94S`.
- **nginx drift guard (`R-QMW4-G94S`, part 2)** — in
  `dropbox/internal/web/nginx_test.go`, replace the hardcoded
  `proxy_pass http://127.0.0.1:3200/` and `proxy_pass http://127.0.0.1:3200/static/`
  assertions with ones built from `registry.BaseURL("dropbox")`: assert the
  fragment contains `"proxy_pass " + registry.BaseURL("dropbox") + "/"` and
  `"proxy_pass " + registry.BaseURL("dropbox") + "/static/"`. Keep the exact-match
  vs prefix distinction, the `auth_request /_session-authn` gate assertion, the
  session-owner propagation checks, and the PRM / bearer-prefix /
  `= /srv/dropbox/content` 404 survival checks exactly as they are — only the port
  value they compare against becomes a `registry` call. (`internal/web` needs
  `registry` in scope; the module already requires it from phase 7. This file
  relocates to `cmd/dropbox` unchanged in phase 9.)
- Touch nothing else. Do **not** edit `etc/manifest.env` or `etc/nginx.conf`
  themselves — their literal `3200` stays; these tests now police it. **No schema
  change — no migration.**

**Done when:**

- R-QLO8-2HE3 — a guard test walks dropbox's `*.go` files (skipping itself, needle
  assembled at runtime) and asserts no bare `127.0.0.1:30xx` loopback-address
  literal remains; it is green after phase 7 + the nginx-test repoint and goes red
  if one is reintroduced.
- R-QMW4-G94S — the manifest byte-equality test emits with
  `registry.MustPort("dropbox")` and matches the committed `etc/manifest.env`, and
  the nginx tests assert the fragment's `proxy_pass` targets against
  `registry.BaseURL("dropbox")`; a `registry` value differing from the committed
  `3200` would fail them (the intended drift alarm). This also delivers the
  executable proof for R-QJ8F-AXWP (phase 7).
- No bare `127.0.0.1:30xx` string literal and no `Port: 3200` integer literal
  remain in dropbox's Go source.
- The suite is green: `cd dropbox && go build ./...`, `cd dropbox && go vet ./...`,
  `cd dropbox && gofmt -l .` (prints nothing), `cd dropbox && go test ./...`, and
  `bin/check-migrations dropbox`.
