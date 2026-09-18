package build

import "path/filepath"

// DistDir returns the app's artifact directory.
func DistDir(app string) string {
	return filepath.Join(app, "dist")
}

// File returns the final artifact path for an app and version.
func File(app, version string) string {
	return filepath.Join(DistDir(app), app+"-"+version+".tar.xz")
}
