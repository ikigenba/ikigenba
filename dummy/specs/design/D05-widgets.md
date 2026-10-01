# D05-widgets

The widgets themselves: what one is, what makes one acceptable, and where the
set of them lives while dummy runs. This is the whole of `internal/widget`,
and it is the part of dummy that has nothing to do with being a web server or
an MCP server. No request, no response, no header, no markup and no JSON
reaches this package; it is reached from `internal/panel` (the form,
`D07-form`) and from `internal/tools` (the MCP tools, `D09-mcp`), and it names
nothing of either. It imports nothing of appkit either: the one appkit
interface a widget type satisfies, `mcp.Enumerator`, is satisfied
structurally, by a method with the right shape. The import direction runs one
way, from those two packages to this one, which is what lets every rule below
be decided by calling a function rather than by driving a handler or a client.

A widget is three fields: a name, a whole-number count, and a status that is
one of three words. The status is an enumeration rather than a bare string so
that the three words exist in exactly one place, and `Statuses` hands them out
in the order the form offers them. It is a function and not a package-level
slice because a slice would be mutable shared state. `Status` also has an
`Enum` method returning the same three words as plain strings, in the same
order. That is appkit's `mcp.Enumerator` (appkit `D08`): a tool argument or
result field of type `Status` is described to an MCP client by a schema whose
`enum` is those three words, and appkit's decoder refuses any other value
before dummy sees it. appkit calls `Enum` on the zero value of the type, so
the answer cannot depend on the receiver, and does not.

The set is in memory and per-process. A new store holds exactly three widgets
— `alpha` 3 `active`, `beta` 0 `paused`, `gamma` 12 `retired`, in that order —
and it survives nothing: a store built after another store has been written to
still holds exactly those three. There is no exported fixture value, because
the fixtures are an outcome of `NewStore` rather than a thing a caller could
hold and modify. `All` returns the widgets in creation order, the starting
three first and every accepted creation after them in the order it was
accepted, so a new widget is last. `All` hands back a fresh slice, which is
what makes "the set was left exactly as it was, down to its order" something a
test can decide without a back door.

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
appending to it must be one step, which is why `Create` is not merely `Check`
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

The store is used concurrently. The panel page polls the table fragment while
a submission is being created, and an MCP client may create a widget while a
browser submits the form, so several goroutines reach one store at once, and
the design says so as an invariant rather than leaving it to be discovered.
Gate 4 runs the race detector, which is what proves it, and two further
requirements cover the half a race detector cannot see: concurrent accepted
creations all survive, none is lost to the other, and no two widgets in the
set ever carry the same name however many creations race. That second one is
the invariant — checking the name and appending the widget is one step with
respect to every other creation — and without it two callers creating one name
could each find it absent and each be accepted.

Everything about HTTP and MCP is elsewhere. The panel page and its banner are
`D04-panel`, the table is `D06-table`, the form, its per-field error placement
and the answers to a submission are `D07-form`, and the tools are `D09-mcp`.
Those documents name `Submission`, `Draft`, `FieldErrors`, `ParseSubmission`,
`Store.Check`, `Store.Create`, `Store.All`, `Status.Enum` and the six
messages; this one declares them.

## REQUIREMENTS

