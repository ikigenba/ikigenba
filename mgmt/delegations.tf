# NS delegations from the apex metaspot.org zone to per-environment subzones.
#
# The prod, test, and sandbox delegations were removed when those accounts were
# torn down: their hosted zones no longer exist, and an NS record pointing at
# nameservers that no longer answer is worse than no record at all.
#
# int has never had a delegation here. If int.metaspot.org is ever meant to
# resolve publicly, add its delegation following the same hardcoded-literal
# discipline used elsewhere in this repo (copy the child zone's NS values in;
# the repo deliberately never wires a terraform_remote_state cross-account read).
