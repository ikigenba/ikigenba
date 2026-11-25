# Bound the request telemetry delivery guarantee

During the accounts build from `0a129295`, D08 R-DYS1-C3JK required the sink to receive both request events for every answered request if the sink returns immediately and request handling finishes before the drain deadline. The released appkit v0.32.0 `telemetry.Writer.Emit` drops events when its queue reaches `QueueCapacity`. An immediate sink can still be outpaced, and completing the drain cannot recover a dropped event. D08 R-0ZQ5-P4SM expressly describes that behavior.

The build cannot guarantee universal delivery while using the required writer as published. Make the delivery assertion conditional on the writer admitting the events, or specify a published backpressure mechanism. The ordering and event contents can still be tested below the queue bound.
