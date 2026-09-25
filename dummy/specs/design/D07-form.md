# D07-form

Creating a widget is the one interaction in dummy that changes state, and this
document owns all of it: the markup of the form that offers it, the card the
form sits in, and the whole of `POST /widgets`.

The form is an ordinary HTML form. It POSTs to `/widgets` with
`application/x-www-form-urlencoded`, it has one control per widget field —
`name`, `count`, `status` — and nothing about a submission is assembled by
JavaScript, so a caller with `curl` submits exactly what a browser submits.
That promise is what makes the two halves of `S5-form` one story: the keys,
the method and the encoding are fixed in the markup, so there is one
submission shape and dummy validates it the same way whoever sent it. The
status control offers exactly the three choices, which is why a person in a
browser cannot reach the status rejection at all and a `curl` caller can.

Markup is contract here only where a story fixes it or a promise needs it to
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
not it is named. The named case is already out, since R-K8NH-VYBR permits no
`<input` carrying a `name` beyond the two field controls; the unnamed case is
the one that gets through, and it is precisely the case a `name` ban cannot
reach. A flat ban on an `<input` whose `type` reads `image` is one scan and
leaves nothing to reason about, which is why it is the form the requirement
takes. Nothing is lost by it: dummy's one submit affordance is a labelled
button, a demo app has no graphical submit to offer, and a picture on the
button is a styling choice that would cost the key parity every other clause
here is spent on.

**The form sits in a card headed `Add widget`.** `S3-panel` and `S5-form` fix
that heading, and `S3-panel` places it as "a heading beneath the page's
`Widgets` heading". The page's heading is an `h1` (`D04-panel`), so the card's
heading is one level below it, an `h2`. The card's markup follows the
platform's mock of this very panel in `design/` (`design/ikigenba/app.html`):
a `section` with the class `card`, whose first child is a `header` holding the
`h2`, and then the form. The `header` is part of the contract, and not just
the class, because the mock heads its card that way. The requirement is
anchored on the page's one `<form` start tag rather than on the count of cards
or sections in the page. The form card is then decidable without an HTML
parser, because it is the chain of tags that runs immediately up to that start
tag, plus the one end tag that immediately follows the form. It also leaves
the rest of the page to `D04-panel`. The chain allows only whitespace between
its links, so nothing can sit between the heading and the form, and nothing
can sit between the form and the end of the card.

Where the card sits is `D04-panel`'s. The page's heading comes first, then the
page's `.panel` wrapper holding the table and then the form card. That is a
statement about how the page composes parts that cannot see each other, and
this document says nothing about the table. The stories' side-by-side and
stacked arrangements are not something a check without a browser can observe,
so the contract for them is that markup.

**The button reads `Add widget` and nothing else.** `S3-panel` fixes "the
form's button is the text `Add widget` alone, with no icon". The platform's
mock puts an icon in its button, and the story overrides it. So the widget
form contains exactly one `button` of any type, that button is the form's only
submit control, and its content is a run of text with no element in it. That
rules out an `svg` and an `img` with a single scan, and the text, once
normalised, is exactly `Add widget`. Counting every `button`, not only the
submit controls, is what makes "the form's button" name one thing: a
`type="button"` or `type="reset"` button carrying an icon would otherwise sit
beside it unchecked, and for the same reason the form holds no `<input` whose
`type` is `button` or `reset`, named or not. This narrows R-UC44-S8EO's "at
least one submit control" to exactly one, and R-UC44-S8EO's definition of a
submit control and its ban on `name`, `formaction`, `formmethod` and
`formenctype` still apply to it.

**The route has four answers and no fifth.** A request without identity is a
500, decided before anything else; that shape is `D04-panel`'s cross-route
rule and is not restated here, but the *ordering* is stated here, because
`S5-form` extends it from path and method to the submitted fields: a body that
would have been accepted and a body that would have been rejected are answered
identically, and neither is read. A submission whose media type is not
`application/x-www-form-urlencoded` is a 415: the objection is to the format,
not to the values, and no retry that keeps the media type can succeed, so a
422 would be an invitation to send better values that could never be taken up.
The body is not read at all, no form and no field errors come back, and there
is no JSON way in through this route. A submission dummy reads and accepts is
a 303 to `/widgets` with an empty body. A submission dummy reads and rejects
is a 422 whose body is a panel page.

