# D09-token

A sandbox keeps one bearer token: the credential an agent working in the worktree sends to the sandbox's apps. A human creates it at the sandbox's auth app; sandbox only stores it and hands it back. `token set` stores the token read from stdin, and `token` prints the stored one. This document owns what counts as a bearer token, how `token set` reads and stores it, what `token` prints, and the diagnostics of both.

Both are worktree commands. They find their sandbox, and are refused, exactly as D03 states (R-EIIF-59MV, R-LKKR-WFZT and the diagnostics it orders); their help text and usage errors, the extra-argument refusal of `token set` included, are D02's (R-00DX-MPC7, R-YGOV-PAQF, R-ZJBC-9WYH); the token's path `<data>/token` and its mode are D03's (R-FE7T-3AWX). D03 also says that `token set` holds the sandbox lock while it reads stdin and writes, that `token` takes no lock and changes nothing, that no command other than `wipe` and `token set` touches the token (so it survives `down` and `up`), and that the token is a kept entry; D07 owns `wipe` deleting it with the data directory.

A token arrives on stdin, never on the command line, so it never shows in a process list or a shell history. One trailing line ending, `\n` or `\r\n`, is dropped, so `echo` and `printf '%s'` store the same thing. What is left must be a bearer token: `ikp_` followed by printable ASCII with no space. Anything else is refused without being echoed, since it may be a secret pasted by mistake. Stdin that holds nothing once its line ending is dropped gets its own, shorter refusal. sandbox reads stdin only once the sandbox is found, known, this worktree's and locked, so a refused command never consumes it.

The token is written to a complete new file that is renamed over `<data>/token`, so `token` never sees half a token and an earlier token is either kept whole or replaced whole. `token set` creates the data directory when the sandbox has none yet; it starts, stops and restarts nothing, so it works the same whether the sandbox is up or down.

`token` prints the stored token and a newline, so `$(sandbox token)` is exactly the token. With no token stored it says so and tells the agent what to ask the human for, naming the sandbox's auth origin from its recorded port whether or not an `auth` app is recorded. A stored file that does not hold a bearer token is reported as a broken file, not printed.

## REQUIREMENTS

- R-QF57-E042: A bearer token MUST be a byte string of `ikp_` followed by one or more bytes each in the range 0x21 through 0x7E (printable ASCII other than space).

- R-CO8E-1KCH: `token set` MUST read stdin to end of file and remove exactly one trailing `\n` or `\r\n` when the bytes read end in one, the rest being the candidate, verified at least by `ikp_a` followed by `\n`, by `\r\n`, and by nothing each storing `ikp_a`, and by `ikp_a` followed by `\n\n`, by `ikp_a` followed by `\r\n\r\n`, and by `ikp_a` followed by a lone `\r` each being refused as not a bearer token.

- R-QHL0-5JLG: When the candidate is empty, `token set` MUST write the no-token-on-stdin diagnostic, exactly `sandbox: no token on stdin` and a newline, to stderr, write nothing to stdout, and make `cli.Run` return 2, verified at least by empty stdin, stdin holding only `\n`, and stdin holding only `\r\n`.

- R-QISW-JBC5: When the candidate is not empty and is not a bearer token, `token set` MUST write the not-a-token diagnostic, exactly the three lines `sandbox: stdin does not hold a bearer token`, an empty line, and `a bearer token is one line beginning 'ikp_'`, each ending in a newline, to stderr, write nothing to stdout, and make `cli.Run` return 2, verified at least by `hunter2\n`, `ikp_`, `IKP_a`, ` ikp_a`, `ikp_a b`, `ikp_a` tab `b`, `ikp_a\nikp_b`, `ikp_a` followed by the bytes 0xC3 0xA9, and `\n\n`.

- R-QK0S-X32U: When the candidate is a bearer token, `token set` MUST leave `<data>/token` holding exactly the candidate's bytes, with no newline added, write nothing to stdout or stderr, and make `cli.Run` return 0, verified at least with no earlier token and with an earlier token of different length.

- R-CWS9-P170: After `token set` stores a token, `<data>/token` MUST have permission bits exactly 0600, verified at least when no earlier token existed, when an earlier `<data>/token` had mode 0644, and when it had mode 0400.

