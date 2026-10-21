# Drive-route distance can exceed its result representation

Filed during the user-authorized build-spec run on `wip-funnel`, starting at `ecc38fcf4b88ad44e29dba03f114732b4fcc49ed`. An independent implementation reviewer found the distance rejection, and a fresh verifier confirmed the contract contradiction.

## Requirements and friction

R-Y5FY-TBFQ accepts any whole nonnegative JSON `distanceMeters`, without a bound. R-NU1L-77ES requires the corresponding rounded miles, while R-N83E-BC2A fixes `Outcome.Miles` as `*float64`. A distance consisting of 400 decimal nines is valid JSON and an exact nonnegative integer, but its miles exceed float64 even if division occurs in arbitrary precision first.

R-6GGQ-ZI8R likewise fixes `RouteFound.Miles` as `*float64`. R-6SNQ-T7NP requires copying the outcome into that value and returning its `json.Marshal` result; R-6TVN-6ZEE requires that result to validate for every qualifying Route result. Positive infinity satisfies R-NJ2H-R9QJ's nonnegative miles invariant but cannot marshal as JSON.

Rejecting the distance violates R-Y5FY-TBFQ; saturating it violates R-NU1L-77ES; returning infinity prevents the required serialization. This cannot be resolved within a build run, which treats design as read-only. The affected initial gap ids are R-Y5FY-TBFQ and R-NU1L-77ES.

## Evidence

The reviewer used an injected fake transport and a temporary Go test overlay. Routing response `{"routes":[{"duration":"60s","distanceMeters":1e309}]}` returned `FailShape`, nil route, `Failed=1`, and cost 5000000, despite the accepted distance shape. No request reached Google.

A fresh verifier ran a standalone temporary Go probe with `GO111MODULE=off GOPROXY=off GOSUMDB=off go run` using 400 decimal nines as the distance:

```text
validJSON=true integer=true nonnegative=true digits=400
ParseFloatInf=true ParseFloatError=true exactMilesExceedFloat64=true
roundedMiles=+Inf atLeastZero=true marshalBytes="" marshalError=json: unsupported value: +Inf
```

The relevant contracts are in `specs/design/D17-drive-route.md` and `specs/design/D16-enrichers.md`. Temporary probes were removed; no dependency or contract was changed.

## Suggested resolution

Replace R-Y5FY-TBFQ with an explicitly bounded distance acceptance rule, classifying excessive distances as `FailShape`. Alternatively change the miles representation and serialization contract to support arbitrarily large distances.
