package checkout

import (
	"errors"
	"io"

	"github.com/BurntSushi/toml"
)

// DecodeManifest decodes the manifest fields consumed by devctl.
func DecodeManifest(r io.Reader) (Manifest, error) {
	contents, err := io.ReadAll(r)
	if err != nil {
		return Manifest{}, err
	}
	var values map[string]any
	metadata, err := toml.Decode(string(contents), &values)
	if err != nil {
		return Manifest{}, err
	}
	if metadata.IsDefined("port") {
		return Manifest{}, errors.New("'port' is not allowed; the host gives the app its socket")
	}
	var manifest Manifest
	if _, err := toml.Decode(string(contents), &manifest); err != nil {
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
