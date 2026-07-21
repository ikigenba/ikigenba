// Package extractprompt provides the production extraction prompt.
package extractprompt

import _ "embed"

// Instructions is the baked-in production extract instruction preamble.
//
//go:embed prompt.txt
var Instructions string
