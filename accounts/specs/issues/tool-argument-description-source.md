# Tool argument descriptions require duplicate copy

Filed during the accounts build-spec run from `40b111be`. The open requirements are R-0UPX-1805, R-0X5P-SRHJ, R-0YDM-6J88 and R-YFUM-415G in D07. R-YFUM-415G was initially matched and extends to the new `delete_host` arguments.

R-0UPX-1805 requires source constants for `keep_data` and `volume`. R-0X5P-SRHJ and R-0YDM-6J88 require the advertised property descriptions to equal those constants. R-0X5P-SRHJ and R-YFUM-415G also require the `host` property to use `ArgHost`. A client listing the tools must receive those descriptions in the argument schemas. The published appkit release pinned in `accounts/AGENTS.md` derives each property description solely from `f.Tag.Get("description")` in `mcp/schema.go`. Its registration API has no member-description override, and Go struct tags take string literals. The `spec` skill permits mirrored copy in struct tags only for appkit's paging members, and `accounts/AGENTS.md` requires one home for the copy. The build cannot satisfy these requirements through the published interface under those rules.

Resolve the contract by explicitly permitting mirrored tags for the named accounts argument constants, with a runtime equality test, or publish an appkit seam that accepts property descriptions from constants and update the pinned release.
