# Phase 2 — The GitHub App installation-token source

*Realizes design Decision 2 (installation-token source), offline ids only.
Depends on Phase 1 (the module skeleton and `internal/gh` package home). The one
live-substrate id of D2, `R-DMUT-QF4A`, is not scheduled as loop work by any
phase — it is verified out of loop by an operator per
`project/github-verification.md`. It is not part of this phase's done-bar.*

## What gets built

`internal/gh/token.go` (+ `token_test.go`): the `tokenSource` that turns the
GitHub App credentials into a cached installation token, and the RS256 app-JWT
signing, all stdlib. Constructed from config (`appID`, `org`, parsed
`*rsa.PrivateKey`, `*http.Client`) passed in from the composition root — it reads
no environment itself. The `apiBase` host is a package `var` so tests redirect it
to a stub server / `RoundTripper`.

Behavior (see D2): mint an RS256 app JWT (`iss`=app id, `iat` backdated 60s, `exp`
≤ 600s); resolve the org installation via `GET /orgs/{org}/installation`; mint the
installation token via `POST /app/installations/{id}/access_tokens`; cache it
against GitHub's `expires_at`; refresh within a 60s slack; force-refresh once on a
401; and fail loudly with `ErrAppAuth` (no retry loop, no key in the message) when
the app credentials are rejected.

The `.envrc` for local runs exports `IKIGENBA_APP_ID`, `IKIGENBA_GITHUB_ORG`, and
`IKIGENBA_APP_PRIVATE_KEY` (already seeded Phase 1); the composition root parses
the PEM once and constructs the source. No live network in the unit suite — a stub
`RoundTripper` returns canned installation/token responses.

Observable end state: given valid config, the source yields a non-empty
installation token and reuses/refreshes it correctly; given rejected credentials
it returns `ErrAppAuth`.

## Done when

All hold on identical repo state, from `github/`:

- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0; `gofmt -l .`
  empty; `go vet ./...` clean.
- Named offline tests cover and pass for `R-DLMX-CNDL` (JWT alg/claims verify
  against the derived public key), `R-DO2Q-46UZ` (the installation token, not the
  JWT, is the REST bearer), `R-DPAM-HYLO` (reuse vs. re-mint across the slack
  boundary using an injected clock), `R-DQII-VQCD` (one retry on 401, bounded), and
  `R-DRQF-9I32` (`ErrAppAuth`, no retry, no key leak) — each id name appears in a
  test name/comment and the suite is green.
The offline suite here covers the faked-auth path only. The live end-to-end proof
of the credentials (`R-DMUT-QF4A`) is verified out of loop per
`project/github-verification.md`, and is **not** part of this phase's done-bar.
