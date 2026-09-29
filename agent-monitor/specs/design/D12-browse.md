# D12-browse

A bare `agent-monitor` run in a terminal opens a browser of the agents on
this machine: the harness menu, one harness's session menu, one session's
agent menu, and one agent's chat, each a screen drawn in the terminal's
alternate screen and kept up to date as following keeps `list -f`, `tree -f`,
and `chat -f` (stories `S5-browse`, and the bare run of `S1-bootstrap`).

The browser is the package `internal/browse`, one concern: the screens, what
each key does, how a screen is drawn at the terminal's size, and when
browsing ends. It is reached only from `cli.Run`, which decides that a run
browses, reports a missing `HOME` before the terminal is touched, refuses a
terminal smaller than the minimum below without touching it further, puts
the terminal in raw mode around the browser, writes the enter sequence
before calling `Run` and the leave sequence after it returns (after a failed
write, as one last best-effort write), and
turns the browser's result into an exit code and, for a failed write, a
diagnostic (`D01-layout-and-run-seam`).
The browser touches nothing of the process: everything it sees arrives
through its one entry point, `Run`.

`Run` is handed a context whose `Done` channel is `cli.Run`'s `Interrupt`
itself, closed when the process is interrupted and nil when the run can
never be interrupted
(`D01-layout-and-run-seam` states what `cli.Run` guarantees once it is
closed); a `Config` with the home directory, the filesystem rooted
at `/`, whether trees are drawn in colour, a `Watcher` that says something it
reads may have changed, and the `Terminal` whose keys and size it follows;
and the writer the screens go to. Its output is screens only: the enter and
leave sequences that switch to the alternate screen and back are `cli.Run`'s
to write, before and after the browser's output. It returns when browsing
ends, with the error of a failed write or nil. `Watcher` and `Terminal` are declared here
with exactly the method sets of `cli.Watcher` and of `cli.Console` less its
`Raw`, so `cli.Run` hands its own values straight through, while this
package never imports `cli`. Raw mode is not the browser's either:
`cli.Run` owns entering and leaving it.

The smallest terminal the browser works in is 40 columns by 5 rows — a
breadcrumb, a blank line, a table header, one row, and the hint line — and
the package declares it once, as the constants `MinCols` and `MinRows`.
They are the minimum used both at the start, where `cli.Run` checks the
size before it opens the browser and reports a smaller terminal as a usage
error naming these values (`D01-layout-and-run-seam`), and while the browser
is open, when the terminal is resized below them. They are untyped, so each
is an `int` where `cli` formats it and compares a size with it.

The sections below say how a screen is drawn (Rendering), what each key
does (Keys and navigation), what each level shows, how it is kept up to
date, and how `Run` starts and ends (Screens, refresh, failures and
lifecycle), and how the chat behaves (Chat).

## Rendering

How a screen reaches the terminal, in bytes a test can take apart. The
browser has four *levels* — the harness menu, a session menu, an agent
menu, and a chat — and whenever it draws the open level it writes one
*screen*: the whole terminal, drawn in a single write. Nothing else ever
goes to its writer, so a test that records each write separately holds
the screens one by one; through `cli.Run` they sit between the enter write
and the leave write.

A screen is drawn at the size the terminal reports just before it is
written. At or above the minimum it is a *normal screen*: for every row of
the terminal, top to bottom, the cursor is moved to the row's first column
(CUP), the row is erased (EL 2), and the row's text follows. Row 1 is the
breadcrumb, row 2 is empty, the last row is the key-hint line, and the rows
between hold the level's body, empty below it. The highlighted row is
wrapped in reverse video (SGR 7) and a full reset (SGR 0); the tree's
coloured dots end in a reset of their own, so reverse video is switched on
again after every attribute change inside the highlighted row. Each row is
cut at the right edge, counting one column per character, escape bytes
not counted and never cut apart, and trailing spaces dropped, which the
eye cannot see anyway since the row was just erased. A chat line longer
than the width is wrapped into pieces of exactly the width, character by
character. Below the minimum the screen is only `terminal too small` on a
cleared display, and nothing about where the developer is changes.

A test decodes a normal screen by splitting it at each `ESC [ r ;1H ESC [2K`:
the piece after the `r`-th is row `r`; the row is highlighted exactly when
it begins `ESC [7m`; its text is the piece with every `ESC [ ... m` removed.
Screens are written once at the start and then once for every value taken
from the keys, the resize channel, or the watcher, even when nothing
changed, so a test counts screens from what it sent, without sleeping.
A channel found closed yields no screen of its own; what the browser does
when the keys, the resize channel, or the watcher's channel closes is
stated with the lifecycle, under "Screens, refresh, failures and
lifecycle".

Moving the cursor to every row and erasing it, rather than clearing the
whole display first, keeps the redraw from flickering, and it also bounds
the damage of a character the terminal draws two columns wide: counting it
as one, the row may spill onto the next, which is erased and drawn right
after. Writing the last column of the last row does not scroll: an
autowrapping terminal wraps only when a further character arrives at the
right border, and none does before the next screen's cursor move.

