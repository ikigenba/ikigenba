# D19-events-wire

A service delivers each event to the `events` broker over the broker's unix socket, one HTTP request per event. Both ends of that wire live in package `events`: the socket sink, which every service's emitter uses by default (D18), and the emit handler, which the broker mounts to receive events. As with telemetry's wire (D13), keeping both ends in one package makes the request a service sends and the answer the broker gives one contract, tested against each other in appkit. The package also names the paths the other routes of the bus live at, so the broker and every service agree on them: `EventsPath`, where a service receives deliveries, and `DeclarationsPath`, where it publishes what it emits and accepts (D20).

## Finding the broker

The broker is the services-file entry named `events.ServiceName` (D05). The socket sink reads the services file on every delivery, at the path in the environment variable `services.Variable` names, and connects to that entry's `Socket`, so a host where the broker is installed after a service started is picked up without a restart. Whether the entry is `Enabled` does not matter: the socket is the address. A missing variable, an unreadable file, or no entry is an ordinary delivery failure, as in D13: the emitter retries for its retry window and then reports the event lost. An event that is not a valid emitted event (D17) is the exception that comes first: it is rejected (`ErrRejected`) without a request, whatever the environment, the services file, and the context hold, since sending it again could never succeed. The emitter never hands the sink such an event, but a broker or test that calls the sink directly might.

## The request and its answer

Every delivery is `POST` to `http://events/emit` (host `events.ServiceName`, path `events.EmitPath`) with `Content-Type: application/json` and, as the body, exactly the bytes `Event.MarshalJSON` returns for a valid emitted event (D17). The answer tells the emitter what to do next:

| Answer | Meaning | Emitter |
|---|---|---|
| 2xx | stored | done |
| 4xx | the event is at fault; sending it again cannot succeed | reports it lost (`ErrRejected`) |
| anything else, or no answer | the broker is unavailable | retries (D18) |

The sink never follows a redirect: a 3xx is retried. An event whose body is longer than `MaxEventBytes` is sent anyway and answered 413, so it is rejected; the limit is checked in one place, the handler.

The limit applies to the event's canonical form, `MarshalJSON`, not only to the bytes that arrived. The broker re-encodes every event it delivers with `MarshalJSON`, which escapes `<`, `>`, and `&` as six-byte `\u003c`-style escapes, so a body that spells those characters raw could pass a check on the raw bytes alone and grow up to six times over, past what a consumer's delivery endpoint accepts (D20). The handler therefore checks both: the raw body before decoding, so it never reads an unbounded body, and the decoded event's canonical form after, so everything the broker accepts it can also deliver. A body that is not a valid emitted event text is 400 whatever its canonical size, since it has none. The socket sink always sends the canonical form, so for an event it sends the two checks agree.

