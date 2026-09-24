# D08-grok

How `list grok` finds the live root sessions of the Grok Build CLI. The
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

## REQUIREMENTS

- R-AJCM-S4R1: The package `internal/harness/grok` (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok`) MUST export exactly one identifier, the function `List(root fs.FS, home string) ([]session.Session, error)`, where `session` is `internal/session` (`D04-sessions-and-table`).
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
