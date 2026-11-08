package space

import (
	"regexp"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

var hostBackupStamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$`)

// HostBackup returns the host tarball with the newest timestamp in its key.
func HostBackup(objects []cloud.Object, prefix string) (cloud.Object, bool) {
	var selected cloud.Object
	var latest time.Time
	found := false
	for _, object := range objects {
		name, hasPrefix := strings.CutPrefix(object.Key, prefix)
		stamp, hasSuffix := strings.CutSuffix(name, ".tar.zst")
		if !hasPrefix || !hasSuffix || !hostBackupStamp.MatchString(stamp) {
			continue
		}
		instant, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			continue
		}
		if !found || instant.After(latest) || instant.Equal(latest) && object.Key > selected.Key {
			selected, latest, found = object, instant, true
		}
	}
	return selected, found
}
