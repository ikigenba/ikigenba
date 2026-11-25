# Define invite token nondisclosure by provenance

During the accounts build from `0a129295`, D04 R-CMU6-AGYV prohibited any answer from sharing an eight-character substring with a token except the token returned from `CreateInvite`. R-0MO4-6JR3 requires exact entropy encoding, so an injected all-zero random stream yields an invite identifier and token that both contain eight zeroes. R-RWHA-3KS4 also requires `CreateInviteAs` to return a token outside the stated exception.

D19 R-0W4T-PDUN has the same coincidence problem. With actor `ssh:daffy` and a schema-accepted `add_key` argument of `daffy`, R-EJXA-RH54 requires refusing the malformed key and R-0TP0-XUD9 requires the event's `user` attribute to contain `daffy`.

D07 R-FSX2-RNH3 likewise forbids the token in any later central tool result. A caller ID equal to the previous 56-byte token passes D07 R-ESC3-AJ89, and R-FRP6-DVQE and R-F23A-CP5T require the next invite to return that caller ID in `created_by`.

These outcomes are observable through the declared test seams. Define the prohibition in terms of disclosure derived from a secret or key and name both designated token returns. Use independently chosen marker values when verifying non-disclosure.
