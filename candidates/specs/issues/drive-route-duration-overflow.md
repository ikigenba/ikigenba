# Drive route duration overflows its required minute count

Filed during the authorized build-spec run on `wip-funnel`, starting at `41b11814fc83497ad282bdeddb449f2c6d23a9eb`. The routes implementer reported this conflict and an independent routes verifier confirmed it on the declared Go toolchain.

Requirements: R-O1CZ-HTUY, R-NU1L-77ES, R-NJ2H-R9QJ.

R-O1CZ-HTUY accepts a duration consisting of decimal digits, an optional fraction, and `s`, with seconds given by `strconv.ParseFloat`. R-NU1L-77ES requires a successful routed outcome whose minute value is `int64(math.Round(sec / 60))`, with empty Error and Why. R-NJ2H-R9QJ requires the nonnil minute value to be nonnegative.

The valid response `{"routes":[{"duration":"999999999999999999999s"}]}` parses to finite `1e21` seconds without error. A temporary Go probe using the required conversion on Go 1.26.5 produced `seconds=1e+21 err=<nil> minutes=-9223372036854775808 nonnegative=false`. The resulting rounded minutes exceed the range of int64. The probe was removed; no test reached Google.

The contract cannot satisfy all three requirements for this input. Rejecting the response as FailShape would contradict the accepted shape and successful conversion requirements; saturating the number would contradict the specified conversion. The build run cannot change the read-only design or choose either behavior on its behalf. Normal representable durations and independent work can still be implemented and verified.

Suggested resolution: replace the affected requirements to bound accepted durations so their rounded minute counts fit int64, and explicitly classify out-of-range durations as FailShape; alternatively revise the numeric representation or conversion rule and its invariant together. Re-mint changed requirement IDs through design authoring, then resume build-spec.
