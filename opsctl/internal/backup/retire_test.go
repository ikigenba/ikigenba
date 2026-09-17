package backup_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var _ func(context.Context, host.Env, cloud.Env, config.Store) (backup.RetireResult, error) = backup.Retire

func TestRetireAPIAndOrderedSuccessfulEffects(t *testing.T) {
	// R-Y91D-XNRC R-HR5Z-IBB4 R-YSJS-1ZMG R-HYHD-SXRA
	// R-YW7H-7AUJ R-YUZK-TJ3U
	wantFields := []struct {
		name string
		typ  reflect.Type
	}{
		{"Services", reflect.TypeFor[[]string]()},
		{"ServicesStopped", reflect.TypeFor[bool]()},
		{"LitestreamStopped", reflect.TypeFor[bool]()},
		{"SyncedDatabases", reflect.TypeFor[[]string]()},
		{"Files", reflect.TypeFor[[]backup.FileResult]()},
		{"Host", reflect.TypeFor[backup.FileResult]()},
		{"FailedStep", reflect.TypeFor[string]()},
	}
	resultType := reflect.TypeFor[backup.RetireResult]()
	if resultType.NumField() != len(wantFields) {
		t.Fatalf("RetireResult has %d fields, want %d", resultType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := resultType.Field(index)
		if field.Name != want.name || field.Type != want.typ {
			t.Fatalf("RetireResult field %d = %s %v, want %s %v", index, field.Name, field.Type, want.name, want.typ)
		}
	}

	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/alpha/etc/manifest.toml", "app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
	writeFile(t, root, "opt/alpha/state/app.db", "database", 0o600)
	writeFile(t, root, "opt/alpha/state/ordinary", "alpha", 0o600)
	writeFile(t, root, "opt/beta/state/value", "data only", 0o600)
	writeFile(t, root, "etc/letsencrypt/live/site/cert.pem", "certificate", 0o600)

	client := newFileCloud()
	opened := 0
	cloudEnv := cloud.Env{Open: func(ctx context.Context, region string) (cloud.Client, error) {
		opened++
		return client.open(ctx, region)
	}}
	executor := newRetireExecutor(map[string]bool{"alpha": true, "beta": false})
	nowCalls := 0
	now := time.Date(2026, 9, 16, 17, 45, 12, 345000000, time.FixedZone("offset", -6*60*60))
	result, err := backup.Retire(context.Background(), host.Env{
		Root: root,
		Now: func() time.Time {
			nowCalls++
			return now
		},
		Execute: executor.execute,
	}, cloudEnv, store)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Services, []string{"alpha"}) || !result.ServicesStopped || !result.LitestreamStopped || result.FailedStep != "" {
		t.Fatalf("Retire() phases = %+v", result)
	}
	if !reflect.DeepEqual(result.SyncedDatabases, []string{"app.db"}) {
		t.Fatalf("SyncedDatabases = %v, want positive proof for app.db", result.SyncedDatabases)
	}
	if got := resultNames(result.Files); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("file results = %v", got)
	}
	if result.Host.Service != "host" || result.Host.Err != nil || opened != 1 || nowCalls != 1 {
		t.Fatalf("Retire() = %+v, cloud opens %d, Now calls %d", result, opened, nowCalls)
	}
	wantObject := "2026-09-16T23:45:12.345Z.tar.zst"
	for _, archive := range append(append([]backup.FileResult{}, result.Files...), result.Host) {
		if archive.Err != nil || archive.Object != wantObject {
			t.Fatalf("archive result = %+v, want common object %q", archive, wantObject)
		}
	}
	if got := executor.systemctlCommands(); !reflect.DeepEqual(got, []string{
		"systemctl show --property=LoadState ikigenba-alpha.service",
		"systemctl stop ikigenba-alpha.service",
		"systemctl show --property=LoadState ikigenba-beta.service",
		"systemctl stop litestream.service",
	}) {
		t.Fatalf("systemctl commands = %v", got)
	}
	wantSync := "litestream sync -wait -timeout 60 -socket " + filepath.Join(root, "var/run/litestream.sock") + " -json " + filepath.Join(root, "opt/alpha/state/app.db")
	if len(executor.commands) < 5 || executor.commands[3] != wantSync || executor.commands[4] != "systemctl stop litestream.service" {
		t.Fatalf("sync and stop commands = %v, want serial sync %q then stop", executor.commands, wantSync)
	}
	if executor.firstCompression < executor.litestreamStop {
		t.Fatalf("archive began before Litestream stopped: commands %v", executor.commands)
	}
	alphaArchive := readTestArchive(t, client.objects["s3://bucket/host/alpha/"+wantObject])
	if _, included := alphaArchive["state/app.db"]; included {
		t.Fatal("retirement service archive included declared database")
	}
	if got := string(alphaArchive["state/ordinary"].data); got != "alpha" {
		t.Fatalf("ordinary state = %q", got)
	}
}

