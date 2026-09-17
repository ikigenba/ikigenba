package checkout

import "github.com/ikigenba/ikigenba/devctl/internal/seam"

// ManifestFile is the path to an application's manifest relative to its root.
const ManifestFile = "etc/manifest.toml"

// Checkout is an open local git checkout.
type Checkout struct {
	Root string
	Deps seam.Deps
}

// App is an application found in a checkout.
type App struct {
	Name     string
	Dir      string
	Manifest Manifest
}

// Manifest contains the app identity and secret names devctl consumes.
type Manifest struct {
	App     string   `toml:"app"`
	Secrets []string `toml:"secrets"`
}
