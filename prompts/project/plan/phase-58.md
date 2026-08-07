# Phase 58 — Prove the framing prompt's three unproven claims, and drop one retired tag

*Realizes design Decision 21 (slice: `R-6AUG-NHQY`), Decision 23 (`R-6I5U-Y474`),
and Decision 26 (slice: `R-FEGC-LVD7`).*

Three Verification ids assert what the assembled conversation `System` string
says, and none of them is currently proven. D19's `R-ZK3C-F6XI` is the only
framing-prompt claim with a test today
(`TestFramingPromptNamesNoLoaderOrIndividualService` in
`internal/runner/runner_test.go`), and it asserts only the *negative* half — no
`load_tools`, no individual service name. The three positive claims each
Decision added on top of it — that `fetch` is named and its content-URL role
stated (D21), that the three poppler binaries are named (D23), and that the six
file tools plus the file-share guidance and the verbatim no-network sentence are
present (D26) — have never been pinned. A rewrite of `framingPrompt` that
dropped any of them would leave the suite green.

**The seam.** `internal/runner/framing_prompt.go` holds the `framingPrompt`
constant; `internal/runner/runner.go`'s `buildSystemPrompt(sysPrompt string)`
assembles what `Conversation.System` receives (the constant alone when the
prompt pins no system text, otherwise the constant, a blank line, and the
prompt's own system text). Both are unexported in `package runner`, and the
existing framing test lives in `internal/runner/runner_test.go` (also
`package runner`), so the new tests go there beside it.

Each Decision's wording says "the framing prompt **sent as the conversation
`System`**". Assert against `buildSystemPrompt("")` and `buildSystemPrompt(<a
non-empty prompt-specific system text>)`, not against the bare constant: the
assembled string is the claim's real subject, and driving both branches proves
the framing text survives composition rather than only that a constant contains
some substrings.

End state — `internal/runner/runner_test.go` additionally holds three
clearly-named tests:

- **`R-6AUG-NHQY` (D21).** The assembled `System` names `fetch` among the
  sandbox tools and states that it takes a content URL and writes the bytes into
  the sandbox — assert on the load-bearing terms of that sentence (the tool
  name, the content-URL role, and that the bytes land in the sandbox), not on
  one long verbatim sentence that any rewording would break. The test also
  re-asserts the composed constraint the id names: no `ikigenba_` prefix and no
  individual service name in the assembled string.
- **`R-6I5U-Y474` (D23).** All three of `pdftotext`, `pdftoppm`, and `pdfinfo`
  appear in the assembled `System`, and the claim places them in Bash. Assert
  each of the three separately so dropping one fails; the id's "while still
  naming no individual suite service" half is asserted too.
- **`R-FEGC-LVD7` (D26).** The assembled `System` names all six file tools
  (list, get, put, delete, move, mkdir — as the framing text spells them),
  states the file-share guidance (the share as the durable shared store versus
  the prompt-private run folder, with the file tools as the channel between
  them), contains the sentence `You have NO network access from bash`
  **verbatim**, and contains no individual service name — in particular not
  `dropbox`, the service actually backing the share, which is the whole point of
  the constraint.

The no-individual-service assertion these three share already exists in
`TestFramingPromptNamesNoLoaderOrIndividualService` under `R-ZK3C-F6XI`. Leave
that test exactly as it is — it is D19's claim about the *constant*, and it must
not be folded into or replaced by any of the three new tests. Sharing a small
unexported helper between them is fine; sharing a tag is not.

**One deletion.** `internal/prompt/content_test.go`'s
`TestRunContentRouteUsesChassisLoopbackGuard` carries two tags:
`// R-6DA9-F18C` and `// R-BI5J-4GM6`. `R-6DA9-F18C` was D22's identity-header
404 guard; D27 moved that guard to the chassis `Router.HandleLoopback` and
explicitly retired the id, replacing it with `R-BI5J-4GM6`, which now tags the
same test. The id belongs to no current Decision — it is a stale tag left behind
by the retirement, not a behavior missing from design. **Delete only the
`// R-6DA9-F18C` comment line.** The test itself and its `R-BI5J-4GM6` tag stay:
the behavior is current and still proven, under its current id.

No non-test source changes are expected. If the framing text turns out not to
support an assertion a Decision requires, that is a spec finding to report — do
not weaken the assertion, and do not edit `framingPrompt` to make a test pass.

**Done when:**

- `R-6AUG-NHQY` — a clearly-named test asserts the assembled `System` from
  `buildSystemPrompt` names `fetch` and states its content-URL-to-sandbox-file
  role while naming no individual service; the id appears verbatim as a
  `// R-6AUG-NHQY` tag on that test.
- `R-6I5U-Y474` — a clearly-named test asserts `pdftotext`, `pdftoppm`, and
  `pdfinfo` are each present in the assembled `System` and claimed as Bash
  tooling; the id appears verbatim as a `// R-6I5U-Y474` tag on that test.
- `R-FEGC-LVD7` — a clearly-named test asserts the six file tools, the
  file-share guidance, the verbatim `You have NO network access from bash`
  sentence, and the absence of any individual service name (including
  `dropbox`) in the assembled `System`; the id appears verbatim as a
  `// R-FEGC-LVD7` tag on that test.
- `internal/prompt/content_test.go` no longer contains the string
  `R-6DA9-F18C`, and `TestRunContentRouteUsesChassisLoopbackGuard` still
  carries its `// R-BI5J-4GM6` tag and still passes. From `prompts/`, this
  prints exactly `0`:

  ```
  grep -rc 'R-6DA9-F18C' --include='*_test.go' --exclude-dir=project . | grep -cv ':0$'
  ```

- All three new tests are genuine — each fails when its subject is broken
  (verify by temporarily deleting the corresponding sentence or term from
  `framingPrompt`, seeing exactly that test fail, then restoring). No `t.Skip`,
  no test that passes vacuously, no assertion satisfied by an empty string.
- The suite is green per design's *Conventions*, from `prompts/`:
  `go build ./...` exits 0, `go test ./...` passes with no failures and no
  `SKIP`, and `gofmt -l .` prints nothing.
- From `prompts/`, all three ids are tagged in **test** files rather than only
  in this spec — this command prints exactly `3`:

  ```
  grep -rhoE 'R-6AUG-NHQY|R-6I5U-Y474|R-FEGC-LVD7' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l
  ```
