# D06-claude

How `list claude` finds the live root sessions of Claude Code, and how
`tree claude` draws one session and its subagents. The package
`internal/harness/claude` turns what Claude Code leaves under `$HOME` into
`session.Session` values (`D04-sessions-and-table`) and `tree.Tree` values
(`D09-tree`), using the process facts of `D05-process-facts` to decide which
registrations are still live. The command (`internal/cli`) calls `List` with
the machine's filesystem and the home directory and prints `session.Table` of
the result, or calls `Tree` with the same two and a session id and prints
`tree.Draw` of the result; it turns a returned `*session.ReadError` into the
`cannot read` diagnostic and `tree.ErrNotFound` into the not-found one.

Claude Code registers every running root session in a file of its own under
`~/.claude/sessions`, named after the process id and holding a JSON object
with the session id, pid, working directory, title (`name`), status (`busy` or
`idle`), and the moment the session started (`startedAt`, milliseconds since
the Unix epoch). Beside each registration sits a `*.key` file holding a
secret; this package never touches one. Subagents are never registered, so
every live registration is a root.

A registration is live when its process is running and did not start after
the session did; a later start means the pid was reused. Claude Code records
the process's own start (`procStart`, the kernel's start tick count as a
decimal string), which allows an exact comparison with `proc.StartTicks`; a
registration without it falls back to comparing `proc.Start` with
`startedAt`, and one with neither is live whenever its process runs. A registration that
is unreadable, malformed, or stale is dropped without a word. Only the
registry directory itself failing to list is an error: every other failure
makes one field fall back on its own.

The session's transcript lives in a directory under `~/.claude/projects` whose
name encodes the working directory. That encoding is Claude Code's business,
so the transcript is found by looking in every project directory for a file
named after the session id, never by computing the encoded name. LAST ACTIVE is
the latest `timestamp` among the transcript's records, under the record,
timestamp, and latest-timestamp definitions `D04-sessions-and-table` shares
with every harness, which also make a half-written last line count for
nothing.

`List` only reads. It reaches the machine solely through the `fs.FS` it is
given, and it can neither write nor lock anything.

## The tree of one session

`Tree` accepts any root session, live or ended. Two places record one: the
projects directory (the locating directory of `D09-tree`), where a direct
subdirectory holding `<id>.jsonl` is the session's transcript, and the
registry, where a live registration names it even before its transcript is
written; a registration whose process is gone records nothing, as a Codex
lock or a Grok index entry does not. Subagents never appear in either: their files live only in the
session's own `subagents/` directory, so an agent id is not found. An id that
is empty, `.`, `..`, or holds a `/` names nothing, so it cannot escape the
directory it is joined to.

The root's status and title follow `list`. A session with a live
registration takes its status from it; one with no live registration is
`ended`; one whose registry cannot be listed cannot be told live or not and
is `unknown`, and its subagents are judged as those of an ended session. The
title is the live registration's `name` when it is non-empty, and otherwise
the transcript's latest non-empty `customTitle`, then its latest non-empty
`aiTitle`.

Every subagent, at any depth, sits flat in
`<project>/<id>/subagents/` as `agent-<agentId>.jsonl` and
`agent-<agentId>.meta.json`; either file is enough for it to be drawn. The
meta file gives the label (`description`), the parent (`parentAgentId`,
absent for a subagent the session started), and, for a subagent its parent
waited on, the shape of the request and the id of the tool call that started
it. A subagent's start moment is the first timestamp in its own transcript,
never anything its parent recorded, so a subagent whose meta file is lost
still sorts among its siblings.

A subagent's status comes from notifications. When a background subagent
stops, Claude Code queues a `<task-notification>` naming the agent id and a
status of `completed`, `failed`, or `killed`. On this machine the session's
own transcript records that queueing, as a `queue-operation` `enqueue`
record, for subagents at every depth, at the moment the subagent stopped;
the parent subagent's transcript receives the same text later, as a user
message or a queued-command attachment, and a subagent that outlives its
parent is notified only in the session's transcript. So the session's
transcript is read first, and the starter's transcript only when the
session's holds nothing for that agent. Within the session's transcript only
the `enqueue` records count: the delivered copies arrive later and would put
an old notification after a newer `SendMessage`. A subagent the parent waited
on gets no notification; its result is the `tool_result` of the tool call
that started it, which a background subagent also receives at once (as an
`async_launched` result) and which is therefore counted only for a
`foreground` request. A `SendMessage` addressed to the agent that is later
than every result recorded for it sends it back to work. Because the two
transcripts are written by different processes, file order is compared only
within one of them; across the two, results and messages are compared by
their `timestamp`, and a record without one is left out of the comparison. The subagent's own transcript never decides
its status: it can end cleanly on a failure.

