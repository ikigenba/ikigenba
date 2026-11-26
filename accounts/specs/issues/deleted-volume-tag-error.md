# Conflicting errors for tagging a deleted volume

Filed during the accounts build-spec run from `40b111be`. The open gap is R-L4HV-TQ6K. It conflicts with R-G4VH-YAKN and the forbidden-key preflight in D05.

Create a host-data volume in the memory cloud with an injected clock, advance it to available, and call `DeleteVolume`. At exactly `memory.VolumeDelay` after that call, call `TagResource` on the same volume ID with `cloud.Tags{cloud.TagKind: cloud.KindHostData}`. R-L4HV-TQ6K requires `InvalidVolume.NotFound` with class `cloud.ClassNotFound`. R-G4VH-YAKN requires `cloud.CodeTagContract` with class `cloud.ClassRefused` for the same input. The cloud error carries one code and one class, so a test cannot prove both outcomes for this call.

Resolve the design by specifying forbidden-tag validation before resource lookup and limiting the R-L4HV-TQ6K tagging case to allowed tag keys.