The sequences are those of the published references. XTerm Control
Sequences (invisible-island.net, updated for XTerm Patch #411): under "C1
(8-Bit) Control Characters", `ESC [` is the Control Sequence Introducer
(CSI); under "Functions using CSI, ordered by the final character(s)",
`CSI Ps ; Ps H` is Cursor Position [row;column] (default [1,1]) (CUP),
`CSI Ps J` with `Ps = 2` is Erase in Display, Erase All (ED), `CSI Ps K`
with `Ps = 2` is Erase in Line, Erase All (EL), and `CSI Pm m` is Character
Attributes (SGR), with `Ps = 0` Normal (default) and `Ps = 7` Inverse.
ECMA-48, 5th edition (June 1991): 5.4 (a control sequence is CSI,
parameter bytes 03/00–03/15, and a final byte), 8.3.21 CUP (defaults 1;1),
8.3.39 ED (2: all character positions of the page erased), 8.3.41 EL (2:
all character positions of the line erased), and 8.3.117 SGR (0: default
rendition, cancelling every preceding SGR; 7: negative image). The VT510
Programmer Information, DECAWM (vt100.net): with autowrap set, graphic
characters received when the cursor is at the right border appear at the
beginning of the next line.

## Keys and navigation

What each key does on each level: decoding a value from the keys channel,
quitting, moving and paging the highlight, opening and going back, and
keeping the highlight on its item as a menu scrolls.

A value from the keys channel is one read of the terminal, so it may hold
several keys: a key held down, keys typed quickly, a paste. The browser
splits it into keys from its bytes alone, with no timer, as the run seam
requires (`D01-layout-and-run-seam`): the value that is the one byte ESC is
the Esc key, and nothing else is. An ESC followed by `[` starts a control
sequence, which runs to its final byte; an ESC followed by `O` starts a
three-byte sequence; an ESC followed by anything else is the Alt prefix of
the key that follows, and an Alt key does nothing. So a value that starts
with ESC is never read as Esc and then other keys, and an ESC left
dangling at the end of a longer value — most likely the first byte of a
sequence whose rest a later read brings — does nothing rather than quit,
which keeps a held arrow key from ever quitting the browser. Every other
byte is taken one character at a time. All keys of a value act in order,
and the value gets one screen (Rendering), unless a key quits, and then
nothing after it acts and no screen is written.

The keys are the terminal's own. The arrows send `CSI A` to `CSI D`, or,
in application cursor mode, `SS3 A` to `SS3 D`; the browser accepts both,
since it never sets the mode and a terminal may have been left in it.
Page Up and Page Down send `CSI 5 ~` and `CSI 6 ~` in either mode. Return
sends CR; a terminal set to send CR LF for it gives CR and then LF, and LF
does nothing, so one press opens once. Keypad Enter in application keypad
mode sends `SS3 M` and opens too. In raw mode ctrl+c is not turned into a
signal and reaches the browser as the byte 0x03, and ctrl+b and ctrl+f as
0x02 and 0x06. `h` and `l` are ← and →, as `k` and `j` are ↑ and ↓, on
every level: on the chat `h` goes back and `l`, like →, does nothing. Any
other key does nothing, though the screen is still written for its value.

A menu's highlight moves over its *menu rows*, the rows that can be
highlighted: the header of a session menu and the empty line and key of an
agent menu never are. ↑ and ↓ move one row, Page Up, Page Down, ctrl+b and
ctrl+f one page — as many rows as the menu has room to show — and `g` and
`G` go to the first and the last row, all stopping at the ends. These menu
keys are not listed in the hint line, like the vim keys. On the chat the
same keys scroll, as "Chat" states.

The highlight belongs to an item, not a position: a row's *key* is its
item's id, with a count of the rows above it with the same id so that two
rows sharing an id stay apart. When a refresh moves the item, the highlight
moves with it; when the item is gone, the highlight goes to the first
surviving row that was below it, else to the new last row. Going back is
different: the developer is returning to where they were, so when the item
they came from has gone, the highlight goes to the nearest surviving row
that was above it, else to the first row.

A menu longer than its room scrolls. Its top row moves only as far as it
must to keep the highlight in view, and, when rows have gone or the
terminal has grown, no further down than leaves the room full; a menu
opened afresh starts at its top, and one returned to keeps where it was.
While the terminal is too small nothing but quitting acts, so the browser
comes back to exactly the place the developer left.

The byte sequences are those of the published references. XTerm Control
Sequences (XTerm Patch #411), "PC-Style Function Keys": the cursor keys
send `CSI A`, `CSI B`, `CSI C`, `CSI D` (Up, Down, Right, Left) in normal
and `SS3 A` to `SS3 D` in application mode (DECCKM); "VT220-Style Function
Keys": the 6-key editing keypad, "not affected by DECCKM or
DECKPNM/DECKPAM", sends `CSI 5 ~` for PageUp and `CSI 6 ~` for PageDown;
the keypad table gives Enter as `CR` in numeric and `SS3 M` in application
mode; "C1 (8-Bit) Control Characters": `ESC [` is CSI and `ESC O` is SS3.
ECMA-48, 5th edition, 5.4: after CSI come parameter bytes 03/00–03/15,
intermediate bytes 02/00–02/15, and one final byte 04/00–07/14. The DEC
VT100 User Guide, chapter 3 (vt100.net): Table 3-5, CTRL with `B`, `C`,
`F` transmits 002 STX, 003 ETX, 006 ACK; RETURN transmits 015 (CR), and can
be set to send CR LF; Table 3-6 gives the arrows as `ESC [ A`… with cursor
key mode reset and `ESC O A`… with it set. termios(3): `cfmakeraw` clears
`ICRNL` and `ISIG`, so CR is not turned into NL and VINTR (003, Ctrl-C) is
not turned into SIGINT.

## Screens, refresh, failures and lifecycle

What each level shows and how it is kept up to date: the harness, session,
and agent menus, refreshing through the watcher, read failures, and how
`Run` starts and ends.

Each level shows what the matching command would print at that moment,
taken from the same harness functions. The harness menu calls all three
`List` functions and shows how many sessions each returned, or `-` when the
call failed; its three rows never change, so the highlight stays on its
harness. A session menu calls its harness's `List` once, puts the sessions
in start order with `session.OrderByStart`, and draws them with
`session.TableRows`: all of them, not only the rows in view, so the columns
keep their widths while the menu scrolls, exactly as `list` would size
them. The header, and `no live sessions` when there is no session, stand
above the rows and are never highlighted. An agent menu calls its harness's
`Tree` for the open session and splits `tree.Draw`'s text into lines: the
tree's lines, one per agent, are its rows; the empty line and the key stand
below them. A chat calls its harness's `Chat` for the open session and
agent, and from then on reads the transcript it got back a pass at a time,
as `chat -f` does. Each of these calls is a *read*, and a menu's rows
between its fixed lines are what the highlight moves over and what scrolls
("Keys and navigation"). A menu's room is what is left of the body once its
fixed lines are placed, but never less than one row, so the highlight is
always in view; on the smallest terminal the agent menu's empty line,
which says nothing, gives way first, so the key stays at the bottom.

A level is read when it is opened, when the developer goes back to it, and
at every value from the watcher, and at no other time: a resize redraws
what is already held, and a key that only moves the highlight or scrolls
reads nothing. This is exactly the timing of following (`D11-follow`),
through the same watcher: every harness function and transcript pass is
handed the browse root, a thin view of `Root` with only the plain read
methods, so every name a read uses is seen, and after the screen the
watcher is told the directories that read touched, whenever they changed.
Only the open level is watched; the levels behind it are read again when
the developer comes back, which is how the harness menu shows a harness
whose data became unreadable as `-` after ←.

A read can fail. The first read of a session menu, an agent menu, or a chat
that fails shows, as the whole body, the diagnostic `list`, `tree`, or
`chat` would print, without the `agent-monitor: ` prefix: nothing is
highlighted, and the level keeps being read at every watcher value until a
read succeeds. On the chat the line wraps like any chat line, and the chat
counts as following. Once a level has shown what it reads, a read that
fails changes nothing on it; the harness menu, which always has its rows,
instead shows `-` for the harness whose latest read failed. No read failure
ends browsing.

`Run` starts with the harness menu open, read once, highlight on Claude
Code, and writes its first screen (the too-small screen on a terminal
below the minimum), even when it was interrupted before it began; then it tells
the watcher what to watch and waits. Before every wait it looks at
`ctx.Done()` first, since a Go `select` picks at random among ready cases,
and once that channel is closed it takes nothing more and returns nil; a
value it already took still gets its whole screen. A nil `Done` never
closes. The keys channel closing ends browsing like a quitting key; the
resize or watcher channel closing only stops that source. A nil `Watcher`
is one that never reports a change, and a nil `Terminal` one that is 0 by
0 and never sends a key. The first failed write ends `Run` at once and is
returned as it is, unwrapped, so `cli.Run` reports the write error's own
text. Whenever `Run` returns, nothing it started goes on reading keys or
writing, so `cli.Run` can leave the alternate screen and restore the
terminal on every path. The browser writes nothing but screens and never
the enter or leave sequence, and it changes nothing it reads.

The channel and context facts relied on are the Go specification's
("Receive operator": receiving from a nil channel blocks forever, and a
receive on a closed channel proceeds at once with the zero value after any
values sent before the close; "Select statements": when several cases can
proceed, one is chosen by uniform pseudo-random selection) and the
`context` package's (`Context.Done` "may return nil if this context can
never be canceled").

## Chat

The chat level: its entries and totals, scrolling, pausing and following,
and its view across refreshes and resizes.

Opening a line of the agent menu calls the open harness's `Chat` for that
line's agent, as the `chat` command does, and keeps the transcript it
returns. What the chat shows is what `chat -f` would have written by then:
the entries of that call and of every later read, in the order they came,
each in `chat.Format`'s form, and the totals of the transcript as it stands.
When and through what the later reads are made, and what a read that fails
shows, belong to "Screens, refresh, failures and lifecycle"; this section
takes the entries they bring. As with `chat -f`, nothing taken in is ever
taken back: a replaced transcript's entries follow the ones already shown.

The printed entries are cut into lines at each newline — each entry ends in
its own empty line, so the empty line between the last entry and the totals
line belongs to the entries — and each line is wrapped at the terminal's
width. Scrolling counts these wrapped rows, the rows the developer sees, so
one line of scrolling is one row of the screen and a page is as many rows as
the chat shows above its totals. The totals line is not scrolled: it is
wrapped too, so no count is lost on a narrow terminal — unless its rows
would leave no row for the entries, as at the smallest sizes, where it is cut
to one row instead — and always sits directly below the entry rows in view, which leaves the empty rows of a short
chat between it and the hint line, as in the stories.

The chat is either following or paused. Following, it shows its end at
every screen, so whatever a refresh or a resize brings is in view. A scroll
key that leaves the end out of view pauses it; a scroll key that brings the
end into view, `G` above all, makes it follow again. A chat that fits whole
cannot scroll at all, so it never pauses. Paused, the chat remembers the
first row in view not as a row number but as a place in the text — which
line, and which character of it began the row — so that entries arriving
below change nothing in view, and a resize, which re-wraps every line, still
starts the view where the developer was reading. If instead the view would
reach the end from there (the terminal grew, say, or a replaced transcript's
totals took fewer rows), the chat is no longer scrolled up, so it follows
again, whatever brought that screen about — a key, a resize, or a refresh:
`[paused]` always means entries are arriving out of view. A scroll key
measures its step at the size of the screen already on the terminal, as the
menu keys do.

The requirements below come in five groups, in this order: the package's
exported names; rendering; keys and navigation; screens, refresh, failures
and lifecycle; and the chat.

## REQUIREMENTS

- R-L6YX-30R6: The `internal/browse` package MUST export `func Run(ctx context.Context, cfg Config, stdout io.Writer) error`, where `context` is the standard library's `context` and `io` its `io`.
- R-L86T-GSHV: The `internal/browse` package MUST export the struct type `type Config struct { Home string; Root fs.FS; Color bool; Watcher Watcher; Terminal Terminal }`, with exactly those five fields in that order, where `fs` is the standard library's `io/fs` and `Watcher` and `Terminal` are the interface types of those names of `internal/browse`.
- R-L9EP-UK8K: The `internal/browse` package MUST export the interface type `type Watcher interface { Watch(names []string); Changes() <-chan struct{} }`, with exactly those two methods.
- R-LAMM-8BZ9: The `internal/browse` package MUST export the interface type `type Terminal interface { Keys() <-chan []byte; Size() (cols, rows int); Resized() <-chan struct{} }`, with exactly those three methods.
- R-0EO5-CU68: The `internal/browse` package MUST export `MinCols`, an untyped integer constant whose value is 40 (`const MinCols = 40`, alone or in a `const` block).
- R-0FW1-QLWX: The `internal/browse` package MUST export `MinRows`, an untyped integer constant whose value is 5 (`const MinRows = 5`, alone or in a `const` block).
- R-2MK2-6KCL: `Run` MUST write to `stdout` nothing but screens: each call it makes to `stdout.Write` MUST pass exactly the bytes of one whole screen, a normal screen or the too-small screen, and `Run` MUST write no other byte — in particular neither the enter sequence nor the leave sequence of `D01-layout-and-run-seam`, and nothing between two screens — so that the calls `Run` makes to `stdout.Write`, taken one by one, are its screens in the order it drew them.
- R-231O-28HH: Each screen `Run` writes MUST be drawn at the size, `W` columns by `H` rows, returned by a call to `cfg.Terminal.Size()` that `Run` makes after it has received the value the screen is written for (for the first screen, after `Run` is called) and before the call to `stdout.Write` that writes the screen, so that a terminal whose `Size` changes and which then sends a value on `Resized` is next drawn at its new size.
- R-249K-G086: When `W` is less than `MinCols` or `H` is less than `MinRows`, the screen MUST be the *too-small screen*, exactly the bytes `"\x1b[H\x1b[2Jterminal too small"` (CUP with its default parameters, to row 1, column 1; ED with parameter 2, erasing the whole display; then the text), whatever level is open and whatever its state, so that at a size of 39 by 40, 120 by 4, or 0 by 0 every screen is those bytes.
- R-WUJ1-10CK: When `W` is at least `MinCols` and `H` is at least `MinRows`, the screen MUST be a *normal screen* of `H` rows: for each row number `r` from 1 to `H`, in increasing order, the bytes `"\x1b[" + strconv.Itoa(r) + ";1H\x1b[2K"` (CUP to row `r`, column 1, then EL with parameter 2, erasing that whole line), followed by the row bytes of row `r`, and no other byte; so a normal screen of 5 rows begins with `"\x1b[1;1H\x1b[2K"`, its fifth row's bytes directly follow `"\x1b[5;1H\x1b[2K"`, and nothing follows them.
- R-WVQX-ES39: The *row bytes* of a row of a normal screen MUST be exactly its row text when the row is not highlighted, and, when it is the highlighted row, `"\x1b[7m"` (SGR 7, reverse video), then its row text with `"\x1b[7m"` inserted directly after each SGR sequence the row text contains, then `"\x1b[0m"` (SGR 0); so a highlighted row whose row text is `Claude Code  2` has the row bytes `"\x1b[7mClaude Code  2\x1b[0m"`, one whose row text is `"\x1b[36m●\x1b[0m [a] x"` has `"\x1b[7m\x1b[36m\x1b[7m●\x1b[0m\x1b[7m [a] x\x1b[0m"`, and the reverse video covers exactly the row text, never the rest of the row.
- R-WY6Q-6BKN: An *SGR sequence* is the byte 0x1b, then `[`, then zero or more bytes each a digit `0` to `9` or `;`, then `m`; the *visible runes* of a string are the runes left once its SGR sequences are removed, each counting as one column. The *row text* of a row that shows a line `L` at width `W` MUST be `L` with every visible rune after its `W`-th removed and then every U+0020 that no remaining visible rune follows removed, each SGR sequence of `L` kept in its place; so at a `W` of 40 a line of 50 visible runes keeps its first 40, `"ab  "` becomes `"ab"`, `"\x1b[36m●\x1b[0m [abc]"` at a `W` of 4 becomes `"\x1b[36m●\x1b[0m [a"`, `"x\x1b[36m●\x1b[0m"` at a `W` of 1 becomes `"x\x1b[36m\x1b[0m"`, the line `a`, U+0020, `b` at a `W` of 2 becomes `a`, and a line of at most `W` visible runes that does not end in U+0020 is kept whole.
- R-WZEM-K3BC: The *shown form* of a line MUST be the line itself when it is a line of a string `tree.Draw` returned, and otherwise the line obtained by replacing, from its first byte to its last: each byte that does not begin a valid UTF-8 encoding with `\x` and the byte's value as two lowercase hexadecimal digits; each TAB (0x09) with `8 − (p mod 8)` U+0020s, where `p` is the number of runes before that TAB once the replacements before it are made; and each other rune for which `unicode.IsControl` is true with `\x` and the rune's value as two lowercase hexadecimal digits; every other rune kept. Every line a normal screen shows MUST be shown in its shown form, which is what is wrapped and cut; so `a` TAB `b` is shown as `a`, seven U+0020, and `b`, the bytes ESC `[31m` as `\x1b[31m`, U+009B as `\x9b`, and the byte 0xFF as `\xff`.
- R-X0MI-XV21: In a normal screen of `H` rows, row 1 MUST show the breadcrumb, row 2 the empty line, and row `H` the hint line; rows 3 to `H`−1, the *body rows*, `H`−3 of them, MUST show from row 3 down, one line per row and in order, the lines of the open level's *body view* — the at most `H`−3 lines that the open level's requirements select for the body rows — and every body row below them the empty line; a normal screen MUST have no highlighted row other than at most one body row; so when the body view has fewer lines than there are body rows, the rows between it and the hint line are empty, and when it has `H`−3 lines the hint line follows its last line directly.
- R-2CSV-4EF1: The *breadcrumb* MUST be `agent-monitor` on the harness menu; on a harness's session menu, on the agent menu of one of its sessions, and on the chat of one of that session's agents, it MUST continue with ` › ` (U+0020, U+203A, U+0020) and the harness's name, `Claude Code`, `Codex`, or `Grok`; on that agent menu and that chat, then with ` › ` and `quote.Field` of the open session's `ID`; and on that chat, then with ` › ` and `quote.Field` of the `ID` of the tree node whose line was opened; so the chat of a session's root, whose `ID` is the session's, shows the session id twice, as in `agent-monitor › Grok › 01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47 › 01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47`.
- R-2E0R-I65Q: The *hint line* MUST be `↑↓ move  → open  q quit` on the harness menu; `↑↓ move  → open  ← back  q quit` on a session menu and an agent menu; and, on a chat, `↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]` while the chat follows and the same with `[paused]` in place of `[following]` while it is paused; where the arrows are U+2191, U+2193, U+2192, and U+2190, and every gap is U+0020s, two between items and three before the bracket, exactly as written here.
- R-X1UF-BMSQ: The *wrapped rows* of a line at width `W` MUST be, for the line's shown form `L`, which holds no SGR sequence: when `L` has no rune, the empty line alone; and otherwise the ⌈`n`/`W`⌉ lines, `n` being the number of runes of `L`, of which the `k`-th holds runes (`k`−1)·`W`+1 to min(`k`·`W`, `n`) of `L`; so each but the last holds exactly `W` runes, `L` is split after every `W`-th rune whatever it is, a U+0020 or a letter in the middle of a word alike, a line of exactly `W` runes is one wrapped row, and a line of `W`+1 runes is two, the second holding its last rune.
- R-2GGK-9PN4: `Run` MUST write its *first screen* before it first receives a value from `cfg.Terminal.Keys()`, `cfg.Terminal.Resized()`, or `cfg.Watcher.Changes()`, and MUST write it whole even when `ctx.Done()` is already closed, so that the first call `Run` makes to `stdout.Write` writes the first screen.
- R-H2CC-8AIE: After its first screen, while no call it has made to `stdout.Write` has returned a non-nil error, `Run` MUST write exactly one screen for each value it receives from `cfg.Terminal.Keys()`, `cfg.Terminal.Resized()`, or `cfg.Watcher.Changes()` while that channel is open — a receive whose second result `ok` would be true — except none for a value from `Keys` whose keys end browsing, and MUST finish writing that screen before it next receives from any of those channels; a receive that finds one of those channels closed MUST NOT by itself cause a screen; `Run` MUST write the screen for a value even when its bytes equal those of the screen before it, and MUST write no screen at any other time; so that, until browsing ends or a write fails, the number of screens `Run` has written is one more than the number of values it has received from those channels while they were open, less one for a value from `Keys` that ended browsing.
- R-4RZV-1ET6: Receiving a value from `cfg.Terminal.Resized()`, and writing the too-small screen in place of a normal screen, MUST NOT change which level is open, which harness, session, and agent are open, which item is highlighted, or whether the chat follows or is paused, save that a paused chat follows from a normal screen at which its view reaches its end, as `R-UKQ1-4N11` states; so a normal screen written after too-small screens shows the same level, with the highlight on the same item, as the normal screen before them would at the new size, save for what refreshes received meanwhile change.
- R-2K49-F0V7: Every call `Run` makes to `tree.Draw` MUST pass `cfg.Color` as its `color` argument, and no byte of a screen other than those of lines of a string `tree.Draw` returned MUST depend on `cfg.Color`; so the harness menu, a session menu, a chat, the breadcrumb, the hint line, and the reverse video of the highlighted row are the same bytes whether `cfg.Color` is true or false, and with `cfg.Color` false the only SGR sequences in any screen are the `"\x1b[7m"` and `"\x1b[0m"` of the highlighted row.
- R-AH14-TZZ7: When `internal/cli`'s `Run` opens the browser (`R-ROX5-PC7X`, `D01-layout-and-run-seam`), the `Config` it passes to `browse.Run` MUST have `Color` true if and only if `sys.NoColor` is the empty string and `sys.Term` is not byte-for-byte equal to `dumb`, so that `Color` is true for a `NoColor` of `""` with each of the `Term` values `""`, `xterm-256color`, `Dumb`, and `dumb ` (with a trailing space), and false for a `NoColor` of `1` or `0`, whatever `Term` is, and for a `Term` of `dumb`.
- R-D81S-JCKP: `Run` MUST take the keys a value received from `cfg.Terminal.Keys()` holds from the value's bytes alone, as follows: a value that is exactly the one byte 0x1b holds one key, Esc; any other value MUST be split, from its first byte to its last, into *tokens*, each starting at the first byte not yet in a token, and holds one key per token, in order: at a byte 0x1b followed by `[`, the token is those two bytes, then the longest run of bytes after them each from 0x20 to 0x3f, then the byte after that run when it is from 0x40 to 0x7e; at a byte 0x1b followed by `O`, the token is those two bytes and the byte after them, if there is one; at a byte 0x1b followed by any other byte, the token is that 0x1b and the whole token that starts at the byte after it; at a byte 0x1b that is the value's last byte, the token is that byte alone; and at any other byte, the token is the UTF-8 encoding of one rune that begins there, or that byte alone when no valid UTF-8 encoding begins there; so `"jj"` holds two tokens `j`, `"\x1b[A\x1b[A"` two tokens `"\x1b[A"`, `"\x1b[5~q"` the tokens `"\x1b[5~"` and `q`, `"\x1bj"`, `"\x1b\x1b"`, and `"\x1b\x1b[A"` one token each, `"j\x1b"` the tokens `j` and `"\x1b"`, `"\x1b[\x03"` the tokens `"\x1b["` and `"\x03"`, and `"\r\n"` the tokens `"\r"` and `"\n"`.
- R-D99O-X4BE: The key a token of `R-D81S-JCKP` is MUST be, by the token's bytes exactly: *quit* for `q` and 0x03 (ctrl+c), as the Esc of a value that is exactly 0x1b is; *up* for `"\x1b[A"`, `"\x1bOA"`, and `k`; *down* for `"\x1b[B"`, `"\x1bOB"`, and `j`; *open* for `"\x1b[C"`, `"\x1bOC"`, 0x0d (Enter), `"\x1bOM"`, and `l`; *back* for `"\x1b[D"`, `"\x1bOD"`, and `h`; *page up* for `"\x1b[5~"` and 0x02 (ctrl+b); *page down* for `"\x1b[6~"` and 0x06 (ctrl+f); *first* for `g`; *last* for `G`; and, for every other token, a key that MUST change nothing — not which level is open, which harness, session, or agent is open, which item is highlighted, a menu's top, or the chat's view or whether it follows — on any level; so `Q`, `J`, `H`, 0x0a, a lone `"\x1b"` ending a longer value, `"\x1bj"`, `"\x1b[1;5A"`, `"\x1b[5"`, and `"\x1bO"` change nothing, and only the value `"\x1b"` is Esc.
- R-DAHL-AW23: `Run` MUST apply the keys a value holds one after another, in order, each to the state the keys before it left, before it writes the screen for that value; the keys of a value *end browsing* exactly when one of them is quit, and then no key after the first quit MUST have any effect; so on the harness menu with `Claude Code` highlighted the value `"jj"` gives one screen, highlighting `Grok`, the value `"\x1b[B\x1b[B"` the same, and the value `"jq"` no screen.
- R-DBPH-ONSS: When the keys of a value it received from `cfg.Terminal.Keys()` end browsing (`R-DAHL-AW23`), `Run` MUST make no further call to `stdout.Write` and no further receive from `cfg.Terminal.Keys()`, `cfg.Terminal.Resized()`, or `cfg.Watcher.Changes()`, and MUST return nil, whichever level is open — the harness menu, a session menu, an agent menu, or a chat, with or without a failure line — and whether the last screen it wrote is a normal screen or the too-small screen; so `q`, Esc, and ctrl+c each end the run from every level.
- R-DCXE-2FJH: When the last screen `Run` wrote before it takes a key is the too-small screen, that key MUST change nothing unless it is quit, in the sense of `R-D99O-X4BE`'s "change nothing"; so while the terminal is too small `j`, `G`, →, and ← leave the level, the open harness, session, and agent, and the highlight as they were, and the normal screen written once the terminal is large enough again shows them as before, while `q`, Esc, and ctrl+c still end browsing.
- R-DE5A-G7A6: The *menu rows* of a menu MUST be the rows of its body that can be highlighted, in the order the menu shows them: of the harness menu, its three harness rows; of a session menu, one row per session drawn, the header row and the `no live sessions` line not among them; of an agent menu, one row per line of the tree, the root's line first, the empty line and the key not among them; and a menu showing a failure line MUST have none. A menu row's *item* is the harness, the session, or the tree node its line draws, and its *key* is that item's id — the harness's name, the session's `ID`, or the node's `ID` — together with the number of menu rows above it whose item has the same id; so of two session rows with the same `ID` the upper has the count 0 and the lower 1.
- R-DFD6-TZ0V: On a menu with a highlighted row, up MUST move the highlight to the menu row directly above it and down to the menu row directly below it, and up on the first menu row and down on the last MUST change nothing; so on the harness menu with `Claude Code` highlighted, down three times highlights `Codex`, `Grok`, and `Grok`, and then `k` three times highlights `Codex`, `Claude Code`, and `Claude Code`.
- R-DGL3-7QRK: On a menu with menu row `h` of its `n` menu rows highlighted, counting from 1, page up MUST move the highlight to menu row max(1, `h`−`r`), page down to menu row min(`n`, `h`+`r`), first to menu row 1, and last to menu row `n`, where `r` is the room (`R-DRK6-NOFT`) the menu has at the `H` of the last screen `Run` wrote; so at 120 by 5, where the harness menu's room is 2, page down or ctrl+f on `Claude Code` highlights `Grok`, page up or ctrl+b on `Grok` highlights `Claude Code`, and at 120 by 40 on a session menu of 50 sessions, whose room is 36, page down on the 20th highlights the 50th, `g` the first, and `G` the last.
- R-DHSZ-LII9: On a menu with no highlighted row — one with no menu rows, such as a session menu showing `no live sessions` or any menu showing a failure line — up, down, page up, page down, first, last, and open MUST change nothing, while back and quit act as on any menu.
- R-DJ0V-ZA8Y: On a menu with a highlighted row, open MUST open the level of the highlighted row's item: on the harness menu, that harness's session menu; on a session menu, the agent menu of the highlighted row's session; on an agent menu, the chat of the agent whose tree line is highlighted; with what that level shows, and which of its rows is highlighted first, as "Screens, refresh, failures and lifecycle" and "Chat" state.
- R-DK8S-D1ZN: On a chat, open MUST change nothing, so →, Enter, and `l` leave the chat as it was; and up, down, page up, page down, first, and last MUST act on a chat as "Chat" states and never as on a menu.
- R-UXR4-CZI9: Back MUST change nothing on the harness menu; on a session menu it MUST go back to the harness menu, making it the open level, with the highlight on the row of that session menu's harness; on an agent menu, to the session menu of the same harness, with the highlight on the menu row whose key is that of the row the agent menu was opened from; and on a chat, to the agent menu of the same session, with the highlight on the menu row whose key is that of the row the chat was opened from — save as `R-UWJ7-Z7RK` states when no menu row has that key — whether or not the level left shows a failure line; going back MUST NOT open the menu it goes back to, in the sense of *opened*; so ← four times from the chat of a subagent opened on the harness menu's `Claude Code` highlights that subagent's tree line, then that session's row, then `Claude Code`, then `Claude Code` again.
- R-UWJ7-Z7RK: When back goes back to a menu and none of that menu's menu rows has the key `x` of the row the level being left was opened from, the highlight MUST go to the menu row whose key is that of the nearest row above `x`, among the menu rows the menu had when that open key acted, whose key some menu row still has; when no row above `x` has such a key, to the first menu row; and when the menu has no menu rows, no row MUST be highlighted; so a menu whose rows were `a`, `x`, `b` and are now `b` alone highlights `b`, one whose rows were `a`, `y`, `x` and are now `a`, `b` highlights `a`, and one whose rows were `x`, `b` and are now none highlights nothing.
- R-DP4D-W4YF: Whenever the menu rows of the open menu are replaced by new ones while it stays open (a refresh, "Screens, refresh, failures and lifecycle") and one new menu row has the key of the row highlighted before, the highlight MUST be on that new row, wherever it now stands; so a session added or removed above the highlighted session's row moves the highlight with that session's row, and a status change in the tree leaves it on the same agent.
- R-DQCA-9WP4: Whenever the menu rows of the open menu are replaced by new ones while it stays open and a row was highlighted whose key no new menu row has, the highlight MUST go to the first new menu row whose key was, among the menu rows before the replacement, that of a row below the highlighted one; when no new menu row's key was, to the last new menu row; and when there are no new menu rows, no row MUST be highlighted; so of the sessions `b41f0c77-…`, `7c2e9a41-…`, and `d2a8f613-…` with `7c2e9a41-…` highlighted, that session dropping out highlights `d2a8f613-…`, and that one dropping out next highlights `b41f0c77-…`, the new last row.
- R-DRK6-NOFT: At each normal screen that shows a menu of `n` menu rows, counting from 1, the menu's *room* MUST be max(1, `H`−3−`F`), where `F` is 0 for the harness menu, 1 for a session menu, and 2 for an agent menu; its *top* MUST be `p` = min(max(`q`, `h`−room+1), `h`, max(1, `n`−room+1)) when menu row `h` is highlighted and min(`q`, max(1, `n`−room+1)) when none is, where `q` is the menu's top at the last normal screen `Run` wrote that showed that menu since the menu was last opened — by open, or, for the harness menu, by `Run` starting; back does not open it anew — and 1 when there is none; and the menu rows the menu's body view shows MUST be exactly menu rows `p` to min(`p`+room−1, `n`), in order; so at 120 by 5 the harness menu shows `Claude Code` and `Codex` with either highlighted, and `Codex` and `Grok` once `Grok` is, a highlight moved among the rows shown moves no row, and a highlight moved past the last row shown scrolls by one row.
- R-0B8T-C4QU: A level is *opened* exactly when the open key opens it from the level above (`R-DJ0V-ZA8Y`) and, for the harness menu, when `Run` starts; going back to a menu (back, `R-UXR4-CZI9`) MUST NOT open it: the menu gone back to keeps whether it has content, its menu rows until its read changes them, and its top (`R-DRK6-NOFT`), and its highlight is placed as back states, not on its first row; so a session menu gone back to from an agent menu, whose read fails, keeps showing its last rows rather than a failure line.
- R-O4FT-OCJ8: In a normal screen, the highlighted row (`R-X0MI-XV21`, `R-WVQX-ES39`) MUST be exactly the body row that shows the open menu's highlighted menu row, and there MUST be no highlighted row when the open level is a chat, when no menu row of the open menu is highlighted, or when its highlighted menu row is not among the menu rows its body view shows; no top line, bottom line, or failure line is ever the highlighted row; so on the first screen at 120 by 40 the row showing `Claude Code  2` is in reverse video and no other row is, with `NO_COLOR` set or `TERM` `dumb` alike.
- R-VPYY-S08F: Every filesystem `Run` passes as the `root` argument of a call of `List`, `Tree`, or `Chat` of `internal/harness/claude`, `internal/harness/codex`, or `internal/harness/grok`, and as the `fsys` of every pass of a `*chat.Transcript`, MUST be one value, the *browse root*, whose method set is exactly the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, and `fs.ReadLinkFS`; each of its methods MUST return exactly what the same-named function of `io/fs` (`fs.ReadDir`, `fs.ReadFile`, `fs.Stat`, `fs.ReadLink`, `fs.Lstat`), or for `Open` the `Open` method, returns for `cfg.Root` and the same name, and MUST pass `cfg.Root` that same name unchanged; and every such harness call MUST pass `cfg.Home` as its `home` argument; so that `fs.Glob` and `fs.Sub` over the browse root reach it only through those methods and every name a read uses reaches its watched set.
- R-VR6V-5RZ4: The *menu rows* of the harness menu MUST be, in this order, one row for Claude Code, one for Codex, and one for Grok, all three always, each row's *item* being its harness; a *read* of the harness menu MUST be exactly one call of each of `claude.List` (Claude Code's), `codex.List` (Codex's), and `grok.List` (Grok's); and each row's line MUST be `Claude Code` followed by two U+0020, `Codex` followed by eight, or `Grok` followed by nine, then the *count*: `strconv.Itoa(len(s))` for the sessions `s` its harness's `List` returned in the latest read of the harness menu when that call returned a nil error, and `-` when it returned a non-nil error; so that with two live Claude Code sessions, a missing Codex lock directory, and a Grok index that is not valid JSON the rows are `Claude Code  2`, `Codex        0`, and `Grok         -`, and a harness whose latest read failed shows `-` even when an earlier read succeeded.
- R-PKKF-1154: When `Run` draws its first screen, the open level MUST be the harness menu, read once by `Run` before that screen is drawn, with the highlight on the Claude Code row; so that the first screen is the harness menu when the terminal is at least `MinCols` by `MinRows`, and the too-small screen otherwise (`R-249K-G086`), the harness menu then being what a later normal screen shows unless keys or reads change it.
- R-VTMN-XBGI: Opening the row of a harness MUST make that harness's session menu the open level; a read of that session menu MUST be exactly one call of that harness's `List`; for the sessions `s` it returns with a nil error, with `ordered := session.OrderByStart(s)` and `header, lines := session.TableRows(ordered)` — every element of `ordered`, not only the rows in view, so column widths do not change while the menu scrolls — the menu's *top lines* MUST be `header`, followed by the line `no live sessions` when `ordered` is empty, and its menu rows MUST be, in order, one row for each `i`, whose line is `lines[i]` and whose item is the session `ordered[i]`, its id `ordered[i].ID`; so that the Claude Code sessions `7c2e9a41-…`, started at 2026-09-23T22:10:42Z, and `b41f0c77-…`, started at 2026-09-23T20:31:07Z, are shown under the header with `b41f0c77-…` first, and a harness with no live root session shows `SESSION  STATUS  LAST ACTIVE  CWD  TITLE` and `no live sessions` with no menu row.
- R-VUUK-B377: Opening the row of a session `s` in the session menu of a harness MUST make the agent menu of `s` the open level; a read of that agent menu MUST be exactly one call of that harness's `Tree` with `s.ID` as its `id` argument; for the tree `t` it returns with a nil error, with the lines of `tree.Draw(t, cfg.Color)` being that string split at each `"\n"` with the empty string after its final `"\n"` dropped, the menu's menu rows MUST be, in order, one row for each line before its first empty line, the `k`-th row's item being the node whose line `tree.Draw` writes `k`-th (`D09-tree`: the root, then the drawn subagents), its id that node's `ID`; and its *bottom lines* MUST be that empty line and every line after it, the key; so that for session `7c2e9a41-…` of the agent-tree story the rows are the root's line and five subagent lines, followed by the empty line and the key.
- R-DCFC-PP6M: Each read of an open chat other than the `Chat` call that opens it (`R-AY3X-9NBN`) MUST be, while the chat has no chat transcript (`R-AZBT-NF2C`), exactly one call of the same harness `Chat` with the same arguments as that opening call; and, while it has a chat transcript `t`, exactly one pass `t.Read(r)`, `r` being the browse root, except that while `t.Path()` is the empty string it MUST instead be one call of `Chat` with those same arguments; so that a chat whose first read failed calls `Chat` again at each later read until one succeeds, and one whose transcript has a file reads only what that file gained, as `chat -f` does.
- R-VYI9-GEFA: When a session menu or an agent menu is opened and the first read of it returns a nil error and gives it at least one menu row, the highlight MUST be on its first menu row — on a session menu the row of `ordered[0]`, on an agent menu the root's line; when that read gives it no menu row, or fails, no row MUST be highlighted.
- R-PLSB-ESVT: The body view of the harness menu, and of a session menu or an agent menu that has content, MUST be its top lines (the harness menu has none), then the menu rows `R-DRK6-NOFT` gives it for that screen, in order, then its bottom lines (only an agent menu has any: the empty line, then the key); when these lines number more than `H`−3, the agent menu's empty line MUST be left out first, and then only the first `H`−3 of the lines left are kept; so that the harness menu at 5 rows shows two of its three rows, a session menu shows its header directly above its menu rows, a session menu with no session shows its header and `no live sessions`, an agent menu at 5 rows shows one tree line directly above the key, and at 6 rows one tree line, the empty line, and the key.
- R-W0Y2-7XWO: The *failure line* of a read that returns a non-nil error `err`, on a level of the harness whose argument name `h` is `claude` for Claude Code, `codex` for Codex, or `grok` for Grok, MUST be: when `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `"cannot read " + quote.Field(e.Path) + ": " + e.Err.Error()`; otherwise, when `errors.Is(err, tree.ErrNotFound)` holds, `"no " + h + " session '" + quote.Arg(sid) + "'"`, `sid` being the `ID` of the open session; otherwise, when `errors.Is(err, chat.ErrAgentNotFound)` holds, `"no " + h + " agent '" + quote.Arg(aid) + "' in session '" + quote.Arg(sid) + "'"`, `aid` being the `ID` of the node whose chat is open; so that each failure line is the diagnostic `D02-cli-grammar` gives the same error, without its `agent-monitor: ` prefix and its newline, and the Grok session menu whose index is not valid JSON has the failure line `cannot read /home/dev/.grok/active_sessions.json: not valid JSON`.
- R-W25Y-LPND: A session menu, agent menu, or chat *has content* from the first read of it since it was opened that returns a nil error (for a chat, the read that gives it a transcript) on; while it has no content, its body view MUST be, for a session menu or an agent menu, the failure line of its latest read alone, and for a chat the first `H`−3, or fewer when there are fewer, of the wrapped rows at `W` of that failure line; no row MUST be highlighted, and a chat MUST follow; so that a chat whose transcript cannot be read shows its wrapped failure line with `[following]` in its hint line, and the harness menu, which always has its rows, never shows a failure line.
- R-4T7R-F6JV: A read of a session menu, agent menu, or chat that has content that returns a non-nil error MUST itself change nothing of that level: its top lines, menu rows, and bottom lines, the item highlighted, and, for a chat, its chat entries, its anchor, and whether it follows are what they were before that read; the screen written for that value MUST then draw the level as any screen does at its own size (`R-DRK6-NOFT`, `R-UKQ1-4N11`); and `Run` MUST NOT end browsing, whatever read fails.
- R-W4LR-D94R: `Run` MUST make a read of a level only as follows, and at no other time: one read of the harness menu before its first screen; for each key of a value received from `cfg.Terminal.Keys()` (as `Keys and navigation` takes the value apart) that opens a level or goes back to one, one read of the level that key makes open, made when that key takes effect, before any later key of the value; and, for each value received from `cfg.Watcher.Changes()`, one read of the level open when it is received; every read made for a value MUST be made before that value's screen is written; so that a value received from `cfg.Terminal.Resized()`, and a key that leaves the same level open, cause no read, and the level each screen shows reflects the reads made before it.
- R-W5TN-R0VG: When a read made for a value received from `cfg.Watcher.Changes()` returns a nil error on a menu, the highlight after it MUST be on no row when the menu now has no menu row, and on its first menu row when no row was highlighted just before that read and the menu now has a menu row; in every other case it MUST be where `Keys and navigation` puts a highlight whose item a refresh keeps, moves, or removes; so that a session starting in a session menu that showed `no live sessions`, or an agent menu whose tree is read after its failure line, has its first row highlighted, and a session menu whose last session ends has none.
- R-W71K-4SM5: The *watched set* of a read MUST be the set of names, without duplicates and in ascending bytewise order, made from each name `n` passed as the `name` argument of a call the read causes to a method of the browse root, other than `proc` and every name that begins with `proc/`, as follows: `n` itself when `fs.Stat(cfg.Root, n)` during the read returns a nil error and a `fs.FileInfo` whose `IsDir` is true, and `path.Dir(n)` otherwise; so that a read that lists `home/dev/.claude/sessions`, opens `home/dev/.claude/sessions/41822.json` and `proc/41822/stat`, and stats the missing `home/dev/.grok` has exactly the watched set `home/dev`, `home/dev/.claude/sessions`.
- R-W89G-IKCU: When `cfg.Watcher` is not nil, `Run` MUST call `cfg.Watcher.Watch` with the watched set of the harness menu's first read once the first screen has been written without error, and before its first wait — so also when `ctx.Done()` is closed before `Run` is called; and, after each later screen for whose value `Run` made at least one read, once that screen has been written without error and before its next wait, it MUST call `Watch` with the watched set of the last of those reads whenever that set differs from the set last passed to `Watch`; an empty set MUST be passed as a slice of length zero; and `Run` MUST make no other call to `Watch`, in particular none after a call to `stdout.Write` that returned a non-nil error.
- R-W9HC-WC3J: When `cfg.Watcher` is nil, `Run` MUST write the same screens and return the same value as with a `cfg.Watcher` whose `Watch` does nothing and whose `Changes` channel never delivers a value and is never closed.
- R-WAP9-A3U8: When `cfg.Terminal` is nil, `Run` MUST write the same screens and return the same value as with a `cfg.Terminal` whose `Size` returns 0 columns and 0 rows and whose `Keys` and `Resized` channels never deliver a value and are never closed; so that every screen it writes is the too-small screen and it returns only when `ctx.Done()` closes or a write fails.
- R-WD52-1NBM: After its first screen, `Run` MUST wait, again and again, until it receives a value from `cfg.Terminal.Keys()`, `cfg.Terminal.Resized()`, or `cfg.Watcher.Changes()` or `ctx.Done()` is closed — each such waiting being a *wait* — and MUST NOT receive from any of those three channels at a wait at which `ctx.Done()` is already closed, even when a value is ready there; at such a wait it MUST return nil without writing another screen; a nil `ctx.Done()` MUST be taken as a channel that never closes.
- R-WECY-FF2B: When `ctx.Done()` closes after `Run` has received a value from `cfg.Terminal.Keys()`, `cfg.Terminal.Resized()`, or `cfg.Watcher.Changes()` and before the screen for that value is completely written, `Run` MUST still make the reads for that value and write that screen whole, and make the call to `Watch` it causes, if any, before its next wait, at which it returns nil.
- R-WFKU-T6T0: When a receive from `cfg.Terminal.Keys()` finds that channel closed, `Run` MUST return nil at once, without writing a screen for it, without waiting for a value from `cfg.Terminal.Resized()` or `cfg.Watcher.Changes()` or for `ctx.Done()` to close, and making no further read, call to `Watch`, or call to `cfg.Terminal.Size()`.
- R-WGSR-6YJP: When a receive from `cfg.Terminal.Resized()` or from `cfg.Watcher.Changes()` finds that channel closed, `Run` MUST NOT receive from that channel again and MUST go on browsing, waiting at each later wait on the other channels and `ctx.Done()`; so that after `Changes` closes the open level is read only when a key opens or goes back to a level.
- R-WI0N-KQAE: When a call `Run` makes to `stdout.Write` returns a non-nil error `err`, `Run` MUST return at once, and MUST return `err` itself, not wrapped, so that for a comparable `err` the returned value `==` `err`; it MUST make no further call to `stdout.Write`, no further read, no further call to `Watch` or to any method of `cfg.Terminal`, and no further receive from any channel.
- R-WJ8J-YI13: `Run` MUST return nil when it ends browsing on a value from `cfg.Terminal.Keys()` that holds a key ending browsing (`Keys and navigation`), on the close of `cfg.Terminal.Keys()`, or on `ctx.Done()` closing, MUST return a non-nil error only as `R-WI0N-KQAE` states, and MUST NOT return in any other case; and once it has returned, it and every goroutine it started MUST NOT call `stdout.Write` or a method of `cfg.Terminal`, `cfg.Watcher`, or `cfg.Root`, nor receive from their channels; so that its caller, which writes the leave sequence and restores the terminal after `Run` returns, restores a terminal nothing is still drawing on, on every path.
- R-WKGG-C9RS: `Run` MUST NOT call, on `cfg.Root` or on any file or other value obtained through it, any method named `Write`, `WriteAt`, `WriteString`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Truncate`, `Chmod`, `Chown`, `Chtimes`, `Symlink`, `Link`, or `Sync`, even when the value has such a method, so that browsing creates, changes, and removes nothing.
- R-AY3X-9NBN: When `Run` opens the highlighted line of an agent menu (as "Keys and navigation" states), the level it opens MUST be the chat of the tree node that line draws, the root or a subagent at any depth, and before writing the screen for the `Keys` value that opened it `Run` MUST call, exactly once for that opening, the `Chat` function of the open harness's package — `claude.Chat` for `Claude Code`, `codex.Chat` for `Codex`, `grok.Chat` for `Grok` — with `cfg.Home` as its home argument, the open session's `ID` as its session id, and that node's `ID` as its agent id, its root argument being the one "Screens, refresh, failures and lifecycle" gives every harness call; so the root's line opens the chat whose agent id is the session id, and the line of a subagent two levels below the root opens that subagent's chat.
- R-AZBT-NF2C: While a chat is open, its *chat transcript* MUST be the `*chat.Transcript` returned by the latest call made for it, since it was last opened, of the harness `Chat` function that returned a nil error, and it has none while no such call has; its *chat entries* MUST be, in order, the entries returned with a nil error by the call that opened it and then by every later pass of its chat transcript and every later `Chat` call made for it as "Screens, refresh, failures and lifecycle" states, each call's or pass's entries in the order returned, and no other; so a chat opened again starts from the entries of the call that opened it alone, the entries of a pass that resets follow the entries already taken in, and no entry taken in is removed while the chat stays open.
- R-VPE2-NQ9H: For a normal screen of `W` by `H`, the *chat lines* MUST be the lines of the string `s` made by concatenating `chat.Format(x)` for each chat entry `x` in order — `s` split at every `"\n"`, the empty string after its final `"\n"` left out — none when there is no chat entry; the *chat rows* MUST be the wrapped rows at `W` of each chat line in turn, concatenated in order, numbered from 0, `N` being their number; the *totals line* MUST be `chat.TotalsLine(t.Usage(), t.Recorded())` with its final `"\n"` removed, `t` being the chat transcript when the screen is drawn; and the *totals rows* MUST be the wrapped rows at `W` of the totals line when there are at most `H`−4 of them, and otherwise the totals line alone, as one line (cut at `W` like every row), `T` being their number; so the two entries `"2026-09-24T20:05:40Z agent\nDraft a fix for refunds that round down.\n\n"` and `"2026-09-24T20:05:42Z assistant\nI'll start from the refund calculation.\n\n"` give at a `W` of 120 the six chat rows `2026-09-24T20:05:40Z agent`, `Draft a fix for refunds that round down.`, the empty line, `2026-09-24T20:05:42Z assistant`, `I'll start from the refund calculation.`, and the empty line, a line of 250 runes gives at that `W` three chat rows, a totals line of 82 runes is three totals rows at 40 by 7 and one totals row, its first 40 runes, at 40 by 6 and at 40 by 5, and at least one body row is always left for chat rows.
- R-B1RM-EYJQ: For a normal screen of `W` by `H` drawn while a chat with a chat transcript is open, the *chat page* `V` MUST be `H`−3−`T` when that is greater than 0 and 0 otherwise, and the *end top* `E` MUST be `N`−`V` when `N` is greater than `V` and 0 otherwise; the chat *fits* when `N` is at most `V`; so at 120 by 9 with one totals row `V` is 5, and six chat rows give an `E` of 1.
- R-B2ZI-SQAF: While a chat with a chat transcript is open, the body view of a normal screen MUST be the chat rows numbered `top` to min(`top`+`V`, `N`)−1, in order, then the totals rows, all but the first `H`−3 of these lines left out, where `top` is the chat's *top* for that screen as its following or paused state gives it; so the totals line is always the last line of the body view below the chat rows in view however the chat is scrolled, a chat that fits shows all its chat rows with the totals line directly below them and empty rows down to the hint line, and at 120 by 9 the six chat rows of the example above, following, show the five chat rows numbered 1 to 5 and the totals line directly above the hint line.
- R-B47F-6I14: A chat MUST follow when it is opened, and, while it follows, its `top` for each normal screen MUST be that screen's `E`; so each screen of a following chat shows the end of its chat rows, and entries taken in since the previous screen, the totals rows as they now are, and a resize are all shown at once.
- R-UKQ1-4N11: While a chat is paused it holds an *anchor* (`a`, `o`) that only a scroll key sets (`R-UNWG-VZFT`); for each normal screen drawn while it is paused, let `r` be the number, among that screen's chat rows, of the wrapped row at that screen's `W` of chat line `a` (numbered from 0) that holds that line's rune at offset `o`, counted from 0 in its shown form — the wrapped row numbered ⌊`o`/`W`⌋ of that line, its only wrapped row when the line is empty: when `r` is less than that screen's `E`, its `top` MUST be `r` and the chat stays paused; when `r` is at least `E`, the chat MUST follow from that screen on, that screen included, so its `top` is `E` and its hint line reads `[following]`; taking in chat entries, receiving a value from `cfg.Terminal.Resized()`, and writing the too-small screen MUST NOT change the anchor; so while the view is scrolled up entries taken in collect below it and change none of the chat rows in view while the totals rows show the new totals, after a resize that leaves the end out of view the first chat row in view begins with the same rune of the same chat line as before, and a resize that brings the end into view makes the chat follow, so entries taken in later come into view under `[following]`, never under `[paused]`.
- R-UNWG-VZFT: While a chat with a chat transcript is open, save as `R-DCXE-2FJH` states, each key of a `Keys` value that is up, down, page up, page down, first, or last (`R-D99O-X4BE`), taken in the value's order, MUST, with `W` and `H` those of the last screen `Run` wrote before it takes the key, `N`, `V`, and `E` those of the chat as it is at that `W` and `H`, and the `top` the chat has just before the key at that size (given by its state after the keys before it), set the *target* to `top`−1 for up, `top`+1 for down, `top`−`V` for page up, `top`+`V` for page down, 0 for first, and `E` for last, then raised to 0 when below 0 and lowered to `E` when above `E`; when the target equals `E` the chat MUST follow after the key, and otherwise it MUST be paused after the key with the anchor (`a`, `o`) where `a` is the number of the chat line whose wrapped row is the chat row numbered by the target and `o` is `W` times that wrapped row's number among the line's wrapped rows; so a key's effect depends only on the size of the screen already on the terminal, as on a menu, in a chat that fits every such key leaves the chat following and changes nothing, first (`g`) in a chat that does not fit pauses it with chat row 0 at the top, last (`G`) always makes it follow, and down or page down that brings the end into view makes it follow again.
- R-UX5E-XI9H: While a chat has no chat transcript, up, down, page up, page down, first, and last (`R-D99O-X4BE`) MUST change nothing, the chat following as `R-W25Y-LPND` states; so a chat showing its failure line never pauses and its hint line keeps `[following]`.
