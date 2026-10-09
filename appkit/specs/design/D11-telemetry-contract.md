# D11-telemetry-contract

Every service in the suite records what it does as a trail of events sent to the `telemetry` service, so that an agent starting from one thing it knows (a request id, a user, an entity id, a time) can reconstruct the whole history of what happened across every service that took part. Package `telemetry` is where that trail's contract lives, once for the whole suite: the shape of one event, the rules its name and attributes follow, the convention for naming entities, and the catalogue of events appkit itself records for every service. The writer that queues and delivers events is D12; the wire between a service and the telemetry service is D13; the request middleware and the sibling client that record requests and sibling calls are D14; `tool.called` is recorded by the `mcp` server (D07, D08).

## Two channels

Every event goes to the telemetry service. A service's standard error has exactly two uses: a condition the service cannot continue from, and an event it could not deliver to telemetry. A handled failure, such as a request answered 500, is a trail event and earns no standard-error line; the `request.finished` event records its status. Standard error stays the human channel journald shows, and the trail is complete without it.

## The event

An event is an envelope of six fields: the time, the service, the event's name, the request id, the user, and the attributes. `telemetry.Event` is that envelope in Go, and `telemetry.Attrs` its attributes. The emitter stamps the time in UTC at microsecond resolution. The request id is the one nginx sets in `X-Request-Id` (or appkit's middleware mints, D14), carried unchanged through every service the request touches; the user is the signed-in user's id. Together they are the correlation keys: the user is the actor, the request id one thing the actor did. Either is the empty string when the event has none, as `service.started` has none.

Attributes are a flat map. Each value is a string, a number, or a boolean; nothing nested, so every attribute is filterable. In Go a value may be of any type whose kind is a boolean, string, integer, or floating-point kind (a named type like `type Effect string` included); the event carries it converted to its basic form (`string`, `bool`, `int64`, `uint64`, or `float64`), so a sink sees only those five types and the wire never depends on a type's own JSON encoding. A floating-point value must be finite. Any other value (nil, a slice, a map, a struct, a pointer, a channel, NaN) makes the event malformed: D12 says what the writer does with one.

An event's JSON form, `Event.MarshalJSON`, is the exact body posted to telemetry (D13) and the exact text written to standard error for an undeliverable event (D12). It is defined in terms of the standard library's `encoding/json`, whose documentation fixes the details that matter here: map keys are sorted, strings are escaped the same way every time (invalid UTF-8 replaced by U+FFFD), and floating-point numbers are formatted the same way every time. The time is RFC 3339 in UTC with exactly six fractional digits and `Z`.

## Names and keys

Event names are lowercase and dotted, a noun then a past-tense verb: `request.started`, `token.minted`, `check.refused`. A name may qualify its noun with more words, as `cron.nightly_backup.fired` does. There is no service prefix, since the service is in the envelope. The mechanically checkable part is enforced everywhere an event is formed: two or more words joined by single dots, each word lowercase ASCII letters and digits in parts joined by single underscores, starting with a letter (`api_key.minted` and `auth.token.minted` are valid; `Token.Minted`, `minted`, `token..minted`, and `cron.*.fired` are not). A name never holds `*`: the event bus's patterns (D17) are for declaring events, and the trail records only events that happened. That the words before the last name a thing and the last is a past-tense verb is the emitting service's design to get right; no code can check grammar.

Attribute keys are snake_case: lowercase ASCII letters and digits in parts joined by single underscores, starting with a letter. Durations are integer microseconds under a key ending `_us`. Sizes are integer bytes under a key ending `_bytes`.

## Metadata, never data

Attributes carry the operation and the ids of the entities it touched. They never carry a value the service's own database can answer, never a secret, never an argument payload, and never a URL's query. The consequence is accepted: a previous value is gone once overwritten unless the service keeps its own history. A tool call records the tool and its outcome, not its arguments. appkit's framework events below follow the rule (a path never carries its query). Every other event is named and specified in its emitting service's own design, and that design is where the rule becomes a testable requirement for that service's events; appkit cannot see another service's data.

## Entity ids

