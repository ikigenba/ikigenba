package options_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/ikigenba/ikigenba/dory/internal/options"
)

type parseSignature func([]string) (options.Flags, error)
type validateSignature func(options.Flags) (options.Options, error)
type usageSignature func() string

var (
	_ parseSignature    = options.ParseFlags
	_ validateSignature = options.Flags.Validate
	_ usageSignature    = options.Usage
)

// R-HIPA-5Z4K
func TestPairPublicShape(t *testing.T) {
	t.Parallel()

	want := []reflect.StructField{
		{Name: "Key", Type: reflect.TypeFor[string]()},
		{Name: "Value", Type: reflect.TypeFor[string]()},
	}
	assertPublicShape(t, reflect.TypeFor[options.Pair](), want)
}

// R-HJX6-JQV9
func TestFlagsPublicShape(t *testing.T) {
	t.Parallel()

	want := []reflect.StructField{
		{Name: "Config", Type: reflect.TypeFor[[]options.Pair]()},
		{Name: "Resume", Type: reflect.TypeFor[string]()},
		{Name: "Version", Type: reflect.TypeFor[bool]()},
	}
	assertPublicShape(t, reflect.TypeFor[options.Flags](), want)
}

// R-HL52-XILY
func TestPublicOperationsAndHelpSentinel(t *testing.T) {
	t.Parallel()

	if options.Usage() == "" {
		t.Fatal("Usage must be non-empty")
	}

	for _, argument := range []string{"-h", "--help"} {
		argument := argument
		t.Run(argument, func(t *testing.T) {
			t.Parallel()
			_, err := options.ParseFlags([]string{argument})
			if !errors.Is(err, options.ErrHelp) {
				t.Fatalf("ParseFlags(%q) error = %v, want ErrHelp", argument, err)
			}
		})
	}
}

// R-HMCZ-BACN
func TestParseFlagsPopulatesFieldsAndDefaultsToZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want options.Flags
	}{
		{name: "zero", want: options.Flags{}},
		{name: "short config", args: []string{"-c", "model=fast"}, want: options.Flags{Config: []options.Pair{{Key: "model", Value: "fast"}}}},
		{name: "long config", args: []string{"--c", "model=fast"}, want: options.Flags{Config: []options.Pair{{Key: "model", Value: "fast"}}}},
		{name: "short resume", args: []string{"-resume", "session"}, want: options.Flags{Resume: "session"}},
		{name: "long resume", args: []string{"--resume", "session"}, want: options.Flags{Resume: "session"}},
		{name: "short version", args: []string{"-V"}, want: options.Flags{Version: true}},
		{name: "long version", args: []string{"--version"}, want: options.Flags{Version: true}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := options.ParseFlags(test.args)
			if err != nil {
				t.Fatalf("ParseFlags(%q) error = %v", test.args, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseFlags(%q) = %#v, want %#v", test.args, got, test.want)
			}
		})
	}
}

// R-HNKV-P23C
func TestParseFlagsPreservesConfigArguments(t *testing.T) {
	t.Parallel()

	got, err := options.ParseFlags([]string{
		"-c", "model=first",
		"--c", "empty=",
		"-c", "model=second=with=equals",
	})
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	want := []options.Pair{
		{Key: "model", Value: "first"},
		{Key: "empty", Value: ""},
		{Key: "model", Value: "second=with=equals"},
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
	}
}

// R-HNKV-P23C
func TestParseFlagsRejectsMalformedConfig(t *testing.T) {
	t.Parallel()

	for _, argument := range []string{"missing-equals", "=empty-key"} {
		argument := argument
		t.Run(argument, func(t *testing.T) {
			t.Parallel()
			_, err := options.ParseFlags([]string{"-c", argument})
			if err == nil {
				t.Fatalf("ParseFlags(-c %q) succeeded, want error", argument)
			}
			if !strings.Contains(err.Error(), argument) {
				t.Fatalf("ParseFlags(-c %q) error = %q, want original argument", argument, err)
			}
		})
	}
}

