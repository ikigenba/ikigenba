# Telemetry token suffix includes the query wrapper

Filed during the second accounts build-spec run against `2e3f0257`.

`D08-trail` R-O4Q5-ZNKA and R-QGWE-9PIH identify the token as the link suffix after the last `/`. With `D07-tools` R-GTU8-J8BM and `D19-box-tools` R-0OTF-EREH the suffix starts `join?token=`, so the literal rule makes `join?tok` a forbidden eight-character window regardless of the minted token. A run meeting the secrecy premises may use `X-User-Id: join?tok` on its signed-in `create_host` request, with its email marker and independent request IDs as usual. The value can be chosen to have no overlap with the actual token, invite IDs or request IDs. `D08-trail` R-IJ8P-GADI then requires both request boundary events to carry `join?tok` as their `User`, which the suffix-based rule forbids.

The compatible tests continue to check the actual minted query token and the key and email markers. The build cannot meet the universal suffix rule and the request identity rule together.

Resolve by replacing both secrecy requirements so that they derive the token from the `token` query parameter and keep their secrecy assertions on that credential.
