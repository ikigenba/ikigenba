package machine

import (
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Redacted stands for a credential value removed from backup output.
const Redacted = "***"

// CopyName returns the request's candidate path, starting at attempt 1.
func CopyName(q protocol.RestoreRequest, attempt int) string {
	suffix := q.AsOf.UTC().Format("20060102T150405Z")
	if attempt > 1 {
		suffix += "-" + strconv.Itoa(attempt)
	}
	if q.Kind == protocol.RestoreFiles {
		if q.Path == "" {
			return "restored/" + suffix
		}
		return "restored/" + suffix + "/" + q.Path
	}
	directoryEnd := strings.LastIndexByte(q.Path, '/') + 1
	extensionStart := strings.LastIndexByte(q.Path, '.')
	if extensionStart <= directoryEnd {
		extensionStart = len(q.Path)
	}
	return q.Path[:extensionStart] + ".restored-" + suffix + q.Path[extensionStart:]
}
