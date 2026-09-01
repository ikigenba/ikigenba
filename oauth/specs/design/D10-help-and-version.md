# D10-help-and-version

**Usage text.** One block, produced by `options.Usage() string` and placed by
its caller: stdout for `-h`/`--help`, stderr on a usage error (routing, D08).
Returning a string rather than writing to a stream is what lets one text serve
both destinations without the producer knowing which, and it makes the text a
value a test can compare rather than output it must capture.

Its structure is part of the product: a usage line, a `Flags:` header with a
two-line row per flag, then worked examples. The flags block, exactly:

```
Usage: oauth [flags]

Flags:
  --auth-url string
        authorization endpoint (required)
  --token-url string
        token endpoint (required)
  --client-id string
        OAuth client id (required)
  --scope string
        space-separated OAuth scopes
  --client-secret string
        client secret sent in the token request body
  --callback-host string
        host used in the redirect URI (default "localhost")
  --port int
        loopback callback port; 0 chooses an available port (default 0)
  --callback-path string
        callback route and redirect URI path (default "/callback")
  --auth-param key=value
        extra authorize parameter (repeatable)
  --token-param key=value
        extra token parameter (repeatable)
  --token-header key=value
        extra token request header (repeatable)
  --no-browser
        print the authorize URL without opening a browser
  --timeout duration
        maximum time to wait for the callback (default 5m)
  -h, --help
        print help and exit
  -V, --version
        print version and exit
```

The last two rows are new. The shipping binary documents only `-h` and `-V`,
but its `--help` long form already works — the standard library `flag` package
returns `ErrHelp` for any undefined `-help`/`--help` — so today the text
understates the surface it actually has. `--version` is the opposite case: it
is documented nowhere and does not exist, and `oauth --version` fails with
`flag provided but not defined: -version`. Both spellings are made real and
documented here, matching the sibling `idgen`, so that a reader of the help
text learns the whole surface and every documented flag works.

**Worked examples.** The block above is followed by two complete, copy-paste
logins and the Basic authentication form:

```
OpenAI example:
  oauth \
    --auth-url  https://auth.openai.com/oauth/authorize \
    --token-url https://auth.openai.com/oauth/token \
    --client-id app_EMoamEEZ73f0CkXaXp7hrann \
    --scope "openid profile email offline_access" \
    --port 1455 --callback-path /auth/callback \
    > auth.json

xAI example:
  oauth \
    --auth-url  https://auth.x.ai/oauth2/authorize \
    --token-url https://auth.x.ai/oauth2/token \
    --client-id b1a00492-073a-47ea-816f-4c329264a828 \
    --scope "openid profile email offline_access grok-cli:access api:access" \
    --callback-host 127.0.0.1 \
    --port 56121 \
    --callback-path /callback \
    > x-ai-auth.json

Basic authentication:
  --token-header "Authorization=Basic $(printf '%s:%s' "$ID" "$SECRET" | base64 -w0)"
```

These examples are the only place in the binary that names a real service, and
they are **documentation, not defaults** — no code branches on a provider, and
a login is still described entirely by flags. They earn their place because
assembling a first working invocation from endpoint documentation is the
fiddliest part of using this tool, and both examples are load-bearing evidence
that the flags they use are not speculative.

The OpenAI constants come from the public Codex client registration, whose
redirect is registered exactly as `http://localhost:1455/auth/callback`. That
fixed port and path are precisely why `--port` and `--callback-path` exist. The
example passes no `--callback-host` because the default `localhost` already
matches, and no `--client-secret`, because the client is public.

