# D05-widgets

The widgets themselves: what one is, what makes a submitted one acceptable,
and where the set of them lives while dummy runs. This is the whole of
`internal/widget`, and it is the part of dummy that has nothing to do with
being a web server. No request, no response, no header, no markup reaches
this package; it is reached from `internal/panel` and from nowhere else, and
it names nothing of `internal/panel`'s. The import direction
(`D01-layout-and-run-seam`) runs one way, panel to widget, which is what lets
every rule below be decided by calling a function rather than by driving a
handler.

A widget is three fields: a name, a whole-number count, and a status that is
one of three words. The status is an enumeration rather than a bare string so
that the three words exist in exactly one place, and `Statuses` hands them out
in the order the form offers them. It is a function and not a package-level
slice because a slice would be mutable shared state; there is no package-level
`var` in this package at all, and the requirement that says so is what
guarantees the widget set lives in a `Store` value rather than in the package.

The set is in memory and per-process. A new store holds exactly three widgets
— `alpha` 3 `active`, `beta` 0 `paused`, `gamma` 12 `retired`, in that order —
and it survives nothing: a store built after another store has been written to
still holds exactly those three. There is no exported fixture value, because
the fixtures are an outcome of `NewStore` rather than a thing a caller could
hold and modify. `All` returns the widgets in creation order, the starting
three first and every accepted creation after them in the order it was
accepted, so a new widget is last. `All` hands back a fresh slice: the store
has no exported field and no other way in, which is what makes "the set was
left exactly as it was, down to its order" something a test can decide without
a back door.

Creating is where the rules live. A caller submits three strings exactly as
they arrived, and `Submission` holds them raw — untrimmed, unshortened —
because the rejection re-display `D07-form` owns has to echo back what the
caller actually typed. `Create` does the trimming itself, on all three values,
before it judges any of them. One trimming rule for every field
is simpler to state and never answers "the count must be a whole number" to a
value that is plainly a number with a space in front of it.

Then: the name is required and is at most forty runes, counted in runes and
not in bytes, because it is a field a person typed and a person who typed
forty accented letters typed forty characters. A whitespace-only name is the
required-field rejection, not a widget with a blank name. Uniqueness is exact
and case-sensitive on the trimmed value, so `alpha` and `Alpha`
are two widgets; it is also the one rule that cannot be decided from the
submission alone, which is why creating is a method on the store rather than a
free function — asking the set and appending to it must be one step. The count
follows Go's `strconv.Atoi` grammar, and the not-a-whole-number message is
keyed on `Atoi` returning a non-nil error rather than on "does not parse":
`Atoi` returns both an error and a clamped value for a number
too large to hold, and a huge negative number must not be routed to the
negative-count message by accident. Zero is a fine count; below zero is not.
The status must be one of the three words exactly, checked rather than trusted,
because a caller with `curl` sends whatever they like.

Every field is judged, and every field that is wrong comes back in the same
answer — a caller who got three things wrong learns all three at once. That is
why the outcome is a struct with one string per field rather than a list or a
map: one message per field is structural, and a renderer cannot be handed two
messages for one field or a message for a field that does not exist. The
message text lives here, beside the rule that raises it, rather than in the
package that renders it; dummy has exactly one presentation, and a reason code
mapped to text somewhere else would only give the rule and its wording room to
drift apart. A rejected submission creates nothing and leaves the set exactly
as it was, order included.

The store is used concurrently. The panel page polls the table fragment while
a submission is being created, so two goroutines reach one store at once, and
the design says so as an invariant rather than leaving it to be discovered.
Gate 4 runs the race detector, which is what proves it, and two
further requirements cover the half a race detector cannot see: concurrent
accepted creations all survive, none is lost to the other, and no two widgets
in the set ever carry the same name however many creations race. That second
one is the invariant — checking the name and appending the widget is one step
with respect to every other creation — and without it two callers submitting
one name could each find it absent and each be accepted.

Everything about HTTP is elsewhere. The panel page and its chrome are
`D04-panel`, the table markup is `D06-table`, and the form, its per-field error
placement and the answers to a submission are `D07-form`. Those documents name
`Submission`, `FieldErrors`, `Store.Create`, `Store.All` and the six messages;
this one declares them.

