// Package diagnostic renders values for turns-authored diagnostic lines.
package diagnostic

import (
	"strconv"
	"strings"
)

// Text escapes control bytes without changing any other byte.
func Text(value string) string {
	var rendered strings.Builder
	for i := range len(value) {
		b := value[i]
		if b < 0x20 || b == 0x7f {
			quoted := strconv.Quote(string([]byte{b}))
			rendered.WriteString(quoted[1 : len(quoted)-1])
		} else {
			rendered.WriteByte(b)
		}
	}
	return rendered.String()
}
