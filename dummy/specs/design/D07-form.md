# D07-form

Creating a widget from the panel is one of the two ways dummy's state changes
(the other is the MCP tool `create_widget`, `D09-mcp`), and this document owns
all of it: the data the form's template receives, the hooks the form and the
card it sits in must carry, and the whole of `POST /widgets`.

The form's markup is not dummy's code. It is the `form` template in
`assets/form.html`, written and approved by a human following the repository's
`design/`, embedded by the module's root package and handed out by its
`Assets` (`D01-layout-and-run-seam`), and parsed into dummy's one template set
(`D04-panel`), where the page invokes it with its data's `Form`. The code
executes that template by name and writes no markup of its own. What the code
owes it is its data, a `FormView`: the `Submission` to echo, the `FieldErrors`
to place beside the fields, the statuses to offer, and the status to select.
On a `GET /widgets` page all but the statuses are zero, so the same template
draws an empty form; on a 422 they carry the rejected submission. Escaping is
`html/template`'s contextual autoescaping and nothing else, and no template
function is named, so the template set carries none of dummy's — the `plus`
icon is markup in the asset, not a value the code supplies. One requirement
ties the code to the asset by use: the form card in a panel page is exactly
what executing that template on the form view for that page writes. Every
other requirement here is a hook that output must carry, stated over what
dummy sends; a hook the asset lacks is an issue for the human who owns it,
never something the build writes around.

The form is an ordinary HTML form. It POSTs to `/widgets` with
`application/x-www-form-urlencoded`, it has one control per widget field —
`name`, `count`, `status` — and nothing about a submission is assembled by
JavaScript, so a caller with `curl` submits exactly what a browser submits.
That promise is what makes the two halves of `S5-form` one story: the keys,
the method and the encoding are fixed in the markup, so there is one
submission shape and dummy validates it the same way whoever sent it. The
status control offers exactly the three choices, which is why a person in a
browser cannot reach the status rejection at all and a `curl` caller can.

A hook is contract here only where a story fixes it or a promise needs it to
be decidable. The card and its button are the first kind, and they are
described below. The rest are anchors, and they have to be, because `S5-form`
fixes *where* a message sits and not merely that one appears: "a reader sees
which field each message is about by where it sits". A promise the design
cannot test is a promise the build run cannot keep. The anchor set is
deliberately the smallest one that makes those promises decidable:
`id="<field>-error"` and `aria-describedby` for the per-field placement,
`method` and `action` on the form, and `name` and `value` on the three
controls. The `enctype` clause belongs to that minimum too, and it costs
nothing: HTML's default for a POST form already satisfies it, and without it a
form could be marked `multipart/form-data` and a browser's own submission
would come back 415. The submit control belongs to it for the same reason, one
step earlier. Keeping a browser's default submission valid is pointless if the
browser cannot submit at all, and three fields with no submit button would
satisfy every other requirement here while `S5-form`'s "a user adds a widget"
failed for the person in front of the page. A person cannot type a `curl`
command into a form.

Which buttons count is settled by HTML's invalid-value default rather than by
the word `submit`. A `<button>` whose `type` is missing, empty, or anything at
all other than `reset` or `button` is a submit button, and a browser submits
the form through it. The definition below is therefore written that way round
— not `reset` and not `button` — so that `<button type="">` or
`<button type="x">` cannot slip past the constraints that follow merely by
failing to say `submit`. An `<input>` is not symmetrical with it: there the
invalid-value default is the text field, so only `type="submit"` submits, and
the one other input that does, `type="image"`, is dealt with separately below.

The submit control is constrained as well as required, because in HTML it can
undo the form it sits in. A submit control may carry `formaction`, `formmethod`
and `formenctype`, and each of those overrides the form's own `action`,
`method` and `enctype` for the submission made through it. A button marked
`formenctype="multipart/form-data"` therefore reproduces exactly the 415 the
`enctype` clause exists to prevent, while `method`, `action` and `enctype`
still read correctly on the `<form` start tag and every other requirement here
passes. Those three attributes are forbidden on the submit control, which is
the cheapest way to make the form's own three the ones a browser actually uses.
A `name` is forbidden for the neighbouring reason: a named button makes a
browser send a fourth key a `curl` caller does not, and one submission shape is
the whole of the promise.

