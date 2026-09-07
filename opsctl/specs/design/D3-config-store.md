# D3-config-store

The host configuration store is one JSON file, `/etc/ikigenba/config.json`,
holding a flat map of string keys to string values. It is the first thing a
bootstrap touches — everything else `opsctl` does reads its inputs from here —
and the first thing backed up. Package `internal/config` owns the file;
the `config` command in `internal/cli` exposes it.

**Keys and values.** Keys match `^[a-z0-9_.-]+$`, so `list` output never needs
quoting and dotted names such as `dns.zones` give later designs a namespace.
Values are arbitrary strings that contain no `\n` or `\r`, so every value fits
on one output line. The empty string is a legal value and is distinct from
the key being absent.

**File format.** A single JSON object whose values are all strings, keys in
sorted order, two-space indentation, trailing newline. Anything else — a
non-object top level, a non-string value, malformed JSON — is *corrupt*, and
every operation reports it rather than treating the file as empty, because
silently starting over is how a bootstrap re-run destroys real data. A missing
file is an empty store; `Set` creates the directory (mode `0700`) and the
file (mode `0600`) on first use.

**Writes are atomic and serialised.** `Set` and `Del` take an exclusive
`flock` on `/etc/ikigenba/config.lock` for the read-modify-write, write the
new content to a temporary file in the same directory, fsync it, and rename it
over `config.json`. A reader always sees a complete file. Two concurrent
writers of different keys both persist.

```go
package config

const (
    Dir      = "/etc/ikigenba"
    FileName = "config.json"
    LockName = "config.lock"
)

var (
    ErrNotSet       = errors.New("key not set")
    ErrInvalidKey   = errors.New("invalid key")
    ErrInvalidValue = errors.New("invalid value")
    ErrCorrupt      = errors.New("config file is corrupt")
)

// Store is the config file under one filesystem root (D1 Deps.Root).
type Store struct {
    Root string
}

type Entry struct {
    Key   string
    Value string
}

func ValidKey(key string) bool
func (s Store) Get(key string) (string, error)   // ErrNotSet when absent
func (s Store) Set(key, value string) error      // ErrInvalidKey, ErrInvalidValue
func (s Store) Del(key string) error             // nil whether or not key was set
func (s Store) List() ([]Entry, error)           // sorted by Key
```

**The `config` command.** Usage, byte for byte, printed by
`opsctl config --help`:

```
Usage: opsctl config <subcommand> [arguments]

Read and write the host configuration store (/etc/ikigenba/config.json).

Subcommands:
  get KEY        print the value of KEY; exit 1 if KEY is not set
  set KEY=VALUE  set KEY to VALUE, creating or replacing it
  del KEY        remove KEY; succeeds whether or not KEY is set
  list           print every KEY=VALUE, one per line, sorted by key

Keys match ^[a-z0-9_.-]+$. Values may not contain newlines.
```

`set` splits its argument on the first `=`, so values may contain `=`.
A missing `=`, an invalid key, or a value with a newline is a usage error
(exit 2). A corrupt file is an operation failure (exit 1) whose message names
the file.

Canonical usage, as an agent would drive it over ssh:

```
$ opsctl config set dns.zones=ikigenba.dev
$ opsctl config set acme.email=ops@ikigenba.dev
$ opsctl config get dns.zones
ikigenba.dev
$ opsctl config get backup.s3_uri; echo "exit $?"
opsctl config: key not set: backup.s3_uri
exit 1
$ opsctl config list
acme.email=ops@ikigenba.dev
dns.zones=ikigenba.dev
$ opsctl config del acme.email
$ opsctl config del acme.email; echo "exit $?"
exit 0
```

## REQUIREMENTS

