# D05-callback-listener

Package `internal/callback` receives the provider's redirect on the local
loopback interface. This document owns **binding only** — which addresses are
bound, on which port, and what happens when half of that succeeds. What the
bound endpoint then does with a request belongs to D06.

```go
package callback

// ListenFunc is the network dependency. net.Listen satisfies it.
type ListenFunc func(network, address string) (net.Listener, error)

// Listen binds IPv4 loopback, then IPv6 loopback on the same port. A returned
// Server is bound; there is no unbound state.
func Listen(listen ListenFunc, port int) (*Server, error)

// Port reports the port actually bound. With port 0 this is the ephemeral
// port the operating system assigned, never 0.
func (s *Server) Port() int

// BindWarning returns the IPv6 bind failure, or nil when both families bound.
func (s *Server) BindWarning() error

// Close releases every bound listener.
func (s *Server) Close() error
```

**Both loopback families, IPv4 first.** RFC 8252 §7.3 governs loopback
redirection for native apps: the redirect URI uses the `http` scheme with the
loopback interface, clients are advised to "attempt to bind to the loopback
interface using both IPv4 and IPv6 and use whichever is available," and
authorization servers "MUST allow any port to be specified at the time of the
request for loopback IP redirect URIs, to accommodate clients that obtain an
available ephemeral port from the operating system at the time of the request."

So `Listen` binds `127.0.0.1` first and takes the resulting port as
authoritative, then attempts `::1` on **that same port**. IPv4 leads because
its port is the one that ends up in the redirect URI; asking the OS for an
ephemeral port on both families independently would yield two different ports
and one of them would be a lie. A user's browser may resolve the callback host
to either family, and which one it picks is not ours to control — binding both
is what makes the choice irrelevant.

**IPv6 failure is tolerated, not fatal.** Hosts with IPv6 disabled are common
enough that a failed `::1` bind must not fail a login that IPv4 can serve
perfectly well. `Listen` therefore succeeds with the IPv4 listener alone and
records the IPv6 error, which the caller retrieves with `BindWarning()`. A
failed **IPv4** bind is fatal — that is the port the redirect URI names, and
there is nothing left to serve.

**The package reports the condition; it does not report the diagnostic.**
`BindWarning` returns an `error`, not a formatted sentence, and `callback`
writes to no stream at all. This is deliberate and is a correction of a real
defect: the predecessor implementation printed its IPv6 warning directly to
the process-global `os.Stderr`, bypassing the injected error writer, which made
the one diagnostic on that path unobservable from a `Run`-level test. Owning a
seam means owning the *fact*; the prose and the stream belong to `cli` (D11),
which already owns every other line the user sees.

**`ListenFunc` is an exported, injected dependency for the same reason.** On a
host with a working IPv6 loopback the fallback path is otherwise unprovokable,
so the tolerance requirement below could never be tested. Injecting the network
makes the failure reachable deterministically, and it makes the exact
`(network, address)` pairs `Listen` requests observable rather than inferred
from side effects. The predecessor carried this as an unexported, test-only
field; a dependency real enough to be substituted is real enough to be part of
the contract.

**Where the redirect URI's host comes from, and a deliberate deviation.** The
addresses bound here are always the loopback literals. The *host string* that
appears in the redirect URI is a separate thing, supplied by `--callback-host`
(D08) and composed by `Options.RedirectURI` (D11), and it defaults to
`localhost` — which RFC 8252 §7.3 explicitly calls NOT RECOMMENDED, preferring
the IP literal because it "avoids inadvertently listening on network interfaces
other than the loopback interface" and is "less susceptible to client-side
firewalls and misconfigured host name resolution."

The deviation is registration reality rather than protocol. Providers match the
registered redirect string exactly, and the widely registered form — including
the public OpenAI client's `http://localhost:1455/auth/callback` — spells it
`localhost`. An IP-literal default would fail against those providers out of
the box, with a redirect-mismatch error that gives the user no hint what to
change. Meanwhile the RFC's two actual safety concerns are met independently by
this document's contract: we bind the loopback literals **explicitly** rather
than by name, so no other interface is ever listened on, and we bind **both
families**, so host-name resolution order cannot strand the callback on an
address nothing is serving. Users whose provider registered the IP literal pass
`--callback-host 127.0.0.1`; the public xAI client, registered at
`http://127.0.0.1:56121/callback`, is exactly that case.

**Lifecycle.** A `Server` that `Listen` returns is bound — there is no unbound
state to guard against, and no boolean recording whether binding happened. The
listeners live until `Close`, whose observable effect is that the same fixed
port binds again immediately afterward.

## REQUIREMENTS

- R-G434-3Z7M: `Listen` MUST request an IPv4 loopback bind on `127.0.0.1` at the requested port before requesting any IPv6 bind, verified through an injected `ListenFunc` that records each `(network, address)` pair it is called with.
- R-G5B0-HQYB: `Listen` called with port `0` MUST return a `Server` whose `Port()` reports the non-zero port the operating system assigned to the IPv4 listener.
- R-G6IW-VIP0: `Listen` MUST request the IPv6 loopback bind on `::1` at the same port the IPv4 listener bound, including when that port was assigned ephemerally rather than requested.
- R-G7QT-9AFP: When the injected `ListenFunc` fails the IPv6 bind and succeeds the IPv4 bind, `Listen` MUST return a usable `Server` with a nil error, and that `Server`'s `BindWarning()` MUST return a non-nil error wrapping the IPv6 failure.
- R-G8YP-N26E: When both loopback families bind successfully, `BindWarning()` MUST return nil.
- R-GA6M-0TX3: When the IPv4 loopback bind fails, `Listen` MUST return a non-nil error and no usable `Server`, regardless of whether an IPv6 bind would have succeeded.
- R-GBEI-ELNS: After `Close` returns, the port the `Server` had bound MUST be immediately bindable again by a fresh `Listen` at that same fixed port.