The xAI constants come from the public grok-cli client, whose redirect is
registered as `http://127.0.0.1:56121/callback`. It is the contrasting case:
the host is the loopback IP literal, so the example **must** pass
`--callback-host 127.0.0.1` — the default `localhost` would not match the
registration. This is also why the default is `localhost` at all. RFC 8252
§7.3 prefers the IP literal and calls `localhost` NOT RECOMMENDED, but
providers match the redirect string exactly and the widely registered form is
the name, so an IP-literal default would fail out of the box against the more
common case with no obvious cause. The RFC's actual safety concerns are met
independently: the listener binds the loopback literals explicitly rather than
by name, and binds both address families (D05), so host-name resolution order
can never strand the callback.

Both examples redirect to generic filenames. Where a caller saves stdout is
its own policy, and consumer default paths are not `oauth`'s to name.

The Basic authentication form is shown because assembling the header is the
fiddly part, and because RFC 6749 §2.3.1 makes Basic the one client
authentication method a conforming server must support — so a provider that
rejects `--client-secret` in the body has this as its only route.

**Version.** The version string is a product fact carried in source — a single
`var version` in `internal/cli` — not injected at build time, so a development
build and a released build report the same string by construction. Its *value*
is release data, not a design fact: the release process may change it freely
without touching this spec. The spec fixes only its **shape**, a `v`-prefixed
`MAJOR.MINOR.PATCH`, and its output form. The release workflow (see
`AGENTS.md`) is what ties a given tag to the string; that check is
infrastructure, outside the spec loop.

This replaces the shipping binary's `-ldflags "-X main.version=..."` injection
and its `dev` sentinel. Injection makes the version untestable without building
a stamped binary inside a test, and it guarantees that the thing developers run
is not the thing users run. `-V` and `--version` print the string bare on its
own line, to stdout. Sending it to stdout rather than stderr is safe despite
the promise that stdout carries only a token response, because a version run
and a login run are mutually exclusive: the flag short-circuits before any
listener binds (D08).

## REQUIREMENTS

- R-R5YG-YXBO: `options.Usage()` MUST begin with the flags block byte-for-byte as quoted above: the usage line, a blank line, the `Flags:` header, and the fifteen two-line flag rows in that order.
- R-R8E9-QGT2: The usage text MUST name every flag spelling: `--auth-url`, `--token-url`, `--client-id`, `--scope`, `--client-secret`, `--callback-host`, `--port`, `--callback-path`, `--auth-param`, `--token-param`, `--token-header`, `--no-browser`, `--timeout`, `-h`, `--help`, `-V`, and `--version`.
- R-R9M6-48JR: The usage text MUST contain the OpenAI worked example's constants: `https://auth.openai.com/oauth/authorize`, `https://auth.openai.com/oauth/token`, `app_EMoamEEZ73f0CkXaXp7hrann`, `--port 1455`, and `--callback-path /auth/callback`.
- R-RAU2-I0AG: The usage text MUST contain the xAI worked example's constants: `https://auth.x.ai/oauth2/authorize`, `https://auth.x.ai/oauth2/token`, `b1a00492-073a-47ea-816f-4c329264a828`, `--callback-host 127.0.0.1`, `--port 56121`, `grok-cli:access`, and `api:access`.
- R-RC1Y-VS15: The usage text MUST contain the Basic authentication form, showing an `Authorization=Basic` value supplied through `--token-header`.
- R-RD9V-9JRU: `-h` and `--help` MUST each write the usage text to stdout, write nothing to stderr, and exit 0.
- R-REHR-NBIJ: A usage error MUST write the usage text to stderr, write nothing to stdout, and exit 2.
- R-IG9T-7Q01: `-V` and `--version` MUST each write the `internal/cli` version string alone on a line — exactly that string followed by a single newline, with nothing else on stdout — and exit 0, with both spellings producing identical stdout and exit code.
- R-VBTO-NEYR: The version string MUST be a `v`-prefixed semantic version of the form `vMAJOR.MINOR.PATCH`, where each of MAJOR, MINOR, and PATCH is a non-negative integer with no leading zeros (matching `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).
- R-M7GH-O5K2: Package `internal/options` MUST export `Usage() string`.
