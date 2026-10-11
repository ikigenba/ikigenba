# Guarantee randomness conflicts with test discipline

Filed during the user-invoked connections build-spec run, initially at commit 505678732f2d666c56e965332f511079faf9ce4c. The independent runtime verifier identified this contradiction, and the fresh guarantee verifier separately confirmed it, withdrawing its earlier pass.

Requirements involved: R-2LCX-7DO1, R-0ZEH-BEW1 and R-10MD-P6MQ in D11-on-a-space.md. R-0Y6K-XN5C remains independently satisfiable.

The D11 guarantee defines minted values through a nil `Process.Rand`, requires a Run-level fake-provider connect matrix that produces those values, and requires the corresponding secrecy observations. The sub-project AGENTS.md test discipline explicitly requires injected randomness and says a test never reads or changes real randomness. Its stated exception concerns the working directory, not randomness.

`cli.Run` supplies `crypto/rand.Reader` when the process random source is nil. Required connect operations reach the broker's state and verifier reads, so the nil-source guarantee matrix actually consumes real randomness. Keeping the source nil violates test discipline; injecting a deterministic reader fails the D11 minted-value definition. Merely starting the server with a nil source does not prove the required connect matrix. The sole permitted binary test cannot provide the required fake-provider exchanges under its permitted scope.

The verified guarantee test initially used a nil random source and passed operationally, but the independent review found it inadmissible under AGENTS.md. The build replaces that test input with an injected deterministic source and retains useful secrecy checks without tagging these three requirements as fully covered.

The build cannot change either read-only input to reconcile them. Suggested resolution: define guarantee minted values using an injected deterministic random source, or explicitly authorize real randomness for this narrowly scoped matrix. Replace affected ids when changing the design text.
