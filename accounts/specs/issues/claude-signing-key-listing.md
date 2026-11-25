# Detect truncated signing-key listings

During the accounts build from `0a129295`, D15 R-SRE3-WA7V required import exactly when captured `Result.Stdout` contained one primary key and its pinned fingerprint. R-SCRB-B1BJ caps that capture at `MaxCapture` with no truncation flag. A listing starting with the pinned key can fill the capture before a second `pub:` record. The required result then cannot distinguish that listing from a listing with just one key.

The build cannot meet the exact import condition while ensuring it never imports an unchecked second primary key. Validate the full output through the bounded streaming `Command.Stdout` seam, or declare a truncation signal and a fail-closed rule.