**Why the success is a redirect.** The browser must not be left showing the
result of a POST: after a 303 it fetches `/widgets` with a GET, so the address
the user ends on is the panel and refreshing re-reads the panel instead of
submitting a second time. There is no flash message and no confirmation
banner. dummy sets no cookie and puts nothing in the URL, so nothing carries a
message across the redirect — the new row in the table is the confirmation,
and the redirect's target is exactly `/widgets`, bare. No requirement below
says "there is no flash message": "a message" is not something a check can
recognise in a page. Two kinds of requirement close it off instead. The first
fixes the carriers, every one a message could ride: the 303's `Location` is
exactly `/widgets` with no query string and no fragment and its body is empty
(R-NQ6Y-D2D3), and no answer this route sends carries a `Set-Cookie` header
(R-NSMR-4LUH). Nothing else survives a redirect. The second fixes the page:
`D04-panel` R-XZF6-1IBR leaves no text on a panel page outside the chrome
header, the heading and the panel wrapper, and the form card's opening is
fixed from its `<section` start tag to the `<form` start tag and its closing
from `</form>` to `</section>` (R-9HU8-5PDQ), so a banner has nowhere to sit
around the card or the table. The contents of the widget form between its
controls are not fixed, so text placed there breaks no requirement; the
carriers are what keep a message from existing to be placed. The absence of
the banner is therefore a consequence of decidable requirements rather than a
requirement of its own, which is the most this contract can honestly claim.

**The 422 body is a panel page**, and "panel page" is a document shape
`D04-panel` owns rather than a route (R-KTB6-1T0H). This document refers to
that shape and re-describes none of the chrome, the head or the page's
heading, all of which are `D04-panel`'s. Because the form card is required of
every panel page, the redrawn form sits in its card headed `Add widget` just
as it does on a `GET /widgets`. What this document does state is what a 422
adds: the form carries the values the caller
submitted, an error message sits beside each rejected field and beside no
other, and nothing was created — so the table the shape requires holds exactly
the widgets that were there before, in the order they were in.

**The echo is raw wherever the control can hold bytes.** All three values are
trimmed before they are validated, but the 422 re-displays the two free-text
fields exactly as the caller submitted them: `S5-form` fixes a 41-character
name coming back "in full, unshortened", and the count field holding the text
`three` "as it was typed". So the requirement below ties those two echoed
attribute values to the `Submission` (`D05-widgets` R-7WR2-CK99), which holds
the raw strings, and never to anything the validation produced. That is also
the reason `D04-panel`'s read value of an attribute unescapes character
references (R-KEOD-GK45): the raw echo of an arbitrary submitted string is
precisely where `&` and `<` show up, and a comparison that did not unescape
would fail on input the caller chose. A read value is unescaped and otherwise
unaltered — not normalised — because whitespace inside a raw echo is part of
what was submitted.

Echoing arbitrary caller bytes into markup is exactly where a design can hand
a caller a tag, and the requirement that closes that is `D04-panel`'s
R-RP4V-R2B5: the tag-name sequence of a document dummy sends cannot depend on
the echoed `Name`, `Count` or `Status`, and neither does how many `>`
characters it holds, so whatever the caller submits comes back as a value and
never as structure. That rule is D04's and is not restated
here; because it holds, the echo below can stay raw, and it does.

