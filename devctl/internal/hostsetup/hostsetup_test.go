package hostsetup

import (
	"context"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func TestConfigureTenKeys(t *testing.T) {
	var commands []string
	deps := seam.Deps{Dir: ".", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		logical := command.Args[len(command.Args)-1]
		commands = append(commands, logical)
		return seam.Result{}, nil
	}}
	target := host.Host{Address: "192.0.2.10", Deps: deps}

	periods := BackupPeriods{
		HostFilesSeconds:    11,
		ServiceFilesSeconds: 22,
		ServiceDBSeconds:    33,
		ServiceWALSeconds:   44,
	}
	cfg := Config{
		Root:    "ikigenba.dev",
		Region:  "us-east-2",
		ZoneID:  "ZONE1",
		Space:   spaceref.Space{Label: "foo", Domain: "foo.ikigenba.dev"},
		Email:   "ops@ikigenba.dev",
		Periods: &periods,
	}
	count, err := Configure(context.Background(), target, "opsctl", cfg)
	if err != nil || count != 10 {
		t.Fatalf("Configure() = %d, %v", count, err)
	}
	want := []string{
		"'sudo' 'opsctl' 'config' 'set' 'host.name=foo.ikigenba.dev'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.provider=route53'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.zones=ikigenba.dev:ZONE1'",
		"'sudo' 'opsctl' 'config' 'set' 'aws.region=us-east-2'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.s3_uri=s3://ikigenba.dev/foo/'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.host_files_seconds=11'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_files_seconds=22'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_db_seconds=33'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_wal_seconds=44'",
		"'sudo' 'opsctl' 'config' 'set' 'acme.email=ops@ikigenba.dev'",
	}
	if got := commands; !reflect.DeepEqual(got, want) {
		t.Fatalf("Configure commands = %#v, want %#v", got, want)
	}
}
