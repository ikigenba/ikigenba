// Package analysisprompt provides the production question-analysis prompt.
package analysisprompt

import _ "embed"

// Instructions is the baked-in production analysis instruction preamble.
//
//go:embed prompt.txt
var Instructions string
