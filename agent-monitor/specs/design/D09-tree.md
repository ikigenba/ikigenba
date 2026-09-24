# D09-tree

`internal/tree` is the vocabulary the three harness packages and the command
share when `agent-monitor tree <harness> <session-id>` runs, as
`internal/session` (`D04-sessions-and-table`) is for `list`. A harness
package's `Tree` function (declared in `D06-claude`, `D07-codex`, and
`D08-grok`) turns what its harness left on disk about one root session into a
`Tree` value; the command hands that value to `Draw`, together with whether to
draw in colour, which the command decides (`D02-cli-grammar`), and what `Draw`
returns is the whole of standard output. The package reaches nothing on the machine:
it only formats. It imports, of this module, `internal/quote` alone, as
`D01-layout-and-run-seam` states.

The tree gets a package of its own rather than growing `internal/session`:
the list table's vocabulary stays exactly as it is, and the tree's statuses,
nodes, and drawing are one concern a reader can hold whole.

A node is one line of the tree: the root session or one subagent. It carries
its id (the session id for the root; a Claude agent id, Codex thread id, or
Grok subagent id for a subagent), the id of the subagent that started it
(empty when the root session started it or when that cannot be told), its
label as the harness recorded it, unescaped (empty when it has none), its
status, and the moment it was started, used only to order siblings. As in
`D04`, a start moment that is not known is said by a separate flag, never by
the zero time. A `Tree` is the root node and a flat list of subagents; the
shape of the tree comes from the parent ids, so a harness never has to build
nested values, and a subagent whose parent cannot be told simply has an empty
parent and is drawn under the root.

Status is one of seven words. A live root is `working`, `idle`, or `unknown`,
as in `list`; a root that is not live is `ended`; a subagent is `working`,
`done`, `failed`, `killed`, or `unknown`. Which harness condition gives which
word is each harness design's business. The harness designs guarantee the
seven, but `Draw` rejects nothing: a status outside them is drawn, coloured,
and counted as `unknown`, so no line is left without a status and the key's
counts always add up to the number of lines.

`Draw` draws as the Unix `tree` command does. The root's line comes first,
with no prefix. Every other line sits under its parent, prefixed by `├── `,
or by `└── ` for the last of its siblings; each deeper level is continued by
`│   ` below a `├── ` line and by four spaces below a `└── ` line. After the
prefix a line is a dot, the node's full id in square brackets, and its label
when it has one; with colour off, two spaces and the status word follow. The
id and the label are escaped with `quote.Field` (`D02-cli-grammar`), exactly
as `list` prints a title, so every node is one line whatever bytes the harness
recorded. A node with no label has nothing after the `]` but, with colour off,
its status word: the id is never repeated as a label. With colour on the
status is carried by the dot alone, written in an SGR colour and reset right
after it (cyan `working`, blue `idle`, green `done`, yellow `killed`, red
`failed`, magenta `ended`, gray `unknown`), and nothing else is coloured.
After the tree comes one empty line and then the key: one line naming the
seven statuses in a fixed order, each with its dot, coloured the same way,
and the number of lines of the tree, the root's included, that have it.
Siblings are drawn oldest first;
siblings whose start moment is not known come after every sibling whose start
is known; ties, and siblings with no known start, are ordered by id, byte by
byte.

`Draw` never fails and never refuses an input. A subagent that names a parent
the tree does not hold, names the root, names itself, or sits on a loop of
parent links is drawn under the root; a second subagent with an id already
seen, or with the root's id, is not drawn. So every `Tree` value draws, and
every subagent that is drawn is drawn exactly once.

