# D06-claude

How `list claude` finds the live root sessions of Claude Code. The package
`internal/harness/claude` turns what Claude Code leaves under `$HOME` into
`session.Session` values (`D04-sessions-and-table`), using the process facts
of `D05-process-facts` to decide which registrations are still live. The
command (`internal/cli`) calls `List` with the machine's filesystem and the
home directory, prints `session.Table` of the result, and turns a returned
`*session.ReadError` into the `cannot read` diagnostic.

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
