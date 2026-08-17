# Phase 26 — Purge the retired `.envrc` / `~/.secrets` references from notify's Go source

*Realizes design Decision 11 (consumer loops / env wiring — the dev-secret
prose it now leaves accurate); structural, docs/prose only, no new behavior and
no design ids.*

The suite-wide `.envrc` → keyring + `etc/env.list` migration retired every
`notify/.envrc` and every `~/.secrets/<NAME>` read, but three references to the
dead mechanism still linger in `cmd/notify/main.go`, describing how notify's two
ntfy secrets (`NTFY_TOPIC`, `NTFY_API_KEY`) reach the process:

- the `ntfyCfg` doc comment (`… injected via the environment (.envrc locally;
  app-config in prod).`), and
- the two `mustNtfyCfg` fail-loud error strings
  (`… inject via .envrc from ~/.secrets/NTFY_TOPIC` /
  `… inject via .envrc from ~/.secrets/NTFY_API_KEY`).

Rewrite the three strings so they name the **real** secret sources: in local
dev the value is resolved from the keyring through `etc/env.list` and injected
into the environment by the launcher; in prod it is app-config. `NTFY_TOPIC`
and `NTFY_API_KEY` stay ordinary `secret` env.list keys (channel 1) — this is a
wording correction, **not** a channel-5 / `state/` change.

Behavior is unchanged: both secrets remain required, and an absent one still
fails loudly at the composition root naming the missing variable. Only the human
text of the comment and the two error messages changes; the config-resolution
logic, the required-ness, and the fail-loud contract are untouched.

**Done when** (from `notify/`, the loop's working directory):
- The suite is green: `go build ./...`, `go vet ./...`, `gofmt -l .` (no
  output), and `go test ./...` all succeed with zero failures.
- `grep -rniE '\.envrc|\.secrets' --exclude-dir=project .` prints nothing — the
  retired-mechanism references are gone from notify's source (a
  `project/`-excluded grep, so it cannot match this phase's own docs).
