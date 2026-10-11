# D07-trail

The artifact events: the three kinds of event artifacts emits on the suite's event bus, their declaration to the events app, and the record of each in artifacts' own trail. It is webhooks' trail with artifacts' events and one difference of naming: the names are fixed words, not a family keyed by the artifact.

## The three artifact events

An artifact's life is told by `artifact.created`, `artifact.replaced` and `artifact.deleted`. Each is made by the one change that makes it: a `PUT /files/<path>` that creates the path or replaces its bytes (`D05-files`), a `DELETE /files/<path>` or a `delete` call (`D05-files`, `D08-tools`), and the deletions a sweep of expired artifacts makes (`D03-serve`). Each event carries four attributes, in this order in its declaration: `artifact`, the artifact's id; `path`; `bytes`, the count of its bytes as an integer; and `content_type`, as stored. Nothing else travels: no byte of a body, in whole or part, no email, no hash and no request header. A consumer that wants the bytes fetches `/files/<path>` with its own credential.

Every artifact event goes two ways at once, under the same name, attributes, request id and user: recorded in the trail through the run's writer, and emitted on the bus through the run's emitter. It takes the context of the change: for a request, its request id, its caller and the event cause `events.Middleware` read from it; for a swept artifact, its owner, an empty request id and no cause (`D03-serve`).

## The orphan record

A record the start's reconcile removes because its file is gone (`D03-serve`, `D04-store`) is not a change any caller made, so it is not told on the bus: a bus event would start subscribers' runs for it. It is recorded in the trail alone, through the writer, as `artifact.orphaned`, with the same four attributes, under the owner's user id and an empty request id. `artifact.orphaned` is not declared at `/declarations`, and the bus never carries it.

## When an event is made

An event is made after the change it tells of is committed, never before, and never for a change that did not happen: a refused or failed put or delete makes none, and an `import`, which changes nothing (`D09-import`), makes none either. A crash between the commit and the emit loses the event while the change stands. This is the window every sibling accepts, since appkit's emitter keeps no outbox; it is stated, not prevented.

## The declarations

`trail.Emits()` declares the three names, literal, each with the four attribute keys in order. The run's emitter is built with them, and `/declarations` publishes them (`D03-serve`).

## Recorded decisions

- Fixed names in place of webhooks' `webhook.<slug>.<kind>`: a path is not an event-name word, and a family keyed by path would be unbounded, as the decisions document says. A subscriber picks artifacts by `artifact.*` or by one name, and by the `path` attribute in its own code.
- One function, `Record`, serves every kind, since the three carry the same attributes; webhooks' `Name` has no counterpart.
- `bytes` is an integer attribute, the store's `int64`; appkit carries any Go integer as a JSON number.
- `artifact.orphaned` is trail-only by the orchestrator's decision: the decisions document asks that an orphan row be recorded in the trail, and the three bus names are reserved for changes callers make. It carries the owner as its user so a trail query for that user shows what became of the artifact.
- The crash window between commit and emit is accepted, not closed (decisions document, decision 11).

## Consumer tasks

The files surface, after `st.Put` answers a create, calls `trail.Record(r.Context(), cfg.Telemetry, cfg.Events, trail.Created, a)`, and `trail.Replaced` after a replace and `trail.Deleted` after `st.Delete`; the tools do the same under the call's context; the sweeper calls `trail.Record(identity.NewContext(bg, identity.Caller{UserID: a.OwnerID, Email: a.OwnerEmail}), w, em, trail.Deleted, a)`, `bg` being a context that carries no caller and no event cause, so the events have the owner's user id, an empty request id and no cause (`D03-serve`), for each artifact `Sweep` answers; `Run` calls `trail.Orphan(identity.NewContext(bg, identity.Caller{UserID: a.OwnerID, Email: a.OwnerEmail}), w, a)` for each artifact `Reconcile` answers. `Run` builds its emitter with `events.Config{Emits: trail.Emits(), ...}`. A test builds a writer over a `*telemetry.Capture` and an emitter over an `*events.Capture` with `trail.Emits()`, calls `trail.Record` under a context it built with `identity.NewContext` and, for the cause, `events.NewContext`, and compares the captured events' `Name`, `Attrs`, `RequestID`, `User`, `Cause` and `Depth`.

## REQUIREMENTS

- R-4PEU-O9QI: The `internal/trail` package, imported from the path `github.com/ikigenba/ikigenba/artifacts/internal/trail` with the package name `trail`, MUST export the string constants `Created = "artifact.created"`, `Replaced = "artifact.replaced"`, `Deleted = "artifact.deleted"` and `Orphaned = "artifact.orphaned"`, `func Emits() []events.Emission`, `func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, a store.Artifact)` and `func Orphan(ctx context.Context, w *telemetry.Writer, a store.Artifact)`, where `events` and `telemetry` are appkit's packages of those names and `store` is artifacts' `internal/store`.
- R-C0BC-DA8N: `trail.Emits` MUST return, on every call, a new slice of exactly three `events.Emission` values, `artifact.created`, `artifact.replaced` and `artifact.deleted` in that order, each with the `Attrs` `artifact`, `path`, `bytes`, `content_type` in that order.
- R-C2R5-4TQ1: `trail.Record` with `name` one of `Created`, `Replaced` and `Deleted` MUST record through `w` exactly one event and emit through `em` exactly one bus event, both named `name`, both with `Attrs` exactly `artifact`, `a.ID`; `path`, `a.Path`; `bytes`, `a.Bytes` as an integer; and `content_type`, `a.ContentType`; and both with the `RequestID` and `User` of the caller `identity.FromContext(ctx)` answers, each empty when it answers none; and with any other `name` it MUST record and emit nothing.
- R-C3Z1-ILGQ: The bus event `trail.Record` emits MUST carry as its `Cause` and `Depth` the event cause `events.FromContext(ctx)` answers, its `ID` and its `Depth` plus one, and an empty `Cause` and `Depth` 0 when `ctx` carries none.

- R-4QMR-21H7: `trail.Orphan` MUST record through `w` exactly one event named `Orphaned`, with `Attrs` exactly `artifact`, `a.ID`; `path`, `a.Path`; `bytes`, `a.Bytes` as an integer; and `content_type`, `a.ContentType`; and the `RequestID` and `User` of the caller `identity.FromContext(ctx)` answers, each empty when it answers none; and `trail.Record` with `name` `Orphaned` MUST record and emit nothing, so that no bus event is ever named `artifact.orphaned`.
