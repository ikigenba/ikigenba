# Expose guarded transaction boundaries to tests

During the accounts build from `0a129295`, D04 R-RQDS-6Q2N, R-RRLO-KHTC, R-RSTK-Y9K1, R-RV9D-PT1F, R-RWHA-3KS4 and R-RXP6-HCIT required the actor guard and the underlying read or mutation to run in the same database transaction. D04's recorded decision says a permitted deterministic test cannot schedule an actor change between these steps. Public calls can prove guard precedence, refusals and successful effects, but two unsynchronized goroutines cannot distinguish a transaction from an implementation that rechecks in a separate transaction between those calls.

Specify a deterministic interleaving seam, or put the transaction placement in design guidance and keep the observable guard behavior in the tagged requirements.
