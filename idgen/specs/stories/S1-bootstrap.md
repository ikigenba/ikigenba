# Stories — bootstrap

Running idgen at all: help, version, and an option idgen does not have.
idgen is a single command with no subcommands. Its grammar is
`idgen [options] [ID ...]`: options come before any positional argument, and
short and long forms of an option behave identically. With no options it mints
one id from the current time (`S2-mint.md`); `--decode` turns it to decoding
ids back into instants (`S3-decode.md`). It exits `0` on success, help and
version included; `1` when a valid command line fails: a malformed id while
decoding, or a clock outside the window ids can hold; and `2` on a usage
error, which writes to stderr the usage text and, where one applies, a line
naming the problem, in an order not fixed here. idgen reads and writes no file
and keeps nothing between runs. Every later group adds to this frame.

## A developer asks how to use idgen

A developer who has just built idgen, or who has forgotten an option, asks for
the usage text. It is printed once, whole, and ends in a single newline. Later
stories that fail with a usage error refer to this text rather than quoting it
again.

Command:

```
$ idgen --help
```

```
$ idgen -h
```

Output:

```
Usage: idgen [options] [ID ...]

Mint an identifier using the current time by default.

Options:
  -n, --number N       mint N identifiers (default 1)
  -p, --prefix PREFIX  use PREFIX (default "R")
      --decode         decode ID arguments, or whitespace-delimited IDs from stdin
  -h, --help           print this help
  -V, --version        print version
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks which idgen they have

The version is carried in the source, never injected at build time, so a
developer's build and a release report the same string. Its shape is
`v<major>.<minor>.<patch>`: a `v`, then three non-negative integers without
leading zeros, separated by dots. It is printed bare, alone on its line. Its
value is data and is not fixed here.

Command:

```
$ idgen --version
```

```
$ idgen -V
```

Output:

```
v<major>.<minor>.<patch>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed.

## A developer gives an option idgen does not have

idgen accepts exactly the options the usage text lists and no others. Anything
else is a usage error: nothing is minted or decoded, and the developer is
shown what idgen does accept.

Command:

```
$ idgen --bogus
```

```
$ idgen -bogus
```

Output: the usage text, on stderr. `<diagnostic>` is a line about the unknown
option; whether it appears, its wording, and its place before or after the
usage text are not fixed here.

```
<diagnostic>
<usage text, as in "A developer asks how to use idgen">
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/idgen` exists.

Postconditions:

- Nothing has changed. No id was minted.
