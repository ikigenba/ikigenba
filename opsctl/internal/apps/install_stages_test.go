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
	// R-UPYU-093W
	t.Run("success is reported after every file check", func(t *testing.T) {
		root := t.TempDir()
		archive := validInstallTar(t, "app = \"notes\"\nsecrets = [\"TOKEN\"]\n")
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
					{"file", "notes", true},
				}
				if !reflect.DeepEqual(reports, want) {
					t.Fatalf("reports at secret read = %#v, want %#v", reports, want)
				}
				if !reflect.DeepEqual(commands, []commandCall{
					{"xz", []string{"--decompress", "--stdout"}},
					{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
					{"systemctl", []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
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
				if len(command.Args) > 0 && command.Args[0] == "show" {
					return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=\n")}, nil
				}
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
				archive: func(t *testing.T) []byte { return validInstallTar(t, "app = \"notes\"\n") },
				setup: func(t *testing.T, root string) {
					writeFixture(t, filepath.Join(root, "opt", "other", "etc", "manifest.toml"), []byte("app = [\n"), 0o600)
				},
			},
			{
				name:    "competing default validation",
				archive: func(t *testing.T) []byte { return validInstallTar(t, "app = \"notes\"\ndefault = true\n") },
				setup: func(t *testing.T, root string) {
					writeFixture(t, filepath.Join(root, "opt", "other", "etc", "manifest.toml"), []byte("app = \"other\"\ndefault = true\n"), 0o600)
				},
			},
			{
				name:    "initial unit state observation",
				archive: func(t *testing.T) []byte { return validInstallTar(t, "app = \"notes\"\n") },
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
					if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show" {
						return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=\n")}, nil
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
						return host.Result{Stdout: validInstallTar(t, "app = \"notes\"\nsecrets = [\"TOKEN\"]\n")}, nil
					}
					if actionFails {
						return host.Result{}, actionFailure
					}
					if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show" {
						return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=\n")}, nil
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

func TestInstallRunsStagesInOrderAndStopsAtConfigurationFailure(t *testing.T) {
	// R-USEM-RSLA
	for _, failAt := range []string{"", "nginx", "litestream"} {
		name := failAt
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newCompletedInstallFixture(t, t.TempDir(), false)
			fixture.archive = validInstallTar(t, "app = \"notes\"\nsecrets = [\"TOKEN\"]\n")
			cause := errors.New("configuration failed")
			fixture.configure = func(_ context.Context, manifest apps.Manifest) error {
				if manifest.App != "notes" {
					t.Fatalf("configured manifest = %#v", manifest)
				}
				for _, stage := range []string{"nginx", "litestream"} {
					if stage == failAt {
						fixture.reports = append(fixture.reports, installReport{stage, cause.Error(), false})
						return cause
					}
					fixture.reports = append(fixture.reports, installReport{stage, "configured", true})
				}
				return nil
			}
			err := fixture.run()
			wantSteps := []string{"fetch", "file", "secrets", "unpack", "unit", "nginx", "litestream", "service"}
			if failAt != "" {
				var failure *apps.InstallError
				if !errors.As(err, &failure) || failure.Code != 1 || !errors.Is(failure.Cause, cause) {
					t.Fatalf("failure = %#v; want callback cause", err)
				}
				if failAt == "nginx" {
					wantSteps = wantSteps[:6]
				} else {
					wantSteps = wantSteps[:7]
				}
			} else if err != nil {
				t.Fatal(err)
			}
			gotSteps := make([]string, 0, len(fixture.reports))
			for _, report := range fixture.reports {
				gotSteps = append(gotSteps, report.step)
			}
			if !reflect.DeepEqual(gotSteps, wantSteps) {
				t.Fatalf("stage order = %#v, want %#v; reports = %#v", gotSteps, wantSteps, fixture.reports)
			}
			if failAt != "" {
				last := fixture.reports[len(fixture.reports)-1]
				if last.step != failAt || last.success || last.detail != cause.Error() {
					t.Fatalf("failed stage outcome = %#v", last)
				}
			}
			if fixture.configureCalls != 1 {
				t.Fatalf("Configure calls = %d", fixture.configureCalls)
			}
			for _, command := range fixture.commands {
				if failAt != "" && command.name == "systemctl" && len(command.args) > 0 &&
					(command.args[0] == "start" || command.args[0] == "restart") {
					t.Fatalf("service started after %s failure: %#v", failAt, fixture.commands)
				}
			}
		})
	}
	t.Run("domain failure", func(t *testing.T) {
		fixture := newCompletedInstallFixture(t, t.TempDir(), false)
		fixture.archive = validInstallTar(t, "app = \"notes\"\nsecrets = [\"TOKEN\"]\n")
		cause := errors.New("parameter unavailable")
		fixture.secretsFailure = cause
		err := fixture.run()
		var failure *apps.InstallError
		if !errors.As(err, &failure) || !errors.Is(failure.Cause, cause) {
			t.Fatalf("failure = %#v; want parameter cause", err)
		}
		want := []string{"fetch", "file", "secrets"}
		var got []string
		for _, report := range fixture.reports {
			got = append(got, report.step)
		}
		if !reflect.DeepEqual(got, want) || fixture.reports[2].success || fixture.reports[2].detail != cause.Error() || fixture.configureCalls != 0 {
			t.Fatalf("reports = %#v, Configure calls = %d", fixture.reports, fixture.configureCalls)
		}
		if _, statErr := os.Stat(filepath.Join(fixture.root, "opt", "notes")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("host mutated after secret failure: %v", statErr)
		}
	})
}
