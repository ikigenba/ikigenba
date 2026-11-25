# Box link prefix conflicts with query enrollment

Filed during the second accounts build-spec run against `2e3f0257`.

`D03-serve` R-H9S1-98I9 requires the box `create_invite` reply to start with `https://accounts.two.test/join/` after the services file changes. `D19-box-tools` R-0OTF-EREH requires this reply to match the central tool's link, and `D07-tools` R-GTU8-J8BM requires that link to start with `https://accounts.two.test/join?token=` for the same base. `D18-enrollment` R-I6G5-R6SK also requires the exact `/join` path and excludes the old path form. A single link cannot have both prefixes.

The build implements the query link as directed. The CLI box test still exercises the compatible wiring, including a services file rewrite and a link using the new base. The build cannot revise the conflicting design or mark R-H9S1-98I9 as fully covered.

Resolve by replacing R-H9S1-98I9 with a requirement that expects the query-form prefix while preserving its other checks.
