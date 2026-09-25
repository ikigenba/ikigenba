# D08-grok

How `list grok` finds the live root sessions of the Grok Build CLI, how
`tree grok` draws one root session with its subagents, and how `chat grok`
reads one agent's chat and its own token usage. The package
`internal/harness/grok` exports three functions. `Tree` returns the
`tree.Tree` that `tree` draws (`D09-tree`), and `Chat` the `chat.Transcript`
and first entries that `chat` prints (`D10-chat`); both are described in
their own sections below. `List` turns what Grok leaves under `$HOME` into
`session.Session` values (`D04-sessions-and-table`), using the process facts
of `D05-process-facts` to decide which index entries are still live. The
command (`internal/cli`) calls `List` with the machine's filesystem and the
home directory, prints `session.Table` of the result, and turns a returned
`*session.ReadError` into the `cannot read` diagnostic.

Grok keeps one index of its running root sessions, `~/.grok/active_sessions.json`:
a JSON array of objects, each with the session id, pid, working directory,
and the moment the session was opened (`opened_at`, an RFC 3339 time with
nanoseconds). Subagents are never in it. An entry can outlive its session, so
an entry is live only while its process runs and did not start after the
session was opened. The index is the one thing whose failure fails the
command: unreadable, or not a JSON array of objects, it is reported as a
`*session.ReadError`, the latter with the cause `session.ErrNotJSON`. A
missing index means Grok has no live session.

Each session keeps its files in `~/.grok/sessions/<encoded-cwd>/<session-id>/`.
The encoding is Grok's business, so the directory is found by looking in
every encoded-cwd directory for one named after the session id. The title is
`generated_title` in `summary.json`. `events.jsonl` gives STATUS and LAST
ACTIVE from its complete records, using the record and timestamp definitions
`D04-sessions-and-table` shares with every harness: a session is idle when
its last record ends a turn or no turn has started yet, and working
otherwise, a session waiting on the developer included; LAST ACTIVE is the
latest `ts`. A half-written last line is ignored. Each field falls back on
its own when its file cannot be read.

`List` only reads. It reaches the machine solely through the `fs.FS` it is
given, touches none of the `*.lock` files Grok keeps beside its session files,
and can neither write nor lock anything.

## The tree of a session

`agent-monitor tree grok <session-id>` calls `Tree` with the same filesystem
and home directory and the id as given, and hands the `tree.Tree` it returns
to `tree.Draw` (`D09-tree`) with the command's colour decision. `D09` already fixes the errors: the locating
directory is `~/.grok/sessions`, only a failure to check or list it is a
`*session.ReadError`, and `tree.ErrNotFound` means no readable place that
records Grok's root sessions holds one with the id. This design says what
those places are and what a root session is.

Two places record root sessions: the sessions directory, and the index. A
subagent is a full Grok session too, with its own session directory beside
the root's (usually in the same encoded-cwd directory, but not always), so
a session directory alone does not make a root. Grok marks a subagent's own
session in its `summary.json`, whose `session_kind` is `subagent` (or
`subagent_resume` for a resumed one); a root's is absent or `headless`. So a
session directory is a root unless its own `summary.json` says it is a
subagent's, which needs no other session's files. When that file cannot be
read, the directory counts as a root: it cannot be told otherwise. The index
never lists a subagent; an entry that is live by `list`'s rule records a root
even before its session directory exists, and a stale entry records nothing.
An id that is empty, holds a `/`, or is `.` or `..` names no session.

The root's line is the `list` row's: the label is `list`'s title
(`generated_title`; when that is empty the line has no label), and the
status is `list`'s STATUS when the root is live and `ended` when it is not. A
headless `grok -p` session is never in the index, so it is always drawn
`ended`. When the index cannot be read, or is not a JSON array of objects,
whether the root is live cannot be told: the root is `unknown`, and its
subagents are judged as in an ended session.

Grok keeps every subagent, at every depth, in the root's directory:
`subagents/<subagent-id>/meta.json`, whose `description` is the label,
`started_at` the start moment, and `status` the status. `running` is
`working` in a live session and `unknown` otherwise, since nothing will
finish it; `completed` is `done`, `failed` is `failed`, and `cancelled` is
`killed`. Each entry of `subagents/` is one subagent; a meta file that cannot
be read leaves that line with its id, `unknown`, and no known start.

