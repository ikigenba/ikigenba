# D06-table

The widgets table, and the one route that serves it on its own. Two things
live here and nowhere else: the markup contract the table satisfies, and the
whole of `GET`/`HEAD /widgets/table`.

The table is one thing rendered twice. The panel page (`D04-panel`) embeds it
in the document it sends, and `/widgets/table` re-renders it alone, so that the
page's inline polling script can replace the table element with whatever the
fragment returns. That replacement is why the two renderings must be the *same
markup*, not merely two markups that satisfy the same contract: if they merely
agreed on a contract they could still differ byte for byte, and the page would
visibly change on its first poll even though nobody had created anything. S4
says it outright: the fragment and the table the panel page embeds are the same
markup, byte for byte. So the identity is exact — not even surrounding
whitespace is excepted — and it holds for every page that embeds the table,
the panel page a `GET /widgets` returns and the 422 redraw alike, because both
are panel pages and the obligation is stated over every panel page. One
document owning the markup and its anchor is how that identity gets stated
once and tested once. `D04-panel` owns the script that performs the swap; it
refers to the anchor this document fixes, `id="widgets-table"`, and declares
nothing about the table. `D04-panel` also owns where on the page the table
sits and what wraps it; this document fixes the table span and nothing around
it, so no wrapper `D04-panel` places around the table can change a byte this
document fixes.

The fragment is a fragment. There is no doctype, no `html` element, no `body`
element and no chrome around it — a caller fetching it with `curl` sees exactly
the markup the page splices in. It also carries no `script` element of its own:
the page's script must sit outside the table, because the first swap deletes
whatever the table contained, and a poller inside the table would make the page
poll exactly once. There is no `style` element either, but that is not stated
here: `D04-panel` forbids one in the documents and the fragment the handler
sends, and a second statement of the same fact is exactly the drift this design
avoids.

Each row shows its widget's status as a word in its own column, never a colour
or an icon alone. That is the whole reason the status is contract at all: a
caller reading a row with `curl` should understand it the way someone looking
at a browser does. The word sits inside a status marker, a `span` with class
`status` whose `data-status` attribute carries the status as data while the
word inside it carries the status as text, the thing a reader reads. The
marker's attribute and its word are both the string value of the widget's
`Status`, whose three values `D05-widgets` declares; this document names them
only through that value. The status cell holds the marker and nothing else, and
every data row has one, the row for a widget created a moment ago included.

The count column is marked numeric, so counts line up: the header cell over it
and every row's count cell carry class `num`. That marking belongs to the count
column alone. A `num` on the name or the status would line up as numbers a
column that holds none, so no other header or data cell carries it. The class
is fixed by exact read value on the count cells — S3 and S4 quote them as
`class="num"` — and the ban on the others looks for `num` as one of possibly
several classes, so a cell cannot slip the marking in beside another class.

The header row is required — S4 fixes "a table with a header row and one row
per widget and nothing around it". Of what its cells say, the stories quote
only the count column's header, `Count`, so that is the one header text fixed,
on the header cell standing over the count column, the second, just as the
count is each data row's second cell. The texts of the other header cells are
not fixed. The rows below it are one per widget, in the order `D05-widgets`'s
`Store.All` returns them, which is fixture order followed by creation order, so
a newly created widget is last.

A cell text is read through the normalisation, whose last step is the
whitespace collapse, so it can never be compared against a raw stored value.
`D05-widgets` trims only the ends of a submitted name and fixes no character
class (R-R5IJ-77CJ), so `my widget` — two spaces — is a name a caller can
create and `D07-form` requires that creation to succeed. Comparing a collapsed
cell text against that raw name would be unsatisfiable the moment such a widget
existed, and three bodies would become unproducible at once: this route's 200,
the panel page's 200, and the 422. So both sides are collapsed, exactly as
`D04-panel` already does for the email it draws into the chrome, in its
definition of being drawn in the chrome. Nothing is lost that was ever
decidable here, and the submitted bytes still have a home: `D07-form` reads the
422's echo out of an attribute, and an attribute's read value, as `D04-panel`
defines it, is unescaped but never collapsed. The count and the status are
collapsed too, though neither can hold whitespace — a decimal integer, and one
of three fixed words — because a rule that bites on one of the three cells and
is merely harmless on the others is a rule a reader has to check twice. The
status cell's text is read the same way, so the marker's tags fall away in the
normalisation and the cell text is the bare word; the marker itself is fixed
separately, over the cell's markup as sent.

