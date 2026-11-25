# Align name refusal headers with the join page

During the accounts build from `0a129295`, D18 R-T5WT-R6LD required the exact join page response with no headers beyond those named by R-20FW-F3PB. The latter lists four security headers. D18 R-T118-83ML specifies the page data, and D06 R-ATBI-JX3X requires `Set.Write` to add exactly one `Content-Type`.

The build cannot both preserve the required template response and exclude its required header. Allow the standard page headers and require the same header set and status for each name-refusal path.
