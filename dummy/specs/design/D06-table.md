# D06-table

The widgets table, and the one route that serves it on its own. Two things
live here and nowhere else: what the table's template receives and the hooks
the table it draws must carry, and the whole of `GET`/`HEAD /widgets/table`.

The table's markup is not dummy's code. It is the `table` template in
`assets/table.html`, written and approved by a human following the
repository's `design/` and embedded by the module's root package, whose
`Assets` (`D01-layout-and-run-seam`) hands it to the panel, which parses it
into dummy's one template set (`D04-panel`). The code executes that template
by name and writes no markup of its own — not a row, not a cell, not an
escape. What it owes the template is its data, and the data is the plainest
thing that could be: the slice `D05-widgets`'s `Store.All` returns, the
widgets in creation order, each with its `Name`, `Count` and `Status`. The
page passes the same slice as its data's `Table`, and the template ranges over
it and draws one row per widget. Escaping is `html/template`'s contextual
autoescaping and nothing else; no template function is named, so the template
set carries none of dummy's, and a widget's name reaches the reader as text
whatever characters it holds. One requirement ties the code to the asset by
use: the fragment's body is exactly what executing that template on that slice
writes. Every other requirement here is a hook the template's output must
carry, stated over what dummy sends; a hook the asset lacks is an issue for
the human who owns it, never something the build writes around.

The table is one thing rendered twice. The panel page (`D04-panel`) embeds it
in the document it sends, and `/widgets/table` re-renders it alone, so that
the page's inline polling script can replace the table element with whatever
the fragment returns. That replacement is why the two renderings must be the
*same markup*, not merely two markups that satisfy the same contract: if they
merely agreed on a contract they could still differ byte for byte, and the
page would visibly change on its first poll even though nobody had created
anything. S4 says it outright: the fragment and the table the panel page
embeds are the same markup, byte for byte. So the identity is exact — not even
surrounding whitespace is excepted — and it holds for every page that embeds
the table, the panel page a `GET /widgets` returns and the 422 redraw alike,
because both are panel pages and the obligation is stated over every panel
page. One template drawing the table in both places, given the same slice, is
what makes the identity hold; one document owning the table's hooks and its
anchor is how that identity gets stated once and tested once. `D04-panel` owns
the script that performs the swap; it refers to the anchor this document
fixes, `id="widgets-table"`, and declares nothing about the table. `D04-panel`
also owns where on the page the table sits and what wraps it; this document
fixes the table span and nothing around it, so no wrapper `D04-panel` places
around the table can change a byte this document fixes.

The fragment is a fragment. There is no doctype, no `html` element, no `body`
element and no banner — a caller fetching it with `curl` sees exactly
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
only through that value. Neither is ever free text, so neither needs more than
the autoescaping every value gets. The status cell holds the marker and
nothing else, and every data row has one, the row for a widget created a
moment ago included.

The count column is marked numeric, so counts line up: the header cell over it
and every row's count cell carry class `num`. That marking belongs to the count
column alone. A `num` on the name or the status would line up as numbers a
column that holds none, so no other header or data cell carries it. The class
is fixed by exact read value on the count cells — S3 and S4 quote them as
`class="num"` — and the ban on the others looks for `num` as one of possibly
several classes, so a cell cannot slip the marking in beside another class.

The header row is required — S4 fixes "a table with a header row and one row
per widget and nothing around it". S3 and S4 now fix it whole: exactly three
cells, in order, `<th>Name</th>`, `<th class="num">Count</th>` and
`<th>Status</th>`. Since the stories quote each cell's markup, the design fixes
each cell as those exact bytes, from its `<th` start tag through its `</th>` end
tag, rather than as a read text: a column heading is a fixed label, never a
value drawn from the request, so there is nothing for the normalisation to
absorb. The count header is the second cell, just as the count is each data
row's second cell. The rows below it are one per widget, in the order
`D05-widgets`'s `Store.All` returns them, which is fixture order followed by
creation order, so a newly created widget is last.

