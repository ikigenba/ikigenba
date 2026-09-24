# D08-grok

How `list grok` finds the live root sessions of the Grok Build CLI, and how
`tree grok` draws one root session with its subagents. The
package `internal/harness/grok` turns what Grok leaves under `$HOME` into
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
to `tree.Draw` (`D09-tree`). `D09` already fixes the errors: the locating
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
(`generated_title`, which `tree.Draw` replaces by the id when empty), and the
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