**R-KEOD-GK45 binds what dummy sends, for every value a caller can submit.**
This is the one place in the design where caller bytes land inside an attribute
value, so it is the one place the distinction matters, and a build run that
misses it ships a defect while every template reads correctly. `D04-panel`'s
R-KEOD-GK45 requires that each attribute a requirement names be written with
its value enclosed in double quotes and that a start tag carry at most one
occurrence of each such attribute. Those two clauses are invariants over the
documents dummy actually sends — over the echo of *any* submitted `Name` or
`Count`, chosen by the caller — and not authoring guidance for the template's
own literal markup. The gap is real and specific: Go's `html/template` escapes
`<`, `>`, `&` and the double quote inside a double-quoted attribute value, but
it escapes neither `=` nor whitespace, so the plain rendering of
`value="{{.Name}}"` answers a submitted name of `x" value="zzz` with a `name`
control whose start tag carries two `value` occurrences. The document dummy
sent then violates R-KEOD-GK45 even though nothing in the template looks wrong.
Escaping `=` as well is the simple way to hold the invariant for every input;
the contract fixes the result and leaves the technique open. What this is not
is a way for a caller to choose what some other requirement reads — the extra
occurrence is dummy's own markup rather than an attacker-chosen string, and
`D04-panel` R-RP4V-R2B5 holds the tag structure still regardless — so no
requirement here that reads a `name`, `id` or `aria-describedby` occurrence is
reachable through it.

The status control is the one field whose echo cannot be literal, and the
difference is the control rather than an inconsistency. A select cannot hold
arbitrary bytes at all: it selects one of its three options, or none. There is
therefore nothing of the caller's to give back byte for byte, and the faithful
rendering is instead the option the submission would actually have used — the
one equal to the *trimmed* status, the value `Create` judges (`D05-widgets`).
So on a 422 the control offers the same three choices and selects the one
matching the trimmed status, and selects none when the trimmed status is not
one of the three words, which is exactly the `archived` case. Selecting on the
raw value instead would answer a submission of `status=%20active` — perfectly
acceptable everywhere else, since trimming happens before validation — with
nothing selected whenever some other field failed. The raw-echo principle does
not ask for that: it exists so a free-text control hands the caller back their
own bytes, and a closed enumeration never held any.

**Every wrong field is reported at once.** The per-field requirements are
written over the `FieldErrors` value `Store.Create` returned rather than over
inputs, so "one message beside each rejected field and none beside any other"
is one rule that holds for one bad field and for three. `D05-widgets` owns the
validation rules and the six message constants — `NameRequiredMessage`,
`NameTooLongMessage`, `NameTakenMessage`, `CountNotWholeMessage`,
`CountNegativeMessage` and `StatusNotAllowedMessage` — and this document names
them only through the `FieldErrors` field that carries them. No rule of theirs
is restated here; a second statement of a rule is a second contract that will
drift. The same applies to the definitions `D04-panel` states once for the
whole design: start tags and end tags (R-KDGH-2SDG), an attribute occurrence
and its read value (R-KEOD-GK45), the script-stripped form (R-KH46-83LJ),
normalisation (R-KIC2-LVC8), an HTML document dummy sends (R-IWKC-JVY4), a
panel page (R-KTB6-1T0H) and the chrome failure shape (R-KS39-O19S). This
document cites each where it uses it and restates no step of any of them.

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
of `S5-form` are covered here as HTTP behavior over whatever `FieldErrors`
reports.

Two requirements below compare two requests that differ in one thing and demand
one answer: the 500 that never looks at the body, and the `Accept` header that
changes nothing. A comparison like that means something only if everything else
really is equal, and the thing most easily unequal is the store — a creation
landing between the two requests changes the table inside a panel page and
falsifies the claim without any handler misbehaving. Both therefore pin the
store's contents equal immediately before each of the two requests, which is
the form `D04-panel` settled for the whole design in R-RP4V-R2B5.

Every check below is runnable with the Go standard library alone: there is no
HTML parser, so attributes are read by `D04-panel`'s single rule for an
attribute occurrence and its read value (R-KEOD-GK45), spans are picked out by
the start tags and end tags it defines (R-KDGH-2SDG), and every "exactly one"
count is taken over the script-stripped form so that the panel's inline script
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

## REQUIREMENTS

