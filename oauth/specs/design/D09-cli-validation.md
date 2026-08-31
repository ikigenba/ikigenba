# D09-cli-validation

Everything `options.Parse` checks, all of it before anything observable
happens. No listener is bound, no authorize URL is composed, no browser is
opened, and no packet leaves the machine until every check below has passed.
The ordering matters to the user: a flag mistake should cost a diagnostic, not
a browser window pointed at a listener that was never created.

Each failure returns an error naming the offending flag; `cli` reports it to
stderr as `oauth: <problem>` and exits `2` (D08).

**Required flags.** `--auth-url`, `--token-url`, and `--client-id` have no
sensible default — they *are* the service — so each must be supplied.

**Endpoint URLs must parse.** `--auth-url` and `--token-url` are parsed here
and carried in `Options` as `*url.URL`. Parsing at the boundary is what lets
`oauth.Client.AuthorizeURL` be infallible (D03): a function that cannot fail
needs no error return, and a caller that cannot receive an error cannot ignore
one. The alternative — parsing lazily inside the protocol core — would push a
`(string, error)` return through every caller to report a mistake the user made
before the flow began.

**`key=value` form.** Each `--auth-param`, `--token-param`, and `--token-header`
value must contain `=` and must have a non-empty key. The error names both the
flag and the offending value, since a run may carry several.

**Reserved keys.** The authorize request and the token request each write a
fixed set of parameters that the flow itself owns; a caller-supplied extra may
not collide with one. `options` decides *when* to check and *what to report*,
but it does not know *which* keys are reserved — it asks
`oauth.ReservedAuthorizeParam` and `oauth.ReservedTokenParam` (D03, D04).

This is the direct fix for a real defect in the shipping implementation, which
carries `authReserved`/`tokenReserved` maps in `cmd/oauth/main.go` *and*
`authorizeReserved`/`exchangeReserved` maps in `internal/oauth` — two
independent transcriptions of one rule, checked twice, free to drift apart.
One encoding, owned by the package that writes those parameters, is the same
arrangement `idgen` uses for `idgen.ValidPrefix` (its D2/D5): the format's
owner exports the predicate, and the flag boundary consumes it rather than
restating it. Widening a reserved set then becomes a single edit that both
layers follow together.

**One client authentication method.** RFC 6749 §2.3.1 states that a client
"MUST NOT use more than one authentication method in each request".
`--client-secret` puts credentials in the request body; a `--token-header`
keyed `Authorization` puts them in a header. Supplying both is that forbidden
double, so it is rejected naming both flags. The comparison is
case-insensitive, because HTTP header names are. Either flag alone is
accepted — the header escape hatch exists precisely because RFC 6749 §2.3.1
also makes HTTP Basic the one method a conforming server must support, while
body-form credentials are optional for servers and "NOT RECOMMENDED" for
clients.

**`--timeout` must be positive.** A zero or negative deadline would expire
before the user could reach the browser.

**`--callback-path` must begin with `/`.** It is both the route the callback
server answers on and the path component of the redirect URI, and the two must
agree. A value like `callback` produces a redirect URI whose path the server
never matches, so the provider's redirect arrives, gets a 404, and the login
hangs until the timeout expires — the worst failure shape available, because it
is silent, slow, and gives the user nothing to act on. Rejecting it at the flag
boundary costs one comparison and converts a five-minute mystery into an
immediate message.

One reserved key earns a message of its own. `--auth-param redirect_uri=...`
is the collision a user is most likely to attempt deliberately, believing it is
how the callback address is configured. Letting it through would produce a
redirect that disagrees with the address actually bound, and therefore a login
that hangs until the timeout with no indication why — the worst failure shape
available. Rejecting it with the generic "reserved key" wording would leave that
user no better off, so the diagnostic names the three flags that do configure the
redirect: `--callback-host`, `--port`, and `--callback-path`.

## REQUIREMENTS

- R-QTRH-57WQ: Omitting `--auth-url`, `--token-url`, or `--client-id` MUST exit 2 with an stderr message naming the missing flag, and MUST write nothing to stdout.
- R-QUZD-IZNF: An `--auth-url` or `--token-url` value that does not parse as a URL MUST exit 2 with an stderr message naming the offending flag.
- R-QW79-WRE4: An `--auth-param`, `--token-param`, or `--token-header` value containing no `=`, or whose key is empty, MUST exit 2 with an stderr message naming both the flag and the offending value.
- R-QXF6-AJ4T: The `--auth-param` accept/reject decision MUST agree with `oauth.ReservedAuthorizeParam` — over every key in the reserved set plus a sample of non-reserved keys, the invocation MUST exit 2 naming the flag and the key exactly when the predicate accepts that key, and MUST proceed exactly when it rejects it.
- R-LCAT-LU5C: An `--auth-param` whose key is `redirect_uri` MUST exit 2 with an stderr message that names `--callback-host`, `--port`, and `--callback-path` as the flags that configure the redirect URI.
- R-QYN2-OAVI: The `--token-param` accept/reject decision MUST agree with `oauth.ReservedTokenParam` — over every key in the reserved set plus a sample of non-reserved keys, the invocation MUST exit 2 naming the flag and the key exactly when the predicate accepts that key, and MUST proceed exactly when it rejects it.
- R-QZUZ-22M7: `--client-secret` supplied together with a `--token-header` whose key equals `Authorization` under case-insensitive comparison MUST exit 2 with an stderr message naming both flags.
- R-R12V-FUCW: `--client-secret` alone, and a `--token-header` keyed `Authorization` alone, MUST each be accepted.
- R-R2AR-TM3L: A `--timeout` value of zero or less MUST exit 2 with an stderr message naming `--timeout`.
- R-R3IO-7DUA: A `--callback-path` value that does not begin with `/` MUST exit 2 with an stderr message naming `--callback-path`.
- R-R4QK-L5KZ: Every validation failure in this document MUST occur before any listener is bound and before the browser launcher is invoked, verified with a `callback.ListenFunc` and a `browser.Launcher` that fail the test if called.
