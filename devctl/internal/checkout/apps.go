package checkout

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Apps discovers the runnable applications at the checkout root.
func (checkout *Checkout) Apps() ([]App, error) {
	return checkout.apps(readManifest)
}

func (checkout *Checkout) apps(read func(string) (Manifest, error)) ([]App, error) {
	entries, err := os.ReadDir(checkout.Root)
	if err != nil {
		return nil, err
	}

	apps := make([]App, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		dir := checkout.Path(name)
		manifestPath := filepath.Join(dir, ManifestFile)
		info, err := os.Stat(manifestPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, manifestFailure(name, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}

		main, err := hasMainPackage(dir)
		if err != nil {
			return nil, err
		}
		if !main {
			continue
		}

		manifest, err := read(dir)
		if err != nil {
			return nil, manifestFailure(name, err)
		}
		if manifest.App != name {
			return nil, &ManifestError{
				App:    name,
				Detail: "app is '" + manifest.App + "', not '" + name + "'",
			}
		}

		apps = append(apps, App{Name: name, Dir: dir, Manifest: manifest})
	}

	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps, nil
}

// App returns the named application from the checkout.
func (checkout *Checkout) App(name string) (App, error) {
	return checkout.app(name, checkout.Apps)
}

func (checkout *Checkout) app(name string, list func() ([]App, error)) (App, error) {
	apps, err := list()
	if err != nil {
		return App{}, err
	}
	for _, app := range apps {
		if app.Name == name {
			return app, nil
		}
	}
	return App{}, &NoAppError{Name: name}
}

func hasMainPackage(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err == nil && parsed.Name.Name == "main" {
			return true, nil
		}
	}
	return false, nil
}

func manifestFailure(app string, err error) error {
	return &ManifestError{App: app, Detail: err.Error(), Err: err}
}

func readManifest(dir string) (Manifest, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Manifest{}, err
	}
	file, err := root.Open(ManifestFile)
	if err != nil {
		_ = root.Close()
		return Manifest{}, err
	}
	manifest, decodeErr := DecodeManifest(file)
	fileCloseErr := file.Close()
	rootCloseErr := root.Close()
	if decodeErr != nil {
		return Manifest{}, decodeErr
	}
	if fileCloseErr != nil {
		return Manifest{}, fileCloseErr
	}
	if rootCloseErr != nil {
		return Manifest{}, rootCloseErr
	}
	return manifest, nil
}
