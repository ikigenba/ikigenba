package options

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

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

// R-ULZY-FKX0
func TestOptionsHasExactlyTheContractFields(t *testing.T) {
	type expectedOptions struct {
		Provider string
		Model    string
		Wire     string
		Auth     string
		AuthFile string
		BaseURL  string
		Settings map[string]string
		Raw      bool
		Version  bool
	}

	gotType := reflect.TypeOf(Options{})
	wantType := reflect.TypeOf(expectedOptions{})
	if gotType.NumField() != wantType.NumField() {
		t.Fatalf("Options field count = %d, want %d", gotType.NumField(), wantType.NumField())
	}
	for index := range wantType.NumField() {
		gotField := gotType.Field(index)
		wantField := wantType.Field(index)
		if gotField.Name != wantField.Name || gotField.Type != wantField.Type {
			t.Fatalf("Options field %d = %s %v, want %s %v", index, gotField.Name, gotField.Type, wantField.Name, wantField.Type)
		}
	}
}

// R-UN7U-TCNP
func TestValidateFoldsConfigLastWinsAndSeparatesSettings(t *testing.T) {
	flags := Flags{
		Config: []Pair{
			{Key: "provider", Value: "anthropic"},
			{Key: "provider", Value: "openai"},
			{Key: "model", Value: "first"},
			{Key: "model", Value: "custom-model"},
			{Key: "wire", Value: "chat"},
			{Key: "wire", Value: "responses"},
			{Key: "auth", Value: "oauth"},
			{Key: "auth", Value: "api_key"},
			{Key: "auth_file", Value: "first.json"},
			{Key: "auth_file", Value: "last.json"},
			{Key: "base_url", Value: "https://first.example"},
			{Key: "base_url", Value: "https://last.example"},
			{Key: "temperature", Value: "0.1"},
			{Key: "temperature", Value: "not-validated"},
		},
		Raw:     true,
		Version: true,
	}

	got, err := flags.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	want := Options{
		Provider: "openai",
		Model:    "custom-model",
		Wire:     "responses",
		Auth:     "api_key",
		AuthFile: "last.json",
		BaseURL:  "https://last.example",
		Settings: map[string]string{"temperature": "not-validated"},
		Raw:      true,
		Version:  true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Validate() = %#v, want %#v", got, want)
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
