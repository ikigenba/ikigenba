# D21-version

Every app shows which code it is running: in its page footer (D03), in its MCP `serverInfo` (D07), and in the `version` of its `service.started` event (D11, D12). Nothing is baked into the binary at build time. The host tells the app instead, through two environment variables: `IKIGENBA_COMMIT` holds the commit the app was built from, and `IKIGENBA_RELEASE`, when the host gave the release a label, holds that label. Package `version` reads both into one `Identity`, holding the release label and the short commit as separate values, so every app shows its identity the same way and none parses the variables itself. The footer shows the two values apart, so the app hands the `Identity` itself to `page.New`, and `page` puts its `Release` and `Commit` in the banner unaltered (D02, D03). `mcp` and `telemetry` take one string: the app hands them the identity's `String`, the display string below, and their `Version string` fields are unchanged.

## The display string

With a label the string is the label, a space, and the short commit in parentheses, as in `r142 (c604e32)`. Without one it is the short commit alone, as in `c604e32`. The short commit is the first seven characters of the commit, git's own default abbreviation and the length of the example above. A developer's sandbox sets `IKIGENBA_COMMIT` to the commit followed by `-dirty` when the tree is modified, as `git describe --dirty` marks one, and gives no label; the suffix survives the shortening, so the commit reads `c604e32-dirty`. A value shorter than seven characters is shown whole. Nothing else about either value is checked or changed: appkit carries what the host wrote.

`Read` returns the label as the host wrote it, empty when there is none, and the short commit, empty when there is no commit; it reads the environment each time it is called. `String` formats an identity as the display string from its two values as they stand, shortening nothing, since `Read` has already shortened the commit.

Until the host writes the variables, an app shows no identity: with neither set, both values and the string are empty. A label without a commit is shown alone. An app calls `Read` once at start-up, hands the identity to `page.New`, and hands its `String` to `mcp.ServerConfig.Version` and `telemetry.Config.Version`. `Display` stays for apps that have not moved to `Read`: it is `Read().String()`, the same string it has always returned.

Tests reach the variables the way an app does, setting them with `testing.T.Setenv` before calling `Read` or `Display`; the variable names are exported so a test names them rather than spelling them.

## REQUIREMENTS

- R-XDPF-GQHC: Package `version` MUST be imported from the path `github.com/ikigenba/ikigenba/appkit/version`, and its package name MUST be `version`.
- R-XEXB-UI81: Package `version` MUST export `const CommitVariable = "IKIGENBA_COMMIT"` and `const ReleaseVariable = "IKIGENBA_RELEASE"`.
- R-XG58-89YQ: Package `version` MUST export `func Display() string`.
- R-A063-27PP: The short commit of a non-empty value `c` MUST be, when `c` ends with `-dirty`, the first 7 runes of the value left after removing that one suffix from the end of `c`, or all of them when there are fewer, followed by `-dirty`; and otherwise the first 7 runes of `c`, or all of them when there are fewer.
- R-XIL0-ZTG4: When, at the time of the call, the variable `CommitVariable` names is set in the process environment to a non-empty value and the variable `ReleaseVariable` names is unset or empty, `version.Display` MUST return exactly the short commit of that value.
- R-XJSX-DL6T: When, at the time of the call, the variables `CommitVariable` and `ReleaseVariable` name are both set in the process environment to non-empty values, `version.Display` MUST return exactly the value of `ReleaseVariable`'s variable, then ` (`, then the short commit of the value of `CommitVariable`'s variable, then `)`.
- R-XL0T-RCXI: When, at the time of the call, the variable `CommitVariable` names is unset or empty in the process environment and the variable `ReleaseVariable` names is set to a non-empty value, `version.Display` MUST return exactly that value.
- R-XM8Q-54O7: When, at the time of the call, the variables `CommitVariable` and `ReleaseVariable` name are both unset or empty in the process environment, `version.Display` MUST return the empty string.
- R-SMOU-QPSS: Package `version` MUST export `type Identity struct { Release, Commit string }`, with exactly these fields in this order.
- R-SNWR-4HJH: Package `version` MUST export `func Read() Identity`.
- R-SP4N-I9A6: Package `version` MUST export the method `func (id Identity) String() string`.
- R-SQCJ-W10V: `version.Read` MUST return an `Identity` whose `Release` is exactly the value, at the time of the call, of the variable `ReleaseVariable` names in the process environment, or the empty string when that variable is unset, and whose `Commit` is the short commit of the value of the variable `CommitVariable` names when that variable is set to a non-empty value at the time of the call, and the empty string when it is unset or empty.
- R-EDZL-0G9N: For an `Identity` whose `Release` is `r` and whose `Commit` is `c`, `String` MUST return exactly `r`, then ` (`, then `c`, then `)` when both are non-empty; exactly `c` when `c` is non-empty and `r` is empty; exactly `r` when `r` is non-empty and `c` is empty; and the empty string when both are empty, in every case using `r` and `c` unaltered.
- R-EF7H-E80C: `version.Display` MUST return exactly what `version.Read().String()` returns for the process environment at the time of the call.
