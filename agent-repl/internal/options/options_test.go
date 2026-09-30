package options

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// R-RNR6-LUCS
func TestOptionsHasTheSpecifiedFields(t *testing.T) {
	provider, model, wire, auth := "provider", "model", "wire", "auth"
	authFile, baseURL, systemFile := "auth-file", "base-url", "system-file"
	settings := map[string]string{"key": "value"}
	raw, version := true, true
	options := Options{
		Provider:   provider,
		Model:      model,
		Wire:       wire,
		Auth:       auth,
		AuthFile:   authFile,
		BaseURL:    baseURL,
		SystemFile: systemFile,
		Settings:   settings,
		Raw:        raw,
		Version:    version,
	}
	if options.Provider != provider || options.Model != model || options.Wire != wire || options.Auth != auth ||
		options.AuthFile != authFile || options.BaseURL != baseURL || options.SystemFile != systemFile ||
		!reflect.DeepEqual(options.Settings, settings) || options.Raw != raw || options.Version != version {
		t.Fatalf("Options = %+v, want the constructed field values", options)
	}
}

// R-NADD-UTOU
func TestValidateFoldsAllConfigPairsLastWins(t *testing.T) {
	got, err := (Flags{Config: []Pair{
		{Key: "provider", Value: "not-a-provider"},
		{Key: "model", Value: ""},
		{Key: "wire", Value: "not-a-wire"},
		{Key: "auth", Value: "not-an-auth-mode"},
		{Key: "auth_file", Value: "old-auth"},
		{Key: "base_url", Value: "old-base"},
		{Key: "system_file", Value: "old-system"},
		{Key: "temperature", Value: "0.1"},
		{Key: "provider", Value: "xai"},
		{Key: "model", Value: "future-model"},
		{Key: "wire", Value: "responses"},
		{Key: "auth", Value: "api_key"},
		{Key: "auth_file", Value: "final-auth"},
		{Key: "base_url", Value: "final-base"},
		{Key: "system_file", Value: "final-system"},
		{Key: "temperature", Value: "0.9"},
		{Key: "unknown.key", Value: "verbatim value"},
	}, Raw: true, Version: true}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	want := Options{
		Provider:   "xai",
		Model:      "future-model",
		Wire:       "responses",
		Auth:       "api_key",
		AuthFile:   "final-auth",
		BaseURL:    "final-base",
		SystemFile: "final-system",
		Settings: map[string]string{
			"temperature": "0.9",
			"unknown.key": "verbatim value",
		},
		Raw:     true,
		Version: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Validate() = %#v, want %#v", got, want)
	}
}

// R-NCT6-MD68
func TestValidateCarriesNonexistentSystemFileWithoutIO(t *testing.T) {
	path := t.TempDir() + "/missing/system-prompt.txt"
	got, err := (Flags{Config: []Pair{{Key: "system_file", Value: path}}}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v for nonexistent system file", err)
	}
	if got.SystemFile != path {
		t.Fatalf("Validate() system file = %q, want %q", got.SystemFile, path)
	}
	if _, found := got.Settings["system_file"]; found {
		t.Fatal("Validate() retained interpreted system_file in Settings")
	}
}

// R-U9SY-LVI2
func TestFlagsValidateReturnsOptions(t *testing.T) {
	got, err := (Flags{}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got.Model == "" {
		t.Fatal("Validate() returned Options with an empty default model")
	}
}

// R-UOFR-74EE
func TestValidateDefaultsAbsentModelAndRejectsExplicitlyEmptyModel(t *testing.T) {
	got, err := (Flags{}).Validate()
	if err != nil {
		t.Fatalf("Validate() default error = %v", err)
	}
	if got.Model != "gpt-5.6-sol" {
		t.Fatalf("Validate() default model = %q, want %q", got.Model, "gpt-5.6-sol")
	}

	_, err = (Flags{Config: []Pair{{Key: "model", Value: ""}}}).Validate()
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("Validate() empty model error = %v, want error naming model", err)
	}
}

// R-UPNN-KW53
func TestValidateRejectsInvalidEnumeratedValuesNamingKeyAndValue(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "unknown provider", key: "provider", value: "unknown-host"},
		{name: "empty provider", key: "provider", value: ""},
		{name: "unknown auth", key: "auth", value: "bearer"},
		{name: "empty auth", key: "auth", value: ""},
		{name: "unknown wire", key: "wire", value: "rpc"},
		{name: "empty wire", key: "wire", value: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (Flags{Config: []Pair{{Key: test.key, Value: test.value}}}).Validate()
			if err == nil {
				t.Fatal("Validate() error = nil")
			}
			if !strings.Contains(err.Error(), test.key) || !strings.Contains(err.Error(), fmt.Sprintf("%q", test.value)) {
				t.Fatalf("Validate() error = %q, want key %q and value %q", err, test.key, test.value)
			}
		})
	}
}

// R-UQVJ-YNVS
func TestValidateDerivesProviderAndRejectsUnknownModelWithoutProvider(t *testing.T) {
	got, err := (Flags{}).Validate()
	if err != nil {
		t.Fatalf("Validate() default model error = %v", err)
	}
	if got.Model != "gpt-5.6-sol" || got.Provider != "openai" {
		t.Fatalf("Validate() default model/provider = %q/%q, want gpt-5.6-sol/openai", got.Model, got.Provider)
	}

	got, err = (Flags{Config: []Pair{{Key: "model", Value: "gpt-5.6-sol"}}}).Validate()
	if err != nil {
		t.Fatalf("Validate() known model error = %v", err)
	}
	if got.Provider != "openai" {
		t.Fatalf("Validate() provider = %q, want %q", got.Provider, "openai")
	}

	_, err = (Flags{Config: []Pair{{Key: "model", Value: "not-in-the-catalog"}}}).Validate()
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("Validate() unknown model error = %v, want error naming model", err)
	}
}

// R-US3G-CFMH
func TestValidateAcceptsUnknownModelWithProvider(t *testing.T) {
	got, err := (Flags{Config: []Pair{
		{Key: "provider", Value: "xai"},
		{Key: "model", Value: "future-model"},
	}}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got.Provider != "xai" || got.Model != "future-model" {
		t.Fatalf("Validate() provider/model = %q/%q, want xai/future-model", got.Provider, got.Model)
	}
}

// R-UTBC-Q7D6
func TestValidateDoesNotRequireEnvironmentOrFiles(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	got, err := (Flags{Config: []Pair{
		{Key: "provider", Value: "openai"},
		{Key: "auth", Value: "api_key"},
		{Key: "auth_file", Value: t.TempDir() + "/does-not-exist.json"},
	}}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !strings.HasSuffix(got.AuthFile, "/does-not-exist.json") {
		t.Fatalf("Validate() auth file = %q, want nonexistent path preserved", got.AuthFile)
	}
}
