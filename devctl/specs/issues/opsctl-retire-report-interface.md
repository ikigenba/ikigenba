# opsctl retire report interface is unavailable

## Filing context

The D06 corrective build for `space destroy` cannot verify or implement the
retire success report required by R-92KI-ZUQK without inventing an external
wire format.

## Requirement

- R-92KI-ZUQK requires the backed-up names to come only from a published
  opsctl interface and requires unknown report data to fail before instance
  termination.

## Friction

`internal/space/destroy.go` currently declares its own private JSON shape:

```go
type retireReport struct {
	BackedUp []string `json:"backed_up"`
}
```

No in-scope published opsctl interface defines that shape, and no installed
`opsctl` is available to query. Accepting this private schema could reject a
real successful retire or accept data that opsctl does not promise to emit.
Changing it to another schema would merely replace one guess with another.

## Evidence

From the devctl project directory, a targeted search outside the private
implementation and its test found no report-schema reference:

```text
$ rg -n --glob '!specs/design/**' --glob '!internal/space/destroy.go' --glob '!internal/space/destroy_test.go' 'backed_up|opsctl retire|retire.*json|retire.*output|Retire' .
./specs/stories/S2-space-lifecycle.md:1064:host take its final backup with `sudo opsctl retire` over ssh, which stops
./specs/stories/S2-space-lifecycle.md:1100:- `sudo opsctl retire` has been run over ssh and exited 0, so
./specs/stories/S2-space-lifecycle.md:1130:devctl: retire: ssh ec2-user@18.220.10.5 sudo opsctl retire: exit status 1
./specs/stories/S2-space-lifecycle.md:1143:- As for the previous story, and `opsctl retire` on the host exits non-zero.
./internal/space/core_test.go:110:func TestRetireStateError(t *testing.T) {
./internal/space/core_test.go:112:\terr := &RetireStateError{
./internal/space/core.go:112:// RetireStateError reports an instance state that prevents retirement.
./internal/space/core.go:113:type RetireStateError struct {
./internal/space/core.go:121:func (e *RetireStateError) Error() string {
./internal/space/core.go:126:func (e *RetireStateError) ExitCode() int { return 1 }
./internal/space/core.go:129:func (e *RetireStateError) Detail() string {

$ command -v opsctl
<no output; exit 1>
```

The references establish that the command is invoked and can fail, but none
publishes its successful stdout contract or a machine-readable report schema.

## Why this is unresolvable in-role

The build may consume a sibling project only through an installed tool's
published interface and may not inspect sibling internals. Neither source of
interface truth is available here, so the builder cannot prove a parser
against the required external contract.

## Suggested resolution

Publish the successful `opsctl retire` report format as an external interface
available to devctl (including field names, encoding, validation rules, and
how host and app backups are represented), then make that interface available
to the build. Alternatively, revise the design so devctl obtains the names
through an already published interface.
