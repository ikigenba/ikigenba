# Spellcheck rejects a mandatory requirement identifier

Filed during the human-authorized build-spec run on `wip-sites`. The web implementer and a fresh independent verifier reproduced this tooling blocker at commit `f6e967e0c2ee9e8f0e91cc1c12d221a3a8553015`. Independent work continues as explicitly authorized by the user.

## Requirement involved

`R-F04K-TWPO` requires shared-file responses from handlers in one process to have identical bodies and ETags for the same path without conditional or range headers. Its behavior is tested by use through `web.Handler`.

The canonical gap requires the identifier to appear verbatim in a test. Gate 5 rejects that identifier as misspelled. The design and AGENTS.md are read-only; the run cannot change the identifier, add a suppression, or disable a linter to pass.

## Evidence

With `GONOSUMDB=github.com/ikigenba/ikigenba` exported and `GOPROXY=off`, the exact declared gate exits 1:

```sh
GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners
```

The same configured command limited to `./internal/web/...` independently exits 1 with only this finding:

```text
internal/web/static_test.go:12:12: `TWPO` is a misspelling of `TWO` (misspell)
        // R-F04K-TWPO
                  ^
1 issues:
* misspell: 1
```

The installed golangci-lint version is `2.12.2`. The verifier inspected its installed source: the default misspell mode scans raw source, including strings, splits the hyphenated identifier into words, and maps `twpo` to `two`. Ordinary comment and string tags encounter the same problem. URL/path exclusions do not supply a legitimate fix; inventing a link to bypass the finding would hide it.

Verified SHA-256 values: `.golangci.yml` `36608c44249ecbe4cf3cfffafd9ff549d908e9887cca533e413b39f682958930`; `internal/web/static_test.go` `6c3f78482a37eea8369506a32f4f60427f05cf6c252e2e52b63ab8d92d630025`; `specs/design/D06-pages.md` `5827b9b585b02fe4f5767e2c585d189e9b7e47c382231576f9440399aa8ac936`.

## Resolution

A human must establish a spelling policy that treats opaque requirement identifiers appropriately, or replace this identifier through the design workflow. Until then, its behavioral test remains, but its identifier is left out of test tags so independent phases can pass the declared gates. This requirement remains mechanically open; the run does not claim completion.
