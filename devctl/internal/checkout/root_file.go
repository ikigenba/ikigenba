package checkout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// ReadRootFile discovers the checkout and reads its platform root file.
func ReadRootFile(ctx context.Context, deps seam.Deps) (RootFile, error) {
	opened, err := Open(ctx, deps)
	if err != nil {
		return RootFile{}, err
	}
	return opened.ReadRootFile()
}

// ReadRootFile reads the platform root file from the checkout root.
func (checkout *Checkout) ReadRootFile() (RootFile, error) {
	root, err := os.OpenRoot(checkout.Root)
	if err != nil {
		return RootFile{}, fmt.Errorf("%s: %w", RootFilePath, err)
	}
	defer func() { _ = root.Close() }()

	info, err := root.Stat(RootFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RootFile{}, &NoRootFileError{Checkout: checkout.Root}
		}
		return RootFile{}, fmt.Errorf("%s: %w", RootFilePath, err)
	}
	if !info.Mode().IsRegular() {
		return RootFile{}, fmt.Errorf("%s: not a regular file", RootFilePath)
	}

	contents, err := root.ReadFile(RootFilePath)
	if err != nil {
		return RootFile{}, fmt.Errorf("%s: %w", RootFilePath, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return RootFile{}, &RootFileError{Detail: "not a JSON object"}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return RootFile{}, &RootFileError{Detail: "not a JSON object"}
	}
	if err := requireJSONEnd(decoder); err != nil {
		return RootFile{}, &RootFileError{Detail: "not a JSON object"}
	}

	domain, err := rootString(object, "domain")
	if err != nil {
		return RootFile{}, err
	}
	region, err := rootString(object, "region")
	if err != nil {
		return RootFile{}, err
	}
	return RootFile{Domain: domain, Region: region}, nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("further JSON value")
	}
	return err
}

func rootString(object map[string]any, key string) (string, error) {
	value, exists := object[key]
	if !exists {
		return "", &RootFileError{Detail: "missing '" + key + "'"}
	}
	text, ok := value.(string)
	if !ok {
		return "", &RootFileError{Detail: "'" + key + "' is not a string"}
	}
	if text == "" {
		return "", &RootFileError{Detail: "missing '" + key + "'"}
	}
	return text, nil
}
