# D05-widgets

The widgets themselves: what one is, what makes one acceptable, and where the
set of them is kept. This is the whole of `internal/widget`, and it is the
part of dummy that has nothing to do with being a web server or an MCP server.
No request, no response, no header, no markup and no JSON reaches this
package; it is reached from `internal/panel` (the form, `D07-form`, the page
and the table, `D04-panel` and `D06-table`) and from `internal/tools` (the MCP
tools, `D09-mcp`), and it names nothing of either. Of appkit it uses one
package, `db`, whose handle the store is built over; the one appkit interface
a widget type satisfies, `mcp.Enumerator`, is satisfied structurally, by a
method with the right shape. The import direction runs one way, from those
packages to this one, which is what lets every rule below be decided by
calling a function rather than by driving a handler or a client.

A widget is four fields: an id, a name, a whole-number count, and a status
that is one of three words; the `Widget` type also carries its `Seq`, its
place in creation order, which no page and no tool shows. The status is an enumeration rather than a bare string so
that the three words exist in exactly one place, and `Statuses` hands them out
in the order the form offers them. It is a function and not a package-level
slice because a slice would be mutable shared state. `Status` also has an
`Enum` method returning the same three words as plain strings, in the same
order. That is appkit's `mcp.Enumerator` (appkit `D08`): a tool argument or
result field of type `Status` is described to an MCP client by a schema whose
`enum` is those three words, and appkit's decoder refuses any other value
before dummy sees it. appkit calls `Enum` on the zero value of the type, so
the answer cannot depend on the receiver, and does not.

The set lives in dummy's SQLite database, `state/dummy.db`, opened through
appkit's `db.Open` (appkit's D15 and D16 own opening, migrations and
transactions; nothing here restates them). `D03-serve` opens it at start with
the migrations the root package's `Migrations` carries (`D01`) and builds the
store over the handle. The store is the widgets' only reader and writer, and
it neither owns nor closes the handle: whoever opened it closes it. Because the
widgets are rows, not memory, they outlive the handle and the process. A store
built over a later handle on the same file lists exactly the widgets created
before, with the same ids, names, counts and statuses, in the same order, and
a widget created through it comes after them. A database that dummy has just
created holds no widgets: there are no fixtures.