`Tree`, like `List`, only reads. It opens only the files named here, reads
each log in one pass of a fresh `session.Log`, reads each meta file and
registration whole with `fs.ReadFile` (each is one small JSON object), takes
no lock, and writes nothing.

## REQUIREMENTS

- R-U803-6ART: The package `internal/harness/claude` (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude`) MUST export the function `List(root fs.FS, home string) ([]session.Session, error)`, where `session` is `internal/session` (`D04-sessions-and-table`).
- R-9RAX-ZEP2: `List` MUST treat the registry directory as the absolute path `path.Join("/", home, ".claude", "sessions")` and the projects directory as `path.Join("/", home, ".claude", "projects")`, and MUST name each of them, and every path below them, to `root` as that absolute path without its leading `/` (for `home` `/home/dev`, the names `home/dev/.claude/sessions` and `home/dev/.claude/projects`).
- R-9SIU-D6FR: When listing the registry directory through `root` fails with an error satisfying `errors.Is(err, fs.ErrNotExist)`, `List` MUST return a slice of length zero and a nil error.
- R-32L6-1AKK: When listing the registry directory through `root` fails with any error not satisfying `errors.Is(err, fs.ErrNotExist)`, `List` MUST return a nil slice and an error whose dynamic type is `*session.ReadError` and whose `Path` is the registry directory's absolute path (for `home` `/home/dev`, `/home/dev/.claude/sessions`), its `Err` being as `R-H0PS-DNFS` (`D04-sessions-and-table`) requires.
- R-9UYN-4PX5: `List` MUST return a nil error whenever the registry directory can be listed, whatever else fails to be read.
- R-9W6J-IHNU: Within the registry directory, `List` MUST pass to `root` only the directory's own name and the names of its entries whose name ends in `.json`; in particular it MUST NOT open, read, stat, or list any entry whose name ends in `.key`.
- R-33T2-F2B9: A registration is the content of an entry of the registry directory whose name ends in `.json`. `List` MUST skip, without returning an error, a registration that cannot be read, that is not a JSON object, whose `sessionId` is absent or not a non-empty JSON string, or whose `pid` is absent or not a JSON number whose value is a positive integer that fits in `int`; every other registration is still considered.
- R-UU1R-EQCU: When a registration's `procStart` is a JSON string of one or more ASCII decimal digits whose value fits in a `uint64`, `List` MUST return a session for it if and only if `proc.StartTicks(root, pid)` (`D05-process-facts`) returns a nil error and a tick count not greater than that value.
- R-350Y-SU1Y: When a registration's `procStart` is absent or is not a JSON string of one or more ASCII decimal digits whose value fits in a `uint64`, and its `startedAt` is a JSON number whose value is an integer that fits in `int64`, `List` MUST return a session for it if and only if `proc.Start(root, pid)` (`D05-process-facts`) returns a nil error and the time it returns is not after `startedAt` read as milliseconds since the Unix epoch.
- R-368V-6LSN: When a registration has neither a `procStart` that is a JSON string of one or more ASCII decimal digits whose value fits in a `uint64` nor a `startedAt` that is a JSON number whose value is an integer that fits in `int64`, `List` MUST return a session for it if and only if `proc.StartTicks(root, pid)` returns a nil error.
- R-A125-1KMM: Each returned session MUST have `ID` equal to its registration's `sessionId`, and `List` MUST return one session per live registration and no other session.
- R-A3HX-T440: A returned session's `Status` MUST be `session.StatusWorking` when its registration's `status` is the JSON string `busy`, `session.StatusIdle` when it is the JSON string `idle`, and `session.StatusUnknown` otherwise, absent included.
- R-37GR-KDJC: A returned session's `Title` MUST be the string `encoding/json` decodes from its registration's `name` when that is a JSON string, apostrophes included, and the empty string otherwise.
- R-A5XQ-KNLE: A returned session's `CWD` MUST be its registration's `cwd` when that is a non-empty JSON string; otherwise it MUST be the path `proc.Cwd(root, pid)` returns, or the empty string when `proc.Cwd` returns an error.
- R-38ON-Y5A1: A session's transcript MUST be the entry named `<sessionId>.jsonl` inside the first direct subdirectory of the projects directory that holds an entry of that name, found by listing the projects directory and not by deriving a directory name from the session's working directory; A direct subdirectory is an entry of the projects directory whose `fs.DirEntry` reports it is a directory (`IsDir` true; a symbolic link is not one), and subdirectories are taken in bytewise order of their names; a subdirectory that cannot be listed or read holds no transcript.
- R-TVR7-PUBW: Below the projects directory, `List` MAY list (as a directory) the projects directory itself and its direct subdirectories, as the transcript search requires, and MUST NOT otherwise open, read, stat, or list anything there: the only non-directory files below the projects directory it opens or reads MUST be the transcripts of the registrations it considers, and it MUST NOT list, open, read, or stat any name below a direct subdirectory of the projects directory other than such a transcript.
- R-A9LF-PYTH: When a registration's `sessionId` contains `/` or is `.` or `..`, `List` MUST NOT look for its transcript, and the session MUST be returned with `HasLastActive` false when it is live.
- R-UXPG-K1KX: A session's `LastActive` and `HasLastActive` MUST be set from the latest timestamp under the member name `timestamp` (`R-HHSD-QFTI`, with records per `R-HE4O-L4LF` and timestamps per `R-HFCK-YWC4`, `D04-sessions-and-table`) of its transcript, where a transcript that is not found (the projects directory missing or unlistable included) or cannot be read counts as a log with no latest timestamp.
- R-39WK-BX0Q: A session's transcript MUST be a log in the sense of `R-HE4O-L4LF` (`D04-sessions-and-table`).
- R-3B4G-PORF: Every non-nil error `List` returns MUST have dynamic type `*session.ReadError`, and whenever `List` returns a non-nil error it MUST return a nil slice.
- R-AEH1-91S9: A session's `Status`, `CWD`, and `Title` MUST NOT depend on whether its transcript exists, can be read, or holds any record, and its `LastActive` and `HasLastActive` MUST NOT depend on its registration's `status`, `cwd`, or `name`.
- R-AFOX-MTIY: `List` MUST call no method of `root`, or of any value obtained from `root`, other than the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.ReadLinkFS`, `fs.File`, `fs.ReadDirFile`, `fs.DirEntry`, and `fs.FileInfo`; in particular it MUST NOT call a `Write`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`, `Chtimes`, or `Symlink` method on any of them.
- R-AGWU-0L9N: `List` MAY return its sessions in any order.
- R-0YC9-NT3U: The package `internal/harness/claude` MUST export the function `Tree(root fs.FS, home, id string) (tree.Tree, error)`, where `tree` is `internal/tree` (`D09-tree`) and `fs` is the standard library's `io/fs`.
- R-6AJ7-HMZN: When `id` is the empty string, is `.` or `..`, or contains `/`, `Tree` MUST return `tree.ErrNotFound` unless `R-52XS-OVAY` (`D09-tree`) requires it to return a `*session.ReadError`, and MUST pass to `root` no name other than the locating directory's.
- R-10S2-FCL8: `Tree` MUST treat the registry directory as the absolute path `path.Join("/", home, ".claude", "sessions")`, and MUST name it, and every path below it or below the locating directory (`R-51PW-B3K9`, `D09-tree`), to `root` as that absolute path without its leading `/` (for `home` `/home/dev`, the name `home/dev/.claude/sessions`).
- R-6BR3-VEQC: The places that record Claude Code's root sessions, in the sense of `R-JERO-H7QH` (`D09-tree`), MUST be exactly the locating directory and the registry directory, and neither holds a root session whose id is the empty string, `.`, `..`, or contains `/`; for any other id `x`, the locating directory holds a root session with id `x` if and only if, for some direct subdirectory `d` of it (an entry whose `fs.DirEntry` reports `IsDir` true, as in `R-38ON-Y5A1`), `fs.Stat` through `root` of `d`'s entry named `x + ".jsonl"` returns a nil error and an `fs.FileInfo` whose `IsDir` is false, and the registry directory holds one if and only if the root session with id `x` is live (`R-6FET-0PYF`), so that it holds none while that root is not live or its liveness cannot be told; no other place, and in particular no file below any directory named `subagents`, records a root session, so a registered session that is not live and has no transcript is not found.
- R-137V-6W2M: For `home` `/home/dev`, a locating directory holding the files `-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93.jsonl`, `-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a1c4e7f09b2d38561.jsonl`, and `-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a1c4e7f09b2d38561.meta.json`, and a registry directory with no registration whose `sessionId` is `a1c4e7f09b2d38561`, `Tree` given the id `a1c4e7f09b2d38561` MUST return `tree.ErrNotFound`.
- R-6E6W-MY7Q: The session's *project directory* MUST be the first, in bytewise order of names, of the direct subdirectories `d` of the locating directory that hold the root session with id `id` as `R-6BR3-VEQC` states; its *root transcript* is that directory's entry `id + ".jsonl"` and its *subagents directory* is `path.Join(d, id, "subagents")`; a session no direct subdirectory holds has no project directory, no root transcript, and no subagents directory. The root transcript and every entry of the subagents directory whose name begins with `agent-` and ends with `.jsonl` are the logs of `Tree`, in the sense of `R-3564-LW25` and `R-BC89-8UF3` (`D04-sessions-and-table`).
- R-6FET-0PYF: The root session MUST be *live* when the registry directory can be listed through `root` and holds a registration that `R-33T2-F2B9` does not skip (a registration that is *not a JSON object* in the sense of `R-6RLS-UFDD` being skipped), whose `sessionId` is `id`, and for which the condition under which `R-UU1R-EQCU`, `R-350Y-SU1Y`, or `R-368V-6LSN` (whichever applies to that registration) requires `List` to return a session holds; its liveness *cannot be told* when listing the registry directory through `root` fails with an error `err` for which `errors.Is(err, fs.ErrNotExist)` is false; and it is *not live* otherwise, a registry directory that does not exist included. Its *live registration* is the first, in bytewise order of entry names, of the registrations that make it live.
- R-183G-PZ1E: When `Tree` returns a nil error, the `Root` of the `tree.Tree` it returns MUST have `ID` equal to `id`, an empty `Parent`, the zero `Started`, and `HasStarted` false.
- R-6GMP-EHP4: `Root.Status` MUST be `tree.StatusWorking` when the root session is live and its live registration's `status` is the JSON string `busy`, `tree.StatusIdle` when it is live and that `status` is the JSON string `idle`, `tree.StatusUnknown` when it is live and that `status` is anything else (absent included), `tree.StatusEnded` when it is not live, and `tree.StatusUnknown` when its liveness cannot be told, whatever the root transcript holds and whether or not it can be read.
- R-1AJ9-HIIS: `Root.Label` MUST be the string `encoding/json` decodes from the live registration's `name` when the root session is live and that `name` is a JSON string other than the empty string, and the session's transcript title otherwise.
- R-1BR5-VA9H: The session's *transcript title* MUST be the string decoded from the top-level `customTitle` of the last record, in file order, of the root transcript whose top-level `type` is the JSON string `custom-title` and whose `customTitle` is a JSON string other than the empty string; when the root transcript holds no such record, the string decoded from the top-level `aiTitle` of the last record whose `type` is the JSON string `ai-title` and whose `aiTitle` is a JSON string other than the empty string; and the empty string when it holds neither, when the session has no root transcript, or when the pass over the root transcript returns an error. So a transcript holding `{"type":"custom-title","customTitle":"invoice export"}` and, after it, `{"type":"ai-title","aiTitle":"Fix the flaky invoice export"}` has the title `invoice export`, and one whose last `custom-title` record has an empty `customTitle` has the `customTitle` of the last earlier `custom-title` record whose `customTitle` is non-empty.
- R-1CZ2-9206: The `Subagents` of the `tree.Tree` that `Tree` returns MUST hold exactly one node for each distinct non-empty string `a` for which listing the subagents directory through `root` yields an entry named `"agent-" + a + ".meta.json"` or `"agent-" + a + ".jsonl"`, that node having `ID` `a`, and no other node; when the session has no subagents directory or listing it fails, `Subagents` MUST have length zero. So a background shell command, which has neither file, is never a node.
- R-6HUL-S9FT: The *meta file* of a subagent `a` MUST be the subagents directory's entry `"agent-" + a + ".meta.json"`, and it is *readable* when `fs.ReadFile` through `root` returns a nil error and content that is a JSON object (not *not a JSON object* in the sense of `R-6RLS-UFDD`); a meta file that does not exist, cannot be read, or is not a JSON object is *unreadable*, and no value of a subagent is taken from an unreadable meta file.
- R-1FEV-0LHK: A subagent's `Label` MUST be the string `encoding/json` decodes from its readable meta file's top-level `description` when that is a JSON string, and the empty string otherwise, its meta file being unreadable included.
- R-6J2I-616I: A subagent `a` whose meta file is readable and has no top-level `parentAgentId` MUST have an empty `Parent` and the root transcript as its *starter transcript*; one whose meta file is readable and whose `parentAgentId` is a JSON string `p` that is not empty, is neither `.` nor `..`, contains no `/`, and differs from `a` MUST have `Parent` `p` and the subagents directory's entry `"agent-" + p + ".jsonl"` as its starter transcript; and every other subagent — its meta file unreadable, or its `parentAgentId` present but not such a string — MUST have an empty `Parent` and no starter transcript, and `Tree` MUST pass to `root` no name built from such a `parentAgentId`.
- R-1HUN-S4YY: A subagent `a`'s `Started` MUST be set from the timestamp under the member name `timestamp` (`R-37LX-DFJJ`) of the first record, in file order, of its own transcript `"agent-" + a + ".jsonl"` that has a timestamp under that name, as `R-38TT-R7A8` states, and its start moment MUST be left unknown when that transcript does not exist, its pass returns an error, or none of its records has such a timestamp; no other file, its meta file and every other log included, affects a subagent's `Started` or `HasStarted`.
- R-1J2K-5WPN: A *notification* MUST be either a record of the root transcript whose top-level `type` is the JSON string `queue-operation`, whose `operation` is the JSON string `enqueue`, and whose `content` is a JSON string beginning with `<task-notification>`; or a record of a log in the subagents directory whose top-level `type` is the JSON string `user`, whose `origin` is a JSON object whose `kind` is the JSON string `task-notification`, and whose `message` is a JSON object whose `content` is a JSON string containing `<task-notification>`; or a record of a log in the subagents directory whose top-level `type` is the JSON string `attachment` and whose `attachment` is a JSON object whose `commandMode` is the JSON string `task-notification` and whose `prompt` is a JSON string containing `<task-notification>`. No other record — in particular no `user` or `attachment` record of the root transcript and no `queue-operation` record whose `operation` is not `enqueue` — is a notification.
- R-1KAG-JOGC: The *agent id* of a notification MUST be the text between the first `<task-id>` that follows the first `<task-notification>` of its decoded string and the first `</task-id>` after that `<task-id>`, and its *status word* the text between the first `<status>` that follows that `<task-notification>` and the first `</status>` after that `<status>`; a notification lacking either is a notification of no subagent. So the string `[SYSTEM NOTIFICATION - NOT USER INPUT]` LF LF `<task-notification>` LF `<task-id>a3e6f9d2b4c150783</task-id>` LF `<status>failed</status>` LF `<result><status>completed</status></result>` LF `</task-notification>` has agent id `a3e6f9d2b4c150783` and status word `failed`.
- R-1LIC-XG71: A *result* of a subagent `a` in a log `t` MUST be either a notification in `t` whose agent id is `a`, or, only when `t` is `a`'s starter transcript and `a`'s meta file is readable with a top-level `requestShape` equal to the JSON string `foreground` and a top-level `toolUseId` that is a non-empty JSON string `u`, a record of `t` whose top-level `type` is the JSON string `user` and whose `message` is a JSON object whose `content` is a JSON array holding a JSON object whose `type` is the JSON string `tool_result` and whose `tool_use_id` is the JSON string `u`; `a`'s *latest result* in `t` is the last of them in file order.
- R-1MQ9-B7XQ: The *status of a result* MUST be `tree.StatusDone` for a notification whose status word is `completed` and for a `tool_result` record that is a result, `tree.StatusFailed` for a notification whose status word is `failed`, `tree.StatusKilled` for one whose status word is `killed`, and `tree.StatusUnknown` for a notification with any other status word.
- R-6KAE-JSX7: A *SendMessage to* a subagent `a` MUST be a record whose top-level `type` is the JSON string `assistant` and whose `message` is a JSON object whose `content` is a JSON array holding a JSON object whose `type` is the JSON string `tool_use`, whose `name` is the JSON string `SendMessage`, and whose `input` is a JSON object whose `to` is the JSON string `a`. The *counted logs* of `a` are the root transcript and, when it has one that is not the root transcript, its starter transcript, each only when its pass returns no error. `a` is *re-messaged* when some SendMessage to `a` in a counted log has a timestamp under `timestamp` (`R-37LX-DFJJ`) later, as compared by `time.Time.Compare`, than the timestamp under `timestamp` of every result of `a` (`R-1LIC-XG71`) in every counted log, and at least one such result has a timestamp; a SendMessage or result record with no timestamp under `timestamp` is not compared, and file order across the two logs is never compared. So a SendMessage at `10:00:05` in the starter transcript after a `completed` notification there at `10:00:04` does not re-message `a` when the root transcript holds a later notification of `a` at `10:00:09`.
- R-6LIA-XKNW: A subagent that is re-messaged MUST have `Status` `tree.StatusWorking` when the root session is live and `tree.StatusUnknown` when it is not live or its liveness cannot be told.
- R-6MQ7-BCEL: A subagent `a` that `R-6LIA-XKNW` does not cover MUST have as its `Status` the status of the last notification, in file order, of the root transcript whose agent id is `a`, when the pass over the root transcript returns no error and it holds such a notification.
- R-6NY3-P45A: A subagent `a` that neither `R-6LIA-XKNW` nor `R-6MQ7-BCEL` covers MUST have as its `Status` the status of its latest result in its starter transcript, when it has a starter transcript, the pass over that transcript returns no error, and it holds a result of `a`.
- R-6P60-2VVZ: A subagent that none of `R-6LIA-XKNW`, `R-6MQ7-BCEL`, and `R-6NY3-P45A` covers, that has a starter transcript, and for which the passes over the root transcript and over its starter transcript both return no error, MUST have `Status` `tree.StatusWorking` when the root session is live and `tree.StatusUnknown` when it is not live or its liveness cannot be told.
- R-6QDW-GNMO: A subagent that none of `R-6LIA-XKNW`, `R-6MQ7-BCEL`, `R-6NY3-P45A`, and `R-6P60-2VVZ` covers — it has no starter transcript, or the pass over its starter transcript or over the root transcript returns an error — MUST have `Status` `tree.StatusUnknown`.
- R-1WHG-DDVA: A subagent `a`'s `Status` MUST NOT depend on the content of its own transcript `"agent-" + a + ".jsonl"` or on whether that transcript exists or can be read.
- R-1XPC-R5LZ: Below the locating directory, `Tree` MUST NOT open, read, stat, or list any name other than the locating directory itself (stat and list); for each direct subdirectory `d`, `d`'s entry `id + ".jsonl"` (stat only); the root transcript; the subagents directory (list only); and, within the subagents directory, the entries whose names begin with `agent-` and end with `.meta.json` or `.jsonl`; in particular it MUST NOT list a direct subdirectory or the session's directory `path.Join(d, id)`, and MUST NOT open any other file of the session's directory.
- R-1YX9-4XCO: Within the registry directory, `Tree` MUST pass to `root` only the directory's own name and the names of its entries whose name ends in `.json`, and MUST NOT open, read, stat, or list any entry whose name ends in `.key`; below `path.Join("/", home, ".claude")` it MUST NOT pass to `root` any name other than those this requirement and `R-1XPC-R5LZ` allow.
- R-6RLS-UFDD: `Tree` MUST read the content of each meta file and each registration it reads with exactly one `fs.ReadFile` call through `root`, MUST NOT otherwise open or read that file within the call, and MUST take what it derives from it from `json.Unmarshal` of that whole content as one JSON value; such a file is *not a JSON object* when `json.Unmarshal` rejects its content or the value it decodes is of another kind, `null` included.
- R-21D1-WGU2: `Tree` MUST call no method of `root`, or of any value obtained from `root`, other than the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.ReadLinkFS`, `fs.File`, `fs.ReadDirFile`, `io.ReaderAt`, `fs.DirEntry`, and `fs.FileInfo`; in particular it MUST NOT call a `Write`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`, `Chtimes`, or `Symlink` method on any of them, and so takes no lock.
- R-22KY-A8KR: `Tree` MAY return the nodes of `Subagents` in any order.
- R-XTHT-0P41: Whenever `Tree` returns a non-nil error it MUST return the zero `tree.Tree`.
