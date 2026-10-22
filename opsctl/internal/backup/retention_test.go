package backup_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestRegenerateSnapshotRetention(t *testing.T) {
	// R-8QV7-053Z
	for _, test := range []struct {
		name      string
		period    *string
		interval  string
		retention string
	}{
		{name: "absent", retention: "216000s"},
		{name: "empty", period: stringPointer(""), retention: "216000s"},
		{name: "zero", period: stringPointer("0"), retention: "216000s"},
		{name: "zero with leading zeros", period: stringPointer("000"), retention: "216000s"},
		{name: "one second", period: stringPointer("1"), interval: "1s", retention: "2.5s"},
		{name: "even seconds", period: stringPointer("2"), interval: "2s", retention: "5s"},
		{name: "odd seconds", period: stringPointer("3"), interval: "3s", retention: "7.5s"},
		{name: "hourly", period: stringPointer("3600"), interval: "3600s", retention: "9000s"},
		{name: "daily", period: stringPointer("86400"), interval: "86400s", retention: "216000s"},
		{name: "leading zeros", period: stringPointer("00001"), interval: "1s", retention: "2.5s"},
		{name: "largest odd", period: stringPointer("3689348813"), interval: "3689348813s", retention: "9223372032.5s"},
		{name: "largest period", period: stringPointer("3689348814"), interval: "3689348814s", retention: "9223372035s"},
	} {
		for _, database := range []bool{false, true} {
			name := test.name + "/no database"
			if database {
				name = test.name + "/database"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				store := config.Store{Root: root}
				for key, value := range map[string]string{
					"aws.region":                 "us-west-2",
					"backup.s3_uri":              "s3://backups/hosts/example/",
					"backup.service_wal_seconds": "5",
				} {
					if err := store.Set(key, value); err != nil {
						t.Fatal(err)
					}
				}
				if test.period != nil {
					if err := store.Set("backup.service_db_seconds", *test.period); err != nil {
						t.Fatal(err)
					}
				}
				if database {
					writeService(t, root, "notes", "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
				}
				if _, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store); err != nil {
					t.Fatal(err)
				}
				configuration := readLitestream(t, root)
				if strings.Count(configuration, "\nsnapshot:\n") != 1 || strings.Count(configuration, "retention:") != 2 {
					t.Fatalf("configuration lacks a single snapshot map and only top-level retention settings: %s", configuration)
				}
				_, snapshot, _ := strings.Cut(configuration, "\nsnapshot:\n")
				var snapshotLines []string
				for _, line := range strings.Split(snapshot, "\n") {
					if !strings.HasPrefix(line, "  ") {
						break
					}
					snapshotLines = append(snapshotLines, line)
				}
				snapshot = strings.Join(snapshotLines, "\n") + "\n"
				if !strings.Contains(snapshot, "  retention: "+test.retention+"\n") {
					t.Fatalf("snapshot = %q; want retention %q", snapshot, test.retention)
				}
				if test.interval != "" && !strings.Contains(snapshot, "  interval: "+test.interval+"\n") {
					t.Fatalf("snapshot = %q; want interval %q", snapshot, test.interval)
				}
				if !strings.Contains(configuration, "\nretention:\n  enabled: true\n") {
					t.Fatalf("retention is not enabled: %s", configuration)
				}
				for _, forbidden := range []string{"l0-retention", "credential", "access-key", "secret-key"} {
					if strings.Contains(configuration, forbidden) {
						t.Fatalf("configuration contains %q: %s", forbidden, configuration)
					}
				}
			})
		}
	}
}