`ErrNotFound` is the single signal, shared by the three harness packages, that
the named root session does not exist, so the command can tell "no such
session" from "the data could not be read" (`*session.ReadError`) without
knowing the harness. A harness returns one of the two, or no error, and
nothing else. Each harness has exactly one locating directory:
`$HOME/.claude/projects`, `$HOME/.codex/sessions`, or `$HOME/.grok/sessions`,
made absolute as the `List` functions make their directories, so it is
absolute even when `HOME` is not. The data could not be read only when that
directory exists but cannot be checked or listed; then the harness cannot
tell whether the session exists, so that failure wins, and the error names
that directory, which is the path the `cannot read` diagnostic prints. Anything else that cannot be read — a subdirectory, a log,
a registry, a lock, a liveness source — only makes lines fall back. The
session does not exist when the locating directory is missing altogether,
whatever else records the session, or when no readable place that records the
harness's root sessions holds one with that id, a place that cannot be read
counting as holding none; a root recorded only in a
registry, lock directory, or `active_sessions.json` while the locating
directory exists is found. What records a root session, and what counts as
one, is each harness design's business.

## REQUIREMENTS

- R-2TXU-JT5P: The `internal/tree` package (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/tree`, package name `tree`) MUST export the named type `type Status string`.
- R-2V5Q-XKWE: The `internal/tree` package MUST export the constants `StatusWorking Status = "working"`, `StatusIdle Status = "idle"`, `StatusUnknown Status = "unknown"`, `StatusEnded Status = "ended"`, `StatusDone Status = "done"`, `StatusFailed Status = "failed"`, and `StatusKilled Status = "killed"`, each declared with the type `Status`.
- R-2WDN-BCN3: The `internal/tree` package MUST export the struct type `type Node struct { ID string; Parent string; Label string; Status Status; Started time.Time; HasStarted bool }`, with exactly these fields in this order.
- R-2XLJ-P4DS: The `internal/tree` package MUST export the struct type `type Tree struct { Root Node; Subagents []Node }`, with exactly these fields in this order.
- R-VZAE-KSAJ: The `internal/tree` package MUST export `func Draw(t Tree, color bool) string`.
- R-301C-GNV6: The `internal/tree` package MUST export the variable `ErrNotFound` of type `error`, and its value MUST be non-nil.
- R-DKI4-OYFH: The `Tree` function of each of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` MUST return, as its error, only a nil error, exactly the value `tree.ErrNotFound` (so that `err == tree.ErrNotFound`), or an error whose dynamic type is `*session.ReadError`.
- R-51PW-B3K9: The *locating directory* of a harness MUST be exactly one directory, the absolute path `path.Join("/", home, ".claude", "projects")` for `internal/harness/claude`, `path.Join("/", home, ".codex", "sessions")` for `internal/harness/codex`, and `path.Join("/", home, ".grok", "sessions")` for `internal/harness/grok`, where `home` is the `home` argument of `Tree`, and it MUST be named to `root` as that path without its leading `/` (for `home` `/home/dev`, the name `home/dev/.codex/sessions`).
- R-52XS-OVAY: The `Tree` function of each of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` MUST return a `*session.ReadError` whose `Path` is exactly its locating directory's absolute path if and only if `fs.Stat` of the locating directory through `root` fails with an error `err` for which `errors.Is(err, fs.ErrNotExist)` is false, or that `fs.Stat` succeeds and `fs.ReadDir` of the locating directory through `root` fails with such an error; a subdirectory, log, registry, lock, or liveness source that cannot be read MUST NOT make it return a `*session.ReadError`.
- R-JERO-H7QH: The `Tree` function of each of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` MUST return `tree.ErrNotFound` if and only if it returns no `*session.ReadError` and either its locating directory does not exist (stat-ing it or listing its entries fails with an error `err` for which `errors.Is(err, fs.ErrNotExist)` is true), or no readable place among the places its harness design names as recording that harness's root sessions — the locating directory and any other such place its design names, such as a registry, a lock directory, or `active_sessions.json` — holds a root session, as that design defines one, with the id it is given, a place that cannot be read counting as holding no root session; a root session recorded only in a readable place other than the locating directory, while the locating directory exists, is found.
- R-W91L-MY83: The *drawn subagents* of a `Tree` `t` MUST be, for each distinct `ID` value among the elements of `t.Subagents` other than `t.Root.ID`, the first element of `t.Subagents` (lowest index) with that `ID`; `Draw` of `t`, with either value of `color`, MUST write exactly one line for each drawn subagent and no line for any other element of `t.Subagents`, so that for `Subagents` of `{ID: "x", Label: "first"}`, `{ID: "x", Label: "second"}`, and an element whose `ID` equals `t.Root.ID`, only the line of `first` is written after the root's.
- R-W7TP-96HE: `Draw(t, color)` MUST return exactly the root's line followed by the lines of the drawn subagents in depth-first pre-order: each node's line is followed immediately by the lines of all the nodes drawn under it, at every depth, before the line of its next sibling, and the nodes directly under one node come in the sibling order of `Draw`; then one `"\n"`, an empty line; then the key; the result MUST contain no byte outside those lines, that empty line, and the key.
- R-34WX-ZQTY: A drawn subagent `e` of a `Tree` `t` that is not on a parent cycle MUST be drawn directly under the drawn subagent whose `ID` equals `e.Parent` when `e.Parent` is non-empty, differs from `t.Root.ID`, and equals the `ID` of a drawn subagent, and directly under the root otherwise — when `e.Parent` is empty, equals `t.Root.ID`, or names no drawn subagent.
- R-WF53-JSXK: A drawn subagent `e` of a `Tree` `t` is on a parent cycle when, starting from `e` and repeatedly moving to the drawn subagent whose `ID` equals the current one's `Parent` (while that `Parent` is non-empty, differs from `t.Root.ID`, and names a drawn subagent), `e` is reached again; `Draw` MUST draw every drawn subagent on a parent cycle directly under the root, so that for `Subagents` `{ID: "p", Parent: "q"}`, `{ID: "q", Parent: "p"}`, `{ID: "c", Parent: "p"}`, and `{ID: "s", Parent: "s"}`, each with an empty `Label`, `HasStarted` false, and `Status` `StatusDone`, `Draw(t, false)` returns the root's line, then exactly `"├── ● [p]  done\n│   └── ● [c]  done\n├── ● [q]  done\n└── ● [s]  done\n"`, then the empty line and the key.
- R-37CQ-RABC: `Draw` MUST order the nodes drawn directly under one node so that every node with `HasStarted` true comes before every node with `HasStarted` false; nodes that both have `HasStarted` true come in ascending order of `Started` as compared by `time.Time.Compare` (oldest first); and two nodes that tie — both `HasStarted` true with `Started` values for which `time.Time.Compare` returns 0, or both `HasStarted` false — come in ascending bytewise order of their raw `ID` strings (not of their labels or escaped form); `Started` of a node with `HasStarted` false MUST NOT affect the output. So siblings with `ID`s `b` and `a` started at the same moment, `d` started a second earlier, and `z` and `c` with no known start are drawn in the order `d`, `a`, `b`, `c`, `z`.
- R-38KN-5221: The root's line MUST have no prefix; the line of a drawn subagent MUST end its prefix with the connector `├── ` (U+251C, U+2500, U+2500, U+0020) when the subagent is not the last of the nodes drawn directly under its parent node, and with `└── ` (U+2514, U+2500, U+2500, U+0020) when it is the last.
- R-WGCZ-XKO9: The prefix of the line of a drawn subagent at depth `d` (a node drawn directly under the root has depth 1) MUST be, before its connector, the concatenation, for each of its ancestors at depths 1 to `d`−1 in order from depth 1, of `│   ` (U+2502 followed by three U+0020) when that ancestor is not the last of the nodes drawn directly under its own parent node and four U+0020 when it is, so that a subagent at depth 1 has only its connector; for example, a root with `ID` `r`, `Label` empty and `Status` `StatusWorking`, and `StatusDone` subagents with no labels `A` (under the root), `A1` (under `A`), `A1a` (under `A1`), `B` (under the root), `B1` (under `B`), `B1a`, and `B1b` (both under `B1`), started in that order, draws with `color` false `"● [r]  working\n├── ● [A]  done\n│   └── ● [A1]  done\n│       └── ● [A1a]  done\n└── ● [B]  done\n    └── ● [B1]  done\n        ├── ● [B1a]  done\n        └── ● [B1b]  done\n\n● working (1)  ● idle (0)  ● done (7)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (0)\n"`.
- R-W0IA-YK18: The *effective status* of a node `n` MUST be `n.Status` when `n.Status` equals one of `StatusWorking`, `StatusIdle`, `StatusDone`, `StatusKilled`, `StatusFailed`, `StatusEnded`, and `StatusUnknown`, and `StatusUnknown` otherwise, so that a node whose `Status` is `Status("stalled")` or `Status("")` has the effective status `StatusUnknown`; `Draw` MUST take a node's dot, status word, and count from its effective status alone.
- R-W1Q7-CBRX: The *dot* of a status `s` in the output of `Draw(t, color)` MUST be exactly `●` (U+25CF) when `color` is false, and, when `color` is true, exactly the byte 0x1b, `[`, the SGR code of `s`, `m`, `●` (U+25CF), the byte 0x1b, and `[0m` (the Go expression `"\x1b[" + code + "m●\x1b[0m"`), where the SGR code is `36` for `StatusWorking`, `34` for `StatusIdle`, `32` for `StatusDone`, `33` for `StatusKilled`, `31` for `StatusFailed`, `35` for `StatusEnded`, and `90` for `StatusUnknown`; so that with `color` true the dot of `StatusIdle` is the Go string `"\x1b[34m●\x1b[0m"`.
- R-W2Y3-Q3IM: The line `Draw(t, color)` writes for a node `n`, the root included, MUST be its prefix, then the dot of `n`'s effective status, then `" ["`, `quote.Field(n.ID)`, and `"]"`, then, only when `n.Label` is non-empty, one U+0020 and `quote.Field(n.Label)`, then, only when `color` is false, exactly two U+0020 and the string value of `n`'s effective status, then one `"\n"`, and no other byte; an empty `Label` MUST NOT be replaced by the id or by any other text. So the root line of a node with `ID` `b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14`, an empty `Label`, and `Status` `StatusIdle` is the Go string `"● [b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14]  idle\n"` with `color` false and `"\x1b[34m●\x1b[0m [b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14]\n"` with `color` true; a `Label` of `fix Bob's checkout` prints ` fix Bob's checkout` after the `]`; and the root line of a node with `ID` `a` TAB `b`, `Label` `x` TAB `y`, and `Status("stalled")` is, with `color` false, `● [a\tb] x\ty  unknown` followed by a newline, each `\t` there being the two bytes `\` and `t`.
- R-W460-3V9B: The *key* of `Draw(t, color)` MUST be, for the statuses `StatusWorking`, `StatusIdle`, `StatusDone`, `StatusKilled`, `StatusFailed`, `StatusEnded`, and `StatusUnknown` in that order, one entry for each status `s` (the dot of `s`, one U+0020, the string value of `s`, `" ("`, the count of `s` in base 10 with no sign and no leading zero, `0` when it is zero, and `")"`), the entries joined by exactly two U+0020 and followed by one `"\n"`, and no other byte; so that with `color` false and counts 3, 0, 1, 1, 1, 0, and 0 in that order the key is `"● working (3)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (0)  ● unknown (0)\n"`.
- R-W6LS-VEQP: The *count* of a status `s` in the key of `Draw(t, color)` MUST be the number of nodes among `t.Root` and the drawn subagents of `t` whose effective status is `s`, whatever the value of `color`, and an element of `t.Subagents` that is not a drawn subagent MUST NOT be counted; so for a root with `Status` `StatusIdle` and `Subagents` `{ID: "x", Status: StatusDone}`, `{ID: "x", Status: StatusKilled}`, `{ID: "y", Status: Status("stalled")}`, and an element whose `ID` equals `t.Root.ID` with `Status` `StatusFailed`, the counts are 1 for `idle`, `done`, and `unknown` and 0 for every other status.
- R-3EO5-1WRI: `Draw` MUST NOT modify its argument: `t.Root`, the length of `t.Subagents`, and every element of `t.Subagents` are the same after the call as before it.
- R-WA9I-0PYS: `Draw` of a `Tree` with no drawn subagents MUST return exactly the root's line, the empty line, and the key, so that `Draw(Tree{Root: Node{ID: "b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14", Status: StatusIdle}}, false)` returns exactly `"● [b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14]  idle\n\n● working (0)  ● idle (1)  ● done (0)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (0)\n"`, for nil and empty `Subagents` alike.
- R-WBHE-EHPH: For a `Tree` whose `Root` has `ID` `7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93`, `Label` `fix Bob's checkout`, and `Status` `StatusWorking`, and whose `Subagents`, in any order in the slice, are `a1c4e7f09b2d38561` (`Find the checkout handler`, `StatusDone`), `a2d5f8e1c3b049672` (`Review the payment tests`, `StatusWorking`), `a3e6f9d2b4c150783` (`Parent` `a2d5f8e1c3b049672`, `Run the payment suite`, `StatusFailed`), `a4f7e0c3d5a261894` (`Profile the cart query`, `StatusKilled`), and `a5b8c1f4e6d372905` (`Draft the refund fix`, `StatusWorking`), each with `HasStarted` true and started in this order, and every `Parent` not given empty, `Draw(t, false)` MUST return exactly `"● [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout  working\n├── ● [a1c4e7f09b2d38561] Find the checkout handler  done\n├── ● [a2d5f8e1c3b049672] Review the payment tests  working\n│   └── ● [a3e6f9d2b4c150783] Run the payment suite  failed\n├── ● [a4f7e0c3d5a261894] Profile the cart query  killed\n└── ● [a5b8c1f4e6d372905] Draft the refund fix  working\n\n● working (3)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (0)  ● unknown (0)\n"`.
- R-WCPA-S9G6: For the `Tree` `t` that R-WBHE-EHPH describes, `Draw(t, true)` MUST return exactly the Go string `"\x1b[36m●\x1b[0m [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout\n├── \x1b[32m●\x1b[0m [a1c4e7f09b2d38561] Find the checkout handler\n├── \x1b[36m●\x1b[0m [a2d5f8e1c3b049672] Review the payment tests\n│   └── \x1b[31m●\x1b[0m [a3e6f9d2b4c150783] Run the payment suite\n├── \x1b[33m●\x1b[0m [a4f7e0c3d5a261894] Profile the cart query\n└── \x1b[36m●\x1b[0m [a5b8c1f4e6d372905] Draft the refund fix\n\n\x1b[36m●\x1b[0m working (3)  \x1b[34m●\x1b[0m idle (0)  \x1b[32m●\x1b[0m done (1)  \x1b[33m●\x1b[0m killed (1)  \x1b[31m●\x1b[0m failed (1)  \x1b[35m●\x1b[0m ended (0)  \x1b[90m●\x1b[0m unknown (0)\n"`.
- R-WDX7-616V: For a `Tree` whose `Root` has `ID` `e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54`, an empty `Label`, and `Status` `StatusIdle`, and whose `Subagents` are `a8e1f4a7b9d2c5306` (`Map the docs routes`, `StatusUnknown`, `HasStarted` true), `a9f2a5b8c0e3d6417` (`Parent` `a8e1f4a7b9d2c5306`, `List the page templates`, `StatusDone`, `HasStarted` true, started later), and `a0a3b6c9d1f4e7528` (empty `Parent`, empty `Label`, `StatusUnknown`), `Draw(t, false)` MUST return exactly `"● [e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54]  idle\n├── ● [a8e1f4a7b9d2c5306] Map the docs routes  unknown\n│   └── ● [a9f2a5b8c0e3d6417] List the page templates  done\n└── ● [a0a3b6c9d1f4e7528]  unknown\n\n● working (0)  ● idle (1)  ● done (1)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (2)\n"` both when `a0a3b6c9d1f4e7528` has `HasStarted` true with a `Started` later than that of `a8e1f4a7b9d2c5306` and when it has `HasStarted` false.
