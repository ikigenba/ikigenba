# D5-input-handling

How each mode takes input and validates it.

**Decode input.** Positional `ID` arguments take precedence; stdin is read
only when there are no positionals, tokenized on whitespace (spaces, tabs,
newlines all delimit). Each input token yields exactly one line, in input
order within its stream:

- a valid id → its instant on stdout, formatted as UTC
  `2006-01-02T15:04:05.000Z` (millisecond precision, `Z` suffix, regardless of
  the `TZ` environment),
- a malformed id → `idgen: <error naming the token>` on stderr.

Partial failure is tolerated: the batch continues past malformed ids, and the
exit code becomes `1` if any input failed. No input at all (no positionals,
empty stdin) is vacuous success: no output, exit `0`.

**Mint validation.** Checked before minting begins; each failure reports
`idgen: <problem>` to stderr and exits `2` — never a silent non-zero exit:

- `--prefix` must satisfy `idgen.ValidPrefix` (non-empty, letters and digits
  only — a separator character would corrupt the decode grammar; this check is
  what guards it, since `MintAt` trusts its precondition, D2). `cli` owns the
  decision to check here and the message it reports; `idgen` owns the grammar
  itself (D2), so `cli` does not restate the character class. Message contains
  `invalid prefix`.
- `--number` must be greater than 0. Message contains `--number must be > 0`.

## REQUIREMENTS

- R-TCHA-H35T: Decoding positional arguments MUST print one UTC line per id on stdout, in argument order, formatted `2006-01-02T15:04:05.000Z`.
- R-TDP6-UUWI: Decoding ids from stdin separated by mixed whitespace (spaces, tabs, newlines) MUST produce output identical to passing the same ids as positionals.
- R-TEX3-8MN7: When positional ids are present, decode MUST NOT read stdin at all (verified with a stdin reader that fails the test if its `Read` is ever called).
- R-THCW-064L: A decode batch containing a malformed token MUST still decode the valid ids in order, report the bad token by name on stderr, and exit 1.
- R-TIKS-DXVA: Decode with no positionals and empty stdin MUST produce no output and exit 0.
- R-TJSO-RPLZ: Round-trip through `Run`: `--decode` of a freshly minted id MUST return the minting instant at millisecond precision.
- R-TL0L-5HCO: A `--prefix` value that is empty, whitespace, or contains any character outside `[A-Za-z0-9]` MUST exit 2 with stderr containing `invalid prefix`.
- R-626N-3DAD: The `--prefix` accept/reject decision MUST agree with `idgen.ValidPrefix` — for a PRNG-seeded sample of candidate prefixes spanning valid runs, empty strings, and strings bearing separator, punctuation, and non-ASCII characters, a mint invocation MUST exit 2 with `invalid prefix` exactly when `ValidPrefix` rejects the candidate and mint successfully exactly when it accepts.
- R-TM8H-J93D: A `--number` value of 0 or below MUST exit 2 with stderr containing `--number must be > 0`.
- R-TNGD-X0U2: Decode output MUST be UTC regardless of the `TZ` environment variable (verified with a non-UTC `TZ` set).
