# Phase 33 — Run tokens

*Realizes design Decision 20 (run tokens). Depends on Phase 32.*

`internal/repos/` gains run-token minting and validation: `POST /run-token`
behind the loopback guard, returning a one-time token, an `expires_at` from the
injected clock plus `REPOS_RUN_TOKEN_TTL`, and a composed `clone_url`; storage
of the sha256 only; the door's credential resolution (401 unknown, 401 expired,
403 wrong repository) replacing Phase 32's stub; the `run` push scope; and the
periodic sweep Worker.

**Done when:** the suite is green and these ids are each covered by a
clearly-named test —

- R-K96Y-3ON9 — the minted token's `expires_at`, `clone_url`, and stored hash
  are correct and the raw token appears in no column; an unknown repository is
  404.
- R-KAEU-HGDY — a real `git clone` with a valid token succeeds; the same token
  against another repository is 403.
- R-KBMQ-V84N — an expired token is 401 at validation, with the sweep Worker not
  running.
- R-KCUN-8ZVC — a real `git push` of a branch succeeds under the run scope while
  a fast-forward push to `main` is refused.
- R-KE2J-MRM1 — `/run-token` 404s a crossed request, and the sweep deletes only
  expired rows.
