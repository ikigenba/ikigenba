# D3-glob-and-grep

`Glob` and `Grep` are the search tools. Both walk a directory tree under the
root (D1) using **one shared walk policy**, so a file `Glob` lists is a file
`Grep` will search and vice versa. The walk honors the repository's own ignore
files, plus any patterns the consumer adds with `WithSkip`. Matching is done in
Go: `github.com/boyter/gocodewalker` walks, `github.com/bmatcuk/doublestar/v4`
matches `**` globs, and the standard `regexp` package (RE2 syntax) matches
`Grep` patterns. There is no dependency on a `rg` binary.

## The walk policy

Starting at `path` (default: the root), the walk descends recursively and skips:

- anything excluded by a `.gitignore` or `.ignore` file at any level, with git's
  nested semantics;
- anything matched by a `WithSkip` pattern, in gitignore syntax, applied as if
  the patterns were in a `.gitignore` at the search directory and every directory
  beneath it — so `.git` skips every `.git` directory and `*.log` every log file;
- symbolic links to directories (never traversed), and any file whose resolved
  path lies outside the root.

Hidden entries are **not** skipped by default. The harness tools skip them
because ripgrep does, but a consumer of toolkit says what it wants explicitly:
`WithSkip(".git")` for the narrow case, `WithSkip(".*")` for ripgrep's default.
Several `WithSkip` options accumulate.

```go
grep, err := toolkit.Grep(root, toolkit.WithSkip(".git", "node_modules"))
```

## Glob

```go
type globInput struct {
	Pattern string `json:"pattern"        jsonschema:"required,minLength=1,description=..."`
	Path    string `json:"path,omitempty" jsonschema:"description=..."`
}
```

`pattern` is a doublestar glob (`**/*.go`, `src/**/test_*.py`) matched against
each file's slash-separated path relative to the search directory. Results are
absolute paths, one per line, newest modification time first — the harness
order, which puts the file the model most likely wants at the top. Only files
are returned, never directories.

## Grep

```go
type grepInput struct {
	Pattern    string `json:"pattern"               jsonschema:"required,minLength=1,description=..."`
	Path       string `json:"path,omitempty"        jsonschema:"description=..."`
	Glob       string `json:"glob,omitempty"        jsonschema:"description=..."`
	OutputMode string `json:"output_mode,omitempty" jsonschema:"enum=files_with_matches|content|count,description=..."`
	IgnoreCase bool   `json:"-i,omitempty"          jsonschema:"description=..."`
	LineNumber *bool  `json:"-n,omitempty"          jsonschema:"description=..."`
	After      *int   `json:"-A,omitempty"          jsonschema:"minimum=0,description=..."`
	Before     *int   `json:"-B,omitempty"          jsonschema:"minimum=0,description=..."`
	Context    *int   `json:"-C,omitempty"          jsonschema:"minimum=0,description=..."`
	Multiline  bool   `json:"multiline,omitempty"   jsonschema:"description=..."`
	HeadLimit  *int   `json:"head_limit,omitempty"  jsonschema:"minimum=1,description=..."`
}
```

`Grep` keeps the harness's parameter names, flag-shaped ones included, because
the model already knows them. The three output modes are ripgrep's:
`files_with_matches` (the default) lists files, `count` lists `path:count`, and
`content` prints matching lines in the classic `path:line:text` form with
optional context. `-n` is a `*bool` because its default is `true` — a plain bool
could not tell "absent" from "false". The `type` parameter of the harness tool
is deliberately absent: it depends on ripgrep's file-type table, and `glob`
covers the same need. Output is ordered by path so results are deterministic
regardless of walker concurrency. `head_limit` (default 250) bounds the number of
entries, in addition to the character cap every tool has (D1).

```
/repo/main.go:12:	fmt.Println("hello")
/repo/main.go-13-	return
--
/repo/util.go:4:	fmt.Println("world")
```

## REQUIREMENTS

