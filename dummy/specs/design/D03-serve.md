# D03-serve

The listener's life: what happens between `cli.Run` binding `127.0.0.1:$PORT`
and the process being gone. `D01-layout-and-run-seam` declares the names this
design behaves through: `cli.Process`, `cli.Run`, the `Listen` factory, the
`Listening` hook, the exit codes, and `server.Serve`. `D02-cli` decides when
`PORT` is valid and that a valid one leads `Run` to bind and call `Serve`;
`D04-pages` decides what the handler answers. This design decides nothing new
structurally. It says how `Serve` treats the listener it is handed and how
`Run` behaves from the bind until it returns, in the three shapes the serve
stories show: a clean run that ends on a signal, a port that is already
taken, and a server that dies.

`Serve` owns `ln` from the call. It serves HTTP/1.1 with `h` and blocks:
plain HTTP/1.1, no h2c, no TLS. `net/http` answers an HTTP/1.0 request with
an HTTP/1.0 status line, and the HTTP/1.1 requirement does not forbid that;
it says which protocol the server speaks, not how it answers an older
client. When `ctx` is done, which is how `SIGTERM` reaches it through
`main`, it closes the listener so nothing new is accepted, lets every
request it already accepted finish and receive its whole response, drops
connections that carry no request, and returns nil. There is no drain timeout. The story's promise is
unconditional, the handler serves one small static page that cannot block on
anything of its own, and the host already bounds a stop that hangs: systemd
sends `SIGKILL` when its stop timeout expires. A timeout constant here would
be a second, weaker promise stacked on the first. `Serve` returns a non-nil
error only when serving fails for some reason other than cancellation.

`Serve` is also silent in its own right. Go's `net/http` server has a sink
of its own: `net/http` documents at `Server.ErrorLog` that, when that field
is nil, errors accepting connections and unexpected behaviour from handlers
are logged through the `log` package's standard logger, which writes to the
real standard error and never passes through `Process.Stderr`. Its source
shows what arrives there: an `Accept` error the server retries, a handler
that panics, a handler that calls `WriteHeader` twice. A client can provoke
none of these against `D04-pages`'s handler, but the sink is open whatever
the handler does, and `D01`'s rule that nothing below `main` reaches the
real streams except through `Process` is exactly the promise it would break.
So `Serve` gives the server an `ErrorLog` that goes nowhere, and the
server's own diagnostics are discarded, because the host's journal holds
only trouble the process itself reports. Either `log.New(io.Discard, "", 0)`
or `slog.NewLogLogger(slog.DiscardHandler, slog.LevelError)` satisfies that;
the silence requirement fixes the outcome, not the constructor. It is
tested as `D01` tests its stream rule: a test points the `log` package at a
buffer, drives `Serve` with a listener whose first `Accept` returns a
temporary error and a handler that panics, and finds the buffer empty.

`Run` is the observable surface. With a valid `PORT` it asks the listen
factory for exactly one listener, `tcp` on `127.0.0.1:<port>`, and never
another address, because nginx on the host proxies to loopback and nothing
else should be reachable. The factory is `Process.Listen`, or `net.Listen`
when that is nil, which is what `main` leaves it; Go's `net` package
documents that `net.Listen("tcp", "127.0.0.1:<port>")` binds that address
and no other. A test that wants the real bind leaves `Listen` nil and reads
the bound address from `Listening`; a test that wants a failure injects a
factory that returns an error, or one that returns a listener whose `Accept`
fails, and never contends for a port. A healthy run is silent from start to
finish: no startup banner, no request log, nothing on either stream when it
stops, so that under systemd the journal holds only trouble. When `ctx` is
done it returns `ExitSuccess`, and by then the listener is closed and every
accepted request has been answered.

