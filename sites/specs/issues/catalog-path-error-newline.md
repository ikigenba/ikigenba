# Catalog path errors have conflicting text requirements

Filed during the human-authorized build-spec run on `wip-sites`, starting at commit `2210af043014792cac736b08968ce21fd1bbdde6`. The catalog reviewer and a second independent reviewer reproduced the contradiction outside the checkout. Independent work continues as explicitly authorized by the user.

## Requirements involved

`R-1DDR-3136` requires every database `Source` other than the empty string and `:memory:` to be an ordinary filesystem path. Such paths can contain newline characters.

`R-UDRH-D0CD` requires a parent-creation failure to return exactly the `Error()` text of `os.MkdirAll` called with permissions `0700` for the supplied parent path, with nothing added. The same requirement says every error returned by `Open` must contain no newline character.

## Evidence

Both reviewers independently created a regular file whose name contained an actual newline, then called `os.MkdirAll` for a child directory beneath it, in temporary directories outside the repository. The second probe exited zero and printed these values using Go `%q` escaping:

```text
parent="/tmp/scratch.1073634298/blocked\nparent/child"
error_type=*fs.PathError
error="mkdir /tmp/scratch.1073634298/blocked\nparent: not a directory"
has_newline=true
```

The displayed `\n` represents an actual newline in the path and error. A `Source` beneath that parent is an ordinary filesystem path whose parent cannot be created. Returning the exact standard-library error necessarily violates the newline prohibition. Escaping or replacing the newline necessarily violates the exact-text clause. Rejecting the path in advance would not fulfill the ordinary-path contract and its specified parent-creation failure behavior.

## Resolution

A human must choose how path errors should be reported: preserve the standard-library text or normalize it for a one-line diagnostic, and revise the contract with a newly minted requirement. This run cannot change the read-only design or tag a test as proving both incompatible clauses of `R-UDRH-D0CD`.
