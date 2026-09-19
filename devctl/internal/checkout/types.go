package checkout

import "github.com/ikigenba/ikigenba/devctl/internal/seam"

const (
	// ManifestFile is the path to an application's manifest relative to its root.
	ManifestFile = "etc/manifest.toml"
	// RootFilePath is the path to the platform root file relative to the checkout.
	RootFilePath = "infra/terraform.tfvars.json"
)

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

// RootFile contains the platform values devctl reads from Terraform's root file.
type RootFile struct {
	Domain string `json:"domain"`
	Region string `json:"region"`
}