`<input type="image">` is a submit control in HTML too, and it is forbidden
outright rather than folded into the definition above, because the `name` ban
would not save it. A browser submitting through an image input sends the
cursor's coordinates as well — `x` and `y` when the control has no name,
`<name>.x` and `<name>.y` when it has one — so it breaks key parity whether or
not it is named. The named case is already out, since R-8D69-HT52 permits no
`<input` carrying a `name` beyond the two field controls; the unnamed case is
the one that gets through, and it is precisely the case a `name` ban cannot
reach. A flat ban on an `<input` whose `type` reads `image` is one scan and
leaves nothing to reason about, which is why it is the form the requirement
takes. Nothing is lost by it: dummy's one submit affordance is a labelled
button, whose picture, the `plus` icon, sits inside the `button` element
rather than replacing it, and a graphical submit would cost the key parity
every other clause here is spent on.

**The form sits in a card headed `Add widget`.** `S3-panel` and `S5-form` fix
that heading, and `S3-panel` places it as "a heading one level beneath the
page's `Widgets` heading". The page's heading is an `h1` (`D04-panel`), so the
card's heading is one level below it, an `h2`. The card's markup is the
platform's mock of this very panel in `design/` (`design/ikigenba/app.html`),
which the stylesheet reads as `.card > header` and `.text-md`: a `section`
with the class `card`, whose first child is a `header` holding the `h2` of
class `text-md`, and then the form. The `header` is part of the contract, and
not just the class, because the stylesheet styles a card's heading through it.
The requirement is anchored on the one `<form` start tag in the page content
(`D04-panel` R-1DYY-P2E0) rather than on the count of cards or sections in the
page; the page's other form, the banner's sign-out form, sits in the appkit
banner (`D04-panel`), outside the page content, and so is never the widget
form. The form card is then decidable without an HTML parser, because it is
the chain of tags that runs immediately up to that start tag, plus the one end
tag that immediately follows the form. It also leaves the rest of the page to
`D04-panel`. The chain allows only whitespace between its links, so nothing
can sit between the heading and the form, and nothing can sit between the form
and the end of the card.

Where the card sits is `D04-panel`'s. The page's heading and subtitle come
first, then the page's `.panel` wrapper holding the table and then the form
card. That is a statement about how the page composes parts that cannot see
each other, and this document says nothing about the table. The stories'
side-by-side and stacked arrangements are not something a check without a
browser can observe, so the contract for them is that markup.

**The button is the `plus` icon, then `Add widget`.** `S3-panel` fixes "the
form's button reads `Add widget`, with the `plus` icon drawn before the text
and hidden from assistive technology", so its accessible text is `Add widget`
alone. That is the platform's mock too. The icon's drawing is the asset's —
the Tabler outline `plus` from `design/`, inline — and no requirement restates
its bytes: which picture an `svg` draws is appearance, and appearance is
`design/`'s. What the story makes observable, and this document fixes, is the
hook: the button's content is one `svg` element, marked `aria-hidden="true"`
and holding no text, followed by a run of text with no element in it. So the
widget form contains exactly one `button` of any type, that button is the
form's only submit control, and its content is exactly that. That rules out an
`img` or a second icon with a single scan, puts the icon before the words, and
the text, once normalised, is exactly `Add widget`; the icon is hidden and
carries no text, so the words are the button's whole accessible text.
Counting every `button`, not only the
submit controls, is what makes "the form's button" name one thing: a
`type="button"` or `type="reset"` button carrying an icon would otherwise sit
beside it unchecked, and for the same reason the form holds no `<input` whose
`type` is `button` or `reset`, named or not. This narrows the "at least one
submit control" fixed above to exactly one, and that definition of a submit
control and its ban on `name`, `formaction`, `formmethod` and `formenctype`
still apply to it.

**The route has four answers and no fifth.** A request without identity is a
500, decided before anything else by appkit's `identity.Require`, which
`D04-panel` wraps around the whole handler; that answer is not restated here,
but the *ordering* is stated here, because `S5-form` extends it from path and
method to the submitted fields: a body that would have been accepted and a
body that would have been rejected are answered identically, and neither is
read. A submission whose media type is not `application/x-www-form-urlencoded`
is a 415: the objection is to the format, not to the values, and no retry that
keeps the media type can succeed, so a 422 would be an invitation to send
better values that could never be taken up. The body is not read at all, no
widget form and no field errors come back, and there is no JSON way in through
this route. The 415 page is drawn with the banner like the 404 and the 405, so
it carries the banner's sign-out form; what it lacks is a form in its page
content, where its message and its link to `/widgets` sit inside the one
`main` (`D04-panel`). A submission dummy reads and accepts is a 303 to
`/widgets` with an empty body. A submission dummy reads and rejects is a 422
whose body is a panel page.