- R-ZMK6-SZTW: The `internal/widget` package MUST export `type Status string` together with these values of that type: `StatusActive Status = "active"`, `StatusPaused Status = "paused"`, and `StatusRetired Status = "retired"`.
- R-VLG5-AAL9: The `internal/widget` package MUST export `func Statuses() []Status`.
- R-AN9G-B7UI: The `internal/widget` package MUST export `func (s Status) Enum() []string`.
- R-I2W8-NTQ6: The `internal/widget` package MUST export `type Widget` as a struct with the fields `Name string`, `Count int`, and `Status Status`.
- R-W66F-SE72: The `internal/widget` package MUST export `const MaxNameRunes = 40`.
- R-ISI4-P0AR: The `internal/widget` package MUST export `type Submission` as a struct with the fields `Name string`, `Count string`, and `Status string`.
- R-APP9-2RBW: The `internal/widget` package MUST export `type Draft` as a struct with the fields `Name string`, `Count int`, and `Status Status`.
- R-EBQ5-GVM3: The `internal/widget` package MUST export `type FieldErrors` as a struct with the fields `Name string`, `Count string`, and `Status string`, each holding at most one message for the field it names, the empty string meaning that field was accepted.
- R-WPOT-WQ26: The `internal/widget` package MUST export `func (e FieldErrors) Any() bool`.
- R-XBN0-SLEO: The `internal/widget` package MUST export six string constants with exactly these values: `NameRequiredMessage = "a name is required"`, `NameTooLongMessage = "the name is too long; the limit is 40 characters"`, `NameTakenMessage = "that name is already taken"`, `CountNotWholeMessage = "the count must be a whole number"`, `CountNegativeMessage = "the count cannot be negative"`, and `StatusNotAllowedMessage = "the status must be one of active, paused, or retired"`.
- R-EFDU-M6U6: The `internal/widget` package MUST export `func NewStore() *Store`.
- R-EJ1J-RI29: The `internal/widget` package MUST export `func (s *Store) All() []Widget`.
- R-EMP8-WTAC: The `internal/widget` package MUST export `func ParseSubmission(sub Submission) (Draft, FieldErrors)`.
- R-ENX5-AL11: The `internal/widget` package MUST export `func (s *Store) Check(d Draft) FieldErrors`.
- R-EQCY-24IF: The `internal/widget` package MUST export `func (s *Store) Create(d Draft) (Widget, FieldErrors)`.
- R-XV5E-WX9S: `Statuses` MUST return a slice of exactly three elements whose values are `StatusActive`, `StatusPaused`, and `StatusRetired`, in that order.
- R-YH3L-SSMA: Each call to `Statuses` MUST return a slice the caller may modify in place without changing the values a later call to `Statuses` returns.
- R-EU0N-7FQI: `Enum`, called on any `Status` value, the zero `Status` and values equal to none of `StatusActive`, `StatusPaused` and `StatusRetired` included, MUST return a slice of exactly three strings: the string values of the three elements `Statuses` returns, in the order `Statuses` returns them.
- R-EWGF-YZ7W: Each call to `Enum` MUST return a slice the caller may modify in place without changing the values a later call to `Enum` or to `Statuses` returns.
- R-Z1TW-AW83: `Any` MUST return true when at least one of its receiver's three fields is a non-empty string, and false when all three are the empty string.
- R-EYW8-QIPA: Every call to `NewStore` MUST return a store whose `All` returns exactly three widgets, in this order: `Name` `"alpha"`, `Count` 3, `Status` `StatusActive`; then `Name` `"beta"`, `Count` 0, `Status` `StatusPaused`; then `Name` `"gamma"`, `Count` 12, `Status` `StatusRetired` — whatever widgets were created through any store a previous call to `NewStore` returned.
- R-F1C1-I26O: `All` MUST return a slice the caller may modify in place, append to, or discard without changing the widgets the store holds or the values a later call to `All` returns.
- R-F4ZQ-NDER: `All` MUST return the store's widgets in creation order: the three the store started with, in their starting order, followed by each widget a later `Create` call accepted, in the order those calls accepted them, so the most recently created widget is last.
- R-F7FJ-EWW5: `ParseSubmission` MUST return a `Draft` whose `Name` is exactly the `Name` field of the `Submission` it was given, untrimmed and otherwise unaltered, and a `FieldErrors` whose `Name` is the empty string, whatever that `Submission` holds.
- R-F9VC-6GDJ: The trimmed count of a `Submission` is the result of applying `strings.TrimSpace` to its `Count` field; when `strconv.Atoi` of the trimmed count returns a non-nil error — which includes the empty string, and includes a value outside the range of `int`, for which `Atoi` returns a non-nil error alongside a clamped value — `ParseSubmission` MUST return a `FieldErrors` whose `Count` is `CountNotWholeMessage` and a `Draft` whose `Count` is 0.
- R-FCB4-XZUX: When `strconv.Atoi` of the trimmed count of a `Submission` returns a nil error, `ParseSubmission` MUST return a `FieldErrors` whose `Count` is the empty string and a `Draft` whose `Count` is the value `Atoi` returned, a value less than zero included.
- R-FFYU-3B30: The trimmed status of a `Submission` is the result of applying `strings.TrimSpace` to its `Status` field; when the trimmed status is equal, byte for byte, to the string value of one of `StatusActive`, `StatusPaused` and `StatusRetired`, `ParseSubmission` MUST return a `FieldErrors` whose `Status` is the empty string and a `Draft` whose `Status` is that one, and otherwise MUST return a `FieldErrors` whose `Status` is `StatusNotAllowedMessage` and a `Draft` whose `Status` is the zero `Status`.
- R-FIEM-UUKE: The trimmed name of a `Draft` is the result of applying `strings.TrimSpace` to its `Name` field; `Check` and `Create` MUST perform that trimming themselves and MUST decide every rule this design states about a name on the trimmed name, so that a caller passes the name exactly as it received it.
- R-FM2C-05SH: `Check` MUST return a `FieldErrors` whose `Name` is `NameRequiredMessage` when the trimmed name is the empty string.
- R-FOI4-RP9V: `Check` MUST return a `FieldErrors` whose `Name` is `NameTooLongMessage` when the trimmed name is not empty and its length in runes, as `utf8.RuneCountInString` counts it, is greater than `MaxNameRunes`; the trimmed name's length in bytes MUST NOT affect this decision.
- R-FQXX-J8R9: `Check` MUST return a `FieldErrors` whose `Name` is `NameTakenMessage` when the trimmed name is not empty, its length in runes is at most `MaxNameRunes`, and it is equal — byte for byte, letter case included — to the `Name` of a widget the store already holds.
- R-FTDQ-AS8N: `Check` MUST return a `FieldErrors` whose `Name` is the empty string when the trimmed name is not empty, its length in runes is at most `MaxNameRunes`, and it is equal byte for byte to the `Name` of no widget the store already holds.
- R-FVTJ-2BQ1: `Check` MUST return a `FieldErrors` whose `Count` is `CountNegativeMessage` when the `Draft`'s `Count` is less than zero.
- R-FZH8-7MY4: `Check` MUST return a `FieldErrors` whose `Count` is the empty string when the `Draft`'s `Count` is zero or greater.
- R-G1X0-Z6FI: `Check` MUST return a `FieldErrors` whose `Status` is `StatusNotAllowedMessage` when the `Draft`'s `Status` is equal to none of `StatusActive`, `StatusPaused` and `StatusRetired`.
- R-G4CT-QPWW: `Check` MUST return a `FieldErrors` whose `Status` is the empty string when the `Draft`'s `Status` is equal to one of `StatusActive`, `StatusPaused` and `StatusRetired`.
- R-G6SM-I9EA: `Check` MUST decide the three fields of the `FieldErrors` it returns independently of one another, so that a `Draft` breaking a rule for more than one field carries a message for every one of those fields back from the single call.
- R-GAGB-NKMD: `Check` MUST leave the store's widgets exactly as they were: a call to `All` after it returns a slice equal, element for element and in the same order, to the slice a call to `All` before it returned.
- R-GCW4-F43R: When no other call on the same store is in progress, `Create` MUST return a `FieldErrors` equal to the one `Check` returns for the same `Draft` on the same store called immediately before it, so that `Create` decides every rule `Check` decides, identically.
- R-GFBX-6NL5: When the `FieldErrors` `Create` returns has all three fields empty, `Create` MUST add to the store, and return, a `Widget` whose `Name` is the trimmed name, whose `Count` is the `Draft`'s `Count`, and whose `Status` is the `Draft`'s `Status`; a call to `All` after it MUST return the sequence `All` returned before it with that widget appended as the last element and no other difference.
- R-GIZM-BYT8: When the `FieldErrors` `Create` returns has any of its three fields non-empty, `Create` MUST return the zero `Widget` and MUST leave the store's widgets exactly as they were: a call to `All` after it returns a slice equal, element for element and in the same order, to the slice a call to `All` before it returned.
- R-GMNB-HA1B: A `*Store` MUST be safe for concurrent use: calls to `All`, `Check` and `Create` made on one store from several goroutines at once MUST complete without Go's race detector reporting a data race.
- R-GQB0-ML9E: When several goroutines each make one `Create` call on the same store with a `Draft` whose `Count` and `Status` the rules accept, with trimmed names that are not empty, are at most `MaxNameRunes` runes long, and differ from one another and from the name of every widget the store already holds, a later call to `All` MUST return exactly one additional widget for each of those calls.
- R-GSQT-E4QS: The slice `All` returns MUST NEVER hold two widgets whose `Name` values are equal, however many `Create` calls run concurrently, because a `Create` call decides the name it was given is held by no widget of the store and adds its widget as one step with respect to every other `Create` call: when several goroutines each make one `Create` call on the same store with `Draft` values whose trimmed names are all equal to one name that is not empty, is at most `MaxNameRunes` runes long and no widget the store already holds carries, and whose `Count` and `Status` the rules accept, exactly one of those calls MUST return a `FieldErrors` all three of whose fields are the empty string, every other one of those calls MUST return a `FieldErrors` whose `Name` is `NameTakenMessage`, and a later call to `All` MUST return exactly one additional widget.
- R-GV6M-5O86: Two stores returned by two separate calls to `NewStore` MUST NOT share widgets: a widget a `Create` call accepted on one of them MUST NOT appear in the slice the other's `All` returns.