func TestRetireStopsOnUnitFailuresWithoutArchiveOrRollback(t *testing.T) {
	// R-YSJS-1ZMG R-HYHD-SXRA
	t.Run("discovery failure before unit operation", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		writeFile(t, root, "opt", "not a directory", 0o600)
		client := newFileCloud()
		executor := newRetireExecutor(map[string]bool{"alpha": true})
		result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
		if err == nil || result.FailedStep != "services" {
			t.Fatalf("Retire() = %+v, %v", result, err)
		}
		if len(executor.commands) != 0 || len(client.puts) != 0 {
			t.Fatalf("discovery failure effects: commands %v uploads %v", executor.commands, client.puts)
		}
	})

	t.Run("cloud opening is preworkflow", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		writeFile(t, root, "opt/alpha/state/value", "alpha", 0o600)
		executor := newRetireExecutor(map[string]bool{"alpha": true})
		result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			return nil, errors.New("storage unavailable")
		}}, store)
		if err == nil || result.FailedStep != "" || result.ServicesStopped || result.LitestreamStopped || len(executor.commands) != 0 {
			t.Fatalf("Retire() = %+v, %v; commands %v", result, err, executor.commands)
		}
	})

	t.Run("invalid discovered name before unit operation", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		writeFile(t, root, "opt/bad_name/state/value", "data", 0o600)
		client := newFileCloud()
		executor := newRetireExecutor(nil)
		result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
		if err == nil || result.FailedStep != "services" || len(executor.commands) != 0 || len(client.puts) != 0 {
			t.Fatalf("Retire() = %+v, %v; commands %v uploads %v", result, err, executor.commands, client.puts)
		}
	})

	t.Run("later service inspection preserves earlier stop", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		writeFile(t, root, "opt/alpha/state/value", "alpha", 0o600)
		writeFile(t, root, "opt/zeta/state/value", "zeta", 0o600)
		client := newFileCloud()
		transport := errors.New("system bus unavailable")
		executor := newRetireExecutor(map[string]bool{"alpha": true, "zeta": true})
		executor.fail = func(command string) (host.Result, error, bool) {
			if command == "systemctl show --property=LoadState ikigenba-zeta.service" {
				return host.Result{Stderr: []byte("bus lost\n")}, transport, true
			}
			return host.Result{}, nil, false
		}
		result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
		var commandErr *host.CommandError
		if !errors.As(err, &commandErr) || !errors.Is(err, transport) || result.FailedStep != "services" || result.ServicesStopped || !reflect.DeepEqual(result.Services, []string{"alpha"}) {
			t.Fatalf("Retire() = %+v, %T %v", result, err, err)
		}
		if len(client.puts) != 0 || containsRetireAction(executor.commands, "start") || containsRetireAction(executor.commands, "restart") {
			t.Fatalf("failure effects: commands %v uploads %v", executor.commands, client.puts)
		}
	})

	t.Run("litestream nonzero preserves stopped services", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		writeFile(t, root, "opt/alpha/state/value", "alpha", 0o600)
		client := newFileCloud()
		executor := newRetireExecutor(map[string]bool{"alpha": true})
		executor.fail = func(command string) (host.Result, error, bool) {
			if command == "systemctl stop litestream.service" {
				return host.Result{ExitCode: 5, Stderr: []byte("stop failed\n")}, nil, true
			}
			return host.Result{}, nil, false
		}
		result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
		var commandErr *host.CommandError
		if !errors.As(err, &commandErr) || result.FailedStep != "litestream" || !result.ServicesStopped || result.LitestreamStopped || !reflect.DeepEqual(result.Services, []string{"alpha"}) {
			t.Fatalf("Retire() = %+v, %T %v", result, err, err)
		}
		if len(client.puts) != 0 || containsRetireAction(executor.commands, "start") || containsRetireAction(executor.commands, "restart") {
			t.Fatalf("failure effects: commands %v uploads %v", executor.commands, client.puts)
		}
	})

	t.Run("service stop transport failure prevents later actions", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		writeFile(t, root, "opt/alpha/state/value", "alpha", 0o600)
		client := newFileCloud()
		transport := errors.New("connection closed")
		executor := newRetireExecutor(map[string]bool{"alpha": true})
		executor.fail = func(command string) (host.Result, error, bool) {
			if command == "systemctl stop ikigenba-alpha.service" {
				return host.Result{Stdout: []byte("partial\n")}, transport, true
			}
			return host.Result{}, nil, false
		}
		result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
		var commandErr *host.CommandError
		if !errors.As(err, &commandErr) || !errors.Is(err, transport) || result.FailedStep != "services" || len(result.Services) != 0 || result.ServicesStopped {
			t.Fatalf("Retire() = %+v, %T %v", result, err, err)
		}
		if len(client.puts) != 0 || len(executor.commands) != 2 {
			t.Fatalf("commands %v uploads %v", executor.commands, client.puts)
		}
	})
}