A cell text is read through the normalisation, whose last step is the
whitespace collapse, so it can never be compared against a raw stored value.
`D05-widgets` trims only the ends of a submitted name and fixes no character
class (R-FIEM-UUKE), so `my widget` — two spaces — is a name a caller can
create and `D07-form` and `D09-mcp` require that creation to succeed.
Comparing a collapsed cell text against that raw name would be unsatisfiable
the moment such a widget existed, and three bodies would become unproducible
at once: this route's 200, the panel page's 200, and the 422. So both sides
are collapsed. Nothing is lost that was ever decidable here, and the submitted
bytes still have a home: `D07-form` reads the 422's echo out of an attribute,
and an attribute's read value, as `D04-panel` defines it, is unescaped but
never collapsed. The count and the status are collapsed too, though neither
can hold whitespace — a decimal integer, and one of three fixed words —
because a rule that bites on one of the three cells and is merely harmless on
the others is a rule a reader has to check twice. The status cell's text is
read the same way, so the marker's tags fall away in the normalisation and the
cell text is the bare word; the marker itself is fixed separately, over the
cell's markup as sent. One character does not survive the round trip, and the
name cell says so rather than pretending otherwise: `html/template` writes a
NUL (U+0000) in a value as U+FFFD, the replacement character, which is also
what an HTML parser makes of a NUL in text. A name can hold one — nothing in
`D05-widgets` forbids it — so the name cell is compared against the name with
each NUL so replaced. That is a fact about Go's `html/template`, observed by
executing a template with such a value while this was drafted; it escapes `<`,
`>`, `&`, `'`, `"` and `+` as character references, which the normalisation
unescapes, and alters nothing else.

Where this document has to read text out of markup, it uses the text
definitions `D04-panel` states — the script-stripped form (R-MW1Y-Z90S), the
normalisation (R-NGS9-HCML), the whitespace collapse (R-MBBO-H5EZ) — and the
plain failure shape it defines alongside the banner failure shape. It restates
no step of any of them: a restatement would be a second contract, and the two
would drift. Which form a tag count is taken over is `D04-panel`'s rule too,
R-07AH-B3FP, and this document neither repeats it nor keeps one of its own.
Read it there; named here only so a reader knows it exists and that it
supplies a *default* rather than a fixed frame. A requirement that names the
form it counts over is counted over the form it names, and the rule yields to
it. Only a requirement that names no form falls to the default, which is the
script-stripped form, except where the requirement names the `script` element
itself, whose default is the raw body.

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

Every failure from this endpoint is plain text — one line, with no banner —
whether or not the caller is identified. No template is executed for either: a
failure here is a fixed line, not a drawing. This is the deliberate exception
to the rule that a failure dummy can name to an identified caller is itself a
page with the banner. The reason is the swap again: the script splices
whatever comes back into a document that is already drawn, so an error drawn
as a page with the banner would arrive as a whole page pushed into a table. So
this document says which shape each of this route's failures takes, and
`D04-panel` keeps the shapes themselves. The missing-identity 500 is the one
place the two documents meet: appkit's `identity.Require` answers it for every
route at once, wrapped around the whole handler as `D04-panel` states, and
this document fixes it for this route, from the fragment side, because being a
fragment is what makes it plain. That holds without dummy doing anything
special, since the middleware answers before any route is chosen, and the
requirement is here so the fragment's story is covered where the fragment is
designed. The two failures are the missing-identity 500 and the 405; their
bodies are appkit's `identity.MissingBody` and `MethodNotAllowedBody`, which
`D04-panel` declares; this document only names them. The 405 names `Allow:
GET, HEAD` and refuses a `POST` rather than redirecting it, because creating a
widget is a different route with its own story.

This route's `ETag` is stated here, whole: every rule about the table's
validator is in this document. It is not the only `ETag` dummy sends — every
shared file under `/_appkit/` carries appkit's own, on appkit's terms
(`D08-assets`) — and nothing here reaches beyond `/widgets/table`. It is a
strong validator, an opaque quoted string determined by the rendered
fragment's bytes. The quoted characters
exclude the comma and whitespace: `If-None-Match` carries a comma-separated
list, so a tag containing a comma would be split into pieces that match
nothing, and the 304 would be unreachable while every requirement here stayed
satisfied. Excluding the comma is narrower than RFC 9110's own grammar allows,
and that is deliberate — it costs an implementation nothing and makes the list
split sound. The contract fixes the *relation* — equal content yields an equal
tag, and the tag changes when a widget is created — and never the value; no
story fixes the value, and a requirement that did would be fixing an
implementation. It is carried by the 200 and the 304 and by nothing else. The missing-identity 500 is the gate's, not this
route's, and appkit promises nothing about its validators, so this document
states none for it. S4 does not fix whether the 405 carries one:
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
commas, and so does this route: every entry of every line counts, and the
field is never refused as malformed — an entry that is not `*`, the current
tag, or `W/` followed by it simply matches nothing. That is this route's own
rule, stated whole below; the shared files follow appkit's, which
`D08-assets` states for them, and neither document leans on the other. RFC
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

