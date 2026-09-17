package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestSetupReplicationAPIAndSharedService(t *testing.T) {
	// R-JLOW-YJOY R-JRSE-VEEF
	requireSetupReplicationAPI(backup.SetupReplication)
	root, store := regenerationFixture(t)
	writeService(t, root, "alpha", "[database]\nengine = \"sqlite\"\npath = \"state/alpha.db\"\n")
	writeService(t, root, "beta", "[database]\nengine = \"sqlite\"\npath = \"state/beta.db\"\n")
	var commands []host.Command
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if _, err := os.Stat(filepath.Join(root, "etc", "litestream.yml")); err != nil {
			t.Fatalf("systemctl ran before regeneration completed: %v", err)
		}
		commands = append(commands, command)
		return host.Result{}, nil
	}}

	if err := backup.SetupReplication(context.Background(), env, store); err != nil {
		t.Fatal(err)
	}
	wantCommands := []host.Command{
		{Name: "systemctl", Args: []string{"enable", "litestream.service"}},
		{Name: "systemctl", Args: []string{"restart", "litestream.service"}},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	configuration := readLitestream(t, root)
	if !strings.Contains(configuration, "retention:\n  enabled: false\n") {
		t.Fatalf("configuration lacks disabled retention: %q", configuration)
	}
	for _, forbidden := range []string{"credential", "access-key", "secret-key", "alpha.service", "beta.service", "timer", "oneshot"} {
		if strings.Contains(strings.ToLower(configuration), forbidden) {
			t.Errorf("configuration contains forbidden %q: %q", forbidden, configuration)
		}
	}
}

func TestSetupReplicationStartsWhenConfigurationIsUnchanged(t *testing.T) {
	// R-JVG4-0PMI
	root, store := regenerationFixture(t)
	writeService(t, root, "notes", "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
	if changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store); err != nil || !changed {
		t.Fatalf("initial Regenerate() = %v, %v", changed, err)
	}
	before := readLitestream(t, root)
	var commands []host.Command
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		return host.Result{}, nil
	}}

	if err := backup.SetupReplication(context.Background(), env, store); err != nil {
		t.Fatal(err)
	}
	want := []host.Command{
		{Name: "systemctl", Args: []string{"enable", "litestream.service"}},
		{Name: "systemctl", Args: []string{"start", "litestream.service"}},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	if after := readLitestream(t, root); after != before {
		t.Fatalf("unchanged setup rewrote configuration: before %q, after %q", before, after)
	}
}

func TestSetupReplicationRejectsMissingExecutionDependencyBeforeRegeneration(t *testing.T) {
	// R-AMEV-4J2G
	root, store := regenerationFixture(t)
	writeService(t, root, "notes", "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
	configurationPath := filepath.Join(root, "etc", "litestream.yml")
	if err := os.MkdirAll(filepath.Dir(configurationPath), 0o750); err != nil {
		t.Fatal(err)
	}
	previous := []byte("previous configuration\n")
	if err := os.WriteFile(configurationPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}

	err := backup.SetupReplication(context.Background(), host.Env{Root: root}, store)
	if err == nil || err.Error() != "setup replication: host execution is not configured" {
		t.Fatalf("SetupReplication() error = %v", err)
	}
	after := readLitestream(t, root)
	if after != string(previous) {
		t.Fatalf("configuration changed before dependency validation: got %q, want %q", after, previous)
	}
}

func TestSetupReplicationStopsAfterCommandFailuresWithoutRollback(t *testing.T) {
	// R-JVG4-0PMI R-GWME-QK2L
	tests := []struct {
		name         string
		unchanged    bool
		failAction   string
		result       host.Result
		failure      error
		wantCommands int
	}{
		{name: "enable transport", failAction: "enable", failure: errors.New("connection lost"), wantCommands: 1},
		{name: "enable exit", failAction: "enable", result: host.Result{ExitCode: 5, Stderr: []byte("enable failed")}, wantCommands: 1},
		{name: "restart transport", failAction: "restart", failure: errors.New("connection lost"), wantCommands: 2},
		{name: "restart exit", failAction: "restart", result: host.Result{ExitCode: 6, Stderr: []byte("restart failed")}, wantCommands: 2},
		{name: "start transport", unchanged: true, failAction: "start", failure: errors.New("connection lost"), wantCommands: 2},
		{name: "start exit", unchanged: true, failAction: "start", result: host.Result{ExitCode: 7, Stderr: []byte("start failed")}, wantCommands: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, store := regenerationFixture(t)
			writeService(t, root, "notes", "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
			if test.unchanged {
				if changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store); err != nil || !changed {
					t.Fatalf("initial Regenerate() = %v, %v", changed, err)
				}
			}
			var commands []host.Command
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				commands = append(commands, command)
				if len(command.Args) > 0 && command.Args[0] == test.failAction {
					return test.result, test.failure
				}
				return host.Result{}, nil
			}}

			err := backup.SetupReplication(context.Background(), env, store)
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) {
				t.Fatalf("error = %T %v, want *host.CommandError", err, err)
			}
			if commandErr.Label != test.failAction+" litestream.service" || !reflect.DeepEqual(commandErr.Result, test.result) {
				t.Fatalf("CommandError = %#v", commandErr)
			}
			if test.failure != nil && !errors.Is(err, test.failure) {
				t.Fatalf("error %v does not preserve transport error %v", err, test.failure)
			}
			if len(commands) != test.wantCommands {
				t.Fatalf("commands after failure = %#v", commands)
			}
			if configuration := readLitestream(t, root); !strings.Contains(configuration, "notes.db") {
				t.Fatalf("generated configuration rolled back after unit failure: %q", configuration)
			}
		})
	}
}

func TestSetupReplicationStopsWhenRegenerationFails(t *testing.T) {
	// R-JVG4-0PMI
	root, store := regenerationFixture(t)
	if err := store.Set("backup.service_wal_seconds", "invalid"); err != nil {
		t.Fatal(err)
	}
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("systemctl ran after regeneration failure")
		return host.Result{}, nil
	}}

	if err := backup.SetupReplication(context.Background(), env, store); err == nil || !strings.Contains(err.Error(), "backup.service_wal_seconds") {
		t.Fatalf("SetupReplication() error = %v, want regeneration error", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "litestream.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration exists after regeneration failure: %v", err)
	}
}

func TestSetupReplicationPreservesCommandError(t *testing.T) {
	// R-JVG4-0PMI
	root, store := regenerationFixture(t)
	existing := &host.CommandError{Label: "remote systemctl", Result: host.Result{ExitCode: 73}}
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		return host.Result{}, existing
	}}

	err := backup.SetupReplication(context.Background(), env, store)
	var got *host.CommandError
	if !errors.As(err, &got) || got != existing {
		t.Fatalf("error = %#v, want original CommandError %#v", err, existing)
	}
}

func requireSetupReplicationAPI(func(context.Context, host.Env, config.Store) error) {}
