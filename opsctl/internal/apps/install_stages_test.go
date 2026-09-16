package apps_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallCompletesFileStageBeforeSecretsOrMutation(t *testing.T) {
	// R-UAOX-761Q
	t.Run("success is reported after every file check", func(t *testing.T) {
		root := t.TempDir()
		archive := validInstallTar(t, "app = \"notes\"\nport = 4100\nsecrets = [\"TOKEN\"]\n")
		var reports []installReport
		var commands []commandCall
		stop := errors.New("stop after file-stage boundary")
		client := &installCloudClient{
			get: func(context.Context, string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("compressed")), nil
			},
			readSecrets: func(context.Context, string) (map[string]string, error) {
				want := []installReport{
					{"fetch", "bundle.tar.xz, 0.0 MiB", true},
					{"file", "notes, port 4100", true},
				}
				if !reflect.DeepEqual(reports, want) {
					t.Fatalf("reports at secret read = %#v, want %#v", reports, want)
				}
				if !reflect.DeepEqual(commands, []commandCall{
					{"xz", []string{"--decompress", "--stdout"}},
					{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
				}) {
					t.Fatalf("commands at secret read = %#v", commands)
				}
				if _, err := os.Stat(filepath.Join(root, "opt", "notes")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("installed state changed before secrets: %v", err)
				}
				return map[string]string{"TOKEN": "secret"}, nil
			},
		}
		err := apps.Install(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
			commands = append(commands, commandCall{command.Name, append([]string(nil), command.Args...)})
			switch command.Name {
			case "xz":
				return host.Result{Stdout: archive}, nil
			case "systemctl":
				return host.Result{ExitCode: 3}, nil
			default:
				return host.Result{}, stop
			}
		}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			return client, nil
		}}, installStoreAt(t, root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"}),
			"s3://bucket/bundle.tar.xz", apps.InstallHooks{
				Report: func(step, detail string, success bool) error {
					reports = append(reports, installReport{step, detail, success})
					return nil
				},
				Configure: func(context.Context, apps.Manifest) error { return nil },
			})
		if !errors.Is(err, stop) {
			t.Fatalf("Install error = %v, want later-stage stop", err)
		}
	})

	t.Run("each failed check owns one failed file outcome", func(t *testing.T) {
		observationFailure := errors.New("systemd unavailable")
		cases := []struct {
			name    string
			archive func(*testing.T) []byte
			setup   func(*testing.T, string)
			execute func(host.Command) (host.Result, error)
		}{
			{
				name:    "complete artifact validation",
				archive: emptyTar,
			},
			{
				name:    "installed manifest discovery",
				archive: func(t *testing.T) []byte { return validInstallTar(t, "app = \"notes\"\nport = 4100\n") },
				setup: func(t *testing.T, root string) {
					writeFixture(t, filepath.Join(root, "opt", "other", "etc", "manifest.toml"), []byte("app = [\n"), 0o600)
				},
			},
			{
				name:    "competing default validation",
				archive: func(t *testing.T) []byte { return validInstallTar(t, "app = \"notes\"\nport = 4100\ndefault = true\n") },
				setup: func(t *testing.T, root string) {
					writeFixture(t, filepath.Join(root, "opt", "other", "etc", "manifest.toml"), []byte("app = \"other\"\nport = 4200\ndefault = true\n"), 0o600)
				},
			},
			{
				name:    "initial unit state observation",
				archive: func(t *testing.T) []byte { return validInstallTar(t, "app = \"notes\"\nport = 4100\n") },
				execute: func(command host.Command) (host.Result, error) {
					if command.Name == "systemctl" {
						return host.Result{}, observationFailure
					}
					return host.Result{}, nil
				},
			},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				root := t.TempDir()
				if test.setup != nil {
					test.setup(t, root)
				}
				secretReads := 0
				client := &installCloudClient{
					get: func(context.Context, string) (io.ReadCloser, error) {
						return io.NopCloser(strings.NewReader("compressed")), nil
					},
					readSecrets: func(context.Context, string) (map[string]string, error) {
						secretReads++
						return nil, nil
					},
				}
				var reports []installReport
				err := apps.Install(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
					if command.Name == "xz" {
						return host.Result{Stdout: test.archive(t)}, nil
					}
					if test.execute != nil {
						return test.execute(command)
					}
					return host.Result{ExitCode: 3}, nil
				}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }},
					installStoreAt(t, root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"}),
					"s3://bucket/bundle.tar.xz", apps.InstallHooks{
						Report: func(step, detail string, success bool) error {
							reports = append(reports, installReport{step, detail, success})
							return nil
						},
						Configure: func(context.Context, apps.Manifest) error { return nil },
					})
				if err == nil || len(reports) != 2 || reports[0].step != "fetch" || !reports[0].success ||
					reports[1].step != "file" || reports[1].success {
					t.Fatalf("error = %v, reports = %#v", err, reports)
				}
				if secretReads != 0 {
					t.Fatalf("secret reads = %d", secretReads)
				}
				if _, statErr := os.Stat(filepath.Join(root, "opt", "notes")); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("incoming installed state changed: %v", statErr)
				}
			})
		}
	})

	t.Run("file report failures stop without retry", func(t *testing.T) {
		for _, actionFails := range []bool{false, true} {
			t.Run(map[bool]string{false: "successful action", true: "failed action"}[actionFails], func(t *testing.T) {
				root := t.TempDir()
				reportFailure := errors.New("report write failed")
				actionFailure := errors.New("observe active state failed")
				fileReports := 0
				secretReads := 0
				client := &installCloudClient{
					get: func(context.Context, string) (io.ReadCloser, error) {
						return io.NopCloser(strings.NewReader("compressed")), nil
					},
					readSecrets: func(context.Context, string) (map[string]string, error) {
						secretReads++
						return nil, nil
					},
				}
				err := apps.Install(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
					if command.Name == "xz" {
						return host.Result{Stdout: validInstallTar(t, "app = \"notes\"\nport = 4100\nsecrets = [\"TOKEN\"]\n")}, nil
					}
					if actionFails {
						return host.Result{}, actionFailure
					}
					return host.Result{ExitCode: 3}, nil
				}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }},
					installStoreAt(t, root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"}),
					"s3://bucket/bundle.tar.xz", apps.InstallHooks{
						Report: func(step, _ string, _ bool) error {
							if step == "file" {
								fileReports++
								return reportFailure
							}
							return nil
						},
						Configure: func(context.Context, apps.Manifest) error { return nil },
					})
				var failure *apps.InstallError
				if !errors.As(err, &failure) || failure.Code != 1 || !errors.Is(failure.Cause, reportFailure) {
					t.Fatalf("failure = %#v", err)
				}
				if actionFails && !errors.Is(failure.Cause, actionFailure) {
					t.Fatalf("failure lost action cause: %#v", failure.Cause)
				}
				if fileReports != 1 || secretReads != 0 {
					t.Fatalf("file reports = %d, secret reads = %d", fileReports, secretReads)
				}
			})
		}
	})
}
