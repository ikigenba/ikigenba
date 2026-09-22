// Package version owns the release version value.
package version

// Version is the release version. It is a source literal so a plain build
// and a deployed binary report the same string with no linker injection.
var Version = "v0.1.1"
