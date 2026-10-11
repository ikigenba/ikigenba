# Unrepresentable provider expiry

Filed during the user-invoked connections build-spec run, initially at commit 505678732f2d666c56e965332f511079faf9ce4c. The foundation verifier and a fresh independent verifier confirmed this contradiction.

Requirements involved: R-A4Z4-7FIJ, R-XDNO-E2L8, R-XEVK-RUBX, R-9MOM-GVE4 and R-AH64-14XH in D05-providers.md.

R-A4Z4-7FIJ admits an `expires_in` JSON number denoting a positive integer without an upper bound. A response with `expires_in: 1e100` and otherwise usable fields therefore qualifies. R-XDNO-E2L8 and R-XEVK-RUBX require a successful grant whose expiry is exactly the injected UTC second plus that many seconds. R-9MOM-GVE4 declares `Grant.Expires` as `time.Time`; R-AH64-14XH also requires UTC and second precision.

The installed Go time.Time implementation stores full UTC wall seconds in a signed 64-bit field (`/usr/local/go/src/time/time.go`, lines 140–160; also visible with `go doc -src time.Time`). An ordinary injected instant plus 1e100 seconds cannot be represented. Rejecting the otherwise usable response, saturating its expiry, or allowing arithmetic wraparound each violates the declared contract. This is separate from duration overflow for representable large values, which can be repaired with seconds arithmetic.

The build cannot resolve this within its authority: changing accepted expiry values, the required result, or the exported type changes the read-only design. Independent implementation and verification continue; this issue prevents claiming the full build complete.

Suggested resolution: define an upper bound on usable expiry values and a failure result for an unrepresentable final timestamp, then replace the affected requirement ids. Alternatively, choose a representation and API that can carry arbitrary-precision timestamps.
