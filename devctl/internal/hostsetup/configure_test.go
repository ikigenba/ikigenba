package hostsetup

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func TestConfigurationContract(t *testing.T) {
	// R-5O2A-WNPM
	keys := []string{
		KeyHostName, KeyHostApex, KeyACMEEmail, KeyAWSRegion, KeyBackupS3URI,
		KeyBackupServiceFilesSeconds, KeyBackupHostFilesSeconds,
		KeyBackupServiceDBSeconds, KeyBackupServiceWALSeconds,
		KeyDNSProvider, KeyDNSZones,
	}
	wantKeys := []string{
		"host.name", "host.apex", "acme.email", "aws.region", "backup.s3_uri",
		"backup.service_files_seconds", "backup.host_files_seconds",
		"backup.service_db_seconds", "backup.service_wal_seconds",
		"dns.provider", "dns.zones",
	}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("configuration keys = %#v, want %#v", keys, wantKeys)
	}

	// R-8B46-A2Q8
	requireExactFields(t, reflect.TypeFor[BackupPeriods](), []fieldSpec{
		{"HostFilesSeconds", reflect.TypeFor[int]()},
		{"ServiceFilesSeconds", reflect.TypeFor[int]()},
		{"ServiceDBSeconds", reflect.TypeFor[int]()},
		{"ServiceWALSeconds", reflect.TypeFor[int]()},
	})

	// R-8CC2-NUGX R-8DJZ-1M7M
	if DefaultHostFilesSeconds != 86400 || DefaultServiceFilesSeconds != 86400 ||
		DefaultServiceDBSeconds != 86400 || DefaultServiceWALSeconds != 300 {
		t.Fatalf("default constants = %d, %d, %d, %d",
			DefaultHostFilesSeconds, DefaultServiceFilesSeconds,
			DefaultServiceDBSeconds, DefaultServiceWALSeconds)
	}
	wantPeriods := BackupPeriods{86400, 86400, 86400, 300}
	if got := DefaultBackupPeriods(); got != wantPeriods {
		t.Fatalf("DefaultBackupPeriods() = %#v, want %#v", got, wantPeriods)
	}

	// R-8ERV-FDYB
	requireExactFields(t, reflect.TypeFor[Config](), []fieldSpec{
		{"Root", reflect.TypeFor[string]()},
		{"Region", reflect.TypeFor[string]()},
		{"ZoneID", reflect.TypeFor[string]()},
		{"Space", reflect.TypeFor[spaceref.Space]()},
		{"Email", reflect.TypeFor[string]()},
		{"Periods", reflect.TypeFor[*BackupPeriods]()},
	})

	// R-8FZR-T5P0 R-8H7O-6XFP
	requireConfigureSignature(Configure)
	requireSetKeySignature(SetKey)
	requireDelKeySignature(DelKey)
	requireGetKeySignature(GetKey)
}

func TestConfigureSetsDerivedDefaultsAndEmailInOrder(t *testing.T) {
	// R-5PA7-AFGB R-8JNG-YGX3 R-8KVD-C8NS
	periods := DefaultBackupPeriods()
	cfg := Config{
		Root:    "ikigenba.dev",
		Region:  "us-east-2",
		ZoneID:  "Z09565073GHK8BYWQ1A78",
		Space:   spaceref.Space{Label: "sbx1", Domain: "sbx1.ikigenba.dev"},
		Email:   " ops+literal@example.test ",
		Periods: &periods,
	}
	var commands []seam.Cmd
	target := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
		return seam.Result{Stdout: []byte("ignored\n"), Stderr: []byte("also ignored\n")}, nil
	})

	count, err := Configure(context.Background(), target, "configuration", cfg)
	if err != nil || count != 10 {
		t.Fatalf("Configure() = %d, %v, want 10, nil", count, err)
	}
	want := []string{
		"'sudo' 'opsctl' 'config' 'set' 'host.name=sbx1.ikigenba.dev'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.provider=route53'",
		"'sudo' 'opsctl' 'config' 'set' 'dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78'",
		"'sudo' 'opsctl' 'config' 'set' 'aws.region=us-east-2'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.s3_uri=s3://ikigenba.dev/sbx1/'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.host_files_seconds=86400'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_files_seconds=86400'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_db_seconds=86400'",
		"'sudo' 'opsctl' 'config' 'set' 'backup.service_wal_seconds=300'",
		"'sudo' 'opsctl' 'config' 'set' 'acme.email= ops+literal@example.test '",
	}
	if got := logicalCommands(commands); !reflect.DeepEqual(got, want) {
		t.Fatalf("Configure commands = %#v, want %#v", got, want)
	}
	for _, command := range commands {
		if command.Path != "ssh" || command.Dir != "/work" {
			t.Fatalf("Configure command = %#v", command)
		}
	}
}

