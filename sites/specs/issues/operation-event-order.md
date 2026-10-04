# Operation event order is not observable

Filed during the human-authorized build-spec run on `wip-sites`, starting at commit `2210af043014792cac736b08968ce21fd1bbdde6`. The limits implementation reviewer and a second independent reviewer confirmed this contradiction. Independent work continues as explicitly authorized by the user.

## Requirements involved

`R-Y63E-T5ZR` declares `Clock` with exactly one field, `After func(d time.Duration) <-chan time.Time`. `R-NL8P-H9HD` declares `Operation(ctx context.Context) (context.Context, context.CancelFunc)`.

`R-TPGD-5CZP` requires the returned operation context to preserve the parent cause when the parent becomes done before the timer value is ready, halt, or explicit cancellation.

`R-WVYP-EEVO` requires the operation cause to match `ErrTimedOut` when the timer value becomes ready before the parent becomes done, halt, or explicit cancellation. It also specifies the already-ready timer case.

These two behavioral requirements cannot both be fulfilled for every context and timer channel admitted by the declared interfaces.

## Evidence

Use an admissible custom parent context with a stable externally closed `Done` channel, `Err` returning nil before closure and `context.Canceled` afterwards, no deadline, and nil values. It exposes no synchronous cancellation callback or cancellation timestamp. Use a buffered timer channel of capacity one returned by the injected `Clock.After`.

After `Operation` returns, the scheduler may leave its observers unscheduled while either of these histories occurs:

1. Close the parent channel, then send a time value to the timer channel.
2. Send the same time value to the timer channel, then close the parent channel.

Resume the observers only after both events. All observations available through the declared interfaces are identical: the parent is canceled, both channels are ready, and the timer contains the same value. Their observation history before the events is also identical. Yet `R-TPGD-5CZP` requires the parent cause for history 1, while `R-WVYP-EEVO` requires the timeout cause for history 2. Neither select priority nor another observer can recover the missing event order. Standard context propagation does not remove the counterexample because the contract admits custom contexts without propagation hooks.

The first independent reviewer checked the limits implementation at source SHA-256 `a677c40fdc2f09221580f2ab88ceefe979e8d2e7395be70448e1686220de70e3` and identified that tests which await the first observed cancellation before triggering the later event omit these histories. A second reviewer independently confirmed the impossibility from the public contract without reading that implementation. Passing package race tests do not prove these ordering requirements.

## Resolution

A human must revise the contract: define precedence when multiple causes are ready when observed, or expose an event mechanism that records order synchronously. Re-mint the changed requirements through the design workflow. This run cannot change the read-only design or tag tests as proving the two contradictory guarantees.