**Why the success is a redirect.** The browser must not be left showing the
result of a POST: after a 303 it fetches `/widgets` with a GET, so the address
the user ends on is the panel and refreshing re-reads the panel instead of
submitting a second time. There is no flash message and no confirmation
notice. dummy sets no cookie and puts nothing in the URL, so nothing carries a
message across the redirect — the new row in the table is the confirmation,
and the redirect's target is exactly `/widgets`, bare. No requirement below
says "there is no flash message": "a message" is not something a check can
recognise in a page. Two kinds of requirement close it off instead. The first
fixes the carriers, every one a message could ride: the 303's `Location` is
exactly `/widgets` with no query string and no fragment and its body is empty
(stated below), and no answer carries a `Set-Cookie` header, which
`D04-panel` states once for every response and this document
does not repeat. Nothing else survives a redirect. The second fixes the page:
`D04-panel` puts everything between the banner and the footer inside the one
`main` and leaves nothing in a panel page's content but the heading
block and the panel wrapper, and the form card's opening is
fixed from its `<section` start tag to the `<form` start tag and its closing
from `</form>` to `</section>` (below), so a notice has nowhere to sit
around the card or the table. The contents of the widget form between its
controls are not fixed, so text placed there breaks no requirement; the
carriers are what keep a message from existing to be placed. The absence of
a notice is therefore a consequence of decidable requirements rather than a
requirement of its own, which is the most this contract can honestly claim.

**The 422 body is a panel page**, and "panel page" is a document shape
`D04-panel` owns rather than a route. This document refers to that shape and
re-describes none of the banner, the head or the page's heading and subtitle,
all of which are `D04-panel`'s. Because those are required of every panel
page, the 422 redraw is drawn with the same banner, carries all of its content
inside the page's one `main`, opens that content with the heading and the
subtitle that counts the rows its own table shows — the widgets as they are,
since nothing was created — carries the script that re-fetches the table every
5 seconds, and sits the redrawn form in its card headed `Add widget` just as a
`GET /widgets` does. What this document does state is what a 422 adds: the
form carries the values the caller submitted, an error message sits beside
each rejected field and beside no other, and nothing was created — so the
table the shape requires holds exactly the widgets that were there before, in
the order they were in.

**The echo is raw wherever the control can hold bytes.** All three values are
trimmed before they are judged, but the 422 re-displays the two free-text
fields exactly as the caller submitted them: `S5-form` fixes a 41-character
name coming back "in full, unshortened", and the count field holding the text
`three` "as it was typed". So the form view carries the `Submission`
(`D05-widgets` R-ISI4-P0AR), which holds the raw strings, and the requirement
below ties those two echoed attribute values to it, never to anything parsing
or the rules produced. `html/template` writes a NUL (U+0000) in an attribute
value as U+FFFD, as an HTML parser would read it anyway — observed by
executing a template with such a value while this was drafted — so that one
character is the exception the requirement names; every other character comes
back, through the character references the autoescaping writes and the read
value unescapes. That is also
the reason `D04-panel`'s read value of an attribute unescapes character
references (R-YLBQ-Z6DA): the raw echo of an arbitrary submitted string is
precisely where `&` and `<` show up, and a comparison that did not unescape
would fail on input the caller chose. A read value is unescaped and otherwise
unaltered — not normalised — because whitespace inside a raw echo is part of
what was submitted.

Echoing arbitrary caller bytes into markup is exactly where a design can hand
a caller a tag, and the requirement that closes that is `D04-panel`'s
R-HZC4-EYC9: the tag-name sequence of a document dummy sends cannot depend on
the echoed `Name`, `Count` or `Status`, and neither does how many `>`
characters it holds, so whatever the caller submits comes back as a value and
never as structure. That rule is D04's and is not restated
here; because it holds, the echo below can stay raw, and it does.

**Caller bytes land inside an attribute value here, and only here.** The
template writes `value="{{...}}"` and `html/template` does the rest: inside a
double-quoted value it writes `"`, `'`, `&`, `<`, `>` and `+` as character
references and leaves every other character as it is, `=` and white space
included. A submitted name of `x" value="zzz` therefore comes back as one
double-quoted value whose text merely contains the characters ` value=`; a
browser reads one attribute, and so must anything that reads attributes out of
what dummy sends. dummy writes no escaping of its own to second-guess that,
because no story asks for anything a browser would see differently.