func TestRetireRejectsInvalidSynchronizationProofAndStops(t *testing.T) {
	// R-YUZK-TJ3U
	tests := []struct {
		name   string
		output func(string) []byte
	}{
		{name: "not an object", output: func(string) []byte { return []byte("[]") }},
		{name: "second JSON value", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"txid":4,"replica_txid":4} {}`, database))
		}},
		{name: "trailing non-whitespace", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"txid":4,"replica_txid":4} trailing`, database))
		}},
		{name: "wrong database", output: func(string) []byte {
			return []byte(`{"db_path":"/wrong.db","txid":4,"replica_txid":4}`)
		}},
		{name: "missing transaction", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"replica_txid":4}`, database))
		}},
		{name: "negative transaction", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"txid":-1,"replica_txid":-1}`, database))
		}},
		{name: "fractional transaction", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"txid":4.0,"replica_txid":4.0}`, database))
		}},
		{name: "overflowing transaction", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"txid":18446744073709551616,"replica_txid":18446744073709551616}`, database))
		}},
		{name: "string transaction", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"txid":"4","replica_txid":"4"}`, database))
		}},
		{name: "unequal transactions", output: func(database string) []byte {
			return []byte(fmt.Sprintf(`{"db_path":%q,"txid":4,"replica_txid":3}`, database))
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			writeFile(t, root, "opt/alpha/etc/manifest.toml", "app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
			client := newFileCloud()
			executor := newRetireExecutor(map[string]bool{"alpha": false})
			databasePath := filepath.Join(root, "opt/alpha/state/app.db")
			executor.fail = func(command string) (host.Result, error, bool) {
				if strings.HasPrefix(command, "litestream sync ") {
					return host.Result{Stdout: test.output(databasePath)}, nil, true
				}
				return host.Result{}, nil, false
			}

			result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
			if err == nil || result.FailedStep != "litestream" || !result.ServicesStopped || !result.LitestreamStopped || len(result.SyncedDatabases) != 0 {
				t.Fatalf("Retire() = %+v, %v", result, err)
			}
			if countCommands(executor.commands, "litestream sync ") != 1 || executor.commands[len(executor.commands)-1] != "systemctl stop litestream.service" {
				t.Fatalf("commands = %v; want one sync followed by mandatory stop", executor.commands)
			}
			if len(client.puts) != 0 || executor.firstCompression != -1 {
				t.Fatalf("invalid proof archived data: commands %v uploads %v", executor.commands, client.puts)
			}
		})
	}
}

func TestRetireSyncCommandFailuresStopWithoutRetry(t *testing.T) {
	// R-YUZK-TJ3U
	transportErr := errors.New("control socket unavailable")
	tests := []struct {
		name    string
		result  host.Result
		err     error
		wantErr error
	}{
		{name: "execution", result: host.Result{Stderr: []byte("socket missing\n")}, err: transportErr, wantErr: transportErr},
		{name: "nonzero", result: host.Result{ExitCode: 8, Stdout: []byte("partial\n")}},
		{name: "cancellation", err: context.Canceled, wantErr: context.Canceled},
		{name: "timeout", err: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			writeFile(t, root, "opt/alpha/etc/manifest.toml", "app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
			client := newFileCloud()
			executor := newRetireExecutor(map[string]bool{"alpha": false})
			executor.fail = func(command string) (host.Result, error, bool) {
				if strings.HasPrefix(command, "litestream sync ") {
					return test.result, test.err, true
				}
				return host.Result{}, nil, false
			}
			result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
			var commandErr *host.CommandError
			if err == nil || !errors.As(err, &commandErr) || result.FailedStep != "litestream" || !result.LitestreamStopped {
				t.Fatalf("Retire() = %+v, %T %v", result, err, err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("Retire() error = %v, want %v", err, test.wantErr)
			}
			if countCommands(executor.commands, "litestream sync ") != 1 || len(client.puts) != 0 {
				t.Fatalf("commands = %v uploads = %v", executor.commands, client.puts)
			}
		})
	}
}

func TestRetireRetainsPartialProofAndOrderedStopError(t *testing.T) {
	// R-YUZK-TJ3U
	root := t.TempDir()
	store := configuredFileStore(t, root)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		writeFile(t, root, "opt/"+name+"/etc/manifest.toml", "app = \""+name+"\"\n[database]\nengine = \"sqlite\"\npath = \"state/"+name+".db\"\n", 0o600)
	}
	client := newFileCloud()
	executor := newRetireExecutor(map[string]bool{"alpha": false, "beta": false, "gamma": false})
	executor.fail = func(command string) (host.Result, error, bool) {
		switch {
		case strings.HasPrefix(command, "litestream sync ") && strings.HasSuffix(command, "/beta/state/beta.db"):
			return host.Result{ExitCode: 9, Stderr: []byte("sync rejected\n")}, nil, true
		case command == "systemctl stop litestream.service":
			return host.Result{ExitCode: 5, Stderr: []byte("stop rejected\n")}, nil, true
		default:
			return host.Result{}, nil, false
		}
	}

	result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
	if err == nil || result.FailedStep != "litestream" || result.LitestreamStopped || !reflect.DeepEqual(result.SyncedDatabases, []string{"alpha.db"}) {
		t.Fatalf("Retire() = %+v, %v", result, err)
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) != 2 {
		t.Fatalf("Retire() error = %T %v, want two joined causes", err, err)
	}
	var syncCommandErr, stopCommandErr *host.CommandError
	causes := joined.Unwrap()
	if !errors.As(causes[0], &syncCommandErr) || !strings.HasPrefix(syncCommandErr.Label, "sync ") || !errors.As(causes[1], &stopCommandErr) || stopCommandErr.Label != "stop litestream.service" {
		t.Fatalf("joined causes = %#v, want sync then stop", causes)
	}
	if len(client.puts) != 0 || countCommands(executor.commands, "litestream sync ") != 2 || executor.commands[len(executor.commands)-1] != "systemctl stop litestream.service" {
		t.Fatalf("commands = %v uploads = %v", executor.commands, client.puts)
	}
}

func TestRetireSuccessfulProofStillRequiresLitestreamStop(t *testing.T) {
	// R-YUZK-TJ3U
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/alpha/etc/manifest.toml", "app = \"alpha\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
	client := newFileCloud()
	executor := newRetireExecutor(map[string]bool{"alpha": false})
	executor.fail = func(command string) (host.Result, error, bool) {
		if command == "systemctl stop litestream.service" {
			return host.Result{ExitCode: 4, Stderr: []byte("busy\n")}, nil, true
		}
		return host.Result{}, nil, false
	}
	result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: time.Now, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
	if err == nil || result.FailedStep != "litestream" || result.LitestreamStopped || !reflect.DeepEqual(result.SyncedDatabases, []string{"app.db"}) {
		t.Fatalf("Retire() = %+v, %v", result, err)
	}
	if len(client.puts) != 0 || executor.firstCompression != -1 {
		t.Fatalf("stop failure archived data: commands %v uploads %v", executor.commands, client.puts)
	}
}

func countCommands(commands []string, prefix string) int {
	count := 0
	for _, command := range commands {
		if strings.HasPrefix(command, prefix) {
			count++
		}
	}
	return count
}

func TestRetireTimestampCollisionDoesNotAdvance(t *testing.T) {
	// R-YW7H-7AUJ
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/alpha/state/value", "alpha", 0o600)
	client := newFileCloud()
	basename := "1970-01-01T00:00:07Z.tar.zst"
	alphaURI := "s3://bucket/host/alpha/" + basename
	client.objects[alphaURI] = []byte("earlier object")
	executor := newRetireExecutor(map[string]bool{"alpha": false})
	nowCalls := 0
	result, err := backup.Retire(context.Background(), host.Env{
		Root: root,
		Now: func() time.Time {
			nowCalls++
			return time.Unix(7, 0)
		},
		Execute: executor.execute,
	}, cloud.Env{Open: client.open}, store)
	if err != nil || nowCalls != 1 || len(result.Files) != 1 || !errors.Is(result.Files[0].Err, cloud.ErrAlreadyExists) || result.Files[0].Object != "" {
		t.Fatalf("Retire() = %+v, %v; Now calls %d", result, err, nowCalls)
	}
	if string(client.objects[alphaURI]) != "earlier object" || result.Host.Object != basename || result.Host.Err != nil {
		t.Fatalf("collision results = %+v, preserved alpha %q", result, client.objects[alphaURI])
	}
	if len(client.puts) != 2 || client.puts[0] != alphaURI || client.puts[1] != "s3://bucket/host/host/"+basename {
		t.Fatalf("uploads = %v", client.puts)
	}
}

func TestRetireArchiveFailuresRetainResultsAndStoppedUnits(t *testing.T) {
	// R-DUOO-S3WL R-YW7H-7AUJ
	root := t.TempDir()
	store := configuredFileStore(t, root)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		writeFile(t, root, "opt/"+name+"/state/value", name, 0o600)
	}
	client := newFileCloud()
	oldObjects := map[string]string{
		"s3://bucket/host/alpha/1970-01-01T00:00:01Z.tar.zst": "old alpha",
		"s3://bucket/host/beta/1970-01-01T00:00:01Z.tar.zst":  "old beta",
		"s3://bucket/host/host/1970-01-01T00:00:01Z.tar.zst":  "old host",
	}
	for uri, contents := range oldObjects {
		client.objects[uri] = []byte(contents)
	}
	client.fail = func(uri string, _ []byte) error {
		if strings.Contains(uri, "/beta/") {
			return errors.New("beta storage rejected")
		}
		return nil
	}
	executor := newRetireExecutor(map[string]bool{"alpha": true, "beta": true, "gamma": true})
	result, err := backup.Retire(context.Background(), host.Env{Root: root, Now: func() time.Time { return time.Unix(4, 0) }, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
	if err != nil || !result.ServicesStopped || !result.LitestreamStopped || result.Host.Err != nil {
		t.Fatalf("Retire() = %+v, %v", result, err)
	}
	if got := resultNames(result.Files); !reflect.DeepEqual(got, []string{"alpha", "beta", "gamma"}) || result.Files[0].Err != nil || result.Files[1].Err == nil || result.Files[2].Err != nil {
		t.Fatalf("file results = %+v", result.Files)
	}
	if len(client.puts) != 4 || !strings.Contains(client.puts[len(client.puts)-1], "/host/") {
		t.Fatalf("uploads = %v", client.puts)
	}
	if containsRetireAction(executor.commands, "start") || containsRetireAction(executor.commands, "restart") || containsRetireAction(executor.commands, "enable") || containsRetireAction(executor.commands, "disable") || containsRetireAction(executor.commands, "mask") {
		t.Fatalf("Retire attempted rollback or unit-state mutation: %v", executor.commands)
	}
	for uri, contents := range oldObjects {
		if got := string(client.objects[uri]); got != contents {
			t.Fatalf("preexisting object %s = %q, want %q", uri, got, contents)
		}
	}
}

func TestRetireInterruptionStopsBeforeHostArchive(t *testing.T) {
	// R-DUOO-S3WL
	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	store := configuredFileStore(t, root)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		writeFile(t, root, "opt/"+name+"/state/value", name, 0o600)
	}
	client := newFileCloud()
	oldObjects := map[string]string{
		"s3://bucket/host/alpha/1970-01-01T00:00:01Z.tar.zst": "old alpha",
		"s3://bucket/host/beta/1970-01-01T00:00:01Z.tar.zst":  "old beta",
		"s3://bucket/host/host/1970-01-01T00:00:01Z.tar.zst":  "old host",
	}
	for uri, contents := range oldObjects {
		client.objects[uri] = []byte(contents)
	}
	client.fail = func(uri string, _ []byte) error {
		if strings.Contains(uri, "/beta/") {
			cancel()
			return context.Canceled
		}
		return nil
	}
	executor := newRetireExecutor(map[string]bool{"alpha": true, "beta": true, "gamma": true})
	result, err := backup.Retire(ctx, host.Env{Root: root, Now: func() time.Time { return time.Unix(5, 0) }, Execute: executor.execute}, cloud.Env{Open: client.open}, store)
	if !errors.Is(err, context.Canceled) || result.FailedStep != "beta" || result.Host.Service != "" || len(result.Files) != 2 || result.Files[0].Err != nil || !errors.Is(result.Files[1].Err, context.Canceled) {
		t.Fatalf("Retire() = %+v, %v", result, err)
	}
	if !result.ServicesStopped || !result.LitestreamStopped || len(client.puts) != 2 || strings.Contains(strings.Join(client.puts, "\n"), "/host/host/") {
		t.Fatalf("interrupted effects = %+v, uploads %v", result, client.puts)
	}
	for uri, contents := range oldObjects {
		if got := string(client.objects[uri]); got != contents {
			t.Fatalf("preexisting object %s = %q, want %q", uri, got, contents)
		}
	}
}

type retireExecutor struct {
	units            map[string]bool
	fail             func(string) (host.Result, error, bool)
	files            fileExecutor
	commands         []string
	litestreamStop   int
	firstCompression int
}

func newRetireExecutor(units map[string]bool) *retireExecutor {
	return &retireExecutor{
		units:            units,
		files:            fileExecutor{uid: 1000, gid: 1000, user: "ikigenba", group: "ikigenba"},
		litestreamStop:   -1,
		firstCompression: -1,
	}
}

func (executor *retireExecutor) execute(ctx context.Context, command host.Command) (host.Result, error) {
	text := strings.Join(append([]string{command.Name}, command.Args...), " ")
	executor.commands = append(executor.commands, text)
	if executor.fail != nil {
		if result, err, handled := executor.fail(text); handled {
			return result, err
		}
	}
	if command.Name == "systemctl" {
		if reflect.DeepEqual(command.Args, []string{"stop", "litestream.service"}) {
			executor.litestreamStop = len(executor.commands) - 1
			return host.Result{}, nil
		}
		if len(command.Args) == 3 && command.Args[0] == "show" && command.Args[1] == "--property=LoadState" {
			name := strings.TrimSuffix(strings.TrimPrefix(command.Args[2], "ikigenba-"), ".service")
			state := "not-found"
			if executor.units[name] {
				state = "loaded"
			}
			return host.Result{Stdout: []byte("LoadState=" + state + "\n")}, nil
		}
		if len(command.Args) == 2 && command.Args[0] == "stop" {
			return host.Result{}, nil
		}
		return host.Result{}, fmt.Errorf("unexpected systemctl command %q", text)
	}
	if command.Name == "litestream" && len(command.Args) == 8 && command.Args[0] == "sync" && command.Args[6] == "-json" {
		proof, err := json.Marshal(map[string]any{
			"db_path":      command.Args[7],
			"txid":         uint64(7),
			"replica_txid": uint64(7),
		})
		if err != nil {
			return host.Result{}, err
		}
		return host.Result{Stdout: append([]byte(" \n"), append(proof, '\n')...)}, nil
	}
	if command.Name == "zstd" && executor.firstCompression < 0 {
		executor.firstCompression = len(executor.commands) - 1
	}
	return executor.files.execute(ctx, command)
}

func (executor *retireExecutor) systemctlCommands() []string {
	var commands []string
	for _, command := range executor.commands {
		if strings.HasPrefix(command, "systemctl ") {
			commands = append(commands, command)
		}
	}
	return commands
}

func containsRetireAction(commands []string, action string) bool {
	for _, command := range commands {
		if strings.HasPrefix(command, "systemctl "+action+" ") {
			return true
		}
	}
	return false
}
