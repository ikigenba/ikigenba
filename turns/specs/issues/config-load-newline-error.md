# config.Load cannot preserve a newline path in a single-line error

Filed during the user-invoked first `build-spec` run for `turns`, against design commit `47ac54fc548849f93ac7f97b28ccb190aba275d3`. The initial gap was 438 additions. The foundation verifier reported this contradiction, and a fresh root validator independently confirmed it with an executed synthetic API test.

Requirement: `R-61YX-53S9` in `specs/design/D03-paths-and-config.md` requires every non-missing read failure to return a non-nil error whose text is a single line consisting of `could not read '<path>': `, with `<path>` being the file's path, followed by non-empty text.

Related requirements: `R-I8XM-BZ81` declares unrestricted string path fields; `R-ITNW-U2TU` declares the directly callable `config.Load`; `R-IJWP-RWWA` accepts an absolute home independently of other inputs; `R-INKE-X84D` derives the host config path from non-empty `etc`. No requirement excludes a newline from a filesystem path.

## Proof

The first verifier gave a logical construction using a temporary directory path containing a newline: the required literal error prefix necessarily contains that newline. A fresh validator then used `paths.Resolve` with injected absolute temporary home and `etc` ending in `host\nroot`, created the returned `HostConfig` path as a directory, and called both `os.ReadFile` and `config.Load`. Every input was synthetic and confined to `t.TempDir`; no real home, environment, configuration, session files, network or external process was used.

The validator ran a temporary Go overlay test outside the repository from `turns/`:

```sh
GOPROXY=off GOSUMDB=off go test -overlay "$proof_overlay" ./internal/config -run '^TestIndependentNewlinePathProof$' -count=1 -v
```

The proof command exited zero and reported these observations (the quoted `\n` represents a newline byte in the actual path and error):

```text
resolved_host_path="/tmp/TestIndependentNewlinePathProof309488019/001/host\nroot/xdg/turns/config"
direct_read_error="read /tmp/TestIndependentNewlinePathProof309488019/001/host\nroot/xdg/turns/config: is a directory" missing=false
load_error="could not read '/tmp/TestIndependentNewlinePathProof309488019/001/host\nroot/xdg/turns/config': read config: is a directory"
has_required_prefix=true detail_nonempty=true single_line=false
escaped_candidate="could not read '/tmp/TestIndependentNewlinePathProof309488019/001/host\\nroot/xdg/turns/config': is a directory" single_line=true has_required_literal_prefix=false
```

For a path P containing a newline, a string consisting of the required literal prefix plus any non-empty detail necessarily contains a newline. Reading the directory fails with a non-missing error, so the requirement applies. Escaping P produces a single line but no longer substitutes the literal file path. Restricting the caller to paths returned by `paths.Resolve` does not avoid the contradiction: the proof uses such a path.

## Why the build cannot resolve it

The requirement is unsatisfiable for an allowed input. Encoding the path, forbidding newline paths or removing the single-line guarantee changes the read-only contract. The build cannot select one of those changes or claim a test closes this id. Work dependent on this exact diagnostic contract is halted; independently verified work may be preserved as a partial result.

## Suggested resolution

Replace `R-61YX-53S9` outside this build with a newly minted requirement specifying escaped path rendering, such as `strconv.Quote`, or requiring a single-line non-nil error without literal path substitution. Alternatively, remove the single-line guarantee. After resolving and deleting this issue, start a new `build-spec` run to recompute the gap.
