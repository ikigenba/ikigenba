package build

import "path/filepath"

// DistDir returns the app's artifact directory.
func DistDir(app string) string {
	return filepath.Join(app, "dist")
}

// File returns the final artifact path for an app and commit sha.
func File(app, sha string) string {
	return filepath.Join(DistDir(app), app+"-"+sha+".tar.xz")
}

// ReleaseFile returns the suite artifact path for a commit sha.
func ReleaseFile(sha string) string {
	return filepath.Join("dist", sha+".tar.xz")
}