The store never hands out the whole set. It answers one window at a time:
`List` returns, oldest first, at most `n` widgets, those after a given
position, or the first ones when no position is given. This is the suite's
storage rule (`shape/`, Storage; appkit's D25): a big list is filtered, ordered
and windowed by the database, so the query carries the position in its
`WHERE`, the order in its `ORDER BY` and the window in its `LIMIT`, the
`seq` primary key backs the order, and no caller ever holds more than the
window it asked for. dummy is the precedent `shape/` names for it: a SQL
window keyed by `seq`, `WHERE seq > ? ORDER BY seq LIMIT ?`. The position is
a widget's `Seq`, the `seq` of its row, which the `Widget` type now carries so
that a caller can ask for the window after the last widget it was given; the
tool and the panel turn it into the opaque token of appkit's `paging`
(`D09-mcp`, `D04-panel`), and the store knows nothing of tokens, limits or
pages. A caller that wants one page and to know whether more follow asks for
one more than the page. A window of fewer than one widget is empty, never
"no limit", whatever SQLite makes of a negative `LIMIT`. Each widget a store
adds gets a `Seq` greater than every one it already holds, so ascending
`Seq` is creation order and a new widget is last. A store whose greatest
`Seq` is the maximum `int64` has no greater one to give, so `Create` answers
it, whatever the draft, with the error it gives when it cannot reach the
widgets, and adds nothing; `List` and `Check` still answer. `List` hands back
a fresh slice each time.

The earlier store had `All`, which read every row into one slice; the
requirements and the panel rested on it. It contradicted the storage rule,
so it is gone, and "the widgets of a store" (`D04-panel`), which every
before-and-after comparison in the design uses, is now defined as the
successive windows of `List` from the start, read back to back. That is what
makes "the set was left exactly as it was, down to its order" something a
test can decide without a back door.

dummy's schema is part of this design because it is dummy's own: the one
baseline migration, `0001`, creates the table `widgets` with the columns
`seq`, the integer primary key that fixes creation order, then `id`, `name`,
`count` and `status`, the four fields of a widget, all required. It creates the
table only if it does not exist and seeds nothing, so a database that already
holds a `widgets` table of that shape, rows included, but has never been
through appkit's migrations, is adopted as it is. A test proves the schema by
opening a fresh database in its own temporary directory with
`dummy.Migrations()` and reading the catalogue inside a `DB.Read`; it proves
the adoption by building such a database (through `db.Open` with migrations of
its own, then dropping `schema_migrations`), opening it again with dummy's,
and listing the rows through a store. That id and name are unique is proved
by the store's behavior, not by inspecting indexes.

Every widget has an id, given by the store when the widget is made and never
changed: `wgt_` and 16 lowercase hexadecimal digits, the encoding of 8 bytes
read from the store's random source. The id is how dummy's telemetry trail
names a widget, since an event never carries a widget's name, count or status
(appkit's D11 registers the prefix `wgt_` for the entity type `widget`, owned
by dummy). It is drawn at random rather than counted so that two widgets, in
one run or across runs, never share one in practice. The source is a parameter
of `NewStore` so a test hands in known bytes and knows every id in advance;
the running binary passes nil, which means `crypto/rand.Reader`. The store
reads 8 bytes for each widget it makes, none for a refused creation, and none
for `Check` or `List`, and never from two goroutines at once, so a test's
`bytes.Reader` is a safe source. Should a draw repeat an id the store already
holds, in this process or from an earlier one, it draws again; should the
source fail, the store takes the bytes from `crypto/rand` instead, so a widget
is never refused for want of an id. The id is shown to MCP clients (`D09-mcp`)
and is not shown on the panel or in the table.

Two callers create widgets, and they arrive with different material. The form
receives three strings exactly as a browser or `curl` sent them; an MCP
client's arguments arrive already typed, because appkit decoded them against
the tool's input schema — the count is an `int`, the status is a `Status` from
the enumeration. So creation is split in two layers, and the rules for a
widget are stated once, over typed values, where both callers meet.

The first layer is the form's alone: `ParseSubmission` turns a `Submission` —
the three strings, held raw, untrimmed and unshortened, because the rejection
re-display `D07-form` owns has to echo back what the caller typed — into a
`Draft`, and reports what cannot be turned into a typed value at all. The
count, trimmed, follows Go's `strconv.Atoi` grammar, and the
not-a-whole-number message is keyed on `Atoi` returning a non-nil error rather
than on "does not parse": `Atoi` returns both an error and a clamped value for
a number too large to hold, and a huge negative number must not be routed to
the negative-count message by accident. The status, trimmed, must be one of
the three words exactly, checked rather than trusted, because a caller with
`curl` sends whatever they like. The name is copied across untouched: whether
a name is acceptable is a rule about the typed value, not a parse, so parsing
never complains about one. A field that fails to parse is zero in the `Draft`.
Parsing touches no database.

The second layer is the rules, and both callers enter it with a `Draft`. A
`Draft` holds typed values with the name as given; the rules trim it
themselves. That keeps one trimming rule where the name is judged and lets the
MCP path pass the argument it received without preparing it. The name is
required and is at most forty runes, counted in runes and not in bytes,
because it is a field a person typed and a person who typed forty accented
letters typed forty characters. A whitespace-only name is the required-field
rejection, not a widget with a blank name. Uniqueness is exact and
case-sensitive on the trimmed value, so `alpha` and `Alpha` are two widgets;
it is also the one rule that cannot be decided from the values alone, which is
why the rules are methods on the store. Zero is a fine count; below zero is
not. The status must be one of the three; an MCP caller cannot break that
rule, since appkit's decoder already refused every other value, and a form
caller whose status did not parse has the zero `Status`, which breaks it with
the same message parsing gave.

`Check` applies the rules and changes nothing. `Create` applies exactly the
same rules and, when they all hold, adds the widget — and asking the set and
adding to it must be one step, which is why `Create` is not merely `Check`
followed by an add. The form uses `Check` to report every offence of a
submission that also failed to parse (`D07-form`); the MCP tool and the form's
accepted path go straight to `Create`.

Every field is judged, and every field that is wrong comes back in the same
answer — a caller who got three things wrong learns all three at once. That is
why the outcome is a struct with one string per field rather than a list or a
map: one message per field is structural, and a renderer cannot be handed two
messages for one field or a message for a field that does not exist.
`ParseSubmission`, `Check` and `Create` all report in that one shape. The
message text lives here, beside the rule that raises it, rather than in the
packages that render it: the form draws a message beside its field, and the
MCP tool writes it after the field's name in its error text, and both draw the
same words, so a reason code mapped to text somewhere else would only give the
rule and its wording room to drift apart. A rejected creation creates nothing
and leaves the set exactly as it was, order included.

`List`, `Check` and `Create` each take a context and return an error beside
their answer, because each reaches the database. A non-nil error is one
kind, with no sentinel to match: the widgets could not be reached, or, from
`Create` alone, could not take another widget because no greater `Seq` is
left (above). The field messages mean nothing beside one. Every consumer
answers it the same way, with the one line
`Unreachable`, declared here as the messages are so the page, the table, the
form and the tools cannot word it differently; how each answers (a 503, an
MCP error result) is theirs. A test provokes it with the handle's
`SetFailing(true)`, under which every call on the store errors and a `Create`
stores nothing; once the switch is off again the store answers as before, with
the widgets it held.

The store is used concurrently. The panel page polls the table fragment while
a submission is being created, and an MCP client may create a widget while a
browser submits the form, so several goroutines reach one store at once, and
the design says so as an invariant rather than leaving it to be discovered.
Gate 4 runs the race detector, which is what proves it, and two further
requirements cover the half a race detector cannot see: concurrent accepted
creations all survive, none is lost to the other, and no two widgets in the
set ever carry the same name however many creations race. That second one is
the invariant — checking the name and adding the widget is one step with
respect to every other creation — and without it two callers creating one name
could each find it absent and each be accepted.

Everything about HTTP and MCP is elsewhere. The panel page and its banner are
`D04-panel`, the table is `D06-table`, the form, its per-field error placement
and the answers to a submission are `D07-form`, and the tools are `D09-mcp`.
Those documents name `Submission`, `Draft`, `FieldErrors`, `ParseSubmission`,
`NewStore`, `Store.List`, `Store.Check`, `Store.Create`, `Status.Enum`, the six
messages and `Unreachable`; this one declares them.

## REQUIREMENTS

- R-ZMK6-SZTW: The `internal/widget` package MUST export `type Status string` together with these values of that type: `StatusActive Status = "active"`, `StatusPaused Status = "paused"`, and `StatusRetired Status = "retired"`.
- R-VLG5-AAL9: The `internal/widget` package MUST export `func Statuses() []Status`.
- R-AN9G-B7UI: The `internal/widget` package MUST export `func (s Status) Enum() []string`.
- R-QI8L-W6P7: The `internal/widget` package MUST export `type Widget` as a struct with the fields `ID string`, `Name string`, `Count int`, `Status Status`, and `Seq int64`.
- R-W66F-SE72: The `internal/widget` package MUST export `const MaxNameRunes = 40`.
- R-ISI4-P0AR: The `internal/widget` package MUST export `type Submission` as a struct with the fields `Name string`, `Count string`, and `Status string`.
- R-APP9-2RBW: The `internal/widget` package MUST export `type Draft` as a struct with the fields `Name string`, `Count int`, and `Status Status`.
- R-EBQ5-GVM3: The `internal/widget` package MUST export `type FieldErrors` as a struct with the fields `Name string`, `Count string`, and `Status string`, each holding at most one message for the field it names, the empty string meaning that field was accepted.
- R-WPOT-WQ26: The `internal/widget` package MUST export `func (e FieldErrors) Any() bool`.
- R-G7RO-91KZ: The `internal/widget` package MUST export six string constants, each a copy constant whose value is not empty: `const NameRequiredMessage string`, `const NameTooLongMessage string`, `const NameTakenMessage string`, `const CountNotWholeMessage string`, `const CountNegativeMessage string`, and `const StatusNotAllowedMessage string`.
- R-EXKM-XZU8: The `internal/widget` package MUST export `const Unreachable string`, a copy constant.
- R-E8TO-J993: The `internal/widget` package MUST export `type Store` and `func NewStore(d *db.DB, src io.Reader) *Store`, where `db` is appkit's package `github.com/ikigenba/ikigenba/appkit/db` and `io` is the standard library's `io`; the handle passed as `d` is the store's **handle**, and the reader passed as `src` is the store's **source**.
- R-QJGI-9YFW: The `internal/widget` package MUST export `func (s *Store) List(ctx context.Context, after *int64, n int) ([]Widget, error)`, where `context` is the standard library's `context`.
- R-EMP8-WTAC: The `internal/widget` package MUST export `func ParseSubmission(sub Submission) (Draft, FieldErrors)`.
- R-EB9H-ASQH: The `internal/widget` package MUST export `func (s *Store) Check(ctx context.Context, d Draft) (FieldErrors, error)`, where `context` is the standard library's `context`.
- R-ECHD-OKH6: The `internal/widget` package MUST export `func (s *Store) Create(ctx context.Context, d Draft) (Widget, FieldErrors, error)`, where `context` is the standard library's `context`.
- R-EL0O-CYO1: When nothing exists at a path, `db.Open` called with a `db.Config` whose `Path` is that path and whose `Migrations` is the root package's `dummy.Migrations()` MUST return a non-nil `*db.DB` and a nil error, and the tables `sqlite_master` then lists whose names do not begin with `sqlite_` MUST be exactly `schema_migrations` and `widgets`.
- R-EM8K-QQEQ: After `db.Open` with `dummy.Migrations()` returns a non-nil `*db.DB` for a path where nothing existed, `PRAGMA table_info(widgets)` MUST report exactly five columns, in this order: `seq`, declared type `INTEGER` and the primary key; `id`, declared type `TEXT`, not null, not part of the primary key; `name`, declared type `TEXT`, not null, not part of the primary key; `count`, declared type `INTEGER`, not null, not part of the primary key; and `status`, declared type `TEXT`, not null, not part of the primary key.
- R-QN47-F9NZ: A store over a handle that `db.Open` with `dummy.Migrations()` returned for a path where nothing existed MUST, before any `Create` call on a store over that database, return from `List` a nil error and a slice of length zero, whatever `after` and `n` it is passed.
- R-QOC3-T1EO: When a path names a SQLite database file that holds a table `widgets` whose columns are those R-EM8K-QQEQ lists, holding rows whose `status` is the string value of one of `StatusActive`, `StatusPaused` and `StatusRetired`, and that holds no table `schema_migrations`, `db.Open` with that path and `dummy.Migrations()` MUST return a non-nil `*db.DB` and a nil error, and the widgets (`D04-panel`) of a store over that handle MUST be exactly one `Widget` for each of those rows, in ascending order of `seq`, whose `ID`, `Name`, `Count`, `Status` and `Seq` are that row's `id`, `name`, `count`, `status` and `seq`.
- R-EG52-TVP9: Every widget `Create` adds MUST have an `ID` that is `wgt_` followed by `hex.EncodeToString` (the standard library's `encoding/hex`) of 8 bytes, which, unless R-L0V4-B9LO or R-EIKV-LF6N applies, are the 8 bytes one `io.ReadFull` call on the store's source returns.
- R-QPK0-6T5D: When `Create` adds a widget, it MUST make one `io.ReadFull` call for 8 bytes on the store's source, giving the `ID` of that widget, unless R-L0V4-B9LO or R-EIKV-LF6N applies; `Create` MUST NOT read from the source when it returns a nil error and a `FieldErrors` with any of its three fields non-empty, nor when it returns a non-nil error while `SetFailing(true)` is in force on the store's handle; `Check` and `List` MUST NOT read from the source.
- R-L0V4-B9LO: When the `ID` the 8 bytes read for a widget would give is the `ID` of a widget the store already holds, the store MUST make another `io.ReadFull` call for 8 bytes on its source and use those instead, until the `ID` is held by no widget of the store, so that no two widgets a store holds share an `ID`.
- R-EIKV-LF6N: When an `io.ReadFull` call on the store's source returns a non-nil error, the store MUST still give the widget an `ID` of the form R-EG52-TVP9 states, held by no other widget of the store, from 8 bytes that do not depend on any bytes that call read, and MUST otherwise behave as when the call succeeds, so that `Create` still adds its widget.
- R-L3AX-2T32: A store MUST NOT read from its source from two goroutines at once, whatever calls are made on it concurrently, so that a source that is not safe for concurrent use, such as a `*bytes.Reader`, is safe to pass to `NewStore`.
- R-EJSR-Z6XC: When `NewStore` is passed a nil `src`, it MUST NOT panic, every widget `Create` adds through its store MUST have an `ID` of the form R-EG52-TVP9 states, held by no other widget of the store, and the `ID` of the first widget created through each of two stores so made, over two databases that held no widgets, MUST differ.
- R-XV5E-WX9S: `Statuses` MUST return a slice of exactly three elements whose values are `StatusActive`, `StatusPaused`, and `StatusRetired`, in that order.
- R-YH3L-SSMA: Each call to `Statuses` MUST return a slice the caller may modify in place without changing the values a later call to `Statuses` returns.
- R-EU0N-7FQI: `Enum`, called on any `Status` value, the zero `Status` and values equal to none of `StatusActive`, `StatusPaused` and `StatusRetired` included, MUST return a slice of exactly three strings: the string values of the three elements `Statuses` returns, in the order `Statuses` returns them.
- R-EWGF-YZ7W: Each call to `Enum` MUST return a slice the caller may modify in place without changing the values a later call to `Enum` or to `Statuses` returns.
- R-Z1TW-AW83: `Any` MUST return true when at least one of its receiver's three fields is a non-empty string, and false when all three are the empty string.
- R-QQRW-KKW2: `List` MUST return a slice the caller may modify in place, append to, or discard without changing the widgets the store holds or the values a later call to `List` returns.
- R-QLWB-1HXA: Every widget `Create` adds MUST have a `Seq` greater than the `Seq` of every widget the store held before it was added, and the `Widget` that `Create` returns for it MUST carry that `Seq`, so that ascending order of `Seq` is the order in which `Create` calls accepted their widgets.
- R-QKOE-NQ6L: When `List` returns a nil error, it MUST return, in ascending order of `Seq`, the widgets the store holds whose `Seq` is greater than `*after`, or every widget the store holds when `after` is nil: when `n` is at least 1, the first `n` of them, or all of them when they number `n` or fewer; and when `n` is less than 1, none, so that the slice is then of length zero.
- R-QRZS-YCMR: When widgets were created through a store over a handle that was then closed, the widgets (`D04-panel`) of a store over a later handle that `db.Open` returned for the same path with `dummy.Migrations()` MUST be those widgets, each with the same `ID`, `Name`, `Count`, `Status` and `Seq` and in the same order, and a widget a `Create` call through the later store then accepts MUST be the last element of the widgets of that later store afterwards.
- R-EN7X-KS77: When the store's handle has not been closed and either `SetFailing(true)` was never called on it or a `SetFailing(false)` call on it returned after the last `SetFailing(true)` call on it, and the context passed is not done, every call to `List` and `Check` on the store MUST return a nil error, whatever `after`, `n` and `Draft` are passed, and so MUST every call to `Create` on the store made while no other `Create` call on the store is in progress and the store holds no widget whose `Seq` is `math.MaxInt64` (the standard library's `math`), whatever `Draft` is passed, whatever calls on the store returned an error before.
- R-LLH8-GUB2: While the store holds a widget whose `Seq` is `math.MaxInt64` (the standard library's `math`), every call to `Create` on the store MUST return a non-nil error, whatever `Draft` is passed, one whose `Count` and `Status` the rules accept and whose trimmed name is not empty, is at most `MaxNameRunes` runes long and is the `Name` of no widget the store holds included.
- R-QUFL-PW45: While `SetFailing(true)` is in force on a store's handle, every call to `List`, `Check` and `Create` on that store MUST return a non-nil error, whatever `after`, `n` and `Draft` are passed.
- R-QVNI-3NUU: When `Create` returns a non-nil error, it MUST add no widget: the widgets (`D04-panel`) of the store when it is first reachable after the call, as when `SetFailing(true)` was called on the store's handle before the call and `SetFailing(false)` after it, MUST be equal, element for element and in the same order, to the widgets of the store when it was last reachable before the call, no other call on the store being made in between.
- R-F7FJ-EWW5: `ParseSubmission` MUST return a `Draft` whose `Name` is exactly the `Name` field of the `Submission` it was given, untrimmed and otherwise unaltered, and a `FieldErrors` whose `Name` is the empty string, whatever that `Submission` holds.
- R-F9VC-6GDJ: The trimmed count of a `Submission` is the result of applying `strings.TrimSpace` to its `Count` field; when `strconv.Atoi` of the trimmed count returns a non-nil error — which includes the empty string, and includes a value outside the range of `int`, for which `Atoi` returns a non-nil error alongside a clamped value — `ParseSubmission` MUST return a `FieldErrors` whose `Count` is `CountNotWholeMessage` and a `Draft` whose `Count` is 0.
- R-FCB4-XZUX: When `strconv.Atoi` of the trimmed count of a `Submission` returns a nil error, `ParseSubmission` MUST return a `FieldErrors` whose `Count` is the empty string and a `Draft` whose `Count` is the value `Atoi` returned, a value less than zero included.
- R-FFYU-3B30: The trimmed status of a `Submission` is the result of applying `strings.TrimSpace` to its `Status` field; when the trimmed status is equal, byte for byte, to the string value of one of `StatusActive`, `StatusPaused` and `StatusRetired`, `ParseSubmission` MUST return a `FieldErrors` whose `Status` is the empty string and a `Draft` whose `Status` is that one, and otherwise MUST return a `FieldErrors` whose `Status` is `StatusNotAllowedMessage` and a `Draft` whose `Status` is the zero `Status`.
- R-FIEM-UUKE: The trimmed name of a `Draft` is the result of applying `strings.TrimSpace` to its `Name` field; `Check` and `Create` MUST perform that trimming themselves and MUST decide every rule this design states about a name on the trimmed name, so that a caller passes the name exactly as it received it.
- R-EURV-F4LL: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Name` is `NameRequiredMessage` when the trimmed name is the empty string.
- R-EVZR-SWCA: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Name` is `NameTooLongMessage` when the trimmed name is not empty and its length in runes, as `utf8.RuneCountInString` counts it, is greater than `MaxNameRunes`; the trimmed name's length in bytes MUST NOT affect this decision.
- R-EYFK-KFTO: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Name` is `NameTakenMessage` when the trimmed name is not empty, its length in runes is at most `MaxNameRunes`, and it is equal — byte for byte, letter case included — to the `Name` of a widget the store already holds.
- R-EZNG-Y7KD: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Name` is the empty string when the trimmed name is not empty, its length in runes is at most `MaxNameRunes`, and it is equal byte for byte to the `Name` of no widget the store already holds.
- R-F0VD-BZB2: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Count` is `CountNegativeMessage` when the `Draft`'s `Count` is less than zero.
- R-F239-PR1R: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Count` is the empty string when the `Draft`'s `Count` is zero or greater.
- R-F3B6-3ISG: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Status` is `StatusNotAllowedMessage` when the `Draft`'s `Status` is equal to none of `StatusActive`, `StatusPaused` and `StatusRetired`.
- R-F4J2-HAJ5: When `Check` returns a nil error, it MUST return a `FieldErrors` whose `Status` is the empty string when the `Draft`'s `Status` is equal to one of `StatusActive`, `StatusPaused` and `StatusRetired`.
- R-F5QY-V29U: When `Check` returns a nil error, it MUST have decided the three fields of the `FieldErrors` it returns independently of one another, so that a `Draft` breaking a rule for more than one field carries a message for every one of those fields back from the single call.
- R-QWVE-HFLJ: `Check` MUST leave the store's widgets exactly as they were: the widgets (`D04-panel`) of the store immediately after the call MUST be equal, element for element and in the same order, to the widgets of the store immediately before it.
- R-F86R-MLR8: When no other call on the same store is in progress, and `Check` called with a `Draft` on the store immediately before a `Create` call with the same `Draft` returns a nil error, and that `Create` call returns a nil error, `Create` MUST return a `FieldErrors` equal to the one `Check` returned, so that `Create` decides every rule `Check` decides, identically.
- R-QZB7-8Z2X: When `Create` returns a nil error and a `FieldErrors` with all three fields empty, `Create` MUST have added to the store, and MUST return, a `Widget` whose `Name` is the trimmed name, whose `Count` is the `Draft`'s `Count`, and whose `Status` is the `Draft`'s `Status`; the widgets (`D04-panel`) of the store immediately after the call MUST be the widgets of the store immediately before it with that `Widget` appended as the last element and no other difference.
- R-R0J3-MQTM: When `Create` returns a nil error and a `FieldErrors` with any of its three fields non-empty, `Create` MUST return the zero `Widget` and MUST leave the store's widgets exactly as they were: the widgets (`D04-panel`) of the store immediately after the call MUST be equal, element for element and in the same order, to the widgets of the store immediately before it.
- R-R1R0-0IKB: A `*Store` MUST be safe for concurrent use: calls to `List`, `Check` and `Create` made on one store from several goroutines at once MUST complete without Go's race detector reporting a data race.
- R-EPNQ-CBOL: While `SetFailing(true)` is not in force on the store's handle and, before the first of those calls, the store holds no widget whose `Seq` is greater than `math.MaxInt64` (the standard library's `math`) minus the number of those calls, when several goroutines each make one `Create` call on the same store, with a context that is never done, with a `Draft` whose `Count` and `Status` the rules accept, with trimmed names that are not empty, are at most `MaxNameRunes` runes long, and differ from one another and from the name of every widget the store already holds, each of those calls MUST return a nil error and a `FieldErrors` all three of whose fields are the empty string, and the widgets (`D04-panel`) of the store once all of them have returned MUST hold exactly one additional widget for each of those calls.
- R-EQVM-Q3FA: The widgets (`D04-panel`) of a store MUST NEVER hold two widgets whose `Name` values are equal, however many `Create` calls run concurrently, because a `Create` call decides the name it was given is held by no widget of the store and adds its widget as one step with respect to every other `Create` call: while `SetFailing(true)` is not in force on the store's handle and, before the first of those calls, the store holds no widget whose `Seq` is greater than `math.MaxInt64` (the standard library's `math`) minus the number of those calls, when several goroutines each make one `Create` call on the same store, with a context that is never done, with `Draft` values whose trimmed names are all equal to one name that is not empty, is at most `MaxNameRunes` runes long and no widget the store already holds carries, and whose `Count` and `Status` the rules accept, every one of those calls MUST return a nil error, exactly one of them MUST return a `FieldErrors` all three of whose fields are the empty string, every other one MUST return a `FieldErrors` whose `Name` is `NameTakenMessage`, and the widgets of the store once all of them have returned MUST hold exactly one additional widget.
