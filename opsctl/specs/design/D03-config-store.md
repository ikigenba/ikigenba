# D03-config-store

Package `internal/config` owns the host configuration store, a flat map of string keys to string values at `/etc/ikigenba/config.json`; `internal/cli` exposes the `config` command. Empty values differ from absent keys. The generic store preserves keys owned by every command group and by operators.

## REQUIREMENTS

- R-W563-Q6GK: Package `internal/config` MUST export the constant `Dir = "/etc/ikigenba"`.
- R-W6E0-3Y79: Package `internal/config` MUST export the constant `FileName = "config.json"`.
- R-W7LW-HPXY: Package `internal/config` MUST export the constant `LockName = "config.lock"`.
- R-W8TS-VHON: Package `internal/config` MUST export the error value `ErrNotSet`.
- R-WA1P-99FC: Package `internal/config` MUST export the error value `ErrInvalidKey`.
- R-WB9L-N161: Package `internal/config` MUST export the error value `ErrInvalidValue`.
- R-WCHI-0SWQ: Package `internal/config` MUST export the error value `ErrCorrupt`.
- R-ETPG-BBIJ: Package `internal/config` MUST export a `Store` struct whose only field is `Root string`.
- R-EUXC-P398: Package `internal/config` MUST export an `Entry` struct whose fields are exactly `Key string` and `Value string`.
- R-EW59-2UZX: Package `internal/config` MUST export `ValidKey(key string) bool`.
- R-EXD5-GMQM: Package `internal/config` MUST export `(Store) Get(key string) (string, error)`.
- R-EYL1-UEHB: Package `internal/config` MUST export `(Store) Set(key, value string) error`.
- R-EZSY-8680: Package `internal/config` MUST export `(Store) Del(key string) error`.
- R-F10U-LXYP: Package `internal/config` MUST export `(Store) List() ([]Entry, error)`.
- R-NLJF-YAN4: `ValidKey` MUST return true exactly for non-empty strings matching `^[a-z0-9_.-]+$`.
- R-NMRC-C2DT: `Set` MUST return an error wrapping `ErrInvalidKey` for a key that `ValidKey` rejects, and an error wrapping `ErrInvalidValue` for a value containing `\n` or `\r`, and in either case MUST leave the file unchanged.
- R-F28Q-ZPPE: After a successful `Set`, `<Root>/etc/ikigenba` MUST exist with mode `0700` and `config.json` with mode `0600`, whether or not either existed before the call.
- R-NP75-3LV7: After `Set(key, value)` returns nil, `Get(key)` MUST return `value`, including when `value` is the empty string, and `config.json` MUST be a JSON object with string values only, keys in sorted order, two-space indentation, and a trailing newline.
- R-R5KV-058Q: `Set` MUST write the characters `<`, `>`, and `&` in a value literally into `config.json`, never as `\u003c`, `\u003e`, or `\u0026` escapes.
- R-NQF1-HDLW: `Get` MUST return an error wrapping `ErrNotSet` for a key that is absent, including when the file does not exist.
- R-2ALD-C3F6: `Del` on a valid, writable store MUST return nil and leave the file without the key whether or not the key was set; `Del` on a missing file MUST return nil without creating it.
- R-NSUU-8X3A: `List` MUST return every entry sorted by key ascending, and an empty slice with a nil error when the file does not exist.
- R-WEXA-SCE4: `Get`, `Del`, and `List`, and `Set` with a valid key and value, MUST return an error wrapping `ErrCorrupt` when `config.json` exists and is not a JSON object whose values are all strings, and `Set` and `Del` MUST leave the corrupt file unchanged.
- R-F3GN-DHG3: `Set` and `Del` MUST publish each changed config file by renaming a temporary file in `<Root>/etc/ikigenba` over `config.json`, so that readers observe only complete JSON files and an existing `config.json` is never observably absent during a write.
- R-F4OJ-R96S: `Set` and `Del` MUST hold an exclusive `flock` on `<Root>/etc/ikigenba/config.lock` for the whole read-modify-write, so concurrent successful writes to different keys from separate processes or goroutines both persist.
- R-F5WG-50XH: `opsctl config --help` and `opsctl config -h` MUST print exactly `"Usage: opsctl config <subcommand> [arguments]\n\nRead and write the host configuration store (/etc/ikigenba/config.json).\n\nSubcommands:\n  get KEY        print the value of KEY; exit 1 if KEY is not set\n  set KEY=VALUE  set KEY to VALUE, creating or replacing it\n  del KEY        remove KEY; succeeds whether or not KEY is set\n  list           print every KEY=VALUE, one per line, sorted by key\n\nKeys match ^[a-z0-9_.-]+$. Values may not contain newlines.\n"` to stdout, write nothing to stderr, and exit 0 for any effective user id without reading or changing host state.
- R-R352-8LRC: `opsctl config get KEY` MUST print the value followed by a single newline to stdout and exit 0 when set, and MUST print nothing to stdout, write exactly the line `opsctl: key not set: KEY` to stderr, and exit 1 when not set.
- R-O1E4-XBA5: `opsctl config set KEY=VALUE` MUST split on the first `=` only, store the result, print nothing to stdout, and exit 0.
- R-8Q6R-PBF6: `opsctl config set` MUST, when its argument lacks `=`, when the key is not `ValidKey`, or when the value contains a newline, leave the store unchanged, print nothing to stdout, write to stderr exactly three lines — the diagnostic, an empty line, and `see 'opsctl config --help' for usage` — and exit 2; the diagnostic is `opsctl: config set needs KEY=VALUE`, `opsctl: invalid key: <key>`, or `opsctl: invalid value: newline in value for '<key>'` respectively, with the conditions tested in that order.
- R-O3TX-OURJ: `opsctl config del KEY` MUST exit 0 and print nothing to stdout whether or not KEY was set.
- R-O51U-2MI8: `opsctl config list` MUST print one `KEY=VALUE` line per entry sorted by key ascending to stdout and exit 0, printing nothing when the store is empty.
- R-D3B7-CGSQ: `opsctl config` with no subcommand MUST write exactly the three lines `opsctl: no config subcommand given`, an empty line, and `see 'opsctl config --help' for usage` to stderr and exit 2, and with an unknown subcommand MUST write exactly the three lines `opsctl: unknown config subcommand '<name>'`, an empty line, and `see 'opsctl config --help' for usage` to stderr and exit 2.
- R-WG57-644T: Every `config` subcommand whose arguments pass validation MUST, when the file is corrupt, print nothing to stdout, leave the file unchanged, write to stderr exactly the line `opsctl: <path> is corrupt` where `<path>` is the resolved path of `config.json`, and exit 1.

- R-F74C-ISO6: A successful `Set` or `Del` MUST preserve every other key and its value; `Get` and `List` MUST leave host state unchanged, including when the requested key or config file is absent.
- R-F8C8-WKEV: The `config` command MUST accept exactly `get KEY`, `set KEY=VALUE`, `del KEY`, and `list`; missing or unknown subcommands and wrong argument counts MUST print nothing to stdout, leave host state unchanged, and exit 2 with an opsctl diagnostic followed by one empty line and `see 'opsctl config --help' for usage` on stderr.
- R-2BT9-PV5V: `Get`, `Set`, `Del`, and `List` MUST return filesystem access failures as errors; a `config` action encountering such an error MUST print nothing to stdout, write an opsctl diagnostic naming the failed operation and resolved config path to stderr, and exit 1.

- R-WHD3-JVVI: The `config` command MUST reject invalid subcommand names, argument counts, and invalid `set` key/value arguments before reading the store; `Store.Set` MUST validate its key and value before reading the store, so invalid input retains its specified usage or validation error even when the store is corrupt or inaccessible.
