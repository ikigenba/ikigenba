package hostsetup

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func TestRestoreSwitchesKnownHostsAndConfigures(t *testing.T) {
	// R-7O81-P1U3 R-7QNU-GLBH R-7UBJ-LWJK R-7Z75-4ZIC
	requireRestoreSignature(Restore)
	for _, program := range []string{"", "/opt/ikigenba/releases/4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a/opsctl/bin/opsctl"} {
		for _, knownHosts := range []string{"", "/tmp/move.Ab12/known_hosts"} {
			t.Run(program+knownHosts, func(t *testing.T) {
				cfg := Config{Root: "ikigenba.dev", Region: "us-east-2", ZoneID: "ZONE", Space: spaceref.Space{Label: "staging", Domain: "staging.ikigenba.dev"}, Opsctl: program}
				var commands []seam.Cmd
				h := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
					return seam.Result{Stdout: []byte("\x00arbitrary\r\n"), Stderr: []byte("\xffopaque\n")}, nil
				})
				h.Address = "3.141.27.86"
				h.KnownHosts = "/tmp/create.Ab12/known_hosts.launch"
				restored, count, err := Restore(t.Context(), h, "restore", cfg, knownHosts)
				if err != nil || count != 5 || len(commands) != 6 {
					t.Fatalf("Restore = %d, %v, commands %v", count, err, commands)
				}
				wantProgram := program
				if wantProgram == "" {
					wantProgram = "opsctl"
				}
				wantFirst := "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' '" + wantProgram + "' 'host' 'restore'"
				if commands[0].Args[len(commands[0].Args)-1] != wantFirst {
					t.Fatal(commands[0])
				}
				for i, cmd := range commands {
					wantKnown := knownHosts
					if i == 0 {
						wantKnown = h.KnownHosts
					}
					assertRestoreTransport(t, cmd, wantKnown)
				}
				var expected []seam.Cmd
				if _, err := Configure(t.Context(), recordingHost(&expected, nil), "restore", cfg); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(logicalCommands(commands[1:]), logicalCommands(expected)) {
					t.Fatal("restored configuration differs", commands)
				}
				assertRestoredHost(t, restored, h, knownHosts)
			})
		}
	}
}

func TestRestoreStopsAtFailureAndPreservesErrorBytes(t *testing.T) {
	// R-7WRC-DG0Y R-7UBJ-LWJK R-7Z75-4ZIC
	for _, failAt := range []int{1, 4} {
		t.Run(strconv.Itoa(failAt), func(t *testing.T) {
			var commands []seam.Cmd
			h := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
				if len(commands) == failAt {
					return seam.Result{ExitCode: 1, Stdout: []byte("\x00raw\r\n"), Stderr: []byte("restore failed\n\xff")}, nil
				}
				return seam.Result{Stdout: []byte("unparsed output")}, nil
			})
			h.Address = "3.141.27.86"
			h.KnownHosts = "/tmp/create.Ab12/known_hosts.launch"
			knownHosts := "/tmp/move.Ab12/known_hosts"
			restored, count, err := Restore(t.Context(), h, "restore", Config{}, knownHosts)
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) || reflect.ValueOf(err) != reflect.ValueOf(commandErr) ||
				commandErr.Step != "restore" || commandErr.Status != 1 || commandErr.Stdout != "\x00raw\r\n" || commandErr.Stderr != "restore failed\n\xff" || len(commands) != failAt {
				t.Fatalf("Restore error = %#v, commands %v", err, commands)
			}
			wantCount := 2
			if failAt == 1 {
				knownHosts = h.KnownHosts
				wantCount = 0
			}
			if count != wantCount {
				t.Fatalf("count %d, want %d", count, wantCount)
			}
			assertRestoredHost(t, restored, h, knownHosts)
		})
	}
}

func TestConfigurationOperationsPreserveFailureBytes(t *testing.T) {
	// R-7Z75-4ZIC
	for _, operation := range []struct {
		name string
		run  func(context.Context, host.Host) error
	}{
		{name: "configure", run: func(ctx context.Context, h host.Host) error {
			_, err := Configure(ctx, h, "configuration", Config{})
			return err
		}},
		{name: "set", run: func(ctx context.Context, h host.Host) error {
			return SetKey(ctx, h, "configuration", "key", "value")
		}},
		{name: "delete", run: func(ctx context.Context, h host.Host) error {
			return DelKey(ctx, h, "configuration", "key")
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			var commands []seam.Cmd
			h := recordingHost(&commands, func(seam.Cmd) (seam.Result, error) {
				return seam.Result{ExitCode: 19, Stdout: []byte("\x00raw\r\n"), Stderr: []byte("\xffraw\n")}, nil
			})
			err := operation.run(t.Context(), h)
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) || reflect.ValueOf(err) != reflect.ValueOf(commandErr) || commandErr.Step != "configuration" || commandErr.Status != 19 || commandErr.Stdout != "\x00raw\r\n" || commandErr.Stderr != "\xffraw\n" || len(commands) != 1 {
				t.Fatalf("error %#v, commands %v", err, commands)
			}
		})
	}
}

func requireRestoreSignature(func(context.Context, host.Host, string, Config, string) (host.Host, int, error)) {
}

func assertRestoreTransport(t *testing.T, cmd seam.Cmd, knownHosts string) {
	t.Helper()
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new"}
	if knownHosts != "" {
		args = append(args, "-o", "UserKnownHostsFile="+knownHosts)
	}
	args = append(args, "ec2-user@3.141.27.86")
	if cmd.Path != "ssh" || cmd.Dir != "/work" || !reflect.DeepEqual(cmd.Args[:len(cmd.Args)-1], args) {
		t.Fatal("transport changed", cmd)
	}
}

func assertRestoredHost(t *testing.T, restored, original host.Host, knownHosts string) {
	t.Helper()
	if restored.Address != original.Address || restored.KnownHosts != knownHosts || restored.Deps.Dir != original.Deps.Dir || restored.Deps.EUID != original.Deps.EUID {
		t.Fatal("returned host changed", restored)
	}
	// Use the returned runner as well as inspecting its plain fields.
	_, err := restored.Run(t.Context(), "probe", "true")
	if err != nil {
		t.Fatal(err)
	}
}