Where this document has to read text out of markup, it uses the text
procedures `D04-panel` states in its "The text procedures" section — the
script-stripped form, the normalisation, the whitespace collapse — and the
plain failure shape it defines alongside the chrome. It restates no step of any
of them: a restatement would be a second contract, and the two would drift.
Which form a tag count is taken over is `D04-panel`'s rule too, R-RMP2-ZITR,
and this document neither repeats it nor keeps one of its own. Read it there;
named here only so a reader knows it exists and that it supplies a *default*
rather than a fixed frame. A requirement that names the form it counts over is
counted over the form it names, and the rule yields to it. Only a requirement
that names no form falls to the default, which is the script-stripped form,
except where the requirement names the `script` element itself, whose default
is the raw body.

That default is not an aside for this document. Over the script-stripped form
a `<script` count is always zero, so a reader who took the stripped form for
the whole rule would read this document's ban on a `<script` start tag in the
fragment as satisfied by every fragment, poller included — and that ban is the
only thing in the whole design keeping a script out of the table the page
swaps. The ban names no form and names the `script` element, so that rule
sends it to the raw body, where it bites. The yielding half is what makes
`D04-panel`'s `style` ban work in the other direction: where it names the raw
body, the count is taken there and not over a form the `style` start tags have
already been stripped out of. The lexical notions go
the same way. What a `<x` start tag and a `</x>` end tag are, and what it
means to read an attribute's value, are defined once by `D04-panel` and cited
here by id; this document defines neither. That matters more than it looks: a
definition written here without `D04-panel`'s delimiter rule would make
`<thead` match a "`<th` start tag", and the two documents would disagree about
the same bytes.

The script-stripped form matters twice here and is easy to miss. Counting the
page's `<table` start tags over the raw bytes lets a `<table` mentioned inside
the polling script's own source be found first, and the identity comparison
then runs against a span that is not the table at all; a conforming page built
that way fails a check it should pass. Taking the span over the script-stripped
form closes exactly that hole.

Every failure from this endpoint is plain text — one line, with no chrome —
whether or not the caller is identified. This is the deliberate exception to
the rule that a failure dummy can name to an identified caller is itself a page
in the chrome. The reason is the swap again: the script splices whatever comes
back into a document that is already drawn, so an error wrapped in the chrome
would arrive as a whole page pushed into a table. So this document says which
shape each of this route's failures takes, and `D04-panel` keeps the shapes
themselves. The missing-identity 500 is the one place the two documents meet:
`D04-panel` fixes that response for every route at once, from the identity
side, and this document fixes it for this route, from the fragment side,
because being a fragment is what makes it plain and `D04-panel` names no route.
The two failures are the missing-identity 500 and the 405; their bodies are
`MissingIdentityBody` and `MethodNotAllowedBody`, which `D04-panel` declares
and this document only names. The 405 names `Allow: GET, HEAD` and refuses a
`POST` rather than redirecting it, because creating a widget is a different
route with its own story.

This route's `ETag` is stated here, whole: every rule about the table's
validator is in this document. It is not the only `ETag` dummy sends —
`D08-assets` gives every asset its own, on its own terms — and nothing here
reaches beyond `/widgets/table`. It is a strong validator, an opaque quoted
string determined by the rendered fragment's bytes. The quoted characters
exclude the comma and whitespace: `If-None-Match` carries a comma-separated
list, so a tag containing a comma would be split into pieces that match
nothing, and the 304 would be unreachable while every requirement here stayed
satisfied. Excluding the comma is narrower than RFC 9110's own grammar allows,
and that is deliberate — it costs an implementation nothing and makes the list
split sound. The contract fixes the *relation* — equal content yields an equal
tag, and the tag changes when a widget is created — and never the value; no
story fixes the value, and a requirement that did would be fixing an
implementation. It is carried by the 200 and the 304 and by nothing else. S4's
preamble says "Every response carries an `ETag`", but its missing-identity 500
explicitly says no `ETag` is sent. S4 does not fix whether the 405 carries one:
its response block omits the header, and omitted headers are unspecified there.
This design leaves the 405 without a validator because it does not represent
table content for a caller to validate.

