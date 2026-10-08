package apps_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
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