An attribute naming an entity has the entity's type as its key (`token`, `widget`, `user`) and the entity's id as its value. Every id carries a suite-wide type prefix, lowercase letters followed by `_`, the way auth's token ids carry `tok_`, so an id from one service never collides with one from another and a search for an id finds it once. The prefixes are registered here, so no two services can claim the same one:

| Prefix | Entity type | Owning service |
|---|---|---|
| `cli_` | `client` | `auth` |
| `evt_` | `event` | `events` |
| `prm_` | `prompt` | `prompts` |
| `prr_` | `prompt_run` | `prompts` |
| `rep_` | `repo` | `repos` |
| `run_` | `run` | `scripts` |
| `scr_` | `script` | `scripts` |
| `sit_` | `site` | `sites` |
| `tok_` | `token` | `auth` |
| `vis_` | `visitor` | `sites` |
| `wgt_` | `widget` | `dummy` |

This table is the registry, and it exists only here: appkit exports no code for it, so adding a row is a design change to this document alone, with no change to appkit's code and no appkit release. A service that adds an entity type adds its row in that change, and its own design then holds the testable requirements: that its ids begin with its registered prefix and that its events name the entity under its registered type. The convention every service follows: an entity attribute's key is the entity type's name exactly as registered (itself a valid attribute key), and its value is a string that begins with the registered prefix, lowercase ASCII letters followed by `_`. appkit's own framework events name no entity, with one exception: `event.lost`, which the `events` package's emitter records, names the bus event it lost under the key `event` with its `evt_` id, following the convention, and D18's requirements hold appkit's code to it. Its `cause` attribute also holds an `evt_` id, the lost event's cause, and deliberately departs from the convention: it names a second bus event under a role key, since one event cannot carry two attributes keyed `event`.

## Framework events

appkit records these for every service, and a service cannot switch them off: every service must import appkit to serve, and a service author must not be able to leave a hop out of the trail. The one exception is `event.lost`, which only a service that builds an `events` emitter with a telemetry writer in its `Config.Telemetry` records (D18). A request started with no finish, or a service started with no stop before it, is evidence of a crash; that is why a request records two events rather than one at the end.

| Event | When | Attributes |
|---|---|---|
| `service.started` | the service is ready (D12) | `version` |
| `service.stopping` | the writer's drain begins; the last event out (D12) | `reason` |
| `request.started` | a request arrives (D14) | `method`, `path` |
| `request.finished` | its answer is complete (D14) | `status`, `duration_us`, `request_bytes`, `response_bytes` |
| `sibling.called` | a call to a sibling service returns (D14) | `target`, `method`, `path`, `status`, `duration_us` |
| `tool.called` | an MCP tool call is answered (D07, D08) | `tool`, `kind`, `outcome`, `duration_us` |
| `event.lost` | the `events` package's emitter gives up an event it could not put on the bus (D18) | `event`, `cause` |

The request id and the user travel in the envelope, never as attributes. This document fixes each event's attribute keys and value types; the producing document fixes when the event is emitted and what values it carries (`event.lost`'s `event` is the lost event's id and its `cause` that event's cause, empty when it has none, both D18's; `tool.called`'s `kind` and `outcome` value sets are D08's: `kind` is `read`, `additive`, or `destructive`, and `outcome` is `ok`, `error`, `invalid_arguments`, `panicked`, or `unencodable_output`). A service records its own events under its own names and does not emit these.

## REQUIREMENTS

