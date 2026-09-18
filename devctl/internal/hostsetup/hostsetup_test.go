package hostsetup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestInstallLatestAndConfigureTenKeys(t *testing.T) {
	// R-EDKC-O6NQ
	var commands []string
	deps := seam.Deps{Dir: ".", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		if command.Path == "curl" {
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v9.8.7","published_at":"2026-09-17T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://downloads.example/opsctl-install.sh"}]}]`)}, nil
		}
		logical := command.Args[len(command.Args)-1]
		commands = append(commands, logical)
		return seam.Result{}, nil
	}}
	target := host.Host{Address: "192.0.2.10", Deps: deps}

	version, err := InstallLatest(context.Background(), target)
	if err != nil || version != "v9.8.7" {
		t.Fatalf("InstallLatest() = %q, %v", version, err)
	}
	if !strings.Contains(commands[0], "https://downloads.example/opsctl-install.sh") ||
		strings.Contains(commands[0], "v9.8.7") {
		t.Fatalf("install command does not select latest dynamically: %q", commands[0])
	}
	if !strings.Contains(commands[1], "'/tmp/opsctl-install' 'v9.8.7'") {
		t.Fatalf("install command does not use selected version: %q", commands[1])
	}

	props := account.Properties{
		Region:                    "us-east-2",
		BackupBucket:              "account-backups",
		BackupHostFilesSeconds:    11,
		BackupServiceFilesSeconds: 22,
		BackupServiceDBSeconds:    33,
		BackupServiceWALSeconds:   44,
	}
	email := "ops@ikigenba.dev"
	count, err := Configure(context.Background(), target, props,
		cloud.Zone{Name: "sbx.ikigenba.dev", ID: "ZONE1"}, "foo.sbx.ikigenba.dev", &email)
	if err != nil || count != 10 {
		t.Fatalf("Configure() = %d, %v", count, err)
	}
	want := []string{
		"'sudo' 'opsctl' 'config' 'set' 'host.name' 'foo.sbx.ikigenba.dev'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.provider' 'route53'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.zones' 'sbx.ikigenba.dev:ZONE1'",
		"'sudo' 'opsctl' 'config' 'set' 'aws.region' 'us-east-2'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.s3_uri' 's3://account-backups/foo.sbx.ikigenba.dev/'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.host_files_seconds' '11'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_files_seconds' '22'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_db_seconds' '33'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_wal_seconds' '44'",
		"'sudo' 'opsctl' 'config' 'set' 'acme.email' 'ops@ikigenba.dev'",
	}
	if got := commands[2:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("Configure commands = %#v, want %#v", got, want)
	}
}

func TestInstallLatestPropagatesDownloadFailure(t *testing.T) {
	// R-EDKC-O6NQ
	var commands []string
	target := host.Host{Address: "192.0.2.10", Deps: seam.Deps{Dir: ".", Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "curl" {
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v1.0.0","published_at":"2026-09-17T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://downloads.example/install.sh"}]}]`)}, nil
		}
		commands = append(commands, cmd.Args[len(cmd.Args)-1])
		return seam.Result{ExitCode: 22}, nil
	}}}

	version, err := InstallLatest(context.Background(), target)
	var commandErr *host.CommandError
	if version != "" || !errors.As(err, &commandErr) || commandErr.Status != 22 {
		t.Fatalf("InstallLatest() = %q, %#v", version, err)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "https://downloads.example/install.sh") {
		t.Fatalf("install commands = %#v", commands)
	}
	if strings.Contains(commands[0], "bash") {
		t.Fatalf("installer ran after download failure: %q", commands[0])
	}
}