The status control is the one field whose echo cannot be literal, and the
difference is the control rather than an inconsistency. A select cannot hold
arbitrary bytes at all: it selects one of its three options, or none. There is
therefore nothing of the caller's to give back byte for byte, and the faithful
rendering is instead the option the submission would actually have used — the
one equal to the *trimmed* status, the value `ParseSubmission` parses
(`D05-widgets`), which is exactly the `Status` of the `Draft` it returns and so
exactly what the form view selects.
So on a 422 the control offers the same three choices and selects the one
matching the trimmed status, and selects none when the trimmed status is not
one of the three words, which is exactly the `archived` case. Selecting on the
raw value instead would answer a submission of `status=%20active` — perfectly
acceptable everywhere else, since trimming happens before validation — with
nothing selected whenever some other field failed. The raw-echo principle does
not ask for that: it exists so a free-text control hands the caller back their
own bytes, and a closed enumeration never held any.

**Every wrong field is reported at once.** A submission is judged in the two
layers `D05-widgets` defines. `ParseSubmission` turns the three strings into a
`Draft`, reporting a count that is not a whole number and a status that is not
one of the three; then the rules for a widget, the same ones the MCP tool
`create_widget` applies (`D09-mcp`), judge the `Draft`. A submission's field
errors combine the two, field by field: the parse message where parsing
failed, and otherwise the rules' message. So
`name=&count=three&status=archived` earns all three messages from one answer —
the name from the rules, the count and status from parsing — and
`name=alpha&count=-1&status=active` earns the taken name and the negative
count, both from the rules. When parsing found nothing, the handler creates
through `Store.Create`, which decides the rules and adds the widget in one
step, so two submissions racing for one name cannot both win; the field
errors are then what `Create` reported, so the answer follows what `Create`
actually did — a 303 only for the submission that created the widget, a 422
with the taken-name message for the one that lost; when parsing
found something, nothing is created, and `Store.Check` supplies the rules'
messages for the fields that did parse. The per-field requirements are written
over the field errors rather than over inputs, so "one message beside each
rejected field and none beside any other" is one rule that holds for one bad
field and for three. `D05-widgets` owns the parsing, the rules and the six
message constants — `NameRequiredMessage`, `NameTooLongMessage`,
`NameTakenMessage`, `CountNotWholeMessage`, `CountNegativeMessage` and
`StatusNotAllowedMessage` — and this document names them only through the
field of the field errors that carries them. No rule of theirs is restated
here; a second statement of a rule is a second contract that will drift. The
same applies to the definitions `D04-panel` states once for the whole design:
start tags and end tags (R-LPDH-LA2H), an attribute occurrence and its read
value (R-YLBQ-Z6DA), the script-stripped form (R-MW1Y-Z90S), normalisation
(R-NGS9-HCML), an HTML document dummy sends (R-YGG5-G3EI), a panel page and
the banner failure shape. The phrase "carries identity" is the one this
document defines for itself, as appkit's `identity.Require` decides it: a
first `X-User-Id` value that is present and not empty. This document cites
each where it uses it and restates no step of any of them.

Reading the message out of the markup rests on one assumption, so the
assumption is stated as contract: the element whose `id` reads `<field>-error`
has no child element. Taking the text from that start tag to the next `<` is
the element's whole content only if nothing is nested inside it; without the
rule, wrapping half a message in an `em` would make the check read a prefix
and pass or fail on markup nobody meant to fix.

That creating a widget is the *only* interaction that changes state is not one
document's to state. `D04-panel` fixes that its page and error routes leave
the store alone, `D06-table` fixes it for the fragment, and this document
fixes it for the two refusals and for the request that never gets as far as
the fields. Those together are the claim; none of them repeats another.

Two things are deliberately absent. The half of `S5-form`'s "nothing is
assembled by JavaScript" that concerns JavaScript is not decidable with the
standard library, and no requirement pretends otherwise; what is decidable —
that the submission's keys, method and encoding are in the markup the server
sent — is fixed below, and that is what a `curl` caller actually depends on.
And no requirement here names a validation outcome for a particular input:
which message a value earns is `D05-widgets`'s, and the nine rejection cases
of `S5-form` are covered here as HTTP behavior over whatever the field errors
report.