## REQUIREMENTS

- R-QICF-XK9C: The `internal/widget` package MUST export `type Status string` together with exactly three values of that type: `StatusActive Status = "active"`, `StatusPaused Status = "paused"`, and `StatusRetired Status = "retired"`.
- R-QJKC-BC01: The `internal/widget` package MUST export `func Statuses() []Status`.
- R-QKS8-P3QQ: The `internal/widget` package MUST export `type Widget` as a struct whose exported fields are exactly `Name string`, `Count int`, and `Status Status`.
- R-QM05-2VHF: The `internal/widget` package MUST export `const MaxNameRunes = 40`.
- R-7WR2-CK99: The `internal/widget` package MUST export `type Submission` as a struct whose exported fields are exactly `Name string`, `Count string`, and `Status string`.
- R-QPNU-86PI: The `internal/widget` package MUST export `type FieldErrors` as a struct whose exported fields are exactly `Name string`, `Count string`, and `Status string`, each holding at most one message for the field it names, the empty string meaning that field was accepted.
- R-QQVQ-LYG7: The `internal/widget` package MUST export `func (e FieldErrors) Any() bool`.
- R-QS3M-ZQ6W: The `internal/widget` package MUST export six string constants with exactly these values: `NameRequiredMessage = "a name is required"`, `NameTooLongMessage = "the name is too long; the limit is 40 characters"`, `NameTakenMessage = "that name is already taken"`, `CountNotWholeMessage = "the count must be a whole number"`, `CountNegativeMessage = "the count cannot be negative"`, and `StatusNotAllowedMessage = "the status must be one of active, paused, or retired"`.
- R-QTBJ-DHXL: The `internal/widget` package MUST export `type Store` as a struct type with no exported field, so that the widgets it holds are reachable only through its exported methods.
- R-QUJF-R9OA: The `internal/widget` package MUST export `func NewStore() *Store`.
- R-QVRC-51EZ: The `internal/widget` package MUST export `func (s *Store) All() []Widget`.
- R-QWZ8-IT5O: The `internal/widget` package MUST export `func (s *Store) Create(sub Submission) (Widget, FieldErrors)`.
- R-QY74-WKWD: `Statuses` MUST return a slice of exactly three elements whose values are `StatusActive`, `StatusPaused`, and `StatusRetired`, in that order.
- R-QZF1-ACN2: Each call to `Statuses` MUST return a slice the caller may modify in place without changing the values a later call to `Statuses` returns.
- R-R1UU-1W4G: Every call to `NewStore` MUST return a store whose `All` returns exactly three widgets, in this order: `Name` `"alpha"`, `Count` 3, `Status` `StatusActive`; then `Name` `"beta"`, `Count` 0, `Status` `StatusPaused`; then `Name` `"gamma"`, `Count` 12, `Status` `StatusRetired` — whatever widgets were created through any store a previous call to `NewStore` returned.
- R-R32Q-FNV5: `All` MUST return a slice the caller may modify in place, append to, or discard without changing the widgets the store holds or the values a later call to `All` returns.
- R-R4AM-TFLU: `All` MUST return the store's widgets in creation order: the three the store started with, in their starting order, followed by each widget a later `Create` call accepted, in the order those calls accepted them, so the most recently created widget is last.
- R-R5IJ-77CJ: The trimmed name, trimmed count, and trimmed status of a `Submission` are the results of applying `strings.TrimSpace` to its `Name`, `Count`, and `Status` fields respectively; `Create` MUST perform that trimming itself and MUST decide every validation rule of this design on the trimmed values, so that a caller passes the values exactly as submitted and still holds those untrimmed values after the call returns.
- R-R6QF-KZ38: `Create` MUST return a `FieldErrors` whose `Name` is `NameRequiredMessage` when the trimmed name is the empty string.
- R-R7YB-YQTX: `Create` MUST return a `FieldErrors` whose `Name` is `NameTooLongMessage` when the trimmed name is not empty and its length in runes, as `utf8.RuneCountInString` counts it, is greater than `MaxNameRunes`; the trimmed name's length in bytes MUST NOT affect this decision.
- R-R968-CIKM: `Create` MUST return a `FieldErrors` whose `Name` is `NameTakenMessage` when the trimmed name is not empty, its length in runes is at most `MaxNameRunes`, and it is equal — byte for byte, letter case included — to the `Name` of a widget the store already holds.
- R-RAE4-QABB: `Create` MUST return a `FieldErrors` whose `Name` is the empty string when the trimmed name is not empty, its length in runes is at most `MaxNameRunes`, and it is equal byte for byte to the `Name` of no widget the store already holds.
- R-RBM1-4220: `Create` MUST return a `FieldErrors` whose `Count` is `CountNotWholeMessage` when `strconv.Atoi` of the trimmed count returns a non-nil error — which includes the empty string, and includes a value outside the range of `int`, for which `Atoi` returns a non-nil error alongside a clamped value.
- R-RCTX-HTSP: `Create` MUST return a `FieldErrors` whose `Count` is `CountNegativeMessage` when `strconv.Atoi` of the trimmed count returns a nil error and a value less than zero.
- R-RE1T-VLJE: `Create` MUST return a `FieldErrors` whose `Count` is the empty string when `strconv.Atoi` of the trimmed count returns a nil error and a value of zero or greater.
- R-RF9Q-9DA3: `Create` MUST return a `FieldErrors` whose `Status` is `StatusNotAllowedMessage` when the trimmed status is not equal, byte for byte, to the string value of any of `StatusActive`, `StatusPaused`, and `StatusRetired`.
- R-RGHM-N50S: `Create` MUST return a `FieldErrors` whose `Status` is the empty string when the trimmed status is equal, byte for byte, to the string value of one of `StatusActive`, `StatusPaused`, and `StatusRetired`.
- R-RHPJ-0WRH: `Create` MUST decide the three fields of the `FieldErrors` it returns independently of one another, so that a submission breaking a rule for more than one field carries a message for every one of those fields back from the single call.
- R-RIXF-EOI6: `Any` MUST return true when at least one of its receiver's three fields is a non-empty string, and false when all three are the empty string.
- R-RLD8-67ZK: When the `FieldErrors` `Create` returns has all three fields empty, `Create` MUST add to the store, and return, a `Widget` whose `Name` is the trimmed name, whose `Count` is the value `strconv.Atoi` returned for the trimmed count, and whose `Status` is the one of `StatusActive`, `StatusPaused`, and `StatusRetired` whose string value equals the trimmed status; a call to `All` after it MUST return the sequence `All` returned before it with that widget appended as the last element and no other difference.
- R-RML4-JZQ9: When the `FieldErrors` `Create` returns has any of its three fields non-empty, `Create` MUST return the zero `Widget` and MUST leave the store's widgets exactly as they were: a call to `All` after it returns a slice equal, element for element and in the same order, to the slice a call to `All` before it returned.
- R-RNT0-XRGY: A `*Store` MUST be safe for concurrent use: calls to `All` and `Create` made on one store from several goroutines at once MUST complete without Go's race detector reporting a data race.
- R-RP0X-BJ7N: When several goroutines each make one `Create` call on the same store that the validation rules accept, with trimmed names that differ from one another and from the name of every widget the store already holds, a later call to `All` MUST return exactly one additional widget for each of those calls.
- R-7XYY-QBZY: The slice `All` returns MUST NEVER hold two widgets whose `Name` values are equal, however many `Create` calls run concurrently, because a `Create` call decides the name it was given is held by no widget of the store and adds its widget as one step with respect to every other `Create` call: when several goroutines each make one `Create` call on the same store with submissions whose trimmed names are all equal to one name no widget the store already holds carries and whose count and status the validation rules accept, exactly one of those calls MUST return a `FieldErrors` all three of whose fields are the empty string, every other one of those calls MUST return a `FieldErrors` whose `Name` is `NameTakenMessage`, and a later call to `All` MUST return exactly one additional widget.
- R-RQ8T-PAYC: The non-test `.go` files of `internal/widget` MUST declare no package-level `var`.
- R-RRGQ-32P1: Two stores returned by two separate calls to `NewStore` MUST NOT share widgets: a widget a `Create` call accepted on one of them MUST NOT appear in the slice the other's `All` returns.
