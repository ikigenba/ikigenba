# Expired delete steps have conflicting transitions

Filed during the accounts build-spec run from `40b111be`. The open requirement is R-9B3X-9AC8. It conflicts with R-L0E6-07I2 in D10.

Use the injected clock to finish a host creation and start `DeleteHost(..., false)`. Put that still-running operation at `StepOrphan` through the store's `Advance`, age it by `StepTimeout + 1s`, and run one engine pass. R-9B3X-9AC8 requires moving to `StepDeleteData` without sending a cloud request. R-L0E6-07I2 requires timing out the operation with `OpFailed` and `ReasonTimedOut`. A delete with the opposite choice at an expired data-deletion step has the same conflict. Neither mismatch is listed among the timeout exceptions, and a failed operation cannot then advance in the same pass.

Specify the precedence by adding the mismatch cases to the timeout exceptions or by limiting the redirects to unexpired operations and specifying when a retry is needed.
