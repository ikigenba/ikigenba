package opsctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvFileSetsKeyValuesAndIgnoresCommentsAndBlanks(t *testing.T) {
	// R-6AIE-QTDC
	keyOne := "OPSCTL_ENV_TEST_LOAD_ONE"
	keyTwo := "OPSCTL_ENV_TEST_LOAD_TWO"
	commentKey := "OPSCTL_ENV_TEST_COMMENT"
	blankKey := "OPSCTL_ENV_TEST_BLANK"
	unsetEnv(t, keyOne)
	unsetEnv(t, keyTwo)
	unsetEnv(t, commentKey)
	unsetEnv(t, blankKey)
	path := filepath.Join(t.TempDir(), "env")
	content := strings.Join([]string{
		"# " + commentKey + "=ignored",
		keyOne + "=alpha",
		"",
		keyTwo + "=beta",
		"#" + blankKey + "=ignored",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error: %v", err)
	}

	if got := os.Getenv(keyOne); got != "alpha" {
		t.Fatalf("%s = %q, want alpha", keyOne, got)
	}
	if got := os.Getenv(keyTwo); got != "beta" {
		t.Fatalf("%s = %q, want beta", keyTwo, got)
	}
	if got, ok := os.LookupEnv(commentKey); ok {
		t.Fatalf("%s was set from a comment line to %q", commentKey, got)
	}
	if got, ok := os.LookupEnv(blankKey); ok {
		t.Fatalf("%s was set from a blank/comment line to %q", blankKey, got)
	}
}

func TestLoadEnvFileDoesNotOverrideExistingEnvironment(t *testing.T) {
	// R-6BQB-4L41
	key := "OPSCTL_ENV_TEST_EXISTING"
	t.Setenv(key, "original")
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte(key+"=replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error: %v", err)
	}

	if got := os.Getenv(key); got != "original" {
		t.Fatalf("%s = %q, want original", key, got)
	}
}

func TestLoadEnvFileMissingPathReturnsNilAndDoesNotSetVariables(t *testing.T) {
	// R-6CY7-ICUQ
	key := "OPSCTL_ENV_TEST_MISSING"
	unsetEnv(t, key)
	path := filepath.Join(t.TempDir(), "missing-env")

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error for missing path: %v", err)
	}

	if got, ok := os.LookupEnv(key); ok {
		t.Fatalf("%s was set while loading a missing file to %q", key, got)
	}
}

func TestLoadEnvFileRejectsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte("OPSCTL_ENV_TEST_OK=value\nmalformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := LoadEnvFile(path); err == nil {
		t.Fatal("LoadEnvFile accepted a malformed non-comment line with no equals")
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, hadOld := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
