# Make the production stdio timeout testable

During the accounts build from `0a129295`, D20 R-7BPN-NG7R required `Run` to pass exactly the production `AnswerTimeout` to `Serve`. D20's recorded decision identifies the missing test seam. Accounts test discipline allows a short injected timeout on `Serve` and bars a wall-clock wait to distinguish exact production timeouts in `Run`. A socket test can verify `Run`'s socket choice and environment lookup, and a short deadline proves `Serve`'s behavior.

Move the exact production wiring to design guidance or declare a deterministic deadline seam for `Run`.