A meta file's `parent_session_id` always names the root, even for a subagent
started by another subagent, so the parent comes from the spawn records
instead. The root's `updates.jsonl` holds one `subagent_spawned` record for
every subagent at every depth, naming its `subagent_id`, its
`child_session_id` (the id of its own session directory), and the
`parent_prompt_id` of the prompt that spawned it. That prompt id appears as
`params._meta.promptId` on the records of the spawning session's own
`updates.jsonl`: the root's for a subagent the root started, the spawning
subagent's own session for a deeper one. When the prompt is the root's, or no
single subagent's session holds it, or the spawn records cannot be read, the
subagent is drawn under the root.

`events.jsonl` and `updates.jsonl` are logs, each read with one pass of a
fresh `session.Log` (`D04-sessions-and-table`); the index, `summary.json`, and
`meta.json` are small JSON files read whole. Like `List`, `Tree` only reads,
through the `fs.FS` it is given, never touches a `*.lock` file, and takes no
lock.

## The chat of an agent

`agent-monitor chat grok <session-id> [<agent-id>]` calls `Chat`
(`D10-chat`), which finds the session exactly as `Tree` does and the agent
among the ids `tree` draws, reading the index, `summary.json`, the
`subagents` listing, and the named subagent's `meta.json` to do so, then
reads the agent's own `updates.jsonl` as its transcript; the only other
logs it reads are those of the root's subagents, while decoding the root's
turn ends, as described below. Every agent, the root and each subagent at every depth, is
a full Grok session with its own session directory, so the transcript is
`updates.jsonl` in that directory. The root's is the directory found for
the session id. A subagent's is the directory of its child session, whose
id its `meta.json` records as `child_session_id`; that directory lives under
the encoding of the child's own working directory, which is usually the
root's but not always, so it is found the way every session directory is,
by looking in every encoded-cwd directory. A root that only the index
knows, or a subagent whose child session has no directory yet, has no
transcript, and its chat is empty. Grok records every one of the six counts.

Each line of `updates.jsonl` is one whole update: Grok writes a reply, a
reasoning summary, or a prompt as a single `*_message_chunk` or
`agent_thought_chunk` record, never streamed across records, so every record
becomes at most one entry, on its own, and nothing is held back waiting for
the next record. The entry's moment is the record's `timestamp`, whole
seconds since the epoch. A prompt is `user` in the root and `agent` in a
subagent, where it is the task its parent gave it; Grok records the text as
typed, and marks the notices it injects as prompts (`hideFromScrollback`),
which are skipped. `agent_message_chunk` is `assistant`,
`agent_thought_chunk` is `reasoning`. A `tool_call` is `tool` with the name
Grok's tool metadata gives (the title when there is none) and its
`rawInput` as the arguments; the `tool_call_update` that carries a final
status is the result: `failed` is an error, and so is a `completed` shell
command whose recorded exit code is not zero, because Grok marks those
completed. The root's `updates.jsonl` also records when a subagent
finishes, with the text it handed back; that is an `agent` entry for a
subagent the root started itself. Hooks, spawn records, retries, plans,
background-task notices, recaps, and turn ends make no entry.

A resumed subagent's session begins with a copy of the session it resumes.
Grok stamps every record with an `eventId` that begins with the id of the
session that wrote it, so a record whose `eventId` names another session is
copied history: it makes no entry and counts no tokens.

Usage comes from `turn_completed`, one per turn. Grok's `inputTokens` is
the whole prompt, the cached part included, and `outputTokens` includes
reasoning. `cacheCreationTokens` has only ever been observed as 0, so
whether `inputTokens` includes it is unknown; subtracting it from
`inputTokens` to get `in` is this design's rule, not an observed fact. A root's turn also counts every subagent the root itself started
that finished during the turn — not deeper subagents, whose usage no turn
of their parent includes — so when the decoder meets a root's
`turn_completed` it subtracts, for each such subagent, the usage that
subagent's own `updates.jsonl` records. The decoder keeps one `session.Log`
per such subagent for its whole life and a running total for it, so each
later read takes in only what that subagent appended since. This design
accepts one limitation: if a subagent's recorded usage grows after a parent
turn has subtracted it (a subagent that finishes in two turns, say), a
running `watch` subtracts the growth at the later turn while a fresh `chat`
subtracts it all at the first, so the two can give the root different
totals. It has not been seen live: each of the 203 finished subagents on
record finished once, with one turn. A
subagent's own `updates.jsonl` never records a spawn, so a subagent's turns
are its own.

## REQUIREMENTS

