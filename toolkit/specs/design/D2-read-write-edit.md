# D2-read-write-edit

`Read`, `Write`, and `Edit` are the file tools. Each takes a root (D1) that
confines every `file_path` it is given, and each mirrors the Claude Code harness
tool of the same name: `Read` returns numbered lines the model can cite and page
through, `Write` replaces a whole file, and `Edit` performs one exact-match
substitution. The three are stateless; the harness's read-before-write gate is
the consumer's to enforce if it wants one.

## Read

```go
type readInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=..."`
	Offset   *int   `json:"offset,omitempty"  jsonschema:"minimum=1,description=..."`
	Limit    *int   `json:"limit,omitempty"   jsonschema:"minimum=1,description=..."`
}
```

`Read` returns the file in `cat -n` form — a right-aligned line number, a tab,
the line — starting at `offset` (a 1-based line number, default 1) and returning
at most `limit` lines (default 2000). The numbering is the point: it is what lets
the model cite `file:line` and page with `offset`. When the file has lines
beyond the returned range, a trailer says which lines were shown of how many.
Very long lines are cut at 2000 characters with a marker, so one minified file
cannot consume the whole result cap. Files that are not text — a NUL byte, or
bytes that are not valid UTF-8 — are refused with an error rather than dumped;
document and image extraction belong to the `ocr` sibling (agentkit D0). Optional
integers are pointers because a JSON schema has no `default` keyword and the
promoted `zero-value-as-absent-sentinel` rule forbids using `0` to mean absent.

```
     1	package main
     2	
     3	func main() {}
[showing lines 1-3 of 40]
```

## Write

```go
type writeInput struct {
	FilePath string `json:"file_path" jsonschema:"required,description=..."`
	Content  string `json:"content"   jsonschema:"required,description=..."`
}
```

`Write` replaces the file's whole content, creating missing parent directories
on the way. A new file is created with mode `0o644` (subject to the process
umask); an existing file keeps its mode. `content` is required but may be empty,
so a model can truncate a file on purpose.

## Edit

```go
type editInput struct {
	FilePath   string `json:"file_path"  jsonschema:"required,description=..."`
	OldString  string `json:"old_string" jsonschema:"required,minLength=1,description=..."`
	NewString  string `json:"new_string" jsonschema:"required,description=..."`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=..."`
}
```

`Edit` replaces `old_string` with `new_string`. The match is exact and
byte-for-byte — no trimming, no regular expression — and it must be **unique**
unless `replace_all` is set, because an ambiguous edit silently applied to the
wrong place is the worst failure a file tool can have. The error texts are
written for the model: they say how many occurrences were found so it can widen
its `old_string` and try again. `replace_all` is a plain `bool`: the schema
declares it optional, and absent and `false` mean the same thing, so the zero
value is not a sentinel.

## REQUIREMENTS

- R-CMDJ-HIZ9: The `Read` tool's schema MUST declare exactly the properties `file_path` (string, required), `offset` (integer, `minimum` 1, optional), and `limit` (integer, `minimum` 1, optional).
- R-CNLF-VAPY: `Read` MUST render each returned line as its 1-based line number right-aligned in a 6-character field, a tab, and the line's content without its line terminator, one rendered line per source line.
- R-COTC-92GN: `Read` MUST return the source lines from `offset` through `offset+limit-1` inclusive, treating an absent `offset` as 1 and an absent `limit` as 2000.
- R-CQ18-MU7C: When the file has lines after the last returned line, `Read` MUST append a final line `[showing lines A-B of N]` where A and B are the first and last returned line numbers and N is the file's line count; when no lines follow, `Read` MUST NOT append it.
- R-CR95-0LY1: `Read` MUST render a source line longer than 2000 characters as its first 2000 characters followed by ` [line truncated]`.
- R-CSH1-EDOQ: `Read` MUST return the text `<file_path> is an empty file`, with `file_path` as given, for a zero-length file.
- R-CTOX-S5FF: `Read` MUST return an error naming `file_path` when the resolved path does not exist or is a directory.
- R-CUWU-5X64: `Read` MUST return an error naming `file_path` when `offset` is greater than the file's line count for a non-empty file.
- R-CW4Q-JOWT: `Read` MUST return an error naming `file_path` when the file's content contains a NUL byte or is not valid UTF-8, without returning any of the content.
- R-CXCM-XGNI: The `Write` tool's schema MUST declare exactly the properties `file_path` (string, required) and `content` (string, required).
- R-CYKJ-B8E7: `Write` MUST create every missing parent directory of the resolved path and then write `content` as the file's entire content, creating a new file with mode `0o644` and preserving the mode of an existing file.
- R-CZSF-P04W: `Write` MUST return the text `wrote N bytes to <file_path>` on success, where N is the byte length of `content` and `file_path` is as given.
- R-D10C-2RVL: `Write` MUST return an error naming `file_path` when the resolved path is an existing directory.
- R-D288-GJMA: The `Edit` tool's schema MUST declare exactly the properties `file_path` (string, required), `old_string` (string, required, `minLength` 1), `new_string` (string, required), and `replace_all` (boolean, optional).
- R-D4O1-833O: `Edit` MUST return an error when `old_string` equals `new_string`, without modifying the file.
- R-D5VX-LUUD: `Edit` MUST return an error naming `file_path` when the resolved path does not exist or is a directory.
- R-D73T-ZML2: `Edit` MUST locate `old_string` by exact byte-for-byte comparison and MUST return an error containing `old_string not found` when the file contains no occurrence, without modifying the file.
- R-D8BQ-DEBR: When `replace_all` is absent or `false` and the file contains more than one occurrence of `old_string`, `Edit` MUST return an error stating the number of occurrences found, without modifying the file.
- R-D9JM-R62G: `Edit` MUST replace the single occurrence when `replace_all` is absent or `false`, or every occurrence when `replace_all` is `true`, rewrite the file preserving its mode, and return the text `replaced N occurrence(s) of old_string in <file_path>` with N the number replaced and `file_path` as given.
