# Prompts large error body closure contradicts unbounded RPC errors

Filed during the user-started appkit build-spec run, starting at commit 34a2451c61ec3ba116a34f0416be7c274b174d0c. The implementation leaf reported this conflict and a fresh independent verifier confirmed it.

Requirements: R-BZVJ-AGJK, R-BSK4-ZU3E, R-O4SV-C8VZ.

R-BZVJ-AGJK requires closing every response body before returning and additionally says a server answering a 500 with a 64 MiB body sees its write fail once the call returns. R-BSK4-ZU3E incorporates R-O4SV-C8VZ and requires preserving a JSON-RPC error's Code, Message and Data regardless of HTTP status, without a response-size bound.

A valid 64 MiB HTTP 500 JSON-RPC error may contain arbitrary data through its final bytes. Returning its exact Data requires receiving those bytes. The server can complete its whole write successfully before the client returns; closing the response afterward cannot retroactively fail that write. A fixed read cap would lose required RPC data. Stopping on a prefix that conclusively excludes JSON-RPC helps some bodies but cannot satisfy the unconditional assertion for arbitrary JSON bodies.

Evidence: reading appkit/mcp/client.go with `nl -ba` shows request defers resp.Body.Close before returning (line 169 in the starting tree), reads non-200 bodies completely with io.ReadAll (line 174), then classifies JSON-RPC errors and truncates HTTPError bodies (lines 185–193). Thus the existing client also drains ordinary large HTTP 500 bodies before closing them. Both implementer and independent verifier confirmed the counterexample above against D27 and D10. No test is tagged for R-BZVJ-AGJK, because ordinary close-before-return alone cannot prove its stronger assertion.

This cannot be resolved in a build run: changing either response classification or the write-failure assertion changes the read-only contract. The other 34 initial gap ids are independent of this contradiction.

Suggested resolution: re-mint R-BZVJ-AGJK, retaining close-before-return but removing the unconditional 500 write-failure example, or narrowing it to a body conclusively identifiable as non-RPC from its prefix. If that narrower early-close behavior is wanted, specify it explicitly and implement the corresponding bounded streaming classification in a subsequent build run.
