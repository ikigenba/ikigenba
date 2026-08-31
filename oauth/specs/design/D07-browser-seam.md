# D07-browser-seam

Package `internal/browser` opens the authorize URL in the user's browser. It is
the smallest seam in the project and exists almost entirely so that the rest of
the program can be tested without a browser ever appearing.

```go
package browser

// Launcher opens a URL in the user's browser.
type Launcher interface {
    Open(url string) error
}

// New returns the platform browser launcher.
func New() Launcher
```

`Launcher` is the injected dependency `cli.Deps` carries (D01); `New` is what
`cmd/oauth` wires into it (D11). Every test above this package substitutes a
fake that records the URL it was handed and returns whatever error the case
calls for.

**Platform selection is a build-tag decision, not a runtime branch.** There is
one `New` per platform: linux launches `xdg-open`, darwin launches `open`, and
every other platform gets a launcher whose `Open` reports that the platform is
unsupported. A runtime `switch runtime.GOOS` would put all three behaviors in
every binary and make the unreached ones untestable in the build that ships;
separate files under build tags mean each binary contains exactly the launcher
it can use.

**Processes are started through an injected command factory.** The package
never spawns a real browser in a test — a passing test suite that opens a
window per case is not a test suite anyone will run twice, and on a headless
build host `xdg-open` would fail for reasons unrelated to what is being
verified. The factory is what makes "linux launches `xdg-open` with exactly
this URL and nothing else" a deterministic assertion instead of an observation
about the developer's desktop.

**Launching is fire-and-forget, and that is contract.** `Open` returns as soon
as the child process has been started; it never waits for the browser to exit.
Waiting is not an option worth having: `xdg-open` may block for as long as the
browser window lives, and the login flow needs to be listening for the callback
long before then. Two consequences follow and are stated rather than
discovered. First, a launcher that starts successfully and *then* fails — no
browser installed behind the handler, a window that never appears — is
invisible to this package; only a failure to start is reported. Second, the
child is not reaped by this program. That is why the launcher's error is not
the flow's source of truth about whether the user can authenticate: `cli`
treats a launch failure as non-fatal, prints a note, and keeps waiting on the
callback (D11), because the authorize URL was already printed to stderr and the
user can open it themselves. The URL is presented first and launched second for
exactly this reason.

**On the unsupported platform.** `Open` returns an error rather than panicking
or silently succeeding. Silently succeeding would be the worst of the three: the
user would be told nothing, no browser would open, and the process would wait
out its full timeout on a callback that could never arrive. GoReleaser builds
only linux and darwin (see `AGENTS.md`), so the `!linux && !darwin` file is
never shipped — it is kept so the package still compiles and type-checks
everywhere, and the project's gates cross-compile-vet it (`GOOS=windows go vet
./...`) precisely so an unshipped file cannot rot unnoticed.

**A note on how thoroughly the platform requirements are proven.** The linux
and darwin assertions below live in build-tagged test files and therefore run
only on their own platform; on a linux build host the darwin requirement is
compiled and type-checked by the `GOOS=darwin go vet ./...` gate but not
executed. That is a weaker standard of proof than the rest of this spec enjoys,
and it is accepted here because the assertion is a single command name — the
narrowest claim in the project, and one the gates still keep honest against
compilation rot.

## REQUIREMENTS

- R-GTP0-55S7: On linux, the `Launcher` returned by `New` MUST, when `Open` is called, start the command `xdg-open` with the supplied URL as its sole argument, verified through an injected command factory rather than by spawning a process.
- R-GUWW-IXIW: On darwin, the `Launcher` returned by `New` MUST, when `Open` is called, start the command `open` with the supplied URL as its sole argument, verified through an injected command factory rather than by spawning a process.
- R-GW4S-WP9L: On a platform that is neither linux nor darwin, `Open` MUST return a non-nil error and MUST NOT attempt to start any command.
- R-GYKL-O8QZ: `Open` MUST return the failure reported by starting the command, so that a command which cannot be started yields a non-nil error from `Open`.
- R-GZSI-20HO: `Open` MUST return once the child process has been started and MUST NOT block on the child's completion, verified with a fake command that starts successfully and whose child never exits.