The emit handler is what the broker answers with. It checks, in order, the method (405, with `Allow: POST`), the content type (415), the size (413, past `MaxEventBytes`), the body (400 unless it is a valid emitted event text, D17, so a body carrying `seq` or `received`, a missing or badly formed `id`, or a `depth` that disagrees with `cause` is refused), and the size again, of the event's canonical form (413 when the decoded event's `MarshalJSON` is past `MaxEventBytes`), and only then hands the decoded event to the sink the broker gives it, which records it in the broker's log. The sink's answer becomes the status: nil is 204; an error wrapping `ErrRejected` is 422, the broker refusing an event it understood (an undeclared name, say: that policy is the broker's own design); any other error is 500, so the sender retries. Every answer has an empty body: the status says everything the sender acts on.

These statuses carry their standard meanings: 204 No Content (RFC 9110 §15.3.5), 400 Bad Request (§15.5.1), 405 Method Not Allowed, whose response must carry an `Allow` header listing the allowed methods (§15.5.6, §10.2.1), 413 Content Too Large (§15.5.14), 415 Unsupported Media Type (§15.5.16), 422 Unprocessable Content, a request whose content type and syntax are understood but whose instructions cannot be processed (§15.5.21), and 500 Internal Server Error (§15.6.1); 2xx is the successful class (§15.3) and 4xx the client-error class (§15.5). `application/json` is JSON's registered media type (RFC 8259 §11).

## Where the broker mounts it

The emit path is for siblings on the host, not for users. Siblings connect to the broker's socket directly and send no identity headers, so the broker mounts the emit handler at `EmitPath` outside `identity.Require`. Whether it sits inside the request middleware (D14), and whether nginx hides the path publicly, is the broker's own design.

## Consumer tasks

A producer does nothing with this document directly: its emitter's nil `Sink` is the socket sink, which finds the broker through the services file on every delivery.

The broker mounts `events.EmitHandler(sink)` at `events.EmitPath` on its mux, where `sink` is its own `events.Sink` that appends the event to its log and returns nil, an error wrapping `events.ErrRejected` for an event it refuses, or another error when its store fails. Its delivery routes and its declarations reader use `events.EventsPath` and `events.DeclarationsPath` (D20).

A test writes a services file into its temporary directory naming an `events` entry whose socket is a short path there, sets `IKIGENBA_SERVICES` with `t.Setenv`, serves `events.EmitHandler` over an `*events.Capture` on that socket, builds an emitter whose `Sink` is `events.NewSocketSink()`, emits, flushes, and asserts that the captured event marshals to the bytes the emitter's event marshals to.

## REQUIREMENTS

- R-GLMI-NKT3: Package `events` MUST export `const ServiceName = "events"`, `const EmitPath = "/emit"`, `const EventsPath = "/events"`, `const DeclarationsPath = "/declarations"`, and the untyped integer constant `MaxEventBytes = 65536`.
- R-GMUF-1CJS: Package `events` MUST export `func NewSocketSink() Sink`.
- R-GO2B-F4AH: Package `events` MUST export `func EmitHandler(sink Sink) http.Handler`, where `http` is the standard library's `net/http`.
- R-GPA7-SW16: On every `Deliver` call for a valid emitted event (R-GJ5O-5JCV), the `Sink` `events.NewSocketSink` returns MUST read the environment variable `services.Variable` names, read the services file at that path anew as `services.Read` reads it, and take the `services.Entry` that `List.Find` returns for `events.ServiceName`, whatever its `Enabled` holds.
- R-GRQ0-KFIK: For a valid emitted event `e` and an entry R-GPA7-SW16 finds, the socket sink's `Deliver` MUST send exactly one HTTP/1.1 request over a unix-domain stream connection to the entry's `Socket`, with method `POST`, host `events.ServiceName`, URL path `events.EmitPath` and no query, exactly one `Content-Type` header whose value is exactly `application/json`, and a body exactly the bytes `e.MarshalJSON()` returns.
- R-GSXW-Y799: The socket sink's `Deliver` MUST return nil when the answer to the request of R-GRQ0-KFIK has a status from 200 through 299; a non-nil error for which `errors.Is(err, events.ErrRejected)` is true when its status is from 400 through 499; and a non-nil error for which `errors.Is(err, events.ErrRejected)` is false for every other status, without following a redirect.
- R-GU5T-BYZY: For a valid emitted event, the socket sink's `Deliver` MUST return a non-nil error for which `errors.Is(err, events.ErrRejected)` is false, without sending a request, when the variable `services.Variable` names is unset or empty, when `services.Read` returns an error for its path, or when `List.Find` returns false for `events.ServiceName`; and when the connection cannot be made or the answer cannot be read.
- R-GVDP-PQQN: For a valid emitted event, when `ctx` is done before the socket sink's `Deliver` has the answer, `Deliver` MUST return a non-nil error for which `errors.Is(err, ctx.Err())` is true and `errors.Is(err, events.ErrRejected)` is false, without waiting for the answer.
- R-GWLM-3IHC: When `e` is not a valid emitted event (R-GJ5O-5JCV), the socket sink's `Deliver` MUST return a non-nil error for which `errors.Is(err, events.ErrRejected)` is true, without sending a request, whatever the environment, the services file, and `ctx` hold.
- R-GXTI-HA81: The socket sink's `Deliver` MUST be safe to call concurrently from multiple goroutines and MUST NOT panic for any `Event`.
- R-GZ1E-V1YQ: `events.EmitHandler` MUST panic, with a message that says the sink is nil, when `sink` is nil.
- R-H09B-8TPF: The handler `events.EmitHandler` returns MUST answer a request whose method is not `POST` with status 405 and exactly one `Allow` header whose value is `POST`, and MUST NOT call `sink.Deliver` for it.
- R-H1H7-MLG4: The handler `events.EmitHandler` returns MUST answer a `POST` whose `Content-Type` header is absent, or whose first `Content-Type` value `mime.ParseMediaType` (the standard library's `mime`) fails on or reads as a media type other than `application/json`, with status 415, and MUST NOT call `sink.Deliver` for it.
- R-H2P4-0D6T: The handler `events.EmitHandler` returns MUST answer a `POST` whose media type R-H1H7-MLG4 reads as `application/json` and whose body is longer than `events.MaxEventBytes` bytes with status 413, and MUST NOT call `sink.Deliver` for it.
- R-H3X0-E4XI: The handler `events.EmitHandler` returns MUST answer a `POST` that R-H1H7-MLG4 and R-H2P4-0D6T do not answer, and whose body is not a valid emitted event text (R-GQH2-G5T1), with status 400, and MUST NOT call `sink.Deliver` for it.
- R-1MTX-6OVB: The handler `events.EmitHandler` returns MUST answer a `POST` that R-H1H7-MLG4, R-H2P4-0D6T, and R-H3X0-E4XI do not answer, and for which the bytes `Event.MarshalJSON` returns for the `Event` that `Event.UnmarshalJSON` sets from the body (R-GU4R-LH14) are longer than `events.MaxEventBytes`, with status 413, and MUST NOT call `sink.Deliver` for it.
- R-1O1T-KGM0: For a `POST` that R-H09B-8TPF, R-H1H7-MLG4, R-H2P4-0D6T, R-H3X0-E4XI, and R-1MTX-6OVB do not answer, the handler `events.EmitHandler` returns MUST call `sink.Deliver` exactly once, with a context derived from the request's context and the `Event` that `Event.UnmarshalJSON` sets from the body (R-GU4R-LH14).
- R-1P9P-Y8CP: For a request R-1O1T-KGM0 describes, the handler `events.EmitHandler` returns MUST answer with status 204 when `sink.Deliver` returns nil, with status 422 when it returns a non-nil error for which `errors.Is(err, events.ErrRejected)` is true, and with status 500 when it returns any other non-nil error.
- R-H7KP-JG5L: Every answer of the handler `events.EmitHandler` returns MUST have an empty body.
- R-HA0I-AZMZ: For every valid emitted event `e` whose `ID`, `Service`, `RequestID`, `User`, `Cause`, and every value of `Attrs` whose basic form is a `string` are valid UTF-8, a `POST` to the handler `events.EmitHandler` returns with media type `application/json` and the body `e.MarshalJSON()` returns MUST call `sink.Deliver` with an `Event` whose `MarshalJSON` returns exactly the same bytes, whenever that body is no longer than `events.MaxEventBytes` bytes.
- R-HB8E-ORDO: The handler `events.EmitHandler` returns MUST be safe to serve concurrent requests from multiple goroutines.
