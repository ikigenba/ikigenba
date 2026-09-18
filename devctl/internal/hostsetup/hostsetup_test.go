package hostsetup

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestInstallLatestAndConfigureTenKeys(t *testing.T) {
	// R-EDKC-O6NQ
	var commands []string
	deps := seam.Deps{Dir: ".", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		logical := command.Args[len(command.Args)-1]
		commands = append(commands, logical)
		if strings.Contains(logical, "opsctl-install.sh") {
			return seam.Result{Stdout: []byte("opsctl v9.8.7\n")}, nil
		}
		return seam.Result{}, nil
	}}
	target := host.Host{Address: "192.0.2.10", Deps: deps}

	version, err := InstallLatest(context.Background(), target)
	if err != nil || version != "v9.8.7" {
		t.Fatalf("InstallLatest() = %q, %v", version, err)
	}
	if !strings.Contains(commands[0], "releases/latest/download/opsctl-install.sh") ||
		strings.Contains(commands[0], "v9.8.7") {
		t.Fatalf("install command does not select latest dynamically: %q", commands[0])
	}

	config := Config{
		Domain:                    "foo.sbx.ikigenba.dev",
		ZoneName:                  "sbx.ikigenba.dev",
		ZoneID:                    "ZONE1",
		Region:                    "us-east-2",
		BackupBucket:              "account-backups",
		ACMEEmail:                 "ops@ikigenba.dev",
		BackupHostFilesSeconds:    11,
		BackupServiceFilesSeconds: 22,
		BackupServiceDBSeconds:    33,
		BackupServiceWALSeconds:   44,
	}
	if err := Configure(context.Background(), target, config); err != nil {
		t.Fatalf("Configure(): %v", err)
	}
	want := []string{
		"'sudo' 'opsctl' 'config' 'set' 'host.name' 'foo.sbx.ikigenba.dev'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.provider' 'route53'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.zones' 'sbx.ikigenba.dev:ZONE1'",
		"'sudo' 'opsctl' 'config' 'set' 'aws.region' 'us-east-2'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.s3_uri' 's3://account-backups/foo.sbx.ikigenba.dev/'",
		"'sudo' 'opsctl' 'config' 'set' 'acme.email' 'ops@ikigenba.dev'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.host_files_seconds' '11'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_files_seconds' '22'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_db_seconds' '33'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_wal_seconds' '44'",
	}
	if got := commands[1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("Configure commands = %#v, want %#v", got, want)
	}
}