Trouble is one line on stderr, in the repository's diagnostic shape: the
program name, a colon, a space, and the reason. For a port already in use the
reason is the error the listen returned, verbatim; the listen is the call to
`Process.Listen`, or to `net.Listen` when that is nil, and the reason is
whichever of them said no. Go's `net` package documents the real one's
shape: a `net.OpError` prints its operation, network, and address followed
by the underlying error, so a failed `net.Listen` on `127.0.0.1:3000` reads
`listen tcp 127.0.0.1:3000: bind: address already in use` on Linux, exactly
the line the story shows. The requirement asserts the composition, `dummy: `
plus the listen error's text, not the literal, so a test either occupies a
port, asks `net` for the error a second listen returns, and compares, or
injects a factory that returns an error of its own choosing and compares
with that. A server that fails after binding is reported the same way with
the error `Serve` returned; a test reaches that path by injecting a factory
whose listener's `Accept` returns an error the server does not retry. Both
exit `ExitServerFailed`.

No read, write, or idle timeouts are contract. The stories fix none, the
only client is the host's proxy, and nothing about the page needs them. The
build is nonetheless expected to set `ReadHeaderTimeout`: the lint gate's
gosec `G112` demands it and suppression is forbidden, and it affects only a
connection that has not yet delivered a request header, which is exactly a
connection that carries no request. The build must not set a write timeout
that could cut short the drain the shutdown requirement promises: a response
accepted before `ctx` was done is delivered whole, however long that takes,
and a `WriteTimeout` would put a second, weaker promise under the first.

## REQUIREMENTS

- R-QLRL-RC1B: `Serve` MUST accept connections on `ln` and answer every request received on them over HTTP/1.1 with the response `h` produces for that request, and MUST NOT return while `ctx` is not done and serving has not failed.
- R-QMZI-53S0: When `ctx` is done, `Serve` MUST stop accepting connections and close `ln`, MUST deliver the complete response to every request accepted before `ctx` was done, however long that takes, MUST close every connection that carries no request, and MUST then return nil.
- R-QO7E-IVIP: When serving fails while `ctx` is not done, `Serve` MUST return a non-nil error, and `Serve` MUST NOT return nil for any reason other than `ctx` being done.
- R-IBR4-T2PU: `Serve` MUST write nothing to the `log` package's default logger and nothing to any process stream, whatever `ln` and `h` do: the diagnostics `net/http`'s server writes through its `ErrorLog`, such as an `Accept` error it retries or a handler that panics, MUST be discarded, so that a test which points the `log` package's output at a buffer and drives `Serve` with a listener whose `Accept` returns a temporary error and a handler that panics finds the buffer empty when `Serve` returns.
- R-ICZ1-6UGJ: When `Run` binds a listener it MUST do so by calling `Listen`, or `net.Listen` when `Listen` is nil, exactly once, with network `tcp` and address `127.0.0.1:<port>` where `<port>` is the value of `PORT`, and the address `Run` passes to `Listening` MUST be the `Addr()` of the listener that call returned.
- R-QQN7-AF03: A `Run` that binds a listener and returns because `ctx` is done MUST write nothing to `Stdout` and nothing to `Stderr` between being called and returning, whatever requests the listener received in between.
- R-8TZB-3NSI: A `Run` that has bound a listener and returns because `ctx` is done MUST return `ExitSuccess`, and MUST NOT return before the listener is closed and every request it accepted before `ctx` was done has received its complete response.
- R-QUAW-FQ86: When `Run` cannot bind `127.0.0.1:<port>`, it MUST write to `Stderr` exactly `dummy: `, the `Error()` text of the error the listen returned, and a newline, MUST write nothing to `Stdout`, MUST return `ExitServerFailed`, and MUST leave whatever holds the port able to accept connections.
- R-QVIS-THYV: When `server.Serve` returns a non-nil error to `Run`, `Run` MUST write to `Stderr` exactly `dummy: `, that error's `Error()` text, and a newline, MUST write nothing to `Stdout`, and MUST return `ExitServerFailed`.
