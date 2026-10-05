# Store timestamp truncation contradicts run validity

Filed during the human-authorized build-spec run on `wip-scripts`, against design at `a3b379459e93d38727768bc3e932f340149f6747`. The store verifier and an independent discovery agent reproduced this through public store APIs without changing the checkout.

## Requirements

R-8RTP-4CV2, R-LO6Q-FU9Q and R-KTP8-VKQD form an unsatisfiable contract. R-LQMJ-7DR4 has the independently reproduced analogous FinishRun contradiction through the same validity definition. These four ids remain unclosed.

R-8RTP-4CV2 accepts a failed run whose raw Started and Finished are nonzero and whose second-truncated Finished is not before second-truncated Started. Set both to `time.Time{}.Add(500*time.Millisecond)`, use an existing script and unique valid run id, and set the remaining fields to a valid failed record. Both raw timestamps are nonzero, and both truncated timestamps are equal.

R-LO6Q-FU9Q requires AddRun to accept that record with a nil error and return its timestamps converted to UTC and truncated to seconds. The returned Finished must therefore be the zero time. R-KTP8-VKQD requires every successfully returned run to have zero Finished exactly when its status is running. The record is failed, so the requirements contradict.

Rejecting the record violates mandatory acceptance; retaining fractions violates truncation; changing status or rounding up violates the required returned record. The build cannot choose a precedence or change the read-only design.

## Evidence

An external temporary Go overlay exercised Open, Create, AddRun and RunByID with an isolated in-memory database. Both reviewers independently observed this failure. The store source checked had SHA256 `e253e7b5b594d6ec489a5287977a2641cb5d83f3a4a2825818bff34a96426ee2`.

Command: `go test -overlay <external scratch overlay> -run '^TestIndependentZeroTimeTruncation$' -count=1 -v ./internal/store`.

```text
input: started=0001-01-01T00:00:00.5Z finished=0001-01-01T00:00:00.5Z nonzero=true truncZero=true
AddRun: err=<nil> status=failed started=0001-01-01T00:00:00Z finished=0001-01-01T00:00:00Z finishedZero=true
RunByID: err=<nil> status=failed finishedZero=true
--- FAIL: TestIndependentZeroTimeTruncation
```

Temporary probes were removed and were not committed. Ordinary-case passing tests cannot prove the contradictory requirements.

A second independent public-API overlay confirmed FinishRun after the unrelated store repairs. A running record with Started equal to `time.Time{}.Add(-time.Second)` accepts a valid exited ending whose Finished is `time.Time{}.Add(500*time.Millisecond)`. R-LQMJ-7DR4 mandates nil success and truncation; the returned exited run has zero Finished, contradicting R-KTP8-VKQD. Command: `go test -overlay <external scratch overlay> ./internal/store -run TestProbeFinishNearZero -count=1`. The assertion failed with `FinishRun mandatory success returns exited run with zero Finished`. Source SHA256: `0ec113e3a4e12e476bcdaafbe5af97d6e421d6e7c243023b78c7c7bc154c1af2`. The probe was removed. R-8T1L-I4LR provides no refusal: the ending is valid and its normalized finish is after the stored start.

## Suggested resolution

Revise and re-mint R-8RTP-4CV2 so an ended Finished must remain nonzero after UTC conversion and second truncation, for both valid failed records and valid endings. This preserves the mandatory normalization and successful-return invariant, and addresses the analogous FinishRun case. A human must settle the contract before those ids can be closed. Independent build work continues under the user's explicit instruction.
