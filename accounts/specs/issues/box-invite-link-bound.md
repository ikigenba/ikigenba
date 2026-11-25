# Specify a maximum box invite link base

During the accounts build from `0a129295`, D19 R-0OTF-EREH and R-0CMF-L1ZJ required a successful box `create_invite` to use the central tool's link contract. D07 R-FRP6-DVQE requires returning the entire base from `AccountsURL` followed by the join path and token. The callback declared by D19 R-ZT41-GQ4F can return a string longer than `protocol.MaxBoxAnswer` with no error. This makes a valid structured answer exceed the hard bounds of D19 R-0XCQ-35LC and D13 R-WB1X-BDCA before JSON framing.

Specify and enforce a base length and URL syntax that bound the escaped response in both representations, with a defined refusal before invite creation. The build cannot infer a limit or excuse truncating a link after its invite has been created.