- R-QMGL-OMK8: `token set` MUST replace `<data>/token` by renaming a complete file over it, so a hard link made to the earlier `<data>/token` still holds the earlier token afterwards.

- R-QNOI-2EAX: When the sandbox is known but `<data>` does not exist, `token set` with a bearer token on stdin MUST create `<data>` and store the token, returning 0.

- R-CPGA-FC36: When `token set` does not return 0, `<data>/token` MUST be byte-for-byte what it was before the command, or still absent, verified at least by the no-token-on-stdin and not-a-token refusals with an earlier token stored and with no token stored.

- R-CQO6-T3TV: When `token set` does not return 0, it MUST have created no file or directory other than the sandbox lock file (D03) and its missing parents, verified at least by the no-token-on-stdin and not-a-token refusals with `<data>` absent.

- R-LQO9-TAPA: `token set` refused by any check D03 orders for a worktree command (R-LKKR-WFZT) MUST read nothing from stdin, verified with a stdin that fails the test when read, for each of those refusals.

- R-QRC7-7PJ0: When reading stdin fails with an error, `token set` MUST write exactly `sandbox: stdin: <error>` and a newline to stderr, where `<error>` is the error's `Error()` text, write nothing to stdout, store nothing, and make `cli.Run` return 1, verified with a stdin that yields `ikp_a` and then the error `boom`, writing `sandbox: stdin: boom`.

- R-CRW3-6VKK: `token` and `token set` MUST run nothing through `Deps.Exec` or `Deps.Stream` other than their `git rev-parse --show-toplevel` run (D03).

- R-CY06-2SXP: `token set` MUST leave `<root>/registry.json` unchanged: the same file, as `os.SameFile` judges it against a `FileInfo` taken before the command, holding the same bytes.

- R-AFEE-4655: When `<data>` cannot be created, `token set` MUST fail with D02's file-error diagnostic (R-AE6H-QEEG) for the path `<data>` and return 1, verified with `<data>` an existing regular file, writing `sandbox: <data>: not a directory`.

- R-AGMA-HXVU: When `<data>` exists but the token cannot be written to `<data>/token`, `token set` MUST fail with D02's file-error diagnostic (R-AE6H-QEEG) for the path `<data>/token` and return 1, verified with `<data>` a directory of mode 0500 holding an earlier token, writing `sandbox: <data>/token: permission denied` and leaving the earlier token unchanged.

- R-QXFP-4K8H: When `<data>/token` holds a bearer token, `token` MUST write exactly those bytes followed by one newline to stdout, write nothing to stderr, and make `cli.Run` return 0.

- R-QZVH-W3PV: When `<data>/token` does not exist, `<data>` absent included, `token` MUST write the no-token-stored diagnostic, exactly the four lines `sandbox: no token stored for sandbox '<name>'`, an empty line, `Ask the human to sign in at <auth origin>, create a bearer`, and `token, and store it with: printf '%s' '<token>' | sandbox token set`, each ending in a newline, to stderr, where `<name>` is the sandbox name and `<auth origin>` is the origin D03 gives the app `auth` (R-0QNY-FX6R) with the port the registry records, whether or not `auth` is a recorded app, and `<token>` is that literal text; MUST write nothing to stdout; and MUST make `cli.Run` return 2; verified at least by sandbox `wip` with port 7400, naming `http://auth.wip.localhost:7400`, and by sandbox `feature-x` with port 7412 and no recorded apps, naming `http://auth.feature-x.localhost:7412`.

- R-AJ23-9HD8: When `<data>/token` exists but its whole content is not a bearer token, `token` MUST fail with D02's file-error diagnostic (R-AE6H-QEEG) for the path `<data>/token` with the reason `does not hold a bearer token`, write nothing to stdout, and return 1, verified at least by an empty file, by `ikp_a` followed by `\n`, and by `hunter2`.

- R-AK9Z-N93X: When `<data>/token` cannot be read, `token` MUST fail with D02's file-error diagnostic (R-AE6H-QEEG) for the path `<data>/token` and return 1, verified with `<data>/token` a directory, writing `sandbox: <data>/token: is a directory`.

- R-R3J7-1EXY: `token` MUST NOT read stdin, verified with a stdin that fails the test when read, both when a token is stored and when none is.
