# Phase 09 — Consumer loops through `Spec.Consumers`

*Realizes design Decision 11 (the chassis-owned consumer declaration) and D9's
narrowed form (peer feed addresses leave notify). Depends on no earlier notify
phase; depends on the appkit chassis providing `Spec.Consumers` (appkit plan
Phase 10), consumed through the committed `replace appkit => ../appkit` as a
fixed external contract.*

Observable end state:

- `cmd/notify/main.go` declares `Consumers` with exactly two entries (`crm`
  with `push.Subscription()`, `prompts` with `push.PromptsSubscriptions()`),
  each handler factory building its own ntfy `push.Client` over the fail-loud
  ntfy config resolution; `runConsumer`, `runPromptsConsumer`, the `Workers`
  field, the `var rt *appkit.Router` capture, and the legacy
  `Consumes`/`Subscriptions` Spec fields are gone.
- The config struct carries only the ntfy remnant (base/topic/token): the
  `feedURL`/`promptsFeedURL`/`from` members and their
  `CRM_FEED_URL`/`PROMPTS_FEED_URL`/`NOTIFY_FROM` reads are deleted, along with
  the tests that pinned them (R-RGCF-4B2L, R-RGPF-4C3M, R-RGEO-4D4N retire with
  their behavior — it is chassis-owned and pinned by appkit's D10 ids).
- `notify/.envrc` no longer exports `CRM_FEED_URL` or `NOTIFY_FROM`.
- `bin/start`'s `launch_notify` swaps its legacy `CRM_FEED_URL`/
  `PROMPTS_FEED_URL` exports for the generic `<APP>_<SRC>_FEED_URL` form (the
  D11 boundary-crossing lines, verified by the live smoke, not Go tests).

**Done when:** the suite is green — `cd notify && go build ./...`,
`cd notify && go vet ./...`, `cd notify && gofmt -l .` (no output), and
`cd notify && go test ./...` all succeed with zero failures — and:

- R-4DG9-3Q97 and R-4EO5-HHZW (D11) are covered by clearly-named tests;
- the manifest byte-equality test still passes with `CONSUMES=crm,prompts`
  unchanged (R-RGDR-4F6Q remains covered);
- `grep -rn "CRM_FEED_URL\|PROMPTS_FEED_URL\|NOTIFY_FROM" notify --include=*.go`
  returns no matches, and
  `grep -n "CRM_FEED_URL\|NOTIFY_FROM" notify/.envrc` returns no matches;
- `grep -n "runConsumer\|runPromptsConsumer\|Workers:" notify/cmd/notify/main.go`
  returns no matches;
- `grep -rn "R-RGCF-4B2L\|R-RGPF-4C3M\|R-RGEO-4D4N" notify --include=*.go`
  returns no matches (the retired ids' tests are deleted with them).
