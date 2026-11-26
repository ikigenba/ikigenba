# Tool registration type identity is not observable

Filed during the accounts build-spec run from `40b111be`. The open requirement is R-0G34-FZ3T in D07.

The requirement names the exact Go type used to register each tool. A test can register the expected types separately, compare the descriptions and schemas returned by `ListTools`, and call the real tools with accepted and rejected arguments. Those observations cannot establish type identity. For example, `type alternateHostArgs HostArgs` can be used in the `host` registration with the callback converting to `HostArgs`. The schema and all client-visible call outcomes are unchanged even though the registered input type differs.

The pinned appkit release captures the input type in private registration closures and exposes wire metadata through the client. Source or layout inspection is prohibited by the `spec` test rules. Resolve the contract by specifying the observable schema and input behavior, or publish a supported registration interface that lets a test use the declared type directly.