// R-HL52-XILY
func TestParseFlagsRejectsUnknownFlagAndPositionals(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		text string
	}{
		{name: "unknown flag", args: []string{"-wat"}, text: "wat"},
		{name: "positional argument", args: []string{"prompt.txt"}, text: "prompt.txt"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := options.ParseFlags(test.args)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("ParseFlags(%q) error = %v, want error containing %q", test.args, err, test.text)
			}
		})
	}
}

// R-HUW9-ZOJI
func TestValidatedOptionsPublicShape(t *testing.T) {
	t.Parallel()

	assertPublicShape(t, reflect.TypeFor[options.Role](), []reflect.StructField{
		{Name: "Provider", Type: reflect.TypeFor[string]()},
		{Name: "Model", Type: reflect.TypeFor[string]()},
		{Name: "Wire", Type: reflect.TypeFor[string]()},
		{Name: "Auth", Type: reflect.TypeFor[string]()},
		{Name: "AuthFile", Type: reflect.TypeFor[string]()},
		{Name: "BaseURL", Type: reflect.TypeFor[string]()},
		{Name: "MaxContext", Type: reflect.TypeFor[int64]()},
		{Name: "Settings", Type: reflect.TypeFor[map[string]string]()},
	})
	assertPublicShape(t, reflect.TypeFor[options.Options](), []reflect.StructField{
		{Name: "Supervisor", Type: reflect.TypeFor[options.Role]()},
		{Name: "Worker", Type: reflect.TypeFor[options.Role]()},
		{Name: "Resume", Type: reflect.TypeFor[string]()},
		{Name: "Version", Type: reflect.TypeFor[bool]()},
	})
}

// R-HW46-DGA7
func TestValidateFoldsAndRoutesRoleConfiguration(t *testing.T) {
	t.Parallel()

	got, err := (options.Flags{Config: []options.Pair{
		{Key: "supervisor.provider", Value: "anthropic"},
		{Key: "supervisor.model", Value: "old"},
		{Key: "supervisor.model", Value: "claude-custom"},
		{Key: "supervisor.wire", Value: "messages"},
		{Key: "supervisor.auth", Value: "oauth"},
		{Key: "supervisor.auth_file", Value: "/tokens/supervisor.json"},
		{Key: "supervisor.base_url", Value: "https://supervisor.example"},
		{Key: "supervisor.temperature", Value: "0.1"},
		{Key: "supervisor.temperature", Value: "0.7=exact"},
		{Key: "worker.provider", Value: "openrouter"},
		{Key: "worker.model", Value: "vendor/custom"},
		{Key: "worker.wire", Value: "chat"},
		{Key: "worker.auth", Value: "api_key"},
		{Key: "worker.auth_file", Value: "/tokens/worker.json"},
		{Key: "worker.base_url", Value: "https://worker.example"},
		{Key: "worker.custom_name", Value: "verbatim=value"},
	}}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	wantSupervisor := options.Role{
		Provider: "anthropic", Model: "claude-custom", Wire: "messages", Auth: "oauth",
		AuthFile: "/tokens/supervisor.json", BaseURL: "https://supervisor.example", MaxContext: -1,
		Settings: map[string]string{"temperature": "0.7=exact"},
	}
	wantWorker := options.Role{
		Provider: "openrouter", Model: "vendor/custom", Wire: "chat", Auth: "api_key",
		AuthFile: "/tokens/worker.json", BaseURL: "https://worker.example", MaxContext: -1,
		Settings: map[string]string{"custom_name": "verbatim=value"},
	}
	if !reflect.DeepEqual(got.Supervisor, wantSupervisor) {
		t.Errorf("Supervisor = %#v, want %#v", got.Supervisor, wantSupervisor)
	}
	if !reflect.DeepEqual(got.Worker, wantWorker) {
		t.Errorf("Worker = %#v, want %#v", got.Worker, wantWorker)
	}

	for _, key := range []string{"model", "manager.model", "other.value"} {
		_, validateErr := (options.Flags{Config: []options.Pair{{Key: key, Value: "x"}}}).Validate()
		assertErrorContains(t, validateErr, key)
	}
}

// R-HXC2-R80W
func TestValidateMaxContext(t *testing.T) {
	t.Parallel()

	got, err := (options.Flags{Config: []options.Pair{
		{Key: "supervisor.max_context", Value: "10"},
		{Key: "supervisor.max_context", Value: "20"},
		{Key: "worker.max_context", Value: "0"},
	}}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got.Supervisor.MaxContext != 20 || got.Worker.MaxContext != 0 {
		t.Fatalf("MaxContext = supervisor %d, worker %d; want 20 and 0", got.Supervisor.MaxContext, got.Worker.MaxContext)
	}

	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "supervisor.max_context", value: "-1"},
		{key: "worker.max_context", value: "1.5"},
		{key: "supervisor.max_context", value: ""},
		{key: "worker.max_context", value: "9223372036854775808"},
	} {
		_, validateErr := (options.Flags{Config: []options.Pair{{Key: test.key, Value: test.value}}}).Validate()
		assertErrorContains(t, validateErr, test.key)
	}
}