func TestConfigureCountsOptionalKeysAndStopsAtFirstFailure(t *testing.T) {
	// R-8M39-Q0EH
	base := Config{
		Root:   "example.test",
		Region: "eu-west-1",
		ZoneID: "ZONE1",
		Space:  spaceref.Space{Label: "app", Domain: "app.example.test"},
	}
	tests := []struct {
		name  string
		alter func(*Config)
		want  int
	}{
		{name: "derived only", want: 5},
		{name: "email", alter: func(cfg *Config) { cfg.Email = "ops@example.test" }, want: 6},
		{name: "periods and email", alter: func(cfg *Config) {
			periods := DefaultBackupPeriods()
			cfg.Periods = &periods
			cfg.Email = "ops@example.test"
		}, want: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			if tc.alter != nil {
				tc.alter(&cfg)
			}
			var commands []seam.Cmd
			count, err := Configure(context.Background(), recordingHost(&commands, nil), "setup", cfg)
			if err != nil || count != tc.want || len(commands) != tc.want {
				t.Fatalf("Configure() = %d, %v with %d commands, want %d, nil", count, err, len(commands), tc.want)
			}
		})
	}

	var commands []seam.Cmd
	target := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
		if len(commands) == 4 {
			return seam.Result{ExitCode: 23, Stdout: []byte("exact stdout\n"), Stderr: []byte("exact stderr\n")}, nil
		}
		return seam.Result{}, nil
	})
	count, err := Configure(context.Background(), target, "custom step", base)
	var commandErr *host.CommandError
	if count != 3 || reflect.TypeOf(err) != reflect.TypeFor[*host.CommandError]() ||
		!errors.As(err, &commandErr) || len(commands) != 4 {
		t.Fatalf("Configure() = %d, %#v with %d commands", count, err, len(commands))
	}
	if commandErr.Step != "custom step" || commandErr.Status != 23 ||
		commandErr.Stdout != "exact stdout\n" || commandErr.Stderr != "exact stderr\n" {
		t.Fatalf("Configure error = %#v", commandErr)
	}
}

func TestSingleKeyOperations(t *testing.T) {
	// R-8OJ2-HJVV R-8PQY-VBMK R-8QYV-93D9 R-8TEO-0MUN
	t.Run("set and delete each issue one exact command", func(t *testing.T) {
		var commands []seam.Cmd
		target := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
			return seam.Result{Stdout: []byte("unparsed stdout"), Stderr: []byte("unparsed stderr")}, nil
		})
		if err := SetKey(context.Background(), target, "apex", KeyHostApex, " sbx1.example.test "); err != nil {
			t.Fatal(err)
		}
		if err := DelKey(context.Background(), target, "apex", KeyHostApex); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"'sudo' 'opsctl' 'config' 'set' 'host.apex= sbx1.example.test '",
			"'sudo' 'opsctl' 'config' 'del' 'host.apex'",
		}
		if got := logicalCommands(commands); !reflect.DeepEqual(got, want) {
			t.Fatalf("commands = %#v, want %#v", got, want)
		}
	})

	t.Run("get returns opaque value without trailing newlines", func(t *testing.T) {
		var commands []seam.Cmd
		target := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
			return seam.Result{Stdout: []byte(" arbitrary value \t\r\n\n"), Stderr: []byte("ignored")}, nil
		})
		got, set, err := GetKey(context.Background(), target, "apex", KeyHostApex)
		if err != nil || !set || got != " arbitrary value \t" {
			t.Fatalf("GetKey() = %q, %t, %v", got, set, err)
		}
		want := []string{"'sudo' 'opsctl' 'config' 'get' 'host.apex'"}
		if logical := logicalCommands(commands); !reflect.DeepEqual(logical, want) {
			t.Fatalf("commands = %#v, want %#v", logical, want)
		}
	})

	t.Run("get maps only status one to unset", func(t *testing.T) {
		var commands []seam.Cmd
		target := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
			return seam.Result{ExitCode: 1, Stdout: []byte("not a value\n"), Stderr: []byte("unset detail\n")}, nil
		})
		got, set, err := GetKey(context.Background(), target, "read", "missing")
		if err != nil || set || got != "" {
			t.Fatalf("GetKey() = %q, %t, %v", got, set, err)
		}
	})

	t.Run("other failure remains an unchanged host error", func(t *testing.T) {
		var commands []seam.Cmd
		target := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
			return seam.Result{ExitCode: 19, Stdout: []byte{0, 'o', '\n'}, Stderr: []byte{0, 'e', '\n'}}, nil
		})
		got, set, err := GetKey(context.Background(), target, "read", "broken")
		var commandErr *host.CommandError
		if got != "" || set || reflect.TypeOf(err) != reflect.TypeFor[*host.CommandError]() ||
			!errors.As(err, &commandErr) {
			t.Fatalf("GetKey() = %q, %t, %#v", got, set, err)
		}
		if commandErr.Step != "read" || commandErr.Status != 19 ||
			commandErr.Stdout != "\x00o\n" || commandErr.Stderr != "\x00e\n" {
			t.Fatalf("GetKey error = %#v", commandErr)
		}
	})

	t.Run("set and delete return process errors without another wrapper", func(t *testing.T) {
		for _, operation := range []struct {
			name string
			run  func(host.Host) error
		}{
			{name: "set", run: func(target host.Host) error {
				return SetKey(context.Background(), target, "write", "key", "value")
			}},
			{name: "delete", run: func(target host.Host) error {
				return DelKey(context.Background(), target, "write", "key")
			}},
		} {
			t.Run(operation.name, func(t *testing.T) {
				cause := errors.New("start sentinel")
				var commands []seam.Cmd
				target := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
					return seam.Result{}, cause
				})
				err := operation.run(target)
				unwrapped := errors.Unwrap(err)
				if unwrapped == nil || reflect.ValueOf(unwrapped).Pointer() != reflect.ValueOf(cause).Pointer() ||
					errors.Unwrap(unwrapped) != nil || err.Error() != "ssh: start sentinel" {
					t.Fatalf("operation error = %#v", err)
				}
				if len(commands) != 1 {
					t.Fatalf("operation issued %d commands, want 1", len(commands))
				}
			})
		}
	})
}

