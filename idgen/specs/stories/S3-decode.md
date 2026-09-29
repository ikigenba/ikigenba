# Stories — decode

Turning ids back into the instants they were minted at. `--decode` switches
idgen from minting to decoding. An id is `<prefix>-XXXX-XXXX`. Its 8-character
body alone encodes the instant, so any valid prefix (a non-empty run of ASCII
letters or digits, either case) is accepted and ignored. The body must be in
canonical form: two groups of four uppercase base-36 characters (`0-9A-Z`)
joined by a dash. The ids come from the positional arguments. When there
are none, idgen reads them from stdin, split on any mix of spaces, tabs, and
newlines. When there are positionals, stdin is not read at all. Each id gives
exactly one line, in input order: a valid id gives its instant on stdout, in
UTC at millisecond precision in the form `2006-01-02T15:04:05.000Z`, whatever
the `TZ` environment variable says. A malformed id gives a line on stderr that
begins `idgen: ` and names the token. Decoding reads and writes nothing on
disk.

## A developer decodes an id to the instant it was minted

A developer who finds an id in a spec, a log, or a commit wants to know when it
was minted. The body `OBCA-0VLA` encodes 2026-03-15T12:00:00.000Z. The prefix
`SPEC` plays no part in the result.

Command:

```
$ idgen --decode SPEC-OBCA-0VLA
```

Options:

- `--decode`: decode the ids given instead of minting a new one.

Output:

```
2026-03-15T12:00:00.000Z
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer decodes several ids of different prefixes

The prefix never changes the instant. The body `0007-J3LA` is the epoch
itself, 2026-01-01T00:00:00.000Z, and the same body under `S` and `SPEC`
decodes to the same line. The output lines follow the order of the arguments.

Command:

```
$ idgen --decode S-0007-J3LA SPEC-OBCA-0VLA SPEC-0007-J3LA
```

Output:

```
2026-01-01T00:00:00.000Z
2026-03-15T12:00:00.000Z
2026-01-01T00:00:00.000Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer pipes ids into idgen to decode them

With no positional ids, idgen reads stdin and treats every
whitespace-separated token as an id. Spaces, tabs, and newlines all separate
ids, and blank lines are skipped. The output is the same as if the tokens had
been given as arguments. Minting and decoding in one pipeline therefore works
too: `idgen -n 3 | idgen --decode` prints three instants, one per minted id,
each the current time when that id was minted.

Command:

```
$ printf 'S-0007-J3LA\t SPEC-OBCA-0VLA\n\nS-0007-J3LA\n' | idgen --decode
```

Output:

```
2026-01-01T00:00:00.000Z
2026-03-15T12:00:00.000Z
2026-01-01T00:00:00.000Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer decodes with nothing to decode

No positional ids and an empty stdin mean there is nothing to decode. That
counts as success, not an error.

Command:

```
$ idgen --decode < /dev/null
```

Output:

```
```

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer decodes where the time zone is not UTC

Decoded instants are always in UTC with a `Z` suffix, so the same id decodes to
the same line on every machine. idgen does not consult the local time zone.

Command:

```
$ TZ=Asia/Tokyo idgen --decode S-0007-J3LA
```

Output:

```
2026-01-01T00:00:00.000Z
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer decodes a batch holding a malformed id

A bad token does not stop the batch. Every valid id still decodes, in order,
and each malformed token gets its own line on stderr. The line begins
`idgen: ` and names the token. The rest of its wording is not fixed here and
is shown as `<message naming BOGUS>`. Because at least one id failed, the exit
code is 1. Each stream keeps input order, but how the stdout lines and the
stderr line interleave on a terminal is not fixed.

Command:

```
$ idgen --decode S-0007-J3LA SPEC-OBCA-0VLA BOGUS
```

Output:

```
2026-01-01T00:00:00.000Z
2026-03-15T12:00:00.000Z
idgen: <message naming BOGUS>
```

Exits 1. The two instant lines are on stdout; the `idgen: ` line is on stderr.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer decodes an id written in lowercase

Only the body has to be uppercase. The prefix may be any ASCII letters or
digits, so `s-0007-J3LA` decodes. A body with a lowercase letter is not the
canonical form, so `S-0007-j3la` is malformed and fails like any other bad
token.

Command:

```
$ idgen --decode s-0007-J3LA S-0007-j3la
```

Output:

```
2026-01-01T00:00:00.000Z
idgen: <message naming S-0007-j3la>
```

Exits 1. The instant line is on stdout; the `idgen: ` line is on stderr.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer gives mint options while decoding

`-n` and `-p` only affect minting. Under `--decode` they are accepted and
ignored, so the output is exactly what the same ids give without them. No id
is minted.

Command:

```
$ idgen -n 3 -p X --decode S-0007-J3LA
```

```
$ idgen --number 3 --prefix X --decode S-0007-J3LA
```

Options:

- `-n`, `--number N`: no effect when decoding.
- `-p`, `--prefix PREFIX`: no effect when decoding.

Output:

```
2026-01-01T00:00:00.000Z
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer gives ids and pipes stdin at once

Positional ids take precedence. When any are given, idgen decodes only those
and does not read stdin at all, so the piped id produces no line.

Command:

```
$ echo SPEC-OBCA-0VLA | idgen --decode S-0007-J3LA
```

Output:

```
2026-01-01T00:00:00.000Z
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.
