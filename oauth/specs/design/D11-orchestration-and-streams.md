# D11-orchestration-and-streams

`internal/cli` is the composition root's behavior: it sequences the login,
composes the redirect URI, decides what reaches which stream, and maps every
outcome to an exit code. It owns no protocol, no flag grammar, and no listener
mechanics — those are D02–D10. What it owns is the order they happen in and
what the caller observes.

**Order of operations.** Chosen so that nothing user-visible happens before
failure has been ruled out, and so the user is never sent to authenticate
against a listener that does not exist:

```
parse and validate (D08, D09)
  → bind the loopback listeners (D05)
  → compose the redirect URI from the port actually bound
  → build the authorize URL (D03)
  → print the authorize URL to stderr
  → launch the browser (D07), unless --no-browser
  → wait for the callback (D06)
  → exchange the code for tokens (D04)
  → write the token response bytes to stdout
```

Binding precedes URL construction because with `--port 0` the port is not
known until the listener exists. Printing precedes launching so that a user
whose browser fails to open still has the URL in front of them.

**The redirect URI** is `http://<callback-host>:<port><callback-path>`, built
with `net.JoinHostPort` so an IPv6 literal host is bracketed
(`--callback-host ::1` yields `http://[::1]:<port>/callback`). The port is
always the one `callback.Listen` actually bound, never the requested `0`.

`--callback-host` names the string the provider matches against; the listener
always binds the loopback literals regardless (D05). The default is
`localhost` because that is the widely registered form — RFC 8252 §7.3 prefers
the IP literal, but providers match the redirect string exactly, and an
IP-literal default would fail out of the box against the common registration
with no obvious cause. The RFC's actual concern, that a name might resolve to
a non-loopback interface, is answered by binding the literals explicitly rather
than by name.

The same redirect URI string is sent to the authorize endpoint and again as the
`redirect_uri` form field at exchange. RFC 6749 §4.1.3 requires them to be
identical, and a mismatch is a failure the provider reports obscurely, so the
two are composed once and reused rather than rebuilt.

**Stream discipline.** Stdout is machine output and nothing else: on success it
receives exactly the token endpoint's response bytes, with nothing added,
removed, or reordered — no trailing newline, no interleaved progress. On every
failure it receives zero bytes, so a redirected `> auth.json` that fails leaves
an empty file rather than one holding a diagnostic that a later reader might
mistake for credentials. Everything meant for a human — the authorize URL,
progress, warnings, and errors — goes to stderr, so it stays visible on a
terminal during a redirected run.

This is also why a failed write to stdout must not be swallowed. If the token
bytes could not be delivered, the login did not succeed from the caller's point
of view, however well it went on the wire; the run reports the write failure on
stderr and exits non-zero.

**Exit codes** are the package constants `exitSuccess = 0`, `exitFailure = 1`,
`exitUsage = 2`. A completed login exits 0. A usage or validation error — a
malformed or unknown flag, a missing required flag, a reserved parameter key —
exits 2 (D09). Every runtime failure — a state mismatch, a provider `error=`
redirect, a non-2xx token response, an expired deadline, a failed stdout write
— exits 1.

**Non-fatal conditions.** Two things go wrong without ending the login. A
browser launcher that returns an error produces a note on stderr and the wait
continues, since the URL is already printed and the user can open it
themselves — the launcher is a convenience, not a dependency. And an IPv6
loopback that will not bind produces a warning on stderr while IPv4 service
continues (D05); `callback` reports that condition through `BindWarning()`
rather than printing it, so the message reaches the *injected* stderr and is
observable in a `Run` test instead of escaping to the process's global stderr.

## REQUIREMENTS

- R-ECWL-ZF4I: `Run` MUST bind the loopback listener before invoking the browser launcher, verified with an injected listen function and launcher that record their call order.
- R-EFCE-QYLW: With `--port 0`, the `redirect_uri` parameter of the authorize URL MUST carry the port the injected listen function actually bound, never `0`.
- R-EGKB-4QCL: With `--callback-host ::1`, the composed redirect URI MUST bracket the IPv6 literal, taking the form `http://[::1]:<port><callback-path>`.
- R-EHS7-II3A: The `redirect_uri` sent as an authorize-URL parameter and the `redirect_uri` sent as a token-request form field MUST be byte-identical within a single run.
- R-EJ03-W9TZ: On a successful login, stdout MUST hold exactly the token endpoint's response bytes — no added trailing newline, no interleaved diagnostic text, and no re-encoding.
- R-EK80-A1KO: For every failure mode — usage error, callback state mismatch, provider `error=` redirect, non-2xx token response, and expired callback deadline — stdout MUST receive zero bytes.
- R-ELFW-NTBD: The authorize URL, progress messages, and error text MUST be written to the injected stderr, and MUST NOT appear on stdout even when stdout is redirected to a file.
- R-EMNT-1L22: `--no-browser` MUST NOT invoke the launcher, and MUST still print the authorize URL to stderr.
- R-ENVP-FCSR: A launcher whose `Open` returns an error MUST NOT fail the run: a note MUST be written to stderr, the wait MUST continue, and a callback arriving afterward MUST still complete the login and exit 0.
- R-EP3L-T4JG: When the write of the token response to stdout returns an error, `Run` MUST report that error on stderr and return `exitFailure`, rather than discarding it and returning `exitSuccess`.
- R-EQBI-6WA5: When `callback.Listen` reports a non-nil `BindWarning()`, `Run` MUST write a warning naming that condition to the injected stderr, and the login MUST proceed to completion over the address that did bind.