Two requirements below compare two requests that differ in one thing and demand
one answer: the 500 that never looks at the body, and the `Accept` header that
changes nothing. A comparison like that means something only if everything else
really is equal, and the thing most easily unequal is the store — a creation
landing between the two requests changes the table inside a panel page and
falsifies the claim without any handler misbehaving. Both therefore pin the
store's contents equal immediately before each of the two requests, which is
the form `D04-panel` settled for the whole design.

Every check below is runnable with the Go standard library alone: there is no
HTML parser, so attributes are read by `D04-panel`'s single rule for an
attribute occurrence and its read value (R-YLBQ-Z6DA), spans are picked out by
the start tags and end tags it defines (R-LPDH-LA2H), and every "exactly one"
count is taken over the script-stripped form, or over the page content that
`D04-panel` cuts from it (R-1DYY-P2E0), so that the panel's inline script
cannot be mistaken for markup. This document states no attribute rule of its
own, and that is the point: D04's occurrence requires a whitespace character
before the attribute's name and an `=` after it, so a scan for the `name`
attribute does not match inside `data-name`, and `id` does not match inside
`data-id` — a rule about values alone would have matched both. That same `=` is
why no attribute is written bare in the markup dummy sends: a boolean attribute
such as `selected` needs a double-quoted value to be an occurrence at all, so
the two requirements below that turn on a `selected` attribute are satisfied by
`selected="selected"` and not by a bare `selected`. Which value it carries is
not fixed — those two turn on whether the occurrence is there, not on what it
reads — but some value must be written.

**A widget created is recorded.** Creating a widget is dummy's one domain event. When the route answers 303, it has created a widget, and the handler's telemetry writer (`D04-panel`) records `widget.created`, whose one attribute, `widget`, is the new widget's id (`D05-widgets`), between the request's `request.started` and `request.finished` and under the same request id and user. It never carries the name, count or status the user submitted: those are the store's to answer. Every other answer — a 422, a 415 or the missing-identity 500 — created nothing and records no `widget.created`; its `request.finished` carries the status.

## REQUIREMENTS

