# Phase 32 — The git smart-HTTP door

*Realizes design Decision 19 (the git door). Depends on Phase 31.*

`internal/repos/` gains the `/git/` handler: path parsing and validation of
`<kind>/<name>.git`, the two authenticated paths (nginx-crossed → owner scope
from the injected headers; loopback → the run-token bearer, whose validation
arrives fully in Phase 33 and is stubbed to "no token configured → 401" until
then), and the `git http-backend` CGI invocation with the environment D19 lists
— including `REPOS_PUSH_SCOPE`, which the Phase 29 hook reads. Ref snapshots
before and after a `receive-pack` request publish one `push` event per branch
that moved.

Every test here drives the **real `git` client** against the shipped handler on
an `httptest` server: this phase is the one place the protocol claim can
actually be falsified.

**Done when:** the suite is green and these ids are each covered by a
clearly-named test —

- R-JZFR-1IPP — a real `git clone` retrieves the full history.
- R-K0NN-FAGE — a real `git push` of a branch lands the ref and a second clone
  can fetch it.
- R-K1VJ-T273 — a force-push to `main` over the owner path fails and `main` is
  unchanged.
- R-K33G-6TXS — a fast-forward push to `main` over the owner path succeeds.
- R-K4BC-KLOH — a two-branch push publishes exactly two `push` events; a refused
  push publishes none.
- R-K5J8-YDF6 — a force-push rewriting a non-`main` branch succeeds.
- R-K6R5-C55V — the `info/refs` advertisement is well-formed; unknown, invalid,
  and archived repositories are identically 404.
- R-K7Z1-PWWK — a crossed request with no `X-Owner-Id` is 401; a loopback
  request with no credential is 401 with a Basic challenge, and an anonymous
  real `git clone` fails.
