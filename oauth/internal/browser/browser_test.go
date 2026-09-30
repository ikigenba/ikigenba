package browser_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
)

var _ func() browser.Launcher = browser.New

type openOnly struct{ opened []string }

func (o *openOnly) Open(url string) error {
	o.opened = append(o.opened, url)
	return nil
}

func TestLauncherHasExactExportedInterface(t *testing.T) {
	// R-LXPA-LZMI
	fake := &openOnly{}
	var launcher browser.Launcher = fake
	hasType[func(url string) error](launcher.Open)
	if err := launcher.Open("https://authorize.example/launcher"); err != nil {
		t.Fatalf("Launcher.Open returned %v, want nil", err)
	}
	if len(fake.opened) != 1 || fake.opened[0] != "https://authorize.example/launcher" {
		t.Fatalf("Launcher.Open forwarded %q, want the single URL passed", fake.opened)
	}
}

func TestNewHasExactExportedSignature(t *testing.T) {
	// R-LYX6-ZRD7
	hasType[func() browser.Launcher](browser.New)
	if browser.New() == nil {
		t.Fatal("browser.New() returned a nil Launcher")
	}
}

// hasType compiles only when its argument is assignable to T.
func hasType[T any](T) {}
