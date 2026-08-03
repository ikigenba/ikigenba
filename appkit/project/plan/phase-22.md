# Phase 22 — `appkit/httpclient`: the shared instrumented outbound client

*Realizes design Decision 19 (the instrumented outbound client). Depends on
Phase 17 and Phase 19.*

**End state.** A new package `appkit/httpclient` provides `New(Options)
*http.Client` and `NewTransport(Options) http.RoundTripper` over a recording
`http.RoundTripper`. Per round trip it sets `correlation.Header` from the
request context **only** when the URL host is a loopback IP literal
(`127.0.0.0/8` or `::1`) — the *name* `localhost` deliberately does not
qualify — and records kind `outbound` with op `<METHOD> <host><path>`, the
numeric status, duration, the response body's size and digest in `outcome`, and
the request body's size and digest in `detail`, both computed streaming through
wrapped body readers so nothing is buffered. Query strings are never captured.
A transport failure records an error **class** (`timeout`,
`connection_refused`, `dns`, or `source_unavailable`) and returns the error to
the caller unchanged. A nil `Recorder` yields a plain working client.

**Done when:**
- These Verification ids are covered by clearly-named tests tagged with the id
  verbatim, driven against **real HTTP** — `httptest.NewServer` for the success
  paths and a genuinely closed port for the failure path:
  - R-22VU-7KW7 — a real `POST` records the op, status, duration, and the real
    response and request bodies' sizes and digests.
  - R-25BM-Z4DL — addressed as `127.0.0.1:<port>`, the **server observes**
    `X-Correlation-Id` equal to the caller's context id.
  - R-26JJ-CW4A — the same server addressed as `localhost:<port>` receives **no**
    `X-Correlation-Id` on an otherwise successful round trip.
  - R-27RF-QNUZ — a closed port returns the transport error to the caller and
    still records an `outbound` record with error class `connection_refused`
    and no raw Go error text.
  - R-28ZC-4FLO — a nil-`Recorder` client completes a real request normally and
    emits no records.
- The suite is green per design's *Conventions* (`go build ./...`, `go vet
  ./...`, `gofmt -l .` empty, `go test ./...`, all from `appkit/`).