- R-IQ93-720D: The `internal/panel` package MUST export `type FormView` as a struct with the fields `Submission widget.Submission`, `Errors widget.FieldErrors`, `Statuses []widget.Status`, and `Selected widget.Status`, where `widget` is the package `internal/widget` (`D05-widgets`).
- R-MMT3-PRQI: dummy's design defines the **field errors** of a form-encoded submission, and every requirement in dummy's design that names the field errors of a submission MUST denote that value, as follows, where `d` and `p` are the `Draft` and the `FieldErrors` that `ParseSubmission` (`D05-widgets`) returns for the `Submission` that submission produces: when `p.Any()` is true, the `FieldErrors` (`D05-widgets`) value each of whose three fields is `p`'s message for the field when it is non-empty, and otherwise the message for the field in the `FieldErrors` that `Store.Check` (`D05-widgets`) returns for `d` when called on the store `Handler` was built over immediately before the request; when `p.Any()` is false, the request's creation errors (R-MLL7-BZZT), so that the field errors are always the offences that decided whether a widget was created.
- R-7R82-LXSK: Every response body that is a panel page, as `D04-panel` defines a panel page, MUST have page content, as `D04-panel` defines it (R-1DYY-P2E0), that contains exactly one `<form` start tag and exactly one `</form>` end tag, as `D04-panel` defines start tags and end tags (R-LPDH-LA2H), the start tag first; the widget form is the text of that page content from that start tag through that end tag, so that the banner's sign-out form (`D04-panel`), which lies in the banner and outside the page content, is never the widget form.
- R-CLWD-QZGX: The widget form's `<form` start tag MUST carry an occurrence of the attribute `method`, as `D04-panel` defines an attribute occurrence and its read value (R-YLBQ-Z6DA), whose read value is `post` compared case-insensitively, and an occurrence of the attribute `action` whose read value is exactly `/widgets`, and MUST carry either no occurrence of the attribute `enctype` or one whose read value is `application/x-www-form-urlencoded` compared case-insensitively.
- R-8D69-HT52: The widget form MUST contain exactly one `<input` start tag, as `D04-panel` defines start tags (R-LPDH-LA2H), carrying an occurrence of the attribute `name`, as `D04-panel` defines an attribute occurrence and its read value (R-YLBQ-Z6DA), whose read value is `name`, exactly one `<input` start tag carrying such an occurrence whose read value is `count`, and exactly one `<select` start tag carrying such an occurrence whose read value is `status`, and MUST contain no other `<input`, `<select` or `<textarea` start tag carrying an occurrence of the attribute `name`; the control of a field is the start tag carrying an occurrence of the attribute `name` whose read value is that field's key.
- R-8XWJ-ZWQV: The status control's element — the text from its `<select` start tag through the next `</select>` end tag (`D04-panel` R-LPDH-LA2H) — MUST contain exactly three `<option` start tags, the read values of whose occurrences of the attribute `value` (`D04-panel` R-YLBQ-Z6DA) are, in document order, the three values `Statuses()` returns (`D05-widgets`).
- R-9IMU-I0CO: The widget form MUST contain at least one submit control, a submit control being a `<button` start tag (`D04-panel` R-LPDH-LA2H) that carries either no occurrence of the attribute `type` or an occurrence whose read value is neither `reset` nor `button`, each compared case-insensitively (`D04-panel` R-YLBQ-Z6DA), or an `<input` start tag carrying an occurrence of the attribute `type` whose read value is `submit` compared case-insensitively; and every submit control the widget form contains MUST carry no occurrence of the attribute `name`, so that a browser's submission carries the same keys a `curl` caller sends, and no occurrence of the attribute `formaction`, `formmethod` or `formenctype`, so that the submission a browser makes through it uses the action, method and encoding R-CLWD-QZGX fixes on the `<form` start tag.
- R-A4L1-DVP6: The widget form MUST contain no `<input` start tag (`D04-panel` R-LPDH-LA2H) carrying an occurrence of the attribute `type` whose read value is `image` compared case-insensitively (`D04-panel` R-YLBQ-Z6DA).
- R-APBB-VZAZ: In the page content of every panel page, as `D04-panel` defines a panel page (R-0ZWM-4W0L) and page content (R-1DYY-P2E0), the widget form's `<form` start tag MUST be immediately preceded, with nothing but ASCII whitespace between successive items, by these items in this order: a `<section` start tag carrying an occurrence of the attribute `class` whose read value is exactly `card` (`D04-panel` R-LPDH-LA2H, R-YLBQ-Z6DA), a `<header` start tag, an `<h2` start tag carrying an occurrence of the attribute `class` whose read value is exactly `text-md`, a run of characters containing no `<`, an `</h2>` end tag and an `</header>` end tag; and the widget form's `</form>` end tag MUST be immediately followed, with nothing but ASCII whitespace between, by a `</section>` end tag; the **form card** is the text from that `<section` start tag through that `</section>` end tag, and the form card's **heading text** is the normalisation, as `D04-panel` defines it (R-NGS9-HCML), of that run of characters.
- R-JDF6-GP3K: The form card's heading text MUST be exactly `Add widget`.
- R-MP8W-HB7W: In every panel page, the form card (R-APBB-VZAZ) MUST be byte-identical to the text that executing the template named `form` in dummy's template set (`D04-panel` R-Y5H2-05Q9) writes when given the page's **form view** as its data, with leading and trailing ASCII whitespace removed from that text; the form view is the data this document names for the template `form`, which a panel page's page data carries as its `Form` (`D04-panel` R-YACN-J8P1); the form view of the body of a 422 answer to a `POST /widgets` request is the `FormView` whose `Submission` is the `Submission` that request produced, whose `Errors` is that submission's field errors (R-MMT3-PRQI), whose `Statuses` is the slice `widget.Statuses()` returns, and whose `Selected` is the `Status` of the `Draft` that `ParseSubmission` returns for that `Submission`; the form view of every other panel page is the `FormView` whose `Statuses` is the slice `widget.Statuses()` returns and whose other fields are zero.
- R-BA1M-E2WS: The widget form MUST contain exactly one `<button` start tag (`D04-panel` R-LPDH-LA2H), whatever its `type`, and no other submit control, as R-9IMU-I0CO defines a submit control, and MUST contain no `<input` start tag carrying an occurrence of the attribute `type` whose read value is `button` or `reset` compared ASCII case-insensitively (`D04-panel` R-YLBQ-Z6DA); that `<button` start tag MUST itself be a submit control and MUST be followed, with nothing but ASCII whitespace between, by an `<svg` start tag carrying an occurrence of the attribute `aria-hidden` whose read value is exactly `true`; the text from that `<svg` start tag through the first `</svg>` end tag following it, the button's **icon**, MUST contain no other `<svg` start tag and MUST have a normalisation (`D04-panel` R-NGS9-HCML) that is the empty string; and the icon MUST be immediately followed by a run of characters containing no `<`, then a `</button>` end tag, the normalisation of that run being exactly `Add widget`, so that the button draws one icon, hidden from assistive technology, before its text, contains no other element, and has `Add widget` alone as its accessible text.
- R-6LRH-LQKY: In the body of a 422 answer to a `POST /widgets` request, the read value of the occurrence of the attribute `value` on the widget form's `name` control, as `D04-panel` defines an attribute occurrence and its read value (R-YLBQ-Z6DA), MUST be exactly the `Name` field of the `Submission` (`D05-widgets` R-ISI4-P0AR) that request produced, and the read value of the occurrence of the attribute `value` on its `count` control MUST be exactly that `Submission`'s `Count` field, each with every U+0000 character replaced by U+FFFD and unaltered in any other way, an absent occurrence counting as the empty string.
- R-JOE9-WMRT: In the body of a 422 answer to a `POST /widgets` request, when the trimmed status (`D05-widgets` R-FFYU-3B30) of the `Submission` that request produced is exactly one of the three `option` values of the status control, that `<option` start tag MUST carry a `selected` attribute and MUST be the only one in the widget form that does, and when that trimmed status is none of the three, the widget form MUST contain no `<option` start tag carrying a `selected` attribute.
- R-JPM6-AEII: In a response body that is a panel page and is not the body of a 422 answer to a `POST /widgets` request, the `value` attributes of the widget form's `name` and `count` controls MUST each be absent or empty, and the widget form MUST contain no `<option` start tag carrying a `selected` attribute.
- R-D08H-WDQ7: The field error text of a field in an HTML document MUST be read as the normalisation, as `D04-panel` defines it (R-NGS9-HCML), of the text, in that document's script-stripped form as `D04-panel` defines it (R-MW1Y-Z90S), from the `>` ending the start tag that carries an occurrence of the attribute `id` whose read value is `<field>-error` (`D04-panel` R-YLBQ-Z6DA) up to the next following `<`, where `<field>` is that field's key; normalising that text script-strips it a second time, which removes nothing from it, because that text is a substring of that script-stripped form and such a form contains no `script` start tag and no `style` start tag (`D04-panel` R-YSN5-9STG).
- R-BURW-W6IL: An HTML document dummy sends, as `D04-panel` defines one (R-YGG5-G3EI), MUST contain, for each of `name-error`, `count-error` and `status-error`, at most one start tag carrying an occurrence of the attribute `id` whose read value is that string (`D04-panel` R-LPDH-LA2H, R-YLBQ-Z6DA), and the element carrying such an occurrence MUST contain no child element: the first `<` at or after the `>` ending that start tag MUST be immediately followed by `/`.
- R-MQGS-V2YL: For each field whose message in the field errors (R-MMT3-PRQI) of a form-encoded submission is non-empty, the body of the 422 answer to that submission MUST contain a start tag carrying an occurrence of the attribute `id` whose read value is `<field>-error` (`D04-panel` R-YLBQ-Z6DA), that field's field error text in that body MUST be exactly that message, and that field's control MUST carry an occurrence of the attribute `aria-describedby` whose read value is exactly `<field>-error`.
- R-MROP-8UPA: For each field whose message in the field errors (R-MMT3-PRQI) of a form-encoded submission is empty, the body of the 422 answer to that submission MUST contain no start tag carrying an occurrence of the attribute `id` whose read value is `<field>-error` (`D04-panel` R-YLBQ-Z6DA), and that field's control MUST carry no occurrence of the attribute `aria-describedby`.
- R-DMHD-83PR: An HTML document dummy sends, as `D04-panel` defines one (R-YGG5-G3EI), that is not the body of a 422 answer to a `POST /widgets` request MUST contain no start tag carrying an occurrence of the attribute `id` whose read value is `name-error`, `count-error` or `status-error`, and MUST contain no `<input` start tag and no `<select` start tag carrying an occurrence of the attribute `aria-describedby` (`D04-panel` R-YLBQ-Z6DA).
- R-K5GV-9F5J: A `POST /widgets` request MUST be a form-encoded submission when the value of its `Content-Type` header, truncated at the first `;` and with leading and trailing whitespace removed, equals `application/x-www-form-urlencoded` compared case-insensitively, and MUST NOT be one otherwise, a request carrying no `Content-Type` header included.
- R-MLL7-BZZT: A request **carries identity** when its first `X-User-Id` value is present and not empty. For a `POST /widgets` request that carries identity and is a form-encoded submission, the `Submission` (`D05-widgets`) that request produces MUST be the one whose `Name`, `Count` and `Status` are the values the request body carries for the keys `name`, `count` and `status` respectively, none of them altered, taking, for each of those three keys, the first value when the body carries the key more than once and the empty string when the body carries no value for it; when `Any()` on the `FieldErrors` that `ParseSubmission` (`D05-widgets`) returns for that `Submission` is false, the request MUST have exactly the effect on the store `Handler` was built over of one call of `Store.Create` (`D05-widgets`) for the `Draft` `ParseSubmission` returns for that `Submission`, made in its place, and no other effect on that store, and this document calls the `FieldErrors` that `Create` call returns the request's **creation errors**; and when it is true, the handler MUST add no widget.
- R-MO10-3JH7: The handler MUST answer a `POST /widgets` request that carries identity and is a form-encoded submission with status 303 when `Any()` on that submission's field errors (R-MMT3-PRQI) is false, which is exactly when the `Store.Create` call of R-MLL7-BZZT added the widget, and with status 422 when it is true, whether parsing or `Store.Create` reported the offences.
- R-KE05-XTCE: The 303 answer to a `POST /widgets` request MUST carry a `Location` header whose value is exactly `/widgets`, with no query string and no fragment, and an empty body.
- R-KGFY-PCTS: After a `POST /widgets` request the handler answered 303, the sequence `Store.All()` (`D05-widgets`) returns MUST be the sequence it returned immediately before that request with exactly one element appended at the end, whose `Name` is the trimmed name (`D05-widgets` R-FIEM-UUKE) of the `Draft` `ParseSubmission` returns for the `Submission` that request produced, and whose `Count` and `Status` are that `Draft`'s `Count` and `Status`.
- R-DOX5-ZN75: The body of the 422 answer to a `POST /widgets` request MUST be a panel page, as `D04-panel` defines a panel page (R-0ZWM-4W0L).
- R-KLBK-8FSK: A `POST /widgets` request the handler answered 415 or 422 MUST leave the sequence `Store.All()` returns equal to the sequence it returned immediately before that request, element for element, in order, and with each element's `Name`, `Count` and `Status` equal, so that no widget is added, removed, reordered or altered.
- R-KNRC-ZZ9Y: A `POST /widgets` request that carries identity and is not a form-encoded submission MUST be answered with status 415 and a response in the banner failure shape for `UnsupportedMediaTypeMessage`, as `D04-panel` defines that shape.
- R-CFI7-EA4E: In answering a `POST /widgets` request that carries identity and is not a form-encoded submission, the handler MUST NOT read any byte of the request body, that answer's body MUST have page content (`D04-panel` R-1DYY-P2E0) containing no `<form` start tag (`D04-panel` R-LPDH-LA2H), and that body MUST contain no start tag carrying an occurrence of the attribute `id` whose read value is `name-error`, `count-error` or `status-error` (`D04-panel` R-YLBQ-Z6DA).
- R-KSMY-J28Q: A `POST /widgets` request that does not carry identity MUST be answered without reading any byte of the request body and without changing the sequence `Store.All()` returns, and two such requests differing only in their bodies, immediately before each of which the sequence `Store.All()` returns is equal element for element, in the same order and with each element's `Name`, `Count` and `Status` equal, MUST receive the same status, the same value for every header the handler sets, and the same body.
- R-KV2R-ALQ4: The handler MUST answer every `POST /widgets` request with status 500, 415, 303 or 422, and with no other status.
- R-KWAN-ODGT: No answer the handler sends to a `POST /widgets` request MUST carry a `Content-Type` header whose media type is `application/json`, and two `POST /widgets` requests differing only in their `Accept` header, immediately before each of which the sequence `Store.All()` returns is equal element for element, in the same order and with each element's `Name`, `Count` and `Status` equal, MUST receive the same status, the same value for every header the handler sets, and the same body.
- R-L5QP-UCKG: When the handler answers a `POST /widgets` request with status 303, its telemetry writer (`D04-panel`) MUST record, between that request's `request.started` and `request.finished` events, exactly one event named `widget.created`, carrying that request's envelope request id and user (`D04-panel` R-KSBT-MVET), whose attributes are exactly `widget`, the `ID` of the element R-KGFY-PCTS states that request appended to the sequence `Store.All()` returns.
- R-L6YM-84B5: When the handler answers a `POST /widgets` request with any status other than 303, its telemetry writer MUST record no event named `widget.created` for that request.