The conditional read follows RFC 9110 §13.1.2 (`If-None-Match`, HTTP Semantics,
RFC 9110, June 2022): the field carries a list, the condition succeeds when any
member matches, and `*` matches whenever a representation exists — which on
this route it always does, since the table renders for every widget set. The
comparison is the weak one that section requires ("A recipient MUST use the
weak comparison function when comparing entity tags for If-None-Match"), so an
entry of `W/` followed by the current tag matches too. A request may carry the
field on several header lines; RFC 9110 §5.3 reads them as one list joined with
commas, and so does this route. `D08-assets` reads the field the same way, word
for word, so `/widgets/table` and `/assets/*` answer the same field alike. RFC
9110 is published documentation of a well-known protocol, so it is the proof
for this behavior and no live observation is needed.

Reading the fragment never creates, alters or removes a widget. That is a fixed
outcome rather than an assumption — S4 asserts it in the postconditions of
every one of its read groups — and the testable form is that the widget set is
equal, element for element and in order, immediately before and immediately
after any request this route handles, whatever its method and whatever it is
answered with.

A requirement that instead compares two requests to each other — the `HEAD`
against the `GET` it mirrors — has to say what is held equal between them. A
successful creation landing in between changes the fragment's bytes and so, by
this document's own rule, changes the `ETag`, which would make a conforming
implementation falsify the comparison. So that requirement names the store:
the two answers are required to agree only when `Store.All` returns the same
widgets in the same order immediately before each request. On this route the
`ETag` is exactly what makes the omission observable rather than theoretical.

Two things deliberately get no requirement. The polling interval is not
contract; S4 declines to fix it, and `D04-panel` says so where the script is
designed. And the claim that "nothing is assembled afterwards by JavaScript" is
owned here only in its decidable half — that the panel page contains the table
in the body it sends — because the other half is not decidable by any procedure
the standard library can run and is in plain tension with the poller the design
puts in the page. It is recorded here rather than silently dropped.

This document declares no exported name. `internal/panel` owns them and
`D04-panel` declares them; nothing that computes the `ETag` is exported.

## REQUIREMENTS

- R-KVPQ-RDIA: The body of a 200 response to a `GET` request whose path is `/widgets/table` MUST be a **table fragment** for the widget set current when the response was rendered, where a table fragment for a widget set is a string that, after optional leading whitespace, begins with a `<table` start tag as `D04-panel` defines start tags and end tags (R-KDGH-2SDG), ends with a `</table>` end tag followed by optional trailing whitespace, and contains no `<html` start tag, no `<body` start tag, no `<script` start tag, and no occurrence of `<!doctype` compared case-insensitively.
- R-KWXN-558Z: The `<table` start tag of a table fragment MUST carry an occurrence of the attribute `id`, as `D04-panel` defines an attribute occurrence and its read value (R-KEOD-GK45), whose read value is exactly `widgets-table`.
- R-M3J7-QWZS: A table fragment MUST contain exactly one **header row** — a span from a `<tr` start tag through the next following `</tr>` end tag that contains at least one `<th` start tag and no `<td` start tag — and that header row MUST precede every **data row**, a span from a `<tr` start tag through the next following `</tr>` end tag that contains at least one `<td` start tag.
- R-M4R4-4OQH: A table fragment for a widget set MUST contain exactly one data row for each widget in that set and no other data row, the data rows appearing in the order `D05-widgets`'s `Store.All` returns those widgets.
- R-Q4HP-7G07: The first three **cell texts** of the data row for a widget MUST be the whitespace collapse of that widget's `Name`, the whitespace collapse of its `Count` written in decimal, and the whitespace collapse of the string value of its `Status`, where the whitespace collapse is the one the text procedures (`D04-panel`) define and the cell text of a `<td` start tag is the normalisation defined by those procedures of the text from that start tag's closing `>` through the next following `</td>` end tag.
- R-AU0W-ONFN: The header row of a table fragment MUST contain at least two `<th` start tags; the second of them MUST carry an occurrence of the attribute `class`, as `D04-panel` defines an attribute occurrence and its read value, whose read value is exactly `num`; and the normalisation, as the text procedures (`D04-panel`) define it, of the text from that start tag's closing `>` through the next following `</th>` end tag MUST be exactly `Count`.
- R-AV8T-2F6C: In every data row of a table fragment, the second `<td` start tag MUST carry an occurrence of the attribute `class` whose read value is exactly `num`.
- R-AWGP-G6X1: In the data row for a widget, the text from the closing `>` of the third `<td` start tag up to the `<` of the next following `</td>` end tag, once its leading and trailing ASCII whitespace is removed, MUST consist of exactly a `<span` start tag carrying an occurrence of the attribute `class` whose read value is exactly `status` and an occurrence of the attribute `data-status` whose read value is exactly the string value of that widget's `Status` (`D05-widgets`), then that same string value, then a `</span>` end tag, and nothing else.
- R-QR6J-7N0Z: Every `<th` or `<td` start tag in a table fragment, other than the second `<th` start tag of its header row and the second `<td` start tag of each of its data rows, MUST NOT carry an occurrence of the attribute `class` whose read value, split on ASCII whitespace, has `num` as a member.
- R-3ABH-NPH7: Every panel page (`D04-panel`) MUST contain, counted over that page's script-stripped form as the text procedures (`D04-panel`) define it, exactly one `<table` start tag and exactly one `</table>` end tag, and the text of that script-stripped form from that start tag through that end tag inclusive — the page's **table span** — MUST be a table fragment for the widget set current when the page was rendered.
- R-AST0-AVOY: For one and the same widget set, the table span of every panel page (`D04-panel`) and the body of a 200 response to a `GET` request whose path is `/widgets/table` MUST be byte-identical, with no leading or trailing whitespace excepted.
- R-62UX-379I: A `GET` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries no `If-None-Match` field whose condition succeeds, MUST be answered with status 200 and the header `Content-Type: text/html; charset=utf-8`.
- R-KY5J-IWZO: A 200 response to a request whose path is `/widgets/table` MUST carry an `ETag` header whose value is a strong validator: a `"`, then at least one character, none of which is a `"`, a comma or an ASCII whitespace character, then a `"`, with no `W/` prefix and nothing before or after the quoted string.
- R-0NC2-JFTW: Two 200 responses to `GET` requests whose path is `/widgets/table` whose bodies are byte-identical MUST carry `ETag` values that are byte-identical.
- R-MDAE-T2XC: A 200 response to a request whose path is `/widgets/table` sent after a widget has been created MUST carry an `ETag` value differing from the `ETag` value carried by the last such response sent before that creation.
- R-WIGA-XCYS: For a `HEAD` request whose path is `/widgets/table` and the otherwise identical `GET` request, immediately before each of which the slice `D05-widgets`'s `Store.All` returns is equal element for element and in the same order, the `HEAD` request MUST be answered with the status, the `ETag` value and every other header the `GET` request is answered with, and with an empty body.
- R-WWH0-TBDR: A `GET` or `HEAD` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries an `If-None-Match` field at least one of whose entries — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is exactly `*`, is byte-identical to the `ETag` value the same request would be answered with were the field absent, or is `W/` followed by that value, MUST be answered with status 304, that same `ETag` value, and an empty body.
- R-WXOX-734G: A `GET` or `HEAD` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries an `If-None-Match` field no entry of which — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is `*`, the `ETag` value the same request would be answered with were the field absent, or `W/` followed by that value, MUST be answered exactly as it would be were the field absent, and the `ETag` value it is answered with MUST be such that no entry of that field is that value or `W/` followed by it.
- R-66IM-8IHL: A request whose path is `/widgets/table` and whose `X-User-Id` header is absent or present with an empty value MUST be answered with status 500 and a response in the plain failure shape (`D04-panel`) for `MissingIdentityBody`, never in the chrome failure shape.
- R-67QI-MA8A: A request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table` and whose method is neither `GET` nor `HEAD`, MUST be answered with status 405, the header `Allow: GET, HEAD`, no `Location` header, and a response in the plain failure shape (`D04-panel`) for `MethodNotAllowedBody`, never in the chrome failure shape.
- R-MLTP-HH47: A response to a request whose path is `/widgets/table` whose status is neither 200 nor 304 MUST carry no `ETag` header.
- R-MN1L-V8UW: Handling a request whose path is `/widgets/table` MUST leave the widget set unchanged, whatever the request's method and whatever status it is answered with: the slice `D05-widgets`'s `Store.All` returns immediately before the request and the slice it returns immediately after are equal element for element and in the same order.