// R-HYJZ-4ZRL
func TestValidateDefaultsModelsAndChecksEnumerations(t *testing.T) {
	t.Parallel()

	defaults, err := (options.Flags{}).Validate()
	if err != nil {
		t.Fatalf("Validate(defaults) error = %v", err)
	}
	if defaults.Supervisor.Model != "gpt-5.6-sol" || defaults.Worker.Model != "gpt-5.6-sol" {
		t.Fatalf("default models = %q and %q, want gpt-5.6-sol", defaults.Supervisor.Model, defaults.Worker.Model)
	}

	accepted := map[string][]string{
		"provider": {"anthropic", "gemini", "openai", "openrouter", "xai"},
		"auth":     {"api_key", "oauth"},
		"wire":     {"messages", "generate-content", "chat", "responses"},
	}
	for name, values := range accepted {
		for index, value := range values {
			role := "supervisor"
			if index%2 == 1 {
				role = "worker"
			}
			config := []options.Pair{{Key: role + "." + name, Value: value}}
			if name == "provider" {
				config = append(config, options.Pair{Key: role + ".model", Value: "off-catalog"})
			}
			if _, validateErr := (options.Flags{Config: config}).Validate(); validateErr != nil {
				t.Errorf("Validate(%s.%s=%q) error = %v", role, name, value, validateErr)
			}
		}
	}

	rejected := []options.Pair{
		{Key: "supervisor.provider", Value: "invalid-host"},
		{Key: "worker.auth", Value: "password"},
		{Key: "supervisor.wire", Value: "completions"},
		{Key: "worker.model", Value: ""},
		{Key: "worker.provider", Value: ""},
		{Key: "supervisor.auth", Value: ""},
		{Key: "worker.wire", Value: ""},
	}
	for _, pair := range rejected {
		_, validateErr := (options.Flags{Config: []options.Pair{pair}}).Validate()
		assertErrorContains(t, validateErr, pair.Key)
		if pair.Value != "" {
			assertErrorContains(t, validateErr, pair.Value)
		}
	}
}

// R-I0ZR-WJ8Z
func TestValidateDerivesProviderOrAcceptsExplicitProvider(t *testing.T) {
	t.Parallel()

	got, err := (options.Flags{Config: []options.Pair{
		{Key: "supervisor.model", Value: "claude-opus-5"},
	}}).Validate()
	if err != nil {
		t.Fatalf("Validate(catalog models) error = %v", err)
	}
	if got.Supervisor.Provider != "anthropic" || got.Worker.Provider != "openai" {
		t.Fatalf("derived providers = %q and %q, want anthropic and openai", got.Supervisor.Provider, got.Worker.Provider)
	}

	explicit, err := (options.Flags{Config: []options.Pair{
		{Key: "worker.provider", Value: "xai"},
		{Key: "worker.model", Value: "future-model"},
	}}).Validate()
	if err != nil {
		t.Fatalf("Validate(explicit provider) error = %v", err)
	}
	if explicit.Worker.Provider != "xai" || explicit.Worker.Model != "future-model" {
		t.Fatalf("Worker = %#v, want explicit off-catalog model", explicit.Worker)
	}

	_, err = (options.Flags{Config: []options.Pair{{Key: "worker.model", Value: "not-in-the-catalog"}}}).Validate()
	assertErrorContains(t, err, "worker.model")
}

