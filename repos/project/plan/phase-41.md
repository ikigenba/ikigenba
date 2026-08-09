# Phase 41 — Run-token lifetime honored verbatim: required `ttl`, no plane-side ceiling

*Realizes design Decision 20 (run tokens — reminted minting ids) and Decision
15 (env-channel conformance — reminted manifest id).*

The `POST /run-token` handler stops applying `REPOS_RUN_TOKEN_TTL`: the `ttl`
field becomes **required** and is honored verbatim (`expires_at =
Clock.Now().Add(requested)`), per the run-lifetime contract
(`root project/design/D26.md`). A missing, unparseable, zero, or negative
`ttl` is 400 and mints nothing. The `REPOS_RUN_TOKEN_TTL` env read, its
`ManifestExtras` declaration, and its `repos/etc/manifest.env` line are
deleted; `REPOS_MAX_COMMIT_BYTES` remains the only manifest knob. Tests
carrying the retired ids R-K96Y-3ON9, R-II0Q-VTSI, R-IJ8N-9LJ7, and
R-L9EG-DDWC are replaced by the new ids' tests; door-validation ids
(R-KAEU-HGDY, R-KBMQ-V84N, R-KCUN-8ZVC, R-KE2J-MRM1) are untouched.

**Done when:**

- R-36US-ASST — mint with `"ttl":"40m"` returns `expires_at` = injected now +
  exactly 40m (and `"3h"` → now + 3h; no cap, no default), `clone_url` from
  `registry.BaseURL("repos")`, stored `token_sha256` = sha256(token) lowercase
  hex, raw token in no column; unknown repository 404 — covered by a tagged
  test.
- R-382O-OKJI — `"ttl":"banana"`, `"-5m"`, `"0s"`, and the field omitted each
  return 400 with no `run_tokens` row — covered by a tagged test.
- R-39AL-2CA7 — resolved `REPOS_MAX_COMMIT_BYTES` equals the committed
  manifest's value; the committed manifest carries no key outside the Spec's
  declared set (in particular no `REPOS_RUN_TOKEN_TTL`); no
  `REPOS_RUN_TOKEN_TTL` read in non-test Go source — covered by a tagged test.
- `grep -rn "REPOS_RUN_TOKEN_TTL" --include='*.go' --exclude-dir=project .`
  from `repos/` returns no non-test hits, and `grep REPOS_RUN_TOKEN_TTL
  etc/manifest.env` returns nothing.
- The retired ids appear in no test file; the suite is green per design
  Conventions.