- R-NHVQ-SZF1: Package `internal/config` MUST export the constants `Dir = "/etc/ikigenba"`, `FileName = "config.json"`, and `LockName = "config.lock"`.
- R-NJ3N-6R5Q: Package `internal/config` MUST export the error values `ErrNotSet`, `ErrInvalidKey`, `ErrInvalidValue`, and `ErrCorrupt`.
- R-NKBJ-KIWF: Package `internal/config` MUST export a `Store` struct whose only field is `Root string`, an `Entry` struct whose fields are exactly `Key string` and `Value string`, and the functions `ValidKey(key string) bool`, `(Store) Get(key string) (string, error)`, `(Store) Set(key, value string) error`, `(Store) Del(key string) error`, and `(Store) List() ([]Entry, error)`.
- R-NLJF-YAN4: `ValidKey` MUST return true exactly for non-empty strings matching `^[a-z0-9_.-]+$`.
- R-NMRC-C2DT: `Set` MUST return an error wrapping `ErrInvalidKey` for a key that `ValidKey` rejects, and an error wrapping `ErrInvalidValue` for a value containing `\n` or `\r`, and in either case MUST leave the file unchanged.
- R-NNZ8-PU4I: `Set` on a store whose directory does not exist MUST create `<Root>/etc/ikigenba` with mode `0700` and `config.json` with mode `0600`.
- R-NP75-3LV7: After `Set(key, value)` returns nil, `Get(key)` MUST return `value`, including when `value` is the empty string, and `config.json` MUST be a JSON object with string values only, keys in sorted order, two-space indentation, and a trailing newline.
- R-NQF1-HDLW: `Get` MUST return an error wrapping `ErrNotSet` for a key that is absent, including when the file does not exist.
- R-NRMX-V5CL: `Del` MUST return nil and leave the file without the key whether or not the key was set, and `Del` on a missing file MUST return nil without creating it.
- R-NSUU-8X3A: `List` MUST return every entry sorted by key ascending, and an empty slice with a nil error when the file does not exist.
- R-NU2Q-MOTZ: `Get`, `Set`, `Del`, and `List` MUST return an error wrapping `ErrCorrupt` when `config.json` exists and is not a JSON object whose values are all strings, and `Set` and `Del` MUST leave the corrupt file unchanged.
- R-NVAN-0GKO: `Set` and `Del` MUST write by creating a temporary file in `<Root>/etc/ikigenba` and renaming it over `config.json`, so that at no observable moment is `config.json` absent or partially written.
- R-NWIJ-E8BD: `Set` and `Del` MUST hold an exclusive `flock` on `<Root>/etc/ikigenba/config.lock` for the whole read-modify-write, verified by concurrent `Set` calls of distinct keys from separate goroutines all persisting.
- R-NYYC-5RSR: `opsctl config --help` and `opsctl config -h` MUST print the `config` usage text quoted above, byte for byte, to stdout and exit 0.
- R-O068-JJJG: `opsctl config get KEY` MUST print the value followed by a single newline to stdout and exit 0 when set, and MUST print nothing to stdout, write `opsctl config: key not set: KEY` to stderr, and exit 1 when not set.
- R-O1E4-XBA5: `opsctl config set KEY=VALUE` MUST split on the first `=` only, store the result, print nothing to stdout, and exit 0.
- R-O2M1-B30U: `opsctl config set` with an argument lacking `=`, an invalid key, or a value containing a newline MUST exit 2 with a diagnostic on stderr and leave the store unchanged.
- R-O3TX-OURJ: `opsctl config del KEY` MUST exit 0 and print nothing to stdout whether or not KEY was set.
- R-O51U-2MI8: `opsctl config list` MUST print one `KEY=VALUE` line per entry sorted by key ascending to stdout and exit 0, printing nothing when the store is empty.
- R-O69Q-GE8X: `opsctl config` with no subcommand or an unknown subcommand MUST print the `config` usage text to stderr and exit 2.
- R-O7HM-U5ZM: Every `config` subcommand MUST exit 1 with a stderr line naming `config.json` when the file is corrupt.