The polling interval is contract, but not this document's: S4 says the page's
script re-fetches the fragment every 5 seconds, and `D04-panel` fixes that
where the script is designed. The fragment carries no trace of it — the panel
subtitle that states the interval is drawn by `D04-panel` above the panel
wrapper, in the page content outside the table span, so the fragment carries
neither the heading nor the subtitle, and a poll leaves both as they were.
That absence is stated over the fragment's tags — no `h1` element, and no
element carrying the subtitle's hook, `id="panel-subtitle"` — rather than over
the subtitle's text, because a widget's name is free text and a caller may
name a widget with words that read like a subtitle; its escaped name can add
text to a cell but never a tag.

One thing deliberately gets no requirement. The claim that "the whole of a
page's content arrives in the response body" and that "script never adds
content of its own" is owned here only in its decidable half — that the panel
page contains the table in the body it sends, and that the fragment the poller
fetches is that same table — because the other half, what a script does once
it runs, is not decidable by any procedure the standard library can run. It is
recorded here rather than silently dropped.

This document declares no exported name. The template's data is a type
`D05-widgets` already declares, `internal/panel`'s names are `D04-panel`'s
but `FormView`, which `D07-form` declares, and nothing that computes the `ETag` is exported.

## REQUIREMENTS

- R-CAXA-B1SO: The body of a 200 response to a `GET` request whose path is `/widgets/table` MUST be byte-identical to the text that executing the template named `table` in dummy's template set (`D04-panel` R-Y5H2-05Q9) writes when given as its data the **table data** for the widget set current when the response was rendered, the table data for a widget set being the slice `D05-widgets`'s `Store.All` returns for it; the table data is the data this document names for the template `table`, which a panel page's page data carries as its `Table` (`D04-panel` R-YACN-J8P1).
- R-76HS-3U6R: The body of a 200 response to a `GET` request whose path is `/widgets/table` MUST be a **table fragment** for the widget set current when the response was rendered, where a table fragment for a widget set is a string that, after optional leading whitespace, begins with a `<table` start tag as `D04-panel` defines start tags and end tags (R-LPDH-LA2H), ends with a `</table>` end tag followed by optional trailing whitespace, and contains no `<html` start tag, no `<body` start tag, no `<script` start tag, and no occurrence of `<!doctype` compared case-insensitively.
- R-CEKZ-GD0R: The `<table` start tag of a table fragment MUST carry an occurrence of the attribute `id`, as `D04-panel` defines an attribute occurrence and its read value (R-YLBQ-Z6DA), whose read value is exactly `widgets-table`.
- R-CI8O-LO8U: A table fragment MUST contain no `<h1` start tag and no start tag carrying an occurrence of the attribute `id` whose read value is exactly `panel-subtitle` (`D04-panel` R-YLBQ-Z6DA), so that neither the page's heading nor its subtitle hook (`D04-panel` R-2HK6-YSN5), which carries the panel subtitle (`D04-panel` R-2F4E-795R), is ever part of what a poll replaces.
- R-H8LI-D5DT: A table fragment MUST contain exactly one **header row** — a span from a `<tr` start tag through the next following `</tr>` end tag that contains at least one `<th` start tag and no `<td` start tag — and that header row MUST precede every **data row**, a span from a `<tr` start tag through the next following `</tr>` end tag that contains at least one `<td` start tag.
- R-HB1B-4OV7: A table fragment for a widget set MUST contain exactly one data row for each widget in that set and no other data row, the data rows appearing in the order `D05-widgets`'s `Store.All` returns those widgets.
- R-MHXI-6ORQ: The first three **cell texts** of the data row for a widget MUST be the whitespace collapse of that widget's `Name` with every U+0000 character in it replaced by U+FFFD, the whitespace collapse of its `Count` written in decimal, and the whitespace collapse of the string value of its `Status`, where the whitespace collapse is the one `D04-panel` defines (R-MBBO-H5EZ) and the cell text of a `<td` start tag is the normalisation `D04-panel` defines (R-NGS9-HCML) of the text from that start tag's closing `>` through the next following `</td>` end tag.
- R-HH4T-1JKO: The header row of a table fragment MUST contain exactly three `<th` start tags and exactly three `</th>` end tags, and the span from each `<th` start tag through the next following `</th>` end tag MUST be exactly `<th>Name</th>` for the first, exactly `<th class="num">Count</th>` for the second, and exactly `<th>Status</th>` for the third.
- R-HICP-FBBD: In every data row of a table fragment, the second `<td` start tag MUST carry an occurrence of the attribute `class` whose read value is exactly `num`.
- R-HM0E-KMJG: In the data row for a widget, the text from the closing `>` of the third `<td` start tag up to the `<` of the next following `</td>` end tag, once its leading and trailing ASCII whitespace is removed, MUST consist of exactly a `<span` start tag carrying an occurrence of the attribute `class` whose read value is exactly `status` and an occurrence of the attribute `data-status` whose read value is exactly the string value of that widget's `Status` (`D05-widgets`), then that same string value, then a `</span>` end tag, and nothing else.
- R-HOG7-C60U: Every `<th` or `<td` start tag in a table fragment, other than the second `<th` start tag of its header row and the second `<td` start tag of each of its data rows, MUST NOT carry an occurrence of the attribute `class` whose read value, split on ASCII whitespace, has `num` as a member.
- R-MJ5E-KGIF: Every panel page (`D04-panel`) MUST contain, counted over that page's script-stripped form as `D04-panel` defines it (R-MW1Y-Z90S), exactly one `<table` start tag and exactly one `</table>` end tag, and the text of that script-stripped form from that start tag through that end tag inclusive — the page's **table span** — MUST be a table fragment for the widget set current when the page was rendered.
- R-HUJP-90QB: For one and the same widget set, the table span of every panel page (`D04-panel`) and the body of a 200 response to a `GET` request whose path is `/widgets/table` MUST be byte-identical, with no leading or trailing whitespace excepted.
- R-HWZI-0K7P: A `GET` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries no `If-None-Match` field whose condition succeeds, MUST be answered with status 200 and the header `Content-Type: text/html; charset=utf-8`.
- R-HZFA-S3P3: A 200 response to a request whose path is `/widgets/table` MUST carry an `ETag` header whose value is a strong validator: a `"`, then at least one character, none of which is a `"`, a comma or an ASCII whitespace character, then a `"`, with no `W/` prefix and nothing before or after the quoted string.
- R-I1V3-JN6H: Two 200 responses to `GET` requests whose path is `/widgets/table` whose bodies are byte-identical MUST carry `ETag` values that are byte-identical.
- R-I4AW-B6NV: A 200 response to a request whose path is `/widgets/table` sent after a widget has been created MUST carry an `ETag` value differing from the `ETag` value carried by the last such response sent before that creation.
- R-I7YL-GHVY: For a `HEAD` request whose path is `/widgets/table` and the otherwise identical `GET` request, immediately before each of which the slice `D05-widgets`'s `Store.All` returns is equal element for element and in the same order, the `HEAD` request MUST be answered with the status, the `ETag` value and every other header the `GET` request is answered with, and with an empty body.
- R-IAEE-81DC: A `GET` or `HEAD` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries an `If-None-Match` field at least one of whose entries — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is exactly `*`, is byte-identical to the `ETag` value the same request would be answered with were the field absent, or is `W/` followed by that value, MUST be answered with status 304, that same `ETag` value, and an empty body.
- R-ICU6-ZKUQ: A `GET` or `HEAD` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries an `If-None-Match` field no entry of which — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is `*`, the `ETag` value the same request would be answered with were the field absent, or `W/` followed by that value, MUST be answered exactly as it would be were the field absent, and the `ETag` value it is answered with MUST be such that no entry of that field is that value or `W/` followed by it.
- R-IF9Z-R4C4: A request whose path is `/widgets/table` and whose `X-User-Id` header is absent or present with an empty value MUST be answered with status 500 and a response in the plain failure shape (`D04-panel`) for appkit's `identity.MissingBody`, never in the banner failure shape.
- R-IIXO-WFK7: A request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table` and whose method is neither `GET` nor `HEAD`, MUST be answered with status 405, the header `Allow: GET, HEAD`, no `Location` header, and a response in the plain failure shape (`D04-panel`) for `MethodNotAllowedBody`, never in the banner failure shape.
- R-0142-4LXJ: A response to a request carrying a non-empty `X-User-Id` header whose path is `/widgets/table` and whose status is neither 200 nor 304 MUST carry no `ETag` header.
- R-INTA-FIIZ: Handling a request whose path is `/widgets/table` MUST leave the widget set unchanged, whatever the request's method and whatever status it is answered with: the slice `D05-widgets`'s `Store.All` returns immediately before the request and the slice it returns immediately after are equal element for element and in the same order.
