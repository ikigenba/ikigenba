package apps_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestPrepareAndPublishEnvironmentForDataOnlyService(t *testing.T) {
	for _, service := range []string{"crm.v2", "backup-host", "ledger"} {
		root := t.TempDir()
		manifest := apps.Manifest{Secrets: []string{"TOKEN", "TOKEN"}, Env: map[string]string{"MODE": "current"}}
		client := &installCloudClient{readSecrets: func(_ context.Context, parameter string) (map[string]string, error) {
			if parameter != "/host.example/"+service {
				t.Fatalf("parameter %q", parameter)
			}
			return map[string]string{"TOKEN": "a secret", "UNUSED": "ignored"}, nil
		}}
		store := installStoreAt(t, root, map[string]string{"apps.drain_seconds": "7", "apps.stop_seconds": "9"})
		before := snapshotTree(t, root)
		data, err := apps.PrepareEnvironment(t.Context(), client, store, "host.example", service, manifest)
		if err != nil {
			t.Fatal(err)
		}
		if manifest.App != "" {
			t.Fatalf("incoming manifest modified: %#v", manifest)
		}
		if !reflect.DeepEqual(before, snapshotTree(t, root)) {
			t.Fatal("preparing environment wrote files")
		}
		want := "TOKEN=\"a secret\"\nMODE=\"current\"\nDRAIN_SECONDS=7\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n"
		if string(data) != want {
			t.Fatalf("environment %q", data)
		}
		destination := filepath.Join(root, "etc", "opt", "ikigenba", service, "env")
		writeFixture(t, destination, []byte("archive environment"), 0644)
		if err := apps.PublishEnvironment(root, service, data); err != nil {
			t.Fatal(err)
		}
		assertFile(t, destination, want)
		info, err := os.Stat(destination)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("mode: %v, %v", info, err)
		}
	}
}

// R-T787-GQP5 R-T8G3-UIFU R-5RIZ-EOX9
func TestPerAppEnvironmentSecretsAndValidation(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest apps.Manifest
		values   map[string]string
		failure  error
		want     string
	}{
		{"empty", apps.Manifest{Env: map[string]string{"MODE": "plain"}}, nil, nil, "MODE=\"plain\"\nDRAIN_SECONDS=7\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n"},
		{"requested only", apps.Manifest{Secrets: []string{"TOKEN", "TOKEN"}}, map[string]string{"TOKEN": "", "OTHER": "ignored"}, nil, "TOKEN=\"\"\nDRAIN_SECONDS=7\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n"},
		{"missing parameter", apps.Manifest{Secrets: []string{"FIRST", "SECOND"}}, nil, fmt.Errorf("missing: %w", cloud.ErrNotFound), "notes: no value for 'FIRST' in /host.example/notes"},
		{"missing in order", apps.Manifest{Secrets: []string{"SECOND", "FIRST"}}, nil, nil, "notes: no value for 'SECOND' in /host.example/notes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := installStoreAt(t, root, map[string]string{"apps.drain_seconds": "7", "apps.stop_seconds": "9"})
			calls := 0
			client := &installCloudClient{readSecrets: func(_ context.Context, parameter string) (map[string]string, error) {
				calls++
				if parameter != "/host.example/notes" {
					t.Fatalf("parameter %q", parameter)
				}
				return test.values, test.failure
			}}
			before := snapshotTree(t, root)
			data, err := apps.PrepareEnvironment(t.Context(), client, store, host.NormalizeName("HOST.Example."), "notes", test.manifest)
			if strings.HasPrefix(test.want, "notes:") {
				if err == nil || err.Error() != test.want {
					t.Fatalf("error %v want %q", err, test.want)
				}
			} else if err != nil || string(data) != test.want {
				t.Fatalf("environment %q error %v", data, err)
			}
			wantCalls := 1
			if len(test.manifest.Secrets) == 0 {
				wantCalls = 0
			}
			if calls != wantCalls || !reflect.DeepEqual(before, snapshotTree(t, root)) {
				t.Fatalf("calls %d or files changed", calls)
			}
		})
	}
	for _, manifest := range []apps.Manifest{
		{Secrets: []string{"BAD-NAME"}}, {Env: map[string]string{"1BAD": "private"}}, {Secrets: []string{"DRAIN_SECONDS"}}, {Env: map[string]string{apps.ServicesEnv: "private"}}, {Secrets: []string{"KEY"}, Env: map[string]string{"KEY": "private"}}, {Env: map[string]string{"KEY": "private\x00"}}, {Env: map[string]string{"KEY": "private\r"}}, {Env: map[string]string{"KEY": "private\n"}},
	} {
		root := t.TempDir()
		store := installStoreAt(t, root, nil)
		before := snapshotTree(t, root)
		client := &installCloudClient{readSecrets: func(context.Context, string) (map[string]string, error) {
			values := map[string]string{}
			for _, name := range manifest.Secrets {
				values[name] = "private"
			}
			return values, nil
		}}
		data, err := apps.PrepareEnvironment(t.Context(), client, store, "host", "notes", manifest)
		if err == nil || data != nil || strings.Contains(err.Error(), "private") || !reflect.DeepEqual(before, snapshotTree(t, root)) {
			t.Fatalf("invalid environment data %q error %v", data, err)
		}
	}
}

