# Status token secrecy conflicts with verbatim remote errors

Filed during the user-invoked first `build-spec` run for `turns`, against design commit `47ac54fc548849f93ac7f97b28ccb190aba275d3`. Workflow's fresh install/status verifier reported the potential conflict. A fresh root validator independently constructed and executed two synthetic API and command proofs.

## Requirements and friction

`R-Q6Y5-62LZ` forbids the stored token's text in status stdout or stderr. `R-AR3G-G9BX` and `R-ASBC-U12M` require status to propagate the space error's detail, unmodified or line-quoted as specified. The space client requirements `R-B6P7-ZTDQ` and `R-BACX-54LT` require that detail to preserve a transport error or an HTTP error body. `R-UKOM-76KH` requires `seam.QuoteOutput` to preserve other bytes when line-quoting, so quoting cannot redact the token either.

An injected transport error or HTTP failure body can echo the synthetic stored token. Exact diagnostic propagation then discloses it. Redacting the detail satisfies secrecy but changes the diagnostic text required by the propagation requirements. The conjunction is unsatisfiable for an allowed response; this is not a reason to classify the space client or its propagation requirements as independently unprovable.

## Independent executed proof

The root validator used a temporary overlay test outside the repository, with all files created using `mktemp "${TMPDIR:-/tmp}/scratch.XXXXXX"`. The test calls `space.New(...).Latest(...)` and `cli.Run` in process, using temporary synthetic session/config/token files, an injected ordinary-user uid, environment and clock, and a fake HTTP transport. No real credential, home, environment, session file, network or external process was used. The token below is a deliberately synthetic fixture.

From `turns/`:

```sh
GOPROXY=off GOSUMDB=off go test -overlay /tmp/scratch.FOPf28 ./internal/cli -run '^TestFreshStatusEchoedTokenProof$' -count=1 -v
```

The proof exited zero, asserting the contradictory observations rather than compliance. With `synthetic-token-echo-proof-only` stored as the token:

- A fake transport returning `errors.New(token)` produced an unquoted space detail containing the token and status stderr containing that same detail.
- A fake HTTP 401 response body `echo: <token>\nrefused\n` produced quoted status detail containing the token.

Both cases reported:

```text
exact_required_diagnostic=true stored_token_on_stdout=false stored_token_on_stderr=true redacted_contains_token=false redacted_equals_required=false
```

The HTTP 401 case produced these stderr bytes in a quoted representation:

```text
"turns: could not read the space: the space refused the token\n\n> echo: synthetic-token-echo-proof-only\n> refused\n"
```

## Why the build cannot resolve it

The builder cannot redact required verbatim detail, discard an allowed error, or weaken the secrecy guarantee without changing the read-only contract. `R-Q6Y5-62LZ` remains open and its coverage claim is removed. The independently satisfied error construction and propagation requirements remain covered. Work depending on the conflicting secrecy guarantee is halted; the retained implementation must not be treated as satisfying it.

## Suggested resolution

Decide outside this build how errors must be sanitized before display. Replace the affected contract with newly minted ids that permit and define token redaction consistently across space error handling and status diagnostics, while retaining the desired secrecy guarantee. Resolve and delete this issue, then start a new `build-spec` run to recompute the gap.
