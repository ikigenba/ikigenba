# Resolve invite token reuse after enrollment

During the accounts build from `0a129295`, D04 R-CHYK-RE03 required every later lookup and enrollment of an enrolled token to return `ErrNotFound`. R-0MO4-6JR3 requires the token to be encoded exactly from the injected randomness and only rejects collisions with still-held invites. Successful enrollment deletes the invite. With a constant random source, creating a second invite after enrollment produces the original token, which R-CD2Z-8B1B then requires lookup to find.

The build cannot satisfy permanent token rejection and required reminting. Define the guarantee per invite lifetime, or specify permanent non-reuse and revise the generation rules and storage bounds accordingly.
