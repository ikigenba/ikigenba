# D01-layout-and-run-seam

turns is one Go module, `github.com/ikigenba/ikigenba/turns`, built into one binary. Everything the program touches outside its own memory enters through one run seam: the effective uid, the environment, the working directory, the name it was invoked by, the system configuration root, the clock, the host name, the HTTP client and the process runner arrive in `seam.Deps`, and standard input, output and error arrive as parameters of `cli.Run`, which D02 declares. Offline tests drive `cli.Run` in process with fakes for all of them; exactly one test execs the built binary.

## Packages

- The root package `turns` (the module root) owns the embedded user units and the names of the unit files and the host binary path.
- `cmd/turns` is the binary: it builds `seam.Deps` from the process and hands control to `cli.Run`.
- `internal/seam` owns the run seam: `Deps`, the process command and result shapes, the real process runner and the quoting of another program's output.
- `internal/cli` owns argument parsing, the usage texts, exit codes and every command's entry through `Run` (D02 and each command's document).
- `internal/paths` resolves every path turns reads or writes from the environment and the system configuration root (D03).
- `internal/config` reads the user's and the host's configuration and the token (D03).
- `internal/session` holds the session model the readers, the space client, push and status share (D04).
- `internal/claude`, `internal/codex` and `internal/grok` read the session files of Claude Code, Codex CLI and Grok CLI (D05, D06, D07).
- `internal/dispatch` reads the dispatch tool's job files and links a dispatched session to its parent; `internal/local` reads all three tools' sessions on this machine through one entry point (D08).
- `internal/space` is turns' client for the space's gateway and the `sessions` tools behind it (D09).

turns/AGENTS.md says `internal/` is everything else; the root package `turns` departs from that. It exists because `//go:embed` can only reach files at or below the directory of the package that embeds them, and the units live in `share/systemd/` at the module root, which no package under `internal/` or `cmd/` can reach. The root package holds nothing else, and it has no test files of its own: the gap greps only `cmd/` and `internal/`, so its requirements are proven by tests under `internal/` that import it.

## Units

`Units()` answers the two user units, `turns.service` and `turns.timer`, embedded from `share/systemd/` when the binary is built. Their text is an input kept under `share/` and is not restated here; the embed itself is the guarantee that `Units()` is byte-equal to those files, so no test reads `share/`. The one line of the service unit that is contract is its `ExecStart=` line, because the host installs the units as they are and runs the binary at `HostBinary`; `install` (D11) rewrites that one line for a laptop. systemd.service(5) allows several `ExecStart=` lines in some service types, each run in turn, so the requirement pins exactly one.

## The run seam

`Deps.Dir` is the working directory, `Deps.Arg0` the program name as invoked, `Deps.Etc` the system configuration root. An empty `Etc` means there is no system configuration at all: no host default space and no host units, so a test that leaves it empty can never read the real `/etc`. Every nil field of `Deps` has a default, as the requirements state. Tests always inject `HTTP`, `Exec`, `Now` and `Hostname`; the nil defaults are proven by tests that observe the default's effect through `cli.Run` (a nil `HTTP` by a test that substitutes the transport of `http.DefaultClient` for its duration; a nil `Exec` by a test that points the test process's `PATH` at a temporary directory holding a stand-in program).

`seam.Exec` is the one real process runner. A program that runs and exits non-zero is a result, not an error: the caller decides what the exit status means. It runs in `Cmd.Dir`, or in turns' own working directory when that is empty, with turns' environment plus `Cmd.Env` and an empty standard input. `seam.QuoteOutput` turns another program's output into the `> `-prefixed detail every diagnostic uses.

## The binary

