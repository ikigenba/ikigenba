# Define live invites after expiry cleanup

During the accounts build from `0a129295`, D04 R-22IF-2RHP defined liveness using creation, expiry and deletions by `Enroll`, `RevokeInvite` and `RemoveAccount`. R-0MO4-6JR3 also deletes expired invites during `CreateInvite`. Create invite A, advance the injected clock to its expiry, create B to remove A, then move the clock back to before A's expiry. R-22IF-2RHP now counts A as live, although the mandated cleanup deleted it.

The build cannot both delete and later count A under these rules. Define a live invite as one that is still held and unexpired, or specify a monotonic time requirement.
