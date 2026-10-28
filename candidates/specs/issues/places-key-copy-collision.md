# Places secrecy and fixed failure copy conflict

Filed during the user-invoked `build-spec` run on `wip-funnel`, starting at `12a2f3d9df953cbe1f15174be772c585236d6356`. The Places verifier raised the concern; a fresh read-only verifier independently confirmed the exact-value contradiction below.

## Requirements and friction

R-05D8-AXRE forbids any returned string field from holding a nonempty `places.Client.Key`. R-WLKP-KHHP requires a failed HTTP status to return `Error = places.FailStatus`, and R-ZPIJ-BX4D requires that copy constant to be nonempty.

Set the client's key to `places.FailStatus` and return HTTP 403 from the fake transport for a valid finite circle. The required error then exactly equals the nonempty key. Changing the copy cannot resolve this: the same construction uses whatever nonempty value the constant has.

The current contract does not exempt static copy. A valid-Google-key syntax precondition also cannot be inferred to exclude artificial keys: R-WLKP-KHHP itself supplies a synthetic key equal to a returned status word and requires suppression of that coincident status. This is a conflict in the quantified contract, not evidence that an actual credential is leaked by the implementation.

## Evidence

The fresh verifier supplied `Circle{Lat: 45, Lon: -93, Radius: 50}`, set `Key` by referencing the copy constant, and injected a transport returning HTTP 403 with an empty JSON object. Its test-only overlay ran from `candidates/`:

```text
GOPROXY=off go test -overlay=/tmp/scratch.5gLNoq -run '^TestFreshVerifierExactCopyKey$' -v ./internal/places
```

The probe exited 0 and established `Error == Key == FailStatus`, `Sent = 1`, `Failed = 1`, and empty `Status`. Its source was `/tmp/scratch.n62ud6`; the independent report was `/tmp/scratch.K54xMM`. All probe files were outside the checkout, and no Google request was made.

An earlier nonfinite-coordinate allegation was independently rejected because NaN and infinite coordinates do not describe a geographic centre. It is not an additional blocker.

## Suggested resolution

Outside this build run, revise the secrecy requirement to allow fixed copy produced independently of the key while forbidding disclosure through transport errors, response fields and status values. Alternatively declare a credential and test-key domain that excludes every fixed failure constant. The build cannot change the read-only requirement; R-05D8-AXRE remains unproved.