- R-UTJ9-KA7E: Package `telemetry` MUST export `type Attrs map[string]any`.
- R-UUR5-Y1Y3: Package `telemetry` MUST export `type Event struct { Time time.Time; Service, Name, RequestID, User string; Attrs Attrs }`, with exactly these fields in this order, where `time` is the standard library's `time`.
- R-UVZ2-BTOS: Package `telemetry` MUST export the method `func (e Event) MarshalJSON() ([]byte, error)`.
- R-KKP5-ILEU: A valid event name MUST be exactly a string that matches the Go `regexp` pattern `^[a-z][a-z0-9]*(_[a-z0-9]+)*(\.[a-z][a-z0-9]*(_[a-z0-9]+)*)+$`.
- R-UZMR-H4WV: A valid attribute key MUST be exactly a string that matches the Go `regexp` pattern `^[a-z][a-z0-9]*(_[a-z0-9]+)*$`.
- R-V0UN-UWNK: A valid attribute value MUST be exactly a non-nil value whose dynamic type's `reflect.Kind` is `Bool`, `String`, `Int`, `Int8`, `Int16`, `Int32`, `Int64`, `Uint`, `Uint8`, `Uint16`, `Uint32`, `Uint64`, `Float32`, or `Float64`, and, for the two floating-point kinds, whose value is neither NaN nor an infinity; its basic form MUST be the value converted to `bool` for kind `Bool`, `string` for kind `String`, `int64` for the signed integer kinds, `uint64` for the unsigned integer kinds, and `float64` for the floating-point kinds.
- R-V22K-8OE9: When `e.Service` is not empty, `e.Name` is a valid event name, every key of `e.Attrs` is a valid attribute key, every value of `e.Attrs` is a valid attribute value, and the year of `e.Time.UTC()` is between 0 and 9999 inclusive, `Event.MarshalJSON` MUST return a nil error and bytes identical to what `json.Marshal` (the standard library's `encoding/json`) returns for a value of type `struct { Time string "json:\"time\""; Service string "json:\"service\""; Event string "json:\"event\""; RequestID string "json:\"request_id\""; User string "json:\"user\""; Attrs map[string]any "json:\"attrs\"" }` whose `Time` is `e.Time.UTC().Format("2006-01-02T15:04:05.000000Z")`, whose `Service`, `Event`, `RequestID`, and `User` are `e.Service`, `e.Name`, `e.RequestID`, and `e.User`, and whose `Attrs` is a non-nil map holding each key of `e.Attrs` with the basic form of its value, empty when `e.Attrs` is nil or empty.
- R-V3AG-MG4Y: `Event.MarshalJSON` MUST return a nil slice and a non-nil error when `e.Service` is empty, `e.Name` is not a valid event name, a key of `e.Attrs` is not a valid attribute key, a value of `e.Attrs` is not a valid attribute value, or the year of `e.Time.UTC()` is below 0 or above 9999, and MUST NOT panic for any `Event`.
- R-VALU-X2L4: Every `service.started` event appkit produces MUST have `Attrs` holding exactly the key `version`, whose value is a `string`.
- R-VBTR-AUBT: Every `service.stopping` event appkit produces MUST have `Attrs` holding exactly the key `reason`, whose value is a `string`.
- R-VD1N-OM2I: Every `request.started` event appkit produces MUST have `Attrs` holding exactly the keys `method` and `path`, both `string` values, where `method` is the request's method as received and `path` is the request URL's path as the standard library's `url.URL.Path` holds it, with no query.
- R-PRAG-ZMSZ: Every `request.finished` event appkit produces MUST have `Attrs` holding exactly the keys `status`, `duration_us`, `request_bytes`, and `response_bytes`, all `int64` values, where `status` is the status D14 records for the request (R-3HP0-MB1R), `duration_us` is a number of whole microseconds no less than zero, and `request_bytes` and `response_bytes` are the numbers of bytes D14 records for the request (R-PW62-IPRR, R-PXDY-WHIG), each no less than zero.
- R-VFHG-G5JW: Every `sibling.called` event appkit produces MUST have `Attrs` holding exactly the keys `target`, `method`, `path`, `status`, and `duration_us`, where `target` (the called service's name), `method` (the outgoing request's method), and `path` (the outgoing request URL's path as `url.URL.Path` holds it, with no query) are `string` values, and `status` and `duration_us` are `int64` values, `status` being the HTTP status code of the sibling's response, or 0 when no response was received, and `duration_us` a number of whole microseconds no less than zero.
- R-VHX9-7P1A: Every `tool.called` event appkit produces MUST have `Attrs` holding exactly the keys `tool`, `kind`, `outcome`, and `duration_us`, where `tool`, `kind`, and `outcome` are `string` values and `duration_us` is an `int64` number of whole microseconds no less than zero.