- R-DARJ-4XT5: `Glob` and `Grep` MUST walk the search directory recursively and MUST exclude every entry excluded by a `.gitignore` or `.ignore` file at any level of the walk, applying git's nested-ignore semantics.
- R-DBZF-IPJU: `Glob` and `Grep` MUST exclude every entry matched by a `WithSkip` pattern, interpreting the patterns in gitignore syntax as if they appeared in a `.gitignore` file at the search directory and every directory beneath it, and patterns from several `WithSkip` options MUST accumulate.
- R-DD7B-WHAJ: `Glob` and `Grep` MUST include hidden files and directories (names beginning with `.`) unless an ignore file or a `WithSkip` pattern excludes them.
- R-DEF8-A918: `Glob` and `Grep` MUST NOT traverse a symbolic link to a directory and MUST omit any file whose symlink-resolved path lies outside the root.
- R-DFN4-O0RX: When `path` is absent, `Glob` and `Grep` MUST search from the root; when present it MUST be resolved per D1, and `Glob` MUST return an error naming `path` when the resolved path is not a directory.
- R-DGV1-1SIM: The `Glob` tool's schema MUST declare exactly the properties `pattern` (string, required, `minLength` 1) and `path` (string, optional).
- R-DI2X-FK9B: `Glob` MUST match `pattern` as a doublestar glob against each regular file's slash-separated path relative to the search directory, and MUST return only files, never directories.
- R-DJAT-TC00: `Glob` MUST return the matching files' absolute paths, one per line, ordered by modification time newest first with ties broken by path ascending.
- R-DKIQ-73QP: `Glob` MUST return the text `No files found` when no file matches.
- R-DLQM-KVHE: `Glob` MUST return an error naming `pattern` when `pattern` is not a valid doublestar glob.
- R-DMYI-YN83: The `Grep` tool's schema MUST declare exactly the properties `pattern` (string, required, `minLength` 1), `path` (string, optional), `glob` (string, optional), `output_mode` (string, optional, `enum` `files_with_matches`|`content`|`count`), `-i` (boolean, optional), `-n` (boolean, optional), `-A` (integer, `minimum` 0, optional), `-B` (integer, `minimum` 0, optional), `-C` (integer, `minimum` 0, optional), `multiline` (boolean, optional), and `head_limit` (integer, `minimum` 1, optional).
- R-DPEB-Q6PH: `Grep` MUST compile `pattern` with Go's `regexp` package (RE2 syntax), case-insensitively when `-i` is `true`, and MUST return an error naming `pattern` when it does not compile.
- R-DQM8-3YG6: When `glob` is present, `Grep` MUST search only files whose slash-separated path relative to the search directory matches `glob` as a doublestar glob, and MUST return an error naming `glob` when it is not a valid doublestar glob.
- R-DRU4-HQ6V: When the resolved `path` is a regular file, `Grep` MUST search only that file; when it is a directory, `Grep` MUST search the files the walk policy yields beneath it.
- R-DT20-VHXK: `Grep` MUST skip any file whose first 8192 bytes contain a NUL byte, treating it as binary.
- R-DU9X-99O9: With `output_mode` absent or `files_with_matches`, `Grep` MUST return the absolute paths of files containing at least one match, one per line, ordered by path ascending.
- R-DVHT-N1EY: With `output_mode` `count`, `Grep` MUST return one line `<absolute path>:<count>` per file containing at least one match, where count is the number of matching lines, ordered by path ascending.
- R-DWPQ-0T5N: With `output_mode` `content`, `Grep` MUST return each matching line as `<absolute path>:<line>:<text>` when `-n` is absent or `true` and as `<absolute path>:<text>` when `-n` is `false`, with files ordered by path ascending and lines within a file in ascending line order.
- R-DXXM-EKWC: With `output_mode` `content`, `Grep` MUST include `-B` lines before and `-A` lines after each matching line, with `-C` setting both when the specific flag is absent, rendering a context line as `<absolute path>-<line>-<text>` (or `<absolute path>-<text>` when `-n` is `false`), and MUST separate non-adjacent groups of lines with a line containing only `--`.
- R-DZ5I-SCN1: `Grep` MUST ignore `-n`, `-A`, `-B`, and `-C` when `output_mode` is not `content`.
- R-E0DF-64DQ: When `multiline` is `true`, `Grep` MUST match `pattern` against the whole file content with `.` matching newlines, MUST count and report a match at the line where it begins, and in `content` mode MUST render every line the match spans.
- R-EJVT-AG8U: `Grep` MUST return at most `head_limit` entries (default 250) — lines of output in `content` mode, files in the other modes — and when entries were dropped MUST append a final line `[truncated to first N entries]` where N is the limit.
- R-EL3P-O7ZJ: `Grep` MUST return the text `No matches found` when no file contains a match.
