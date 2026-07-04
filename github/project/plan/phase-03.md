# Phase 3 — The typed GitHub REST v3 client

*Realizes design Decision 3 (the typed REST v3 client). Depends on Phase 2 (the
`tokenSource` supplies the bearer for every call).*

## What gets built

`internal/gh/client.go` (+ `client_test.go`) and the response types: the `Client`
with the fourteen org-scoped methods in D3's endpoint map — `ReposList`,
`RepoGet`, `PRList`, `PRGet` (PR + changed files), `PRComment`, `PRReview`,
`PRMerge`, `IssueList` (PRs excluded), `IssueGet`, `IssueCreate`, `IssueComment`,
`IssueUpdate`, `FileGet` (base64-decoded), `FilePut` (message + base64 content +
optional sha, **no** author/committer) — plus the typed errors `ErrNotFound`
(404) and `ErrInvalid` (422) and the generic status wrap. Every request sets the
installation-token bearer (Phase 2), `Accept: application/vnd.github+json`, and
`X-GitHub-Api-Version: 2022-11-28`.

All methods are exercised offline against an injected `http.RoundTripper` stub
that asserts the outbound request (method, path, query, body) and returns canned
GitHub payloads for decode. Write-path tests additionally assert the request body
carries **no** owner-identifying field.

Observable end state: each verb constructs the correct request and decodes the
response into its typed result; status codes map to typed errors.

## Done when

All hold on identical repo state, from `github/`:

- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0; `gofmt -l .`
  empty; `go vet ./...` clean.
- A clearly-named offline test covers and passes for **each** of the fifteen ids
  `R-DVE4-ETB5`, `R-DWM0-SL1U`, `R-DXTX-6CSJ`, `R-DZ1T-K4J8`, `R-E09P-XW9X`,
  `R-E1HM-BO0M`, `R-E2PI-PFRB`, `R-E3XF-37I0`, `R-E55B-GZ8P`, `R-E6D7-UQZE`,
  `R-E7L4-8IQ3`, `R-EA0X-027H`, `R-EB8T-DTY6`, `R-ECGP-RLOV`, `R-D0IM-VQ7H` — the id
  appears in the test name/comment, the assertion pins the discriminating property
  named in the Decision (endpoint/query/body/decoded value/error mapping), and the
  suite is green.
- The write-path tests (`R-E09P-XW9X`, `R-E1HM-BO0M`, `R-E6D7-UQZE`, `R-E7L4-8IQ3`,
  `R-EA0X-027H`, `R-ECGP-RLOV`) assert the outbound JSON body contains no
  owner-identifying key (no `author`/`committer` on `FilePut`).