- R-UGJD-UOYO: The package `internal/harness/grok` (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok`) MUST export the function `List(root fs.FS, home string) ([]session.Session, error)`, where `session` is `internal/session` (`D04-sessions-and-table`).
- R-AN0B-XFZ4: `List` MUST treat the index as the absolute path `path.Join("/", home, ".grok", "active_sessions.json")` and the sessions directory as `path.Join("/", home, ".grok", "sessions")`, and MUST name each of them, and every path below the sessions directory, to `root` as that absolute path without its leading `/` (for `home` `/home/dev`, the names `home/dev/.grok/active_sessions.json` and `home/dev/.grok/sessions`).
- R-AO88-B7PT: When reading the index through `root` fails with an error satisfying `errors.Is(err, fs.ErrNotExist)`, `List` MUST return a slice of length zero and a nil error.
- R-3CCD-3GI4: When reading the index through `root` fails with any error not satisfying `errors.Is(err, fs.ErrNotExist)`, `List` MUST return a nil slice and an error whose dynamic type is `*session.ReadError` and whose `Path` is the index's absolute path (for `home` `/home/dev`, `/home/dev/.grok/active_sessions.json`), its `Err` being as `R-H0PS-DNFS` (`D04-sessions-and-table`) requires.
- R-AQO1-2R77: When the index can be read but its content is not a JSON array every element of which is a JSON object (an empty file, a truncated array, `null`, an object, or an array holding a non-object all included), `List` MUST return a nil slice and an error whose dynamic type is `*session.ReadError`, whose `Path` is the index's absolute path, and whose `Err` is `session.ErrNotJSON` itself.
- R-ARVX-GIXW: `List` MUST return a nil error whenever the index can be read and is a JSON array of objects, whatever else fails to be read.
- R-3DK9-H88T: `List` MUST skip, without returning an error, an index entry whose `session_id` is absent or not a non-empty JSON string, or whose `pid` is absent or not a JSON number whose value is a positive integer that fits in `int`; every other entry is still considered.
- R-3ES5-UZZI: When an index entry's `opened_at` is a JSON string that parses with `time.Parse(time.RFC3339Nano, …)`, `List` MUST return a session for the entry if and only if `proc.Start(root, pid)` (`D05-process-facts`) returns a nil error and the time it returns is not after the parsed `opened_at`.
- R-AVJM-LU5Z: When an index entry's `opened_at` is absent, is not a JSON string, or does not parse with `time.Parse(time.RFC3339Nano, …)`, `List` MUST return a session for it if and only if `proc.Start(root, pid)` returns a nil error.
- R-AWRI-ZLWO: Each returned session MUST have `ID` equal to its index entry's `session_id`, and `List` MUST return one session per live index entry and no other session.
- R-AXZF-DDND: A returned session's `CWD` MUST be its index entry's `cwd` when that is a non-empty JSON string; otherwise it MUST be the path `proc.Cwd(root, pid)` returns, or the empty string when `proc.Cwd` returns an error.
- R-3H7Y-MJGW: A session's directory MUST be the entry named `<session_id>` inside the first direct subdirectory of the sessions directory that holds an entry of that name, found by listing the sessions directory and not by deriving a directory name from the session's working directory; A direct subdirectory is an entry of the sessions directory whose `fs.DirEntry` reports it is a directory (`IsDir` true; a symbolic link is not one), and subdirectories are taken in bytewise order of their names; a subdirectory that cannot be listed holds no session directory.
- R-B0F8-4X4R: When an index entry's `session_id` contains `/` or is `.` or `..`, `List` MUST NOT look for its session directory, and the session, when live, MUST be returned as one whose session directory is not found.
- R-B1N4-IOVG: Below the sessions directory, `List` MUST read no file other than `summary.json` and `events.jsonl` inside the session directories of the entries it considers; in particular it MUST NOT open, read, or stat any entry whose name ends in `.lock`.
- R-3IFV-0B7L: A returned session's `Title` MUST be the string `encoding/json` decodes from the top-level `generated_title` of the JSON object in its session directory's `summary.json` when that is a JSON string; it MUST be the empty string when no session directory is found, when `summary.json` cannot be read or is not a JSON object, or when its `generated_title` is absent or not a JSON string.
- R-B42X-A8CU: A returned session's `Status` MUST be `session.StatusUnknown` when no session directory is found for it or its `events.jsonl` does not exist or cannot be read.
- R-UYXC-XTBM: When a session's `events.jsonl` can be read, its `Status` MUST be `session.StatusIdle` when the last of its records (`R-HE4O-L4LF`, `D04-sessions-and-table`) has a top-level `type` equal to the JSON string `turn_ended`, `session.StatusIdle` when none of its records has a top-level `type` equal to the JSON string `turn_started` (a file holding no record included), and `session.StatusWorking` otherwise.
- R-V059-BL2B: A session's `LastActive` and `HasLastActive` MUST be set from the latest timestamp under the member name `ts` (`R-HHSD-QFTI`, with records per `R-HE4O-L4LF` and timestamps per `R-HFCK-YWC4`, `D04-sessions-and-table`) of the `events.jsonl` in its session directory, where an `events.jsonl` that does not exist, cannot be read, or has no session directory found for it counts as a log with no latest timestamp.
- R-3JNR-E2YA: A session's `events.jsonl` MUST be a log in the sense of `R-HE4O-L4LF` (`D04-sessions-and-table`).
- R-3KVN-RUOZ: Every non-nil error `List` returns MUST have dynamic type `*session.ReadError`, and whenever `List` returns a non-nil error it MUST return a nil slice.
- R-BBEB-KUT0: A session's `Title` MUST NOT depend on whether its `events.jsonl` exists or can be read, its `Status`, `LastActive`, and `HasLastActive` MUST NOT depend on whether its `summary.json` exists or can be read, and its `CWD` MUST NOT depend on either file.
- R-BCM7-YMJP: `List` MUST call no method of `root`, or of any value obtained from `root`, other than the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.ReadLinkFS`, `fs.File`, `fs.ReadDirFile`, `fs.DirEntry`, and `fs.FileInfo`; in particular it MUST NOT call a `Write`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`, `Chtimes`, or `Symlink` method on any of them.
- R-BDU4-CEAE: `List` MAY return its sessions in any order.
- R-P1AC-UJHA: The package `internal/harness/grok` MUST export the function `Tree(root fs.FS, home, id string) (tree.Tree, error)`, where `fs` is the standard library's `io/fs` and `tree` is `internal/tree` (`D09-tree`).
- R-TYJL-B57J: The places `internal/harness/grok` names as recording Grok's root sessions, in the sense of `R-JERO-H7QH` (`D09-tree`), MUST be exactly its locating directory and the *index*, the file `path.Join("/", home, ".grok", "active_sessions.json")`, where `home` is the `home` argument of `Tree`.
- R-TZRH-OWY8: `Tree` MUST name the index, and every path below its locating directory, to `root` as that absolute path without its leading `/` (for `home` `/home/dev`, the names `home/dev/.grok/active_sessions.json` and `home/dev/.grok/sessions/<encoded-cwd>/<session-id>/summary.json`).
- R-OXMN-P897: When `id` is the empty string, is `.` or `..`, or contains `/`, `Tree` MUST return `tree.ErrNotFound` unless `R-52XS-OVAY` (`D09-tree`) requires it to return a `*session.ReadError`, and MUST pass to `root` no name other than the locating directory's.
- R-U27A-GGFM: `Tree`'s locating directory MUST hold a root session with id `id` if and only if `id` is non-empty, contains no `/`, and is neither `.` nor `..`, a session directory for `id` is found as `R-3H7Y-MJGW` defines a session's directory (the sessions directory there being the locating directory), the `fs.DirEntry` of that entry in the listing of the subdirectory holding it reports `IsDir` true, and that session directory is not a subagent's session directory (`R-U3F6-U86B`).
- R-U3F6-U86B: A session directory MUST be a *subagent's session directory* if and only if its `summary.json` can be read through `root` and holds a JSON object whose top-level member `session_kind` is a JSON string beginning with `subagent` (so `subagent` and `subagent_resume` both are); a session directory whose `summary.json` does not exist, cannot be read, or is not a JSON object, or whose `session_kind` is absent, is not a JSON string, or does not begin with `subagent` (such as `headless`), MUST NOT be one.
- R-P4Y1-ZUPD: The index MUST hold a root session with id `id`, and that root session is *live*, if and only if `id` is non-empty, contains no `/`, and is neither `.` nor `..`, and the index can be read through `root`, is a JSON array every element of which is a JSON object, and has an element that `R-3DK9-H88T` does not skip, whose `session_id` equals `id`, and for which the condition under which `List` returns a session for an entry holds (`R-3ES5-UZZI` when its `opened_at` parses, `R-AVJM-LU5Z` otherwise); a root session held by the locating directory alone is not live; and the root's liveness *cannot be told* when reading the index through `root` fails with an error not satisfying `errors.Is(err, fs.ErrNotExist)` or the index can be read but is not a JSON array every element of which is a JSON object.
- R-P02G-GRQL: The `Root` of the `tree.Tree` `Tree` returns with a nil error MUST have `ID` equal to `id`, an empty `Parent`, the zero `Started`, `HasStarted` false, and `Label` equal to the `Title` `R-3IFV-0B7L` gives a session whose session directory is the one found for `id` — the empty string when no session directory is found for `id` or its `summary.json` gives no title — whether or not the root session is live.
- R-8ZUD-GIUS: The root's `Status` MUST be `tree.StatusUnknown` when the root's liveness cannot be told (`R-P4Y1-ZUPD`); otherwise it MUST be `tree.StatusEnded` when the root session is not live, a missing index included, and, when it is live, the `tree.Status` whose string equals that of the `session.Status` that `R-B42X-A8CU` and `R-UYXC-XTBM` give a session whose session directory is the one found for `id`: `tree.StatusUnknown` when no session directory is found for `id` or its `events.jsonl` does not exist or cannot be read, and `tree.StatusIdle` or `tree.StatusWorking` by the rule of `R-UYXC-XTBM` otherwise.
- R-U8AS-DB53: The `Subagents` of the `tree.Tree` `Tree` returns with a nil error MUST hold, for each entry `fs.ReadDir` returns for the directory `subagents` inside the session directory found for `id`, whatever the entry's type, exactly one element whose `ID` is that entry's name, and no other element; `Subagents` MUST have length zero when no session directory is found for `id`, or its `subagents` directory does not exist or cannot be listed.
- R-U9IO-R2VS: A subagent's *meta file* MUST be `meta.json` inside the entry of the `subagents` directory named by its `ID`, and its `Label` MUST be the string `encoding/json` decodes from the top-level `description` of the JSON object in its meta file when that is a JSON string, and the empty string when the meta file does not exist, cannot be read, or is not a JSON object, or its `description` is absent or not a JSON string.
- R-UAQL-4UMH: When the JSON object in a subagent's meta file has a top-level `started_at` that is a JSON string `s` for which `time.Parse(time.RFC3339Nano, s)` returns a time `t` and a nil error, `Tree` MUST set the subagent's `Started` to a time for which `time.Time.Compare` with `t` returns 0 and its `HasStarted` to true; when the meta file does not exist, cannot be read, or is not a JSON object, or its `started_at` is absent, is not a JSON string, or does not so parse, `Tree` MUST set `HasStarted` to false.
- R-O2GJ-U46Q: A subagent's `Status` MUST be taken from the top-level `status` of the JSON object in its meta file: `tree.StatusDone` for the JSON string `completed`, `tree.StatusFailed` for `failed`, `tree.StatusKilled` for `cancelled`, and, for `running`, `tree.StatusWorking` when the root session is live (`R-P4Y1-ZUPD`) and `tree.StatusUnknown` when it is not live or its liveness cannot be told (`R-P4Y1-ZUPD`); it MUST be `tree.StatusUnknown` for any other value, when `status` is absent or not a JSON string, and when the meta file does not exist, cannot be read, or is not a JSON object.
- R-UD6D-WE3V: A *spawn record* of a subagent whose `ID` is `a` MUST be a record of the `updates.jsonl` in the session directory found for `id` whose top-level `params` is a JSON object whose member `update` is a JSON object whose member `sessionUpdate` is the JSON string `subagent_spawned` and whose member `subagent_id` is the JSON string `a`; the subagent's *spawn prompt* and *child session* MUST be the members `parent_prompt_id` and `child_session_id` of `params.update` of the last of its spawn records in that file, each only when it is a non-empty JSON string, so that a subagent with no spawn record, or whose last spawn record lacks one of them, has not that one.
- R-UFM6-NXL9: The *prompt ids* of a session directory MUST be the JSON string values of `params._meta.promptId` — the member `promptId` of the JSON object that is the member `_meta` of the JSON object that is the top-level `params` — of the records of the `updates.jsonl` in that session directory; a session directory whose `updates.jsonl` does not exist or cannot be read has no prompt ids.
- R-W2G6-CN1A: A subagent's `Parent` MUST be the `ID` `b` of another element of `Subagents` when the subagent has a spawn prompt `p`, `p` is not among the prompt ids of the session directory found for `id`, and `b` is the only element of `Subagents`, the subagent itself excluded, that has a child session `c` that is non-empty, contains no `/`, is neither `.` nor `..`, and has a session directory, found as `R-3H7Y-MJGW` defines one, whose prompt ids include `p`; it MUST be the empty string otherwise — when the root's `updates.jsonl` does not exist or cannot be read, when the subagent has no spawn prompt, when `p` is among the root's prompt ids, and when no such `b`, or more than one, exists — and the `parent_session_id` of its meta file MUST NOT affect it.
- R-P2I9-8B7Z: For `Tree`, every `events.jsonl` and `updates.jsonl` inside a session directory MUST be a log in the sense of `R-3564-LW25` and `R-BC89-8UF3` (`D04-sessions-and-table`).
- R-P3Q5-M2YO: `Tree` MUST read the content of the index, of each `summary.json`, and of each `meta.json` it reads with exactly one `fs.ReadFile` call through `root`, MUST NOT otherwise open or read that file within the call, and MUST take what it derives from it from `json.Unmarshal` of that whole content as one JSON value; a file is *not a JSON object* (or, for the index, not a JSON array every element of which is a JSON object, as `R-AQO1-2R77` lists) when `json.Unmarshal` rejects its content or the value it decodes is of another kind, `null` included, which is the sense `R-3IFV-0B7L`, `R-U3F6-U86B`, `R-P4Y1-ZUPD`, `R-U9IO-R2VS`, `R-UAQL-4UMH`, and `R-O2GJ-U46Q` use.
- R-W4VZ-46IO: Below its locating directory, `Tree` MUST list no directory other than the locating directory, its direct subdirectories, and the `subagents` directory inside the session directory found for `id`, and MUST open or read no file other than `summary.json`, `events.jsonl`, and `updates.jsonl` in the session directory found for `id`, `meta.json` in each entry of that `subagents` directory, and `updates.jsonl` in the session directory found for a subagent's child session; in particular it MUST NOT open, read, or stat any entry whose name ends in `.lock`.
- R-W63V-HY9D: `Tree` MUST call no method of `root`, or of any value obtained from `root`, other than the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.ReadLinkFS`, `fs.File`, `fs.ReadDirFile`, `fs.DirEntry`, `fs.FileInfo`, and `io.ReaderAt`; in particular it MUST NOT call a `Write`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`, `Chtimes`, or `Symlink` method on any of them.
- R-OYUK-2ZZW: Whenever `Tree` returns a non-nil error it MUST return the zero `tree.Tree`.
- R-UEEA-A5UK: `Tree` MAY return the elements of `Subagents` in any order.
- R-PN5X-UWUN: For `Chat(root, home, sessionID, agentID)` of `internal/harness/grok` with `agentID` byte-for-byte equal to `sessionID`, the agent is the *root*, its *own session id* MUST be `sessionID`, and its *transcript* MUST be the file `updates.jsonl` inside the session directory found for `sessionID` as `R-3H7Y-MJGW` defines a session's directory, the sessions directory there being `path.Join("/", home, ".grok", "sessions")`; the root MUST have no transcript file when no session directory is found for `sessionID`.
- R-PODU-8OLC: For `Chat(root, home, sessionID, agentID)` of `internal/harness/grok` with `agentID` equal to the `ID` of a drawn subagent (`D09-tree`) and not to `sessionID`, the agent is a *subagent*, its *own session id* MUST be the string `encoding/json` decodes from the top-level `child_session_id` of the JSON object in its meta file (`R-U9IO-R2VS`), and its *transcript* MUST be the file `updates.jsonl` inside the session directory found for that id as `R-3H7Y-MJGW` defines a session's directory; the subagent MUST have no transcript file when its meta file does not exist, cannot be read, or is not a JSON object, when that `child_session_id` is absent, is not a JSON string, is empty, contains `/`, or is `.` or `..`, and when no session directory is found for it.
- R-PPLQ-MGC1: The `path` with which `Chat` of `internal/harness/grok` calls `chat.NewTranscript` MUST be the absolute path of the agent's transcript (for `home` `/home/dev`, `/home/dev/.grok/sessions/<encoded-cwd>/<own session id>/updates.jsonl`) and the empty string when the agent has no transcript file, and `Chat` and every `Decode` call of the decoders it hands to `chat.NewTranscript` MUST name each path below `path.Join("/", home, ".grok")` to the filesystem they are given as that absolute path without its leading `/`.
- R-PQTN-082Q: The `chat.Recorded` with which `Chat` of `internal/harness/grok` calls `chat.NewTranscript` MUST have all six fields true.
- R-EODI-QJ1P: For `Chat` of `internal/harness/grok` and for every `Decode` call of the decoders it hands to `chat.NewTranscript`, every `updates.jsonl` inside a session directory MUST be a log in the sense of `R-5HSP-OSAY` and `R-GTCN-9NFH` (`D10-chat`); in the sense of `R-GTCN-9NFH`, the design of `internal/harness/grok` MUST name as recording a subagent's usage only the `updates.jsonl` of a subtracted child, read only through the `session.Log` the decoder keeps for it under `R-DLG6-8HL8`, as the directories a decoder may list or stat to find those files only `path.Join("/", home, ".grok", "sessions")` and its direct subdirectories, and as the only record that needs them an own record of kind `turn_completed` (`R-PT9F-RRK4`) for which `R-DLG6-8HL8` gives at least one subtracted child.
- R-PT9F-RRK4: For the decoder that `Chat` of `internal/harness/grok` hands to `chat.NewTranscript` for an agent whose own session id is `s`, a record's *update* MUST be the member `update` of its top-level `params` and its *kind* the JSON string value of the update's member `sessionUpdate`; a record MUST be *copied* when its top-level `params` is a JSON object whose member `_meta` is a JSON object whose member `eventId` is a JSON string that does not begin with `s` followed by `-`, and *own* otherwise; `Decode` of a copied record MUST return no entry and the zero `chat.Usage`, and MUST leave the decoder's later results as they would be had that `Decode` call not been made.
- R-PUHC-5JAT: Every entry the decoder of `internal/harness/grok` returns for a record MUST have `HasTime` true and a `Time` for which `time.Time.Compare` with `time.Unix(n, 0)` returns 0 when the record's top-level `timestamp` is a JSON number whose literal `strconv.ParseInt(literal, 10, 64)` parses as `n` with a nil error, and `HasTime` false otherwise.
- R-PVP8-JB1I: `Decode` by the decoder of `internal/harness/grok` of an own record whose kind is `user_message_chunk`, `agent_message_chunk`, or `agent_thought_chunk` and whose update's member `content` is a JSON object with a member `text` that is a JSON string MUST return exactly one entry, with `Text` the string `encoding/json` decodes from that `text`, unchanged, an empty `Tool`, and `Kind` `chat.KindAssistant` for `agent_message_chunk`, `chat.KindReasoning` for `agent_thought_chunk`, and, for `user_message_chunk`, `chat.KindUser` when the agent is the root and `chat.KindAgent` when it is a subagent; except that it MUST return no entry for a `user_message_chunk` record whose update's member `_meta` is a JSON object whose member `hideFromScrollback` is JSON `true`, or whose `content` or `text` is not so; the text of one record MUST NOT be joined to that of another.
- R-PWX4-X2S7: `Decode` by the decoder of `internal/harness/grok` of an own record whose kind is `tool_call` MUST return exactly one entry, with `Kind` `chat.KindTool`, `Text` the bytes of the JSON value of the update's member `rawInput` exactly as they appear in the record (the empty string when that member is absent), and `Tool` the string `encoding/json` decodes from the member `name` of the JSON object that is the member `x.ai/tool` of the JSON object that is the update's member `_meta` when that is a non-empty JSON string, and otherwise from the update's member `title` when that is a JSON string, and the empty string otherwise.
- R-PY51-AUIW: `Decode` by the decoder of `internal/harness/grok` of an own record whose kind is `tool_call_update` MUST return exactly one entry, with an empty `Text` and `Tool`, when the update's member `status` is the JSON string `failed` or `completed`, and no entry otherwise; its `Kind` MUST be `chat.KindResultError` for `failed`, and for `completed` `chat.KindResultError` when the update's member `rawOutput` is a JSON object whose member `type` is the JSON string `Bash` and whose member `exit_code` is a JSON number whose value is not 0, and `chat.KindResultOK` otherwise.
- R-PZCX-OM9L: When the decoder of `internal/harness/grok` decodes a record, a subagent id `a` MUST be *direct* if and only if among the own records that decoder decoded before it there is one of kind `subagent_spawned` whose update's member `subagent_id` is the JSON string `a`, and the update's member `parent_prompt_id` of the last such record is a JSON string equal to the JSON string value of `params._meta.promptId` of at least one own record that decoder decoded before the record being decoded.
- R-GQ9H-3SFD: `Decode` by the decoder of `internal/harness/grok` of an own record whose kind is `subagent_finished` MUST return exactly one entry, with `Kind` `chat.KindAgent`, an empty `Tool`, and `Text` the string `encoding/json` decodes from the update's member `output` when that is a JSON string, otherwise from its member `error` when that is a JSON string (a failed subagent's finish record carries `error` and no `output`), and the empty string otherwise, when the update's member `subagent_id` is a JSON string naming a direct subagent (`R-PZCX-OM9L`), and no entry otherwise.
- R-GV52-MVE5: `Decode` by the decoder of `internal/harness/grok` of a record MUST return no entry other than those `R-PVP8-JB1I`, `R-PWX4-X2S7`, `R-PY51-AUIW`, and `R-GQ9H-3SFD` require, so that a record whose kind is `turn_completed`, `subagent_spawned`, `hook_execution`, `retry_state`, `plan`, or any other kind, or that has no kind, makes no entry; and it MUST return the zero `chat.Usage` for every record that is not an own record of kind `turn_completed`.
- R-Q48J-7P8D: The *turn usage* of a record of kind `turn_completed` MUST be the `chat.Usage` with `In` equal to `I - R - W`, `CacheWrite` equal to `W`, `CacheRead` equal to `R`, `Out` equal to `O`, `Reasoning` equal to `G`, and `Calls` equal to `M`, where `I`, `R`, `W`, `O`, `G`, and `M` are the values of the members `inputTokens`, `cachedReadTokens`, `cacheCreationTokens`, `outputTokens`, `reasoningTokens`, and `modelCalls` of the JSON object that is the update's member `usage`, each being 0 when `usage` is absent or not a JSON object, or the member is absent or not a JSON number whose literal `strconv.ParseInt(literal, 10, 64)` parses with a nil error.
- R-DMO2-M9BX: `Decode` by the decoder of `internal/harness/grok` of an own record of kind `turn_completed` MUST return, for each of the six fields, that field of the record's turn usage minus the sum, over every subtracted child `c` of the record (`R-DLG6-8HL8`), of that field of `c`'s current total after this call's pass (`R-DLG6-8HL8`) minus the sum of what the same decoder already subtracted for `c` at the own `turn_completed` records it decoded before this one; the `subagent_finished` records counted for a `turn_completed` record MUST be the own records of kind `subagent_finished` that the same decoder decoded after the latest own record of kind `turn_completed` it decoded before this one (or, when there is none, after it was made) and whose `subagent_id` named a direct subagent (`R-PZCX-OM9L`) when that `subagent_finished` record was decoded; so that two finishes of one child in a turn subtract its usage once, and a later finish of a child subtracts only the usage it recorded since the last subtraction.
- R-DLG6-8HL8: A *subtracted child* of an own record of kind `turn_completed` MUST be each distinct string `c` that is the JSON string value of the update's member `child_session_id` of an own record of kind `subagent_finished` counted by `R-DMO2-M9BX` for that `turn_completed` record, where `c` is non-empty, contains no `/`, is neither `.` nor `..`, and is not the decoder's own session id; the decoder MUST keep, for each subtracted child `c` for the rest of its life, one `session.Log` for the `updates.jsonl` in the session directory found for `c` as `R-3H7Y-MJGW` defines a session's directory, a zero-value `Log` when first made, and a *current total*, the zero `chat.Usage` when first made; within the `Decode` call of each `turn_completed` record of which `c` is a subtracted child the decoder MUST make exactly one pass of that `Log` and add to the current total, field by field, the turn usage of every record of kind `turn_completed` among the lines that pass returns that is own for the own session id `c` in the sense of `R-PT9F-RRK4`, setting the current total back to the zero `chat.Usage` before adding when that pass resets; when no session directory is found for `c`, or the pass returns an error, the current total MUST stay as it was; a `subagent_finished` record whose `child_session_id` is not so MUST cause no directory to be listed or stat'ed and no file to be opened or read.
- R-DP3V-DSTB: Below `path.Join("/", home, ".grok", "sessions")`, `Chat` of `internal/harness/grok` MUST list no directory other than that directory, its direct subdirectories, and the `subagents` directory inside the session directory found for `sessionID`, and MUST open or read no file other than `summary.json` in the session directories it considers as `R-U27A-GGFM` and `R-U3F6-U86B` do, the meta file of the named subagent, and the agent's transcript; a `Decode` call of the decoders it hands to `chat.NewTranscript` MUST list no directory other than that directory and its direct subdirectories and MUST open or read no file other than those `R-DLG6-8HL8` names; `Chat` MUST read the index, each `summary.json`, and the meta file each with exactly one `fs.ReadFile` call as `R-P3Q5-M2YO` states for `Tree`; and `Chat` and its `Decode` calls MUST NOT open, read, or stat any entry whose name ends in `.lock`, and MUST NOT call a method outside those `R-W63V-HY9D` allows `Tree`.
