# Stories — mint

Minting ids from the current time, which is what idgen does unless
`--decode` is given (`S3-decode.md`). An id is a prefix, a dash, and an
8-character body written in uppercase base 36 (`0-9`, `A-Z`) and split 4-4
with a dash: `<prefix>-XXXX-XXXX`. The body is derived from the current UTC
millisecond, counted from an epoch of 2026-01-01T00:00:00.000Z, so decoding an
id gives back the millisecond it was minted in; the epoch itself mints the
body `0007-J3LA`, and 2026-03-15T12:00:00.000Z mints `OBCA-0VLA`. The prefix
defaults to `R`; a prefix given with `--prefix` must be one or more ASCII
letters or digits, and it replaces the default whole. Ids minted by one
invocation are distinct: each comes from a strictly later millisecond than the
one before, so minting several takes at least one millisecond per id after the
first. Nothing guarantees distinctness across separate invocations. Each id is
written on its own line, in mint order. Minting reads and writes no file.

## A developer mints an id

A developer needs a fresh, traceable id. `<XXXX>-<XXXX>` is the body
for the millisecond the command ran in; it varies from run to run.

Command:

```
$ idgen
```

Output:

```
R-<XXXX>-<XXXX>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists, built from the checkout with `make`.
- The system clock reads an instant from 2026-01-01T00:00:00.000Z up to, but
  not including, 2115-05-26T17:38:27.456Z.

Postconditions:

- Nothing has changed.
- Decoding the id gives back the millisecond the command ran in.

## A developer mints several ids at once

A developer who needs a batch asks for it in one invocation rather than
running idgen repeatedly, so every id in the batch is distinct. The
example asks for three; each `<XXXX>-<XXXX>` is a different body.

Command:

```
$ idgen -n 3
```

```
$ idgen --number 3
```

Options:

- `-n`, `--number N`: mint `N` ids instead of one.

Output:

```
R-<XXXX>-<XXXX>
R-<XXXX>-<XXXX>
R-<XXXX>-<XXXX>
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.
- The system clock reads an instant from 2026-01-01T00:00:00.000Z up to, but
  not including, 2115-05-26T17:38:27.456Z.

Postconditions:

- Nothing has changed.
- The three ids are pairwise distinct. Decoded in the order printed,
  each gives a millisecond strictly later than the one before it, and the last
  is at least two milliseconds after the first.

## A developer mints ids under their own prefix

A developer whose ids name something other than a requirement gives
them their own prefix. The prefix may be one character or several, and it
takes the place of `R` rather than being added to it.

Command:

```
$ idgen -p SPEC
```

```
$ idgen --prefix SPEC
```

Options:

- `-p`, `--prefix PREFIX`: use `PREFIX` in place of `R`.

Output:

```
SPEC-<XXXX>-<XXXX>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.
- The system clock reads an instant from 2026-01-01T00:00:00.000Z up to, but
  not including, 2115-05-26T17:38:27.456Z.

Postconditions:

- Nothing has changed.
- Decoding the id gives back the millisecond the command ran in, the same as
  an id minted in that millisecond under any other prefix.

## A developer asks for zero or a negative number of ids

The number of ids must be greater than zero. Anything less is a usage error,
checked before anything is minted.

Command:

```
$ idgen -n 0
```

```
$ idgen --number -1
```

Output: `<diagnostic>` is a line naming the invalid number; it begins
`idgen: ` and contains `--number must be > 0`. Its wording otherwise, and its
place before or after the usage text, are not fixed here.

```
<diagnostic>
<usage text, as in "A developer asks how to use idgen">
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed. No id was minted.

## A developer gives a prefix idgen does not allow

A prefix is one or more ASCII letters or digits. An empty prefix, whitespace,
a dash or other punctuation, or a non-ASCII letter is refused, so no id is
minted that could not be decoded back. The check is made before anything is
minted.

Command:

```
$ idgen -p ''
```

```
$ idgen -p ' '
```

```
$ idgen -p A-B
```

```
$ idgen --prefix Ä
```

Output: `<diagnostic>` is a line naming the invalid prefix; it begins
`idgen: ` and contains `invalid prefix`. Its wording otherwise, and its place
before or after the usage text, are not fixed here.

```
<diagnostic>
<usage text, as in "A developer asks how to use idgen">
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed. No id was minted.

## A developer passes an argument to mint

Minting takes no positional argument; only decoding does. A word given after
the options is a usage error whatever it is, even one that reads like a
command, such as `help` or `version`: idgen has no commands, only options.

Command:

```
$ idgen extra
```

```
$ idgen help
```

```
$ idgen version
```

Output: `<diagnostic>` is a line naming the unexpected argument; its wording,
and its place before or after the usage text, are not fixed here.

```
<diagnostic>
<usage text, as in "A developer asks how to use idgen">
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed. No id was minted.

## A developer mints while the clock is outside the window ids can hold

An id can only hold instants from the epoch up to, but not including,
2115-05-26T17:38:27.456Z, the epoch plus 36⁸ milliseconds. When the clock
reads an instant outside that window, idgen fails rather than print an id that
would decode to the wrong time. `<diagnostic>` is a non-empty message; its
wording is not fixed here.

Command:

```
$ idgen
```

Output:

```
<diagnostic>
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `bin/idgen` exists.
- The system clock reads an instant before 2026-01-01T00:00:00.000Z, or at or
  after 2115-05-26T17:38:27.456Z.

Postconditions:

- Nothing has changed. No id was printed.
