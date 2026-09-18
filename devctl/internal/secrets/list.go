package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

const invalidObjectReason = "not a JSON object of strings"

// List returns the secret names held by each direct app parameter for domain.
func List(ctx context.Context, acct *account.Account, domain string) ([]Entry, error) {
	prefix := Prefix(domain)
	parameters, err := acct.Clients.SSM.ListParameters(ctx, prefix)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(parameters))
	for _, parameter := range parameters {
		app, ok := directApp(prefix, parameter.Name)
		if !ok {
			continue
		}
		keys, err := objectKeys(parameter.Name, parameter.Value)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{App: app, Keys: keys})
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].App < entries[j].App })
	return entries, nil
}

// Names returns the sorted secret names held by one app parameter.
func Names(ctx context.Context, acct *account.Account, domain, app string) ([]string, error) {
	name := Parameter(domain, app)
	value, err := acct.Clients.SSM.GetParameter(ctx, name)
	if err != nil {
		var cloudError *cloud.Error
		if errors.As(err, &cloudError) && cloudError.Code == "ParameterNotFound" {
			return nil, nil
		}
		return nil, err
	}
	return objectKeys(name, value)
}

func directApp(prefix, name string) (string, bool) {
	app, found := strings.CutPrefix(name, prefix+"/")
	return app, found && app != "" && !strings.Contains(app, "/")
}

func objectKeys(name, value string) ([]string, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
	}

	keySet := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
		}

		var rawValue json.RawMessage
		if err := decoder.Decode(&rawValue); err != nil {
			return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
		}
		rawValue = bytes.TrimSpace(rawValue)
		if len(rawValue) == 0 || rawValue[0] != '"' {
			return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
		}
		var stringValue string
		if err := json.Unmarshal(rawValue, &stringValue); err != nil {
			return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
		}
		keySet[key] = struct{}{}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, &ObjectError{Parameter: name, Reason: invalidObjectReason}
	}

	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}