func TestOnlySingleKeyOperationsCanNameHostApex(t *testing.T) {
	// R-5QI3-O770
	var commands []seam.Cmd
	deps := seam.Deps{Dir: "/work", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		if command.Path == "curl" {
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v1.2.3","published_at":"2026-01-01T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://example.test/install"}]}]`)}, nil
		}
		return seam.Result{Stdout: []byte("v1.2.3\n")}, nil
	}}
	target := host.Host{Address: "192.0.2.10", Deps: deps}
	cfg := Config{Root: "example.test", Region: "us-east-1", ZoneID: "ZONE", Space: spaceref.Space{Label: "sbx", Domain: "sbx.example.test"}}
	if _, err := Configure(context.Background(), target, "configuration", cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallLatest(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := Upgrade(context.Background(), target, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := Version(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	for _, command := range logicalCommands(commands) {
		if strings.Contains(command, KeyHostApex) {
			t.Fatalf("general operation named %q in %q", KeyHostApex, command)
		}
	}
}

func TestHostsetupProductionDoesNotImportCloud(t *testing.T) {
	// R-8UMK-EELC
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "github.com/ikigenba/ikigenba/devctl/internal/cloud" ||
				path == "github.com/ikigenba/ikigenba/devctl/internal/cloud/awssdk" {
				t.Fatalf("%s imports forbidden package %q", entry.Name(), path)
			}
		}
	}
}

type fieldSpec struct {
	name string
	typ  reflect.Type
}

func requireExactFields(t *testing.T, typ reflect.Type, want []fieldSpec) {
	t.Helper()
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ, typ.NumField(), len(want))
	}
	for i, field := range want {
		got := typ.Field(i)
		if got.Name != field.name || got.Type != field.typ {
			t.Fatalf("%s field %d = %s %s, want %s %s", typ, i, got.Name, got.Type, field.name, field.typ)
		}
	}
}

func requireConfigureSignature(func(context.Context, host.Host, string, Config) (int, error)) {}

func requireSetKeySignature(func(context.Context, host.Host, string, string, string) error) {}

func requireDelKeySignature(func(context.Context, host.Host, string, string) error) {}

func requireGetKeySignature(func(context.Context, host.Host, string, string) (string, bool, error)) {}

func recordingHost(commands *[]seam.Cmd, result func(seam.Cmd) (seam.Result, error)) host.Host {
	return host.Host{Address: "192.0.2.10", Deps: seam.Deps{
		Dir: "/work",
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			*commands = append(*commands, command)
			if result == nil {
				return seam.Result{}, nil
			}
			return result(command)
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			panic("unexpected streaming command")
		},
	}}
}

func logicalCommands(commands []seam.Cmd) []string {
	logical := make([]string, 0, len(commands))
	for _, command := range commands {
		if command.Path == "ssh" {
			logical = append(logical, command.Args[len(command.Args)-1])
		}
	}
	return logical
}