`main` in `cmd/turns` calls `cli.Run` with a context, `os.Args[1:]`, `os.Stdin`, `os.Stdout`, `os.Stderr` and `seam.Process()`, and exits with the code `Run` returns. `seam.Process` builds the process's `Deps`: `EUID` from `os.Geteuid`, `Getenv` as `os.Getenv`, `Dir` the working directory (empty when it cannot be read), `Arg0` from `os.Args[0]`, `Etc` `/etc`, and `Now`, `Hostname`, `HTTP` and `Exec` left nil so each reads as its real default. `seam.Process` is proven in process; what `main` holds is not a requirement, and the one test that execs the built binary proves the wiring end to end through `--help`, which reads nothing and changes nothing. The same test as root cannot run in the gates, which run as an ordinary user, so the root refusal is proven through `cli.Run` alone.

## REQUIREMENTS

- R-JSP9-R2Q3: The root package `turns` MUST export `Units() fs.FS`.
- R-JTX6-4UGS: The `fs.FS` that `turns.Units` returns MUST hold at its root exactly two entries, regular files named `turns.service` and `turns.timer`, and nothing else.
- R-JV52-IM7H: The file `turns.service` in the `fs.FS` that `turns.Units` returns MUST contain exactly one line beginning `ExecStart=`, and that line MUST be exactly `ExecStart=/usr/local/bin/turns push`.
- R-JWCY-WDY6: The root package `turns` MUST export the untyped string constant `ServiceUnit = "turns.service"`.
- R-JXKV-A5OV: The root package `turns` MUST export the untyped string constant `TimerUnit = "turns.timer"`.
- R-JYSR-NXFK: The root package `turns` MUST export the untyped string constant `HostBinary = "/usr/local/bin/turns"`.
- R-K00O-1P69: Package `internal/seam` MUST export a `Cmd` struct whose fields are exactly `Path string`, `Args []string`, `Dir string`, and `Env []string`.
- R-K18K-FGWY: Package `internal/seam` MUST export a `Result` struct whose fields are exactly `Stdout []byte`, `Stderr []byte`, and `ExitCode int`.
- R-K2GG-T8NN: Package `internal/seam` MUST export the type `Runner func(ctx context.Context, cmd Cmd) (Result, error)`.
- R-K3OD-70EC: Package `internal/seam` MUST export `Exec(ctx context.Context, cmd Cmd) (Result, error)`, and `Exec` MUST be assignable to a `Runner`.
- R-3O48-7RR6: `seam.Exec` MUST return a non-nil error if and only if the process could not be started or `ctx` ended before the process exited.
- R-3PC4-LJHV: `seam.Exec` MUST report a process that ran and exited non-zero as a `Result` whose `ExitCode` is that exit status, with a nil error.
- R-3QK0-ZB8K: `seam.Exec` MUST report a process ended by a signal while `ctx` had not ended as a `Result` whose `ExitCode` is -1, with a nil error.
- R-SGHY-D4MH: When `ctx` ends before the process exits, `seam.Exec` MUST end the process and return promptly with a non-nil error; verified at least with a process that would run far longer than the test's `ctx` deadline, `Exec` returning within a bound well under that process's run time.
- R-K645-YJVQ: `seam.Exec` given a `Cmd` whose `Path` is empty MUST return a non-nil error without starting any process.
- R-UEL4-ABV0: `seam.Exec` MUST run the program named by `Cmd.Path`, looked up on the `PATH` of the turns process's own environment when `Cmd.Path` contains no `/`, and taken as a file path when it contains one.
- R-UFT0-O3LP: `seam.Exec` MUST pass the process exactly `Cmd.Args`, in order, as its arguments after the program name.
- R-UH0X-1VCE: `seam.Exec` given a `Cmd` whose `Dir` is non-empty MUST run the process with `Cmd.Dir` as its working directory.
- R-K9RV-3V3T: `seam.Exec` given a `Cmd` whose `Dir` is empty MUST run the process in the working directory of the turns process.
- R-KAZR-HMUI: `seam.Exec` MUST give the process the environment the turns process holds plus the entries of `Cmd.Env`, where an entry of `Cmd.Env` overrides an inherited entry of the same name and a later entry of `Cmd.Env` overrides an earlier one of the same name.
- R-3RRX-D2Z9: `seam.Exec` MUST give the process an empty standard input, never the turns process's own; verified at least with the turns process's standard input replaced by a pipe holding bytes, the process reading none of them.
- R-SHPU-QWD6: `seam.Exec` MUST capture the process's standard output into `Result.Stdout` and its standard error into `Result.Stderr`, each holding exactly the bytes the process wrote to that stream; verified at least with a process writing more than 1 MiB to each stream, each captured whole.
- R-KDFK-96BW: Package `internal/seam` MUST export a `Deps` struct whose fields are exactly `EUID int`, `Getenv func(key string) string`, `Dir string`, `Arg0 string`, `Etc string`, `Now func() time.Time`, `Hostname func() (string, error)`, `HTTP *http.Client`, and `Exec Runner`.
- R-ULWI-KYB6: Package `internal/seam` MUST export `Process() Deps`.
- R-UN4E-YQ1V: The `Deps` that `seam.Process` returns MUST have `EUID` equal to `os.Geteuid()`.
- R-UOCB-CHSK: The `Deps` that `seam.Process` returns MUST have a `Getenv` that answers, for every key, what `os.Getenv` answers for it at the time of the call.
- R-UPK7-Q9J9: The `Deps` that `seam.Process` returns MUST have `Dir` equal to the directory `os.Getwd` returns when it succeeds, and the empty string when it fails.
- R-UQS4-419Y: The `Deps` that `seam.Process` returns MUST have `Arg0` equal to `os.Args[0]`.
- R-UT7W-VKRC: The `Deps` that `seam.Process` returns MUST have `Etc` equal to `/etc`.
- R-UUFT-9CI1: The `Deps` that `seam.Process` returns MUST have `Now`, `Hostname`, `HTTP` and `Exec` nil, so each reads as its real default.
- R-KENG-MY2L: `cli.Run` given a `seam.Deps` whose `Getenv` is nil MUST behave exactly as when given the same `Deps` with a `Getenv` that returns the empty string for every key.
- R-SIXR-4O3V: When `cli.Run` given a `seam.Deps` whose `Now` is nil runs `turns push` (not a dry run) and exits 0, the file `last-push` in the state directory D03 resolves MUST hold one line `YYYY-MM-DDTHH:MM:SSZ` naming, in UTC, a moment at or after the wall-clock time just before Run, truncated to the second, and at or before the wall-clock time just after Run returned; verified at least with the test process's local time zone set to a zone other than UTC.
- R-KH39-EHJZ: `cli.Run` given a `seam.Deps` whose `Hostname` is nil MUST behave as when given the same `Deps` with `Hostname` set to `os.Hostname`.
- R-KIB5-S9AO: `cli.Run` given a `seam.Deps` whose `HTTP` is nil MUST make every HTTP request through `http.DefaultClient`.
- R-3U7Q-4MGN: When `cli.Run` is given a `seam.Deps` whose `Exec` is nil, every process it starts MUST be started as the `seam.Exec` requirements state: the program looked up as they state, with its arguments, working directory and environment as they state, an empty standard input, and its output captured.
- R-KLYU-XKIR: Package `internal/seam` MUST export `QuoteOutput(text string) string`.
- R-UKOM-76KH: `seam.QuoteOutput` MUST remove every trailing `\n` byte from its input and nothing else, return the empty string when nothing remains, and otherwise return each `\n`-separated line of what remains prefixed with `> `, the quoted lines joined by single `\n` bytes with no `\n` after the last, every other byte (including `\r` and spaces) preserved; verified at least by the empty string, `\n` and `\n\n` each giving the empty string, ` a \r` giving `>  a \r`, `a\r\n` giving `> a\r`, and `a\n\nb\n\n` giving `> a\n> \n> b`.
- R-KOEN-P405: The binary built from `./cmd/turns`, run with the single argument `--help` as a non-root user, MUST print the top-level usage text D02 states to stdout, write nothing to stderr, and exit 0.
