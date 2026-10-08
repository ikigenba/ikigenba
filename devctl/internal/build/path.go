package build

import "path/filepath"

// ReleaseFile returns the suite artifact path for a commit sha.
func ReleaseFile(sha string) string {
	return filepath.Join("dist", sha+".tar.xz")
}