// R-T8G3-UIFU
func TestPerAppEnvironmentPreservesLiteralEncodingAndRejectsSecretControls(t *testing.T) {
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	manifest := apps.Manifest{Secrets: []string{"TOKEN"}, Env: map[string]string{"MODE": "quote\" backslash\\ dollar$ tab\t single'"}}
	client := &installCloudClient{readSecrets: func(context.Context, string) (map[string]string, error) {
		return map[string]string{"TOKEN": "secret\"\\$\t'"}, nil
	}}
	before := snapshotTree(t, root)
	data, err := apps.PrepareEnvironment(t.Context(), client, store, "host", "notes", manifest)
	want := map[string]string{"TOKEN": "secret\"\\$\t'", "MODE": manifest.Env["MODE"], "DRAIN_SECONDS": "5", apps.ServicesEnv: apps.PerAppServicesPath}
	if err != nil || !reflect.DeepEqual(decodeEnvironmentFile(t, data), want) || !reflect.DeepEqual(before, snapshotTree(t, root)) {
		t.Fatalf("environment %q error %v", data, err)
	}
	for _, entry := range []string{"DRAIN_SECONDS=5\n", apps.ServicesEnv + "=" + apps.PerAppServicesPath + "\n"} {
		if !strings.Contains(string(data), entry) {
			t.Fatalf("required unquoted entry absent from %q", data)
		}
	}
	for _, value := range []string{"private\x00", "private\r", "private\n"} {
		client.readSecrets = func(context.Context, string) (map[string]string, error) {
			return map[string]string{"TOKEN": value}, nil
		}
		data, err := apps.PrepareEnvironment(t.Context(), client, store, "host", "notes", apps.Manifest{Secrets: []string{"TOKEN"}})
		if err == nil || data != nil || strings.Contains(err.Error(), "private") || !reflect.DeepEqual(before, snapshotTree(t, root)) {
			t.Fatalf("forbidden secret data %q error %v", data, err)
		}
	}
}

// decodeEnvironmentFile interprets the EnvironmentFile forms independently:
// quoted values preserve whitespace; unquoted values trim exterior whitespace.
// A double-quoted backslash escapes only backslash, quote, dollar and backtick.
func decodeEnvironmentFile(t *testing.T, data []byte) map[string]string {
	t.Helper()
	result := map[string]string{}
	text := strings.ReplaceAll(string(data), "\\\n", "")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			t.Fatalf("invalid environment entry %q", line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		var decoded strings.Builder
		quote := byte(0)
		if len(value) > 0 && (value[0] == '"' || value[0] == '\'') {
			quote = value[0]
			value = value[1:]
		}
		closed := quote == 0
		for i := 0; i < len(value); i++ {
			ch := value[i]
			if quote != 0 && ch == quote {
				if strings.TrimSpace(value[i+1:]) != "" {
					t.Fatalf("content after quoted value %q", line)
				}
				closed = true
				break
			}
			if ch == '\\' && quote != '\'' && i+1 < len(value) {
				next := value[i+1]
				if quote == 0 || strings.ContainsRune("\\\"$`", rune(next)) {
					decoded.WriteByte(next)
					i++
					continue
				}
			}
			decoded.WriteByte(ch)
		}
		if !closed {
			t.Fatalf("unclosed environment value %q", line)
		}
		if _, present := result[name]; present {
			t.Fatalf("duplicate environment key %q", name)
		}
		result[name] = decoded.String()
	}
	return result
}
