# Phase 5 — The loopback GET /pr twin for scripts

*Realizes design Decision 5 (the loopback `GET /pr` twin). Depends on Phase 3 (the
`Client.PRGet` the route builds on).*

## What gets built

`internal/gh/pr_route.go` (+ `pr_route_test.go`): `Client.PRHandler()` returning
the `GET /pr` handler, and its registration in `internal/githubapp/spec.go`
`Handlers` mounted **without** `RequireIdentity` (verbatim, like dropbox's
`/content`).

Contract (D5): `GET /pr?repo=<name>&number=<n>` on loopback (no identity headers)
→ `200 application/json` with the `pr_get` PR shape via `Client.PRGet`; any request
carrying `X-Owner-Email` or `X-Forwarded-Proto` → `404` (self-guard against the
public front door); missing/malformed `repo`/`number` → `400`; a not-found PR
(`ErrNotFound`) → `404`; no `500` or panic for client/input conditions.

Tests drive the handler with `httptest` and a stubbed transport for `PRGet`.

Observable end state: a loopback caller (scripts) fetches a PR by repo+number;
a request bearing nginx identity headers is refused.

## Done when

All hold on identical repo state, from `github/`:

- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0; `gofmt -l .`
  empty; `go vet ./...` clean.
- Clearly-named offline tests cover and pass for `R-EPVL-Z2UI` (loopback, no
  identity headers → 200 + PR JSON matching the `pr_get` shape), `R-ER3I-CUL7` (a
  request with `X-Owner-Email` **and** one with `X-Forwarded-Proto` each → 404, no
  PR body), and `R-ETJB-4E2L` (bad/missing params → 400; `ErrNotFound` → 404; no
  500/panic) — each id named in a test.
