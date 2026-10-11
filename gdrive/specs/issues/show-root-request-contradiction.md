# Show of root requires incompatible requests

Filed by the user-invoked gdrive build-spec run starting at commit `fda872c880b1866b9634594a3ea4cd1d58d64e68`. The independent Drive verifier reported this contradiction and a second verifier confirmed it. Design is unchanged.

`R-JXU1-D4EV` in `specs/design/D05-drive.md` requires a nonempty `Show` call first to GET `/drive/v3/files/root` with `fields=id` and make no other request to that path. `R-LGPI-KSS4` requires `Show` to GET `/drive/v3/files/` followed by its supplied id escaped as one path segment. `R-PJ89-6GRK` requires the Show request for that id to use `fields=id,name,mimeType,size,modifiedTime,parents,driveId,webViewLink,trashed,shortcutDetails`.

For the permitted nonempty input `root`, the metadata request must use the same `/drive/v3/files/root` path as the preliminary root lookup. Two requests violate the first requirement's no-other-request clause. One request cannot carry both required fields values. Fetching metadata at the resolved root id instead violates the supplied-id path requirement; rejecting this nonempty input also fails it. Separating the lookup and metadata purposes does not remove the shared-path prohibition.

This is an incompatible request contract, not a provider failure or an implementation difficulty. Tests for ordinary ids cannot prove the missing root case, and the affected coverage tags cannot claim the full requirements. Independent Drive work remains testable.

Suggested resolution: clarify the root-input case by permitting the second request, requiring metadata at the resolved id, or specifying special handling of `root`. That changes read-only design text and needs new ids outside this build run. This issue prevents full completion.
