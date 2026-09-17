package backup_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestZeroPeriodsConfigureReplicationWithoutRunningBackups(t *testing.T) {
	// R-RTJZ-N1L5
	for _, test := range []struct {
		name   string
		period *string
	}{
		{name: "absent"},
		{name: "empty", period: stringPointer("")},
		{name: "zero", period: stringPointer("0")},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := config.Store{Root: root}
			for key, value := range map[string]string{
				"aws.region":                   "us-west-2",
				"backup.s3_uri":                "s3://backups/hosts/example/",
				"backup.host_files_seconds":    "0",
				"backup.service_files_seconds": "0",
			} {
				if err := store.Set(key, value); err != nil {
					t.Fatalf("Set(%q): %v", key, err)
				}
			}
			if test.period != nil {
				for _, key := range []string{"backup.service_db_seconds", "backup.service_wal_seconds"} {
					if err := store.Set(key, *test.period); err != nil {
						t.Fatalf("Set(%q): %v", key, err)
					}
				}
			}
			writeService(t, root, "notes", "app = \"notes\"\n")

			changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store)
			if err != nil || !changed {
				t.Fatalf("Regenerate() = %v, %v, want true, nil", changed, err)
			}
			wantConfiguration := "region: 'us-west-2'\n" +
				"socket:\n  enabled: true\n  path: '" + filepath.ToSlash(filepath.Join(root, "var/run/litestream.sock")) + "'\n  permissions: 0600\n" +
				"retention:\n  enabled: false\n" +
				"dbs: []\n"
			if got := readLitestream(t, root); got != wantConfiguration {
				t.Fatalf("litestream.yml = %q, want %q", got, wantConfiguration)
			}

			var commands []host.Command
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				commands = append(commands, command)
				return host.Result{}, nil
			}}
			if err := backup.SetupReplication(context.Background(), env, store); err != nil {
				t.Fatal(err)
			}
			if err := backup.SetupTimers(context.Background(), env, store); err != nil {
				t.Fatal(err)
			}

			wantCommands := []host.Command{
				{Name: "systemctl", Args: []string{"enable", "litestream.service"}},
				{Name: "systemctl", Args: []string{"start", "litestream.service"}},
				{Name: "systemctl", Args: []string{"daemon-reload"}},
				{Name: "systemctl", Args: []string{"disable", "ikigenba-backup-host.timer"}},
				{Name: "systemctl", Args: []string{"stop", "ikigenba-backup-host.timer"}},
				{Name: "systemctl", Args: []string{"disable", "ikigenba-backup-services.timer"}},
				{Name: "systemctl", Args: []string{"stop", "ikigenba-backup-services.timer"}},
				{Name: "systemctl", Args: []string{"enable", "ikigenba-renew-certificate.timer"}},
				{Name: "systemctl", Args: []string{"restart", "ikigenba-renew-certificate.timer"}},
			}
			if !reflect.DeepEqual(commands, wantCommands) {
				t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
			}
			assertPublishedTimerUnits(t, root, expectedTimerUnits("", ""))
		})
	}
}
