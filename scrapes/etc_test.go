package scrapes_test

import (
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
)

// R-74KL-9L24
func TestNginxBytes(t *testing.T) {
	b, err := fs.ReadFile(scrapes.Etc(), "nginx.conf")
	if err != nil || string(b) != cli.NginxConf {
		t.Fatal(string(b), err)
	}
}

// R-CZ59-KWG7
func TestManifestBytes(t *testing.T) {
	b, err := fs.ReadFile(scrapes.Etc(), "manifest.toml")
	if err != nil || string(b) != cli.Manifest {
		t.Fatal(string(b), err)
	}
}
