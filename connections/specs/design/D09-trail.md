# D09-trail

The connection events: the three kinds of event connections emits on the suite's event bus, their declaration to the events app, the record of each in connections' own trail, and the one trail-only record of a revoke the provider did not honour. It is artifacts' trail with connections' events: fixed names, one function for the three bus kinds, and one trail-only record beside them.

## The three connection events

A connection's life is told by `connection.created`, `connection.deleted` and `connection.broken`. Each is made by the one change that makes it: a callback that stores a grant, first connect and reconnect alike (`D06-connect`); a disconnect from the landing page's form (`D08-pages`) or the `disconnect` tool (`D10-tools`), both through the broker's disconnect (`D07-token-and-disconnect`); and a refresh the provider refuses on the token route (`D07-token-and-disconnect`). Each event carries three attributes, in this order in its declaration: `provider`, the provider's name (`google`); `account`, the connected account's email; and `scopes`, the granted scopes as stored, joined with single spaces. Nothing else travels: no token, code, verifier, state value or secret, in whole or part, no user email and no connection id. A consumer that wants a token asks the token route on the host with the user's identity.

Every connection event goes two ways at once, under the same name, attributes, request id and user: recorded in the trail through the run's writer, and emitted on the bus through the run's emitter. It takes the context of the change: its request id, its caller and the event cause `events.Middleware` read from it. A connection is private to the user who made it and every route and tool acts only on the caller's own (`D04-store`), so the caller of a change is always the connection's owner; `connection.broken` is told as the owner because the sibling that asked the token route asked as that user.

## The revoke-failure record

A disconnect revokes the grant at the provider and deletes the connection whatever the provider answers (`D07-token-and-disconnect`). A revoke the provider refuses or that fails is not a change any subscriber should act on, so it is not told on the bus; it is recorded in the trail alone, through the writer, as `connection.revoke_failed`, with the attributes `provider`, `account` and `status`, the provider's HTTP status as an integer, 0 when no answer came. It is not declared at `/declarations`, and the bus never carries it.

## When an event is made

An event is made after the change it tells of is committed, never before, and never for a change that did not happen: a refused callback makes none, a token answered from the store or refreshed makes none, and a disconnect with no connection makes none. Which change calls which function is the caller's requirement (`D06-connect`, `D07-token-and-disconnect`); this file says what each call records and emits. A crash between the commit and the emit loses the event while the change stands, the window artifacts states and every sibling accepts.

## The declarations

`trail.Emits()` declares the three names, literal, each with the three attribute keys in order. The run's emitter is built with them, and `/declarations` publishes them (`D03-serve`).

## Recorded decisions

- Fixed names and one `Record` for the three bus kinds, as artifacts: the three carry the same attributes.
- `scopes` is one space-joined string, not a list, as the decisions document (decision 11) says, since an event attribute a script matches on is simplest as a string; a connection with no stored scope gives the empty string.
- `connection.revoke_failed` is trail-only, by the brief's decision 7 on the decisions document's decision 8 ("a failed revoke is recorded in the trail"), following artifacts' `artifact.orphaned`: a function of its own, `RevokeFailure`, which takes the status, and `Record` given its name records and emits nothing.
- `status` is the provider's HTTP status as an integer attribute, 0 for an unreachable provider or one whose answer was not read; it says which of the broker's two failure kinds (`D05-providers`) the revoke met without carrying any byte of the provider's body, which could echo the token.
- User and request id come from the caller on the context, as artifacts; the owner for `connection.broken` is the caller by construction (above), so no separate owner argument exists. This is the reading of decision 11's "as the owner for broken" that needs no new shape.
- The crash window between commit and emit is accepted, not closed, as artifacts.

## Consumer tasks

The callback, after `Store.Upsert` commits, calls `trail.Record(r.Context(), w, em, trail.Created, c)` with the stored connection; the broker's disconnect, after `Store.Delete` answers the deleted connection, calls `trail.Record(ctx, w, em, trail.Deleted, c)`, and before or after it, when the revoke was refused or failed, `trail.RevokeFailure(ctx, w, c, status)`; the token route, after `Store.MarkBroken` commits, calls `trail.Record(r.Context(), w, em, trail.Broken, c)`. `Run` builds its emitter with `events.Config{Emits: trail.Emits(), ...}`. A test builds a writer over a `*telemetry.Capture` and an emitter over an `*events.Capture` with `trail.Emits()`, calls `trail.Record` and `trail.RevokeFailure` under a context it built with `identity.NewContext` and, for the cause, `events.NewContext`, and compares the captured events' `Name`, `Attrs`, `RequestID`, `User`, `Cause` and `Depth`.

## REQUIREMENTS

- R-A9R2-BZGJ: The `internal/trail` package, imported from the path `github.com/ikigenba/ikigenba/connections/internal/trail` with the package name `trail`, MUST export the string constants `Created = "connection.created"`, `Deleted = "connection.deleted"`, `Broken = "connection.broken"` and `RevokeFailed = "connection.revoke_failed"`, `func Emits() []events.Emission`, `func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, c store.Connection)` and `func RevokeFailure(ctx context.Context, w *telemetry.Writer, c store.Connection, status int)`, where `events` and `telemetry` are appkit's packages of those names and `store` is connections' `internal/store`.
- R-AAYY-PR78: `trail.Emits` MUST return, on every call, a new slice of exactly three `events.Emission` values, `connection.created`, `connection.deleted` and `connection.broken` in that order, each with the `Attrs` `provider`, `account`, `scopes` in that order.
- R-AC6V-3IXX: `trail.Record` with `name` one of `Created`, `Deleted` and `Broken` MUST record through `w` exactly one event and emit through `em` exactly one bus event, both named `name`, both with `Attrs` exactly `provider`, `c.Provider`; `account`, `c.AccountEmail`; and `scopes`, the elements of `c.Scopes` in order joined with single spaces, the empty string when it has none; and both with the `RequestID` and `User` of the caller `identity.FromContext(ctx)` answers, each empty when it answers none; and with any other `name` it MUST record and emit nothing.
- R-ADER-HAOM: The bus event `trail.Record` emits MUST carry as its `Cause` and `Depth` the event cause `events.FromContext(ctx)` answers, its `ID` and its `Depth` plus one, and an empty `Cause` and `Depth` 0 when `ctx` carries none.
- R-AFUK-8U60: `trail.RevokeFailure` MUST record through `w` exactly one event named `RevokeFailed`, with `Attrs` exactly `provider`, `c.Provider`; `account`, `c.AccountEmail`; and `status`, `status` as an integer; and the `RequestID` and `User` of the caller `identity.FromContext(ctx)` answers, each empty when it answers none; and it MUST emit nothing on any emitter; and `trail.Record` with `name` `RevokeFailed` MUST record and emit nothing, so that no bus event is ever named `connection.revoke_failed`.