// R-I27O-AAZO
func TestValidateDoesNotResolveCredentialsOrAuthFiles(t *testing.T) {
	for _, variable := range []string{"ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "XAI_API_KEY"} {
		t.Setenv(variable, "")
	}
	nonexistent := filepath.Join(t.TempDir(), "missing", "oauth.json")

	got, err := (options.Flags{Config: []options.Pair{
		{Key: "supervisor.auth_file", Value: nonexistent},
		{Key: "worker.auth_file", Value: nonexistent},
	}}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got.Supervisor.AuthFile != nonexistent || got.Worker.AuthFile != nonexistent {
		t.Fatalf("auth files = %q and %q, want %q", got.Supervisor.AuthFile, got.Worker.AuthFile, nonexistent)
	}
	if _, statErr := os.Stat(nonexistent); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("auth file stat error = %v, want file to remain nonexistent", statErr)
	}
}

// R-HOSS-2TU1
func TestValidateRequiresCanonicalLowercaseResumeUUID(t *testing.T) {
	t.Parallel()

	const valid = "123e4567-e89b-12d3-a456-426614174000"
	got, err := (options.Flags{Resume: valid, Version: true}).Validate()
	if err != nil {
		t.Fatalf("Validate(valid resume) error = %v", err)
	}
	if got.Resume != valid || !got.Version {
		t.Fatalf("Resume, Version = %q, %t; want %q, true", got.Resume, got.Version, valid)
	}

	for _, value := range []string{
		"123E4567-E89B-12D3-A456-426614174000",
		"urn:uuid:123e4567-e89b-12d3-a456-426614174000",
		"123e4567e89b12d3a456426614174000",
		"not-a-uuid",
	} {
		_, validateErr := (options.Flags{Resume: value}).Validate()
		assertErrorContains(t, validateErr, "resume")
	}
}

// R-HQ0O-GLKQ
func TestUsageNamesGrammarAndEveryRoleKey(t *testing.T) {
	t.Parallel()

	usage := options.Usage()
	firstLine, _, _ := strings.Cut(usage, "\n")
	const wantFirstLine = "usage: dory [-c key=value ...] [-resume UUID] [-V] [-h] < prompt"
	if firstLine != wantFirstLine {
		t.Fatalf("Usage first line = %q, want %q", firstLine, wantFirstLine)
	}
	named := make(map[string]bool)
	for _, token := range strings.FieldsFunc(usage, func(character rune) bool {
		return unicode.IsSpace(character) || character == ','
	}) {
		named[token] = true
	}
	for _, role := range []string{"supervisor", "worker"} {
		for _, key := range []string{"provider", "model", "wire", "auth", "auth_file", "base_url", "max_context"} {
			name := role + "." + key
			if !named[name] {
				t.Errorf("Usage() does not name %q", name)
			}
		}
	}
}

func assertErrorContains(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Validate() succeeded, want error containing %q", text)
	}
	if !strings.Contains(err.Error(), text) {
		t.Fatalf("Validate() error = %q, want it to contain %q", err, text)
	}
}

func assertPublicShape(t *testing.T, got reflect.Type, want []reflect.StructField) {
	t.Helper()
	if got.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", got, got.NumField(), len(want))
	}
	for index, wantField := range want {
		gotField := got.Field(index)
		if gotField.Name != wantField.Name || gotField.Type != wantField.Type || gotField.PkgPath != "" {
			t.Errorf("%s field %d = %s %s (PkgPath %q), want exported %s %s", got, index, gotField.Name, gotField.Type, gotField.PkgPath, wantField.Name, wantField.Type)
		}
	}
}
