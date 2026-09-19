package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/keyring"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// Push gathers and writes the secret object for each app.
func Push(ctx context.Context, deps seam.Deps, ssm cloud.SSM, domain string, apps []checkout.App) ([]Entry, error) {
	type object struct {
		app    string
		keys   []string
		values map[string]string
	}

	objects := make([]object, 0, len(apps))
	for _, app := range apps {
		values := make(map[string]string, len(app.Manifest.Secrets))
		for _, name := range app.Manifest.Secrets {
			value, err := keyring.Lookup(ctx, deps, name)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", app.Name, err)
			}
			values[name] = value
		}

		keys := make([]string, 0, len(values))
		for name := range values {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		objects = append(objects, object{app: app.Name, keys: keys, values: values})
	}

	entries := make([]Entry, 0, len(objects))
	for _, object := range objects {
		value, err := json.Marshal(object.values)
		if err != nil {
			return entries, fmt.Errorf("%s: encode secrets: %w", object.app, err)
		}
		if err := ssm.PutSecureParameter(ctx, Parameter(domain, object.app), string(value)); err != nil {
			return entries, err
		}
		entries = append(entries, Entry{App: object.app, Keys: object.keys})
	}
	return entries, nil
}
