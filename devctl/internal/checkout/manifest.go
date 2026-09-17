package checkout

import (
	"errors"
	"io"

	"github.com/BurntSushi/toml"
)

// DecodeManifest decodes the manifest fields consumed by devctl.
func DecodeManifest(r io.Reader) (Manifest, error) {
	var manifest Manifest
	if _, err := toml.NewDecoder(r).Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.App == "" {
		return Manifest{}, errors.New("app must be a non-empty string")
	}
	if manifest.Secrets == nil {
		manifest.Secrets = []string{}
	}
	return manifest, nil
}
