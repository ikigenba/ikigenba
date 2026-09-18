package hostsetup

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// R-G03J-1692
var _ func(context.Context, host.Host, account.Properties, cloud.Zone, string, *string) (int, error) = Configure

func TestConfigureSetsNineKeysInOrder(t *testing.T) {
	// R-YHTO-8IAI
	// R-G7EX-BSP8
	var commands []string
	target := host.Host{Address: "192.0.2.10", Deps: seam.Deps{Dir: "/work", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command.Args[len(command.Args)-1])
		return seam.Result{}, nil
	}}}
	props := account.Properties{
		Region:                    "us-east-2",
		BackupBucket:              "account-backups",
		BackupHostFilesSeconds:    11,
		BackupServiceFilesSeconds: 22,
		BackupServiceDBSeconds:    33,
		BackupServiceWALSeconds:   44,
	}

	count, err := Configure(context.Background(), target, props,
		cloud.Zone{Name: "sbx.ikigenba.dev", ID: "ZONE1"}, "foo.sbx.ikigenba.dev", nil)
	if err != nil || count != 9 {
		t.Fatalf("Configure() = %d, %v", count, err)
	}
	want := []string{
		"'sudo' 'opsctl' 'config' 'set' 'host.name=foo.sbx.ikigenba.dev'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.provider=route53'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.zones=sbx.ikigenba.dev:ZONE1'",
		"'sudo' 'opsctl' 'config' 'set' 'aws.region=us-east-2'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.s3_uri=s3://account-backups/foo.sbx.ikigenba.dev/'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.host_files_seconds=11'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_files_seconds=22'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_db_seconds=33'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_wal_seconds=44'",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("Configure commands = %#v, want %#v", commands, want)
	}
}

func TestConfigureOptionalEmailAndFirstFailure(t *testing.T) {
	// R-G8MT-PKFX
	props := account.Properties{
		Region:                    "us-west-2",
		BackupBucket:              "backups",
		BackupHostFilesSeconds:    1,
		BackupServiceFilesSeconds: 2,
		BackupServiceDBSeconds:    3,
		BackupServiceWALSeconds:   4,
	}
	email := "ops@example.com"

	t.Run("email is the tenth and final key", func(t *testing.T) {
		var commands []string
		target := host.Host{Address: "192.0.2.10", Deps: seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command.Args[len(command.Args)-1])
			return seam.Result{}, nil
		}}}
		count, err := Configure(context.Background(), target, props,
			cloud.Zone{Name: "example.com", ID: "ZONE2"}, "app.example.com", &email)
		if err != nil || count != 10 {
			t.Fatalf("Configure() = %d, %v", count, err)
		}
		if len(commands) != 10 || commands[9] != "'sudo' 'opsctl' 'config' 'set' 'acme.email=ops@example.com'" {
			t.Fatalf("Configure commands = %#v", commands)
		}
	})

	t.Run("failure returns completed count and stops", func(t *testing.T) {
		var commands []string
		target := host.Host{Address: "192.0.2.10", Deps: seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			commands = append(commands, command.Args[len(command.Args)-1])
			if len(commands) == 4 {
				return seam.Result{ExitCode: 23}, nil
			}
			return seam.Result{}, nil
		}}}
		count, err := Configure(context.Background(), target, props,
			cloud.Zone{Name: "example.com", ID: "ZONE2"}, "app.example.com", &email)
		var commandErr *host.CommandError
		if count != 3 || !errors.As(err, &commandErr) || commandErr.Step != "opsctl" || commandErr.Status != 23 {
			t.Fatalf("Configure() = %d, %#v", count, err)
		}
		if len(commands) != 4 {
			t.Fatalf("Configure executed %d commands after failure, want 4", len(commands))
		}
	})
}
