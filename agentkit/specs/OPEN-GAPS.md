# Open design gaps — structural-requirement compliance

None. Every exported seam named in the design has a structural requirement that
pins its shape, and every requirement id is proved by an offline `*_test.go`.

The three gaps surfaced by the original compliance pass (G1 `ReplayEncoding`,
G2 the generic wire constructor + per-package `Option` shape, G3 the vendor
packages beyond anthropic/openai) are resolved: G1's undefined seam was removed
from the design, and G2/G3 were pinned with structural requirements
(`NewForWire`, `R-YPOE-6GA4`, and the per-vendor credential requirements in D7).

New gaps get appended here as they are surfaced.