- R-K67P-4EUD: Every response body that is a panel page, as `D04-panel` defines a panel page (R-KTB6-1T0H), MUST contain, in its script-stripped form as `D04-panel` defines it (R-KH46-83LJ), exactly one `<form` start tag and exactly one `</form>` end tag, as `D04-panel` defines start tags and end tags (R-KDGH-2SDG), the start tag first; the widget form is the text from that start tag through that end tag.
- R-K7FL-I6L2: The widget form's `<form` start tag MUST carry an occurrence of the attribute `method`, as `D04-panel` defines an attribute occurrence and its read value (R-KEOD-GK45), whose read value is `post` compared case-insensitively, and an occurrence of the attribute `action` whose read value is exactly `/widgets`, and MUST carry either no occurrence of the attribute `enctype` or one whose read value is `application/x-www-form-urlencoded` compared case-insensitively.
- R-K8NH-VYBR: The widget form MUST contain exactly one `<input` start tag, as `D04-panel` defines start tags (R-KDGH-2SDG), carrying an occurrence of the attribute `name`, as `D04-panel` defines an attribute occurrence and its read value (R-KEOD-GK45), whose read value is `name`, exactly one `<input` start tag carrying such an occurrence whose read value is `count`, and exactly one `<select` start tag carrying such an occurrence whose read value is `status`, and MUST contain no other `<input`, `<select` or `<textarea` start tag carrying an occurrence of the attribute `name`; the control of a field is the start tag carrying an occurrence of the attribute `name` whose read value is that field's key.
- R-K9VE-9Q2G: The status control's element — the text from its `<select` start tag through the next `</select>` end tag (`D04-panel` R-KDGH-2SDG) — MUST contain exactly three `<option` start tags, the read values of whose occurrences of the attribute `value` (`D04-panel` R-KEOD-GK45) are, in document order, the three values `Statuses()` returns (`D05-widgets`).
- R-UC44-S8EO: The widget form MUST contain at least one submit control, a submit control being a `<button` start tag (`D04-panel` R-KDGH-2SDG) that carries either no occurrence of the attribute `type` or an occurrence whose read value is neither `reset` nor `button`, each compared case-insensitively (`D04-panel` R-KEOD-GK45), or an `<input` start tag carrying an occurrence of the attribute `type` whose read value is `submit` compared case-insensitively; and every submit control the widget form contains MUST carry no occurrence of the attribute `name`, so that a browser's submission carries the same keys a `curl` caller sends, and no occurrence of the attribute `formaction`, `formmethod` or `formenctype`, so that the submission a browser makes through it uses the action, method and encoding R-K7FL-I6L2 fixes on the `<form` start tag.
- R-M49S-JQ63: The widget form MUST contain no `<input` start tag (`D04-panel` R-KDGH-2SDG) carrying an occurrence of the attribute `type` whose read value is `image` compared case-insensitively (`D04-panel` R-KEOD-GK45).
- R-9HU8-5PDQ: In the script-stripped form of every panel page, as `D04-panel` defines a panel page (R-KTB6-1T0H) and the script-stripped form (R-KH46-83LJ), the widget form's `<form` start tag MUST be immediately preceded, with nothing but ASCII whitespace between successive items, by these items in this order: a `<section` start tag carrying an occurrence of the attribute `class` whose read value is exactly `card` (`D04-panel` R-KDGH-2SDG, R-KEOD-GK45), a `<header` start tag, an `<h2` start tag, a run of characters containing no `<`, an `</h2>` end tag and an `</header>` end tag; and the widget form's `</form>` end tag MUST be immediately followed, with nothing but ASCII whitespace between, by a `</section>` end tag; the **form card** is the text from that `<section` start tag through that `</section>` end tag, and the form card's **heading text** is the normalisation, as `D04-panel` defines it (R-KIC2-LVC8), of that run of characters.
- R-9J24-JH4F: The form card's heading text MUST be exactly `Add widget`.
- R-JH6I-XDW1: The widget form MUST contain exactly one `<button` start tag (`D04-panel` R-KDGH-2SDG), whatever its `type`, and no other submit control, as R-UC44-S8EO defines a submit control, and MUST contain no `<input` start tag carrying an occurrence of the attribute `type` whose read value is `button` or `reset` compared ASCII case-insensitively (`D04-panel` R-KEOD-GK45); that `<button` start tag MUST itself be a submit control and MUST be immediately followed by a run of characters containing no `<` and then a `</button>` end tag, the normalisation of that run (`D04-panel` R-KIC2-LVC8) being exactly `Add widget`, so that the button contains no element, neither an `svg` nor an `img`, and no text but `Add widget`.
- R-XBL8-ZHD9: In the body of a 422 answer to a `POST /widgets` request, the read value of the occurrence of the attribute `value` on the widget form's `name` control, as `D04-panel` defines an attribute occurrence and its read value (R-KEOD-GK45), MUST be exactly the `Name` field of the `Submission` (`D05-widgets` R-7WR2-CK99) that request produced, and the read value of the occurrence of the attribute `value` on its `count` control MUST be exactly that `Submission`'s `Count` field, each unaltered in any other way and an absent occurrence counting as the empty string; the tag structure of that body stays independent of both values by `D04-panel` R-RP4V-R2B5, which this document does not restate.
- R-4NOW-M9YH: In the body of a 422 answer to a `POST /widgets` request, when the trimmed status (`D05-widgets`) of the `Submission` that request produced is exactly one of the three `option` values of the status control, that `<option` start tag MUST carry a `selected` attribute and MUST be the only one in the widget form that does, and when that trimmed status is none of the three, the widget form MUST contain no `<option` start tag carrying a `selected` attribute.
- R-OPQ1-FGDT: In a response body that is a panel page and is not the body of a 422 answer to a `POST /widgets` request, the `value` attributes of the widget form's `name` and `count` controls MUST each be absent or empty, and the widget form MUST contain no `<option` start tag carrying a `selected` attribute.
- R-JFYM-JM5C: The field error text of a field in an HTML document MUST be read as the normalisation, as `D04-panel` defines it (R-KIC2-LVC8), of the text, in that document's script-stripped form as `D04-panel` defines it (R-KH46-83LJ), from the `>` ending the start tag that carries an occurrence of the attribute `id` whose read value is `<field>-error` (`D04-panel` R-KEOD-GK45) up to the next following `<`, where `<field>` is that field's key; normalising that text script-strips it a second time, which removes nothing from it, because that text is a substring of that script-stripped form and such a form contains no `script` start tag and no `style` start tag (`D04-panel` R-Y4AR-KLAJ).
- R-W2WT-9FY8: An HTML document dummy sends, as `D04-panel` defines one (R-IWKC-JVY4), MUST contain, for each of `name-error`, `count-error` and `status-error`, at most one start tag carrying an occurrence of the attribute `id` whose read value is that string (`D04-panel` R-KDGH-2SDG, R-KEOD-GK45), and the element carrying such an occurrence MUST contain no child element: the first `<` at or after the `>` ending that start tag MUST be immediately followed by `/`.
- R-KEQZ-ST18: For each field whose message in the `FieldErrors` value that `Store.Create` (`D05-widgets`) returned for a submission is non-empty, the body of the 422 answer to that submission MUST contain a start tag carrying an occurrence of the attribute `id` whose read value is `<field>-error` (`D04-panel` R-KEOD-GK45), that field's field error text in that body MUST be exactly that message, and that field's control MUST carry an occurrence of the attribute `aria-describedby` whose read value is exactly `<field>-error`.
- R-KFYW-6KRX: For each field whose message in the `FieldErrors` value that `Store.Create` returned for a submission is empty, the body of the 422 answer to that submission MUST contain no start tag carrying an occurrence of the attribute `id` whose read value is `<field>-error` (`D04-panel` R-KEOD-GK45), and that field's control MUST carry no occurrence of the attribute `aria-describedby`.
- R-W44P-N7OX: An HTML document dummy sends, as `D04-panel` defines one (R-IWKC-JVY4), that is not the body of a 422 answer to a `POST /widgets` request MUST contain no start tag carrying an occurrence of the attribute `id` whose read value is `name-error`, `count-error` or `status-error`, and MUST contain no `<input` start tag and no `<select` start tag carrying an occurrence of the attribute `aria-describedby` (`D04-panel` R-KEOD-GK45).
- R-NLBC-TZEB: A `POST /widgets` request MUST be a form-encoded submission when the value of its `Content-Type` header, truncated at the first `;` and with leading and trailing whitespace removed, equals `application/x-www-form-urlencoded` compared case-insensitively, and MUST NOT be one otherwise, a request carrying no `Content-Type` header included.
- R-6OLO-3T75: For a `POST /widgets` request that carries identity, meaning one the condition `D04-panel` R-LXQ7-A81H answers 500 does not hold of, and is a form-encoded submission, the handler MUST call `Store.Create` (`D05-widgets`) exactly once, with a `Submission` whose `Name`, `Count` and `Status` are the values the request body carries for the keys `name`, `count` and `status` respectively, taking the first value when the body carries a key more than once and the empty string when the body carries no value for that key, and MUST alter none of those three values.
- R-GUU9-JB3W: The handler MUST answer a `POST /widgets` request that carries identity and is a form-encoded submission with status 303 when `Any()` on the `FieldErrors` value `Store.Create` returned for that request is false, and with status 422 when it is true.
- R-NQ6Y-D2D3: The 303 answer to a `POST /widgets` request MUST carry a `Location` header whose value is exactly `/widgets`, with no query string and no fragment, and an empty body.
- R-NREU-QU3S: After a `POST /widgets` request the handler answered 303, the sequence `Store.All()` (`D05-widgets`) returns MUST be the sequence it returned immediately before that request with exactly one element appended at the end, equal in `Name`, `Count` and `Status` to the `Widget` `Store.Create` returned for that request.
- R-NSMR-4LUH: No answer the handler sends to a `POST /widgets` request MUST carry a `Set-Cookie` header.
- R-KIEO-Y49B: The body of the 422 answer to a `POST /widgets` request MUST be a panel page, as `D04-panel` defines a panel page (R-KTB6-1T0H).
- R-NV2J-W5BV: A `POST /widgets` request the handler answered 415 or 422 MUST leave the sequence `Store.All()` returns equal to the sequence it returned immediately before that request, element for element, in order, and with each element's `Name`, `Count` and `Status` equal, so that no widget is added, removed, reordered or altered.
- R-KJML-BW00: A `POST /widgets` request that carries identity and is not a form-encoded submission MUST be answered with status 415 and a response in the chrome failure shape for `UnsupportedMediaTypeMessage`, as `D04-panel` defines that shape (R-KS39-O19S).
- R-KKUH-PNQP: In answering a `POST /widgets` request that carries identity and is not a form-encoded submission, the handler MUST NOT read any byte of the request body, and that answer's body MUST contain no `<form` start tag (`D04-panel` R-KDGH-2SDG) and no start tag carrying an occurrence of the attribute `id` whose read value is `name-error`, `count-error` or `status-error` (`D04-panel` R-KEOD-GK45).
- R-W5CM-0ZFM: A `POST /widgets` request that does not carry identity MUST be answered without reading any byte of the request body and without changing the sequence `Store.All()` returns, and two such requests differing only in their bodies, immediately before each of which the sequence `Store.All()` returns is equal element for element, in the same order and with each element's `Name`, `Count` and `Status` equal, MUST receive the same status, the same value for every header the handler sets, and the same body.
- R-NZY5-F8AN: The handler MUST answer every `POST /widgets` request with status 500, 415, 303 or 422, and with no other status.
- R-W6KI-ER6B: No answer the handler sends to a `POST /widgets` request MUST carry a `Content-Type` header whose media type is `application/json`, and two `POST /widgets` requests differing only in their `Accept` header, immediately before each of which the sequence `Store.All()` returns is equal element for element, in the same order and with each element's `Name`, `Count` and `Status` equal, MUST receive the same status, the same value for every header the handler sets, and the same body.
