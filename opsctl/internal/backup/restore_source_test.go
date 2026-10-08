package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var _ func(context.Context, host.Env, cloud.Env, config.Store, string, *time.Time, string, backup.NginxRegenerator) (backup.RestoreReport, error) = backup.Restore

func TestRestoreSourceContracts(t *testing.T) {
	// R-Z5ZK-14HE R-Z8FC-SNYS
	stepErr := errors.New("step failed")
	step := backup.RestoreStep{Name: "source", Detail: "s3://bucket/notes.tar.zst", Err: stepErr}
	var (
		stepName   string
		stepDetail string
		stepError  error
	)
	stepName, stepDetail, stepError = step.Name, step.Detail, step.Err
	if stepName != "source" || stepDetail != "s3://bucket/notes.tar.zst" || !errors.Is(stepError, stepErr) {
		t.Fatalf("RestoreStep = %+v", step)
	}

	// R-FMVM-I2DD
	report := backup.RestoreReport{Steps: []backup.RestoreStep{step}}
	steps := append([]backup.RestoreStep(nil), report.Steps...)
	if len(steps) != 1 || steps[0].Name != "source" {
		t.Fatalf("RestoreReport = %+v", report)
	}

	cause := errors.New("archive unavailable")
	failure := &backup.RestoreError{Service: "notes", Stage: "source", Err: cause, Stopped: []string{"ikigenba-notes.service"}}
	var (
		failureService string
		failureStage   string
		failureErr     error
		failureStopped []string
	)
	failureService, failureStage, failureErr, failureStopped = failure.Service, failure.Stage, failure.Err, failure.Stopped
	if failureService != "notes" || failureStage != "source" || !errors.Is(failureErr, cause) || len(failureStopped) != 1 || failureStopped[0] != "ikigenba-notes.service" {
		t.Fatalf("RestoreError fields = %+v", failure)
	}
	if failure.Error() != "notes: source: archive unavailable" || !errors.Is(failure, cause) {
		t.Fatalf("RestoreError = %q, unwrap %v", failure.Error(), errors.Unwrap(failure))
	}
}

func TestRestoreSelectsSourceByArchiveTimestamp(t *testing.T) {
	// R-1ZQA-A8XZ R-FDQ6-KYS5 R-DK7F-CAV0
	root := t.TempDir()
	restoreInstalled(t, root, true)
	store := restoreConfiguredStore(t, root)
	writeFile(t, root, "opt/notes/etc/manifest.toml", "app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
	writeFile(t, root, "var/opt/ikigenba/other/state/private", "do not read or report this secret", 0o600)

	olderURI := "s3://bucket/host/notes/2026-09-15T23:00:00Z.tar.zst"
	selectedURI := "s3://bucket/host/notes/2026-09-16T00:00:00Z.tar.zst"
	laterURI := "s3://bucket/host/notes/2026-09-17T00:00:00Z.tar.zst"
	modified := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc", typeflag: tar.TypeDir, mode: 0o750},
		restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
		restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0o750},
		restoreMember{name: "state/value", data: []byte(strings.Repeat("x", 1531234)), mode: 0o640},
	)
	client := &restoreCloud{
		objects: []cloud.Object{
			{URI: "s3://bucket/host/notes/not-a-time.tar.zst", Modified: modified.Add(100 * time.Hour)},
			{URI: "s3://bucket/host/notes/2026-09-18T00:00:00Z.tar.zst/nested"},
			{URI: "s3://bucket/host/other/2027-01-01T00:00:00Z.tar.zst"},
			{URI: olderURI, Modified: modified.Add(200 * time.Hour)},
			{URI: selectedURI, Modified: time.Time{}},
			{URI: laterURI, Modified: modified.Add(-200 * time.Hour)},
		},
		bodies: map[string][]byte{selectedURI: body, laterURI: body},
	}
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	nginxCalls := 0
	report, err := backup.Restore(context.Background(), restoreServiceHostEnv(t, root), cloud.Env{Open: client.open}, store, "notes", &at, "", func(context.Context) error {
		nginxCalls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDetail := fmt.Sprintf("notes/2026-09-16T00:00:00Z.tar.zst, %.1f MiB", float64(len(body))/1048576)
	wantReport := backup.RestoreReport{Steps: []backup.RestoreStep{
		{Name: "source", Detail: wantDetail},
		{Name: "secrets", Detail: "0 keys"},
		{Name: "stop", Detail: "litestream.service; ikigenba-notes.socket, ikigenba-notes.service already inactive"},
		{Name: "files", Detail: "/etc/opt/ikigenba/notes/env, /var/opt/ikigenba/notes/state, 1 files"},
		{Name: "db", Detail: "/var/opt/ikigenba/notes/state/app.db, at 2026-09-16T12:00:00Z"},
		{Name: "litestream", Detail: "state/app.db"},
		{Name: "start", Detail: "litestream.service; ikigenba-notes.socket, ikigenba-notes.service left inactive"},
	}}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("Restore() report = %+v, want %+v", report, wantReport)
	}
	if !reflect.DeepEqual(client.opened, []string{"us-east-2"}) || !reflect.DeepEqual(client.listed, []string{"s3://bucket/host/notes/"}) || !reflect.DeepEqual(client.got, []string{selectedURI}) {
		t.Fatalf("cloud access = opened %v listed %v got %v", client.opened, client.listed, client.got)
	}
	if client.puts != 0 || nginxCalls != 1 || len(client.readers) != 1 || !client.readers[0].closed {
		t.Fatalf("unexpected effects: puts %d nginx %d readers %#v", client.puts, nginxCalls, client.readers)
	}
	if data := readHostRestoreFile(t, root, "var/opt/ikigenba/other/state/private"); string(data) != "do not read or report this secret" {
		t.Fatalf("restore changed another service: %q", data)
	}
	joined := fmt.Sprint(report, err)
	if strings.Contains(joined, root) || strings.Contains(joined, "do not read or report this secret") {
		t.Fatalf("report exposed rooted or sensitive data: %s", joined)
	}
}

func TestRestoreNewestSourceWhenAtIsNil(t *testing.T) {
	// R-1ZQA-A8XZ
	root := t.TempDir()
	restoreInstalled(t, root, false)
	store := restoreConfiguredStore(t, root)
	oldURI := "s3://bucket/host/notes/2026-09-15T00:00:00Z.tar.zst"
	newURI := "s3://bucket/host/notes/2026-09-17T00:00:00.25Z.tar.zst"
	body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("new")})
	client := &restoreCloud{objects: []cloud.Object{{URI: newURI}, {URI: oldURI}}, bodies: map[string][]byte{newURI: body}}
	report, err := backup.Restore(context.Background(), restoreServiceHostEnv(t, root), cloud.Env{Open: client.open}, store, "notes", nil, "", func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 5 || !strings.HasPrefix(report.Steps[0].Detail, "notes/2026-09-17T00:00:00.25Z.tar.zst, ") || !reflect.DeepEqual(client.got, []string{newURI}) {
		t.Fatalf("Restore() = %+v, %v; got %v", report, err, client.got)
	}
}

func TestRestorePreworkflowValidationHasNoSourceStepOrEffects(t *testing.T) {
	// R-1YID-WH7A R-FMVM-I2DD R-G7FZ-2AO2 R-DK7F-CAV0
	tests := []struct {
		name    string
		service string
		setup   func(*testing.T, config.Store)
		nginx   backup.NginxRegenerator
		want    string
	}{
		{name: "empty service", service: "", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "dot service", service: ".", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "dot dot service", service: "..", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "slash service", service: "bad/name", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "nul service", service: "bad\x00name", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "host service", service: "host", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "snapshots service", service: "snapshots", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "seed service", service: "seed", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "deploy service", service: "deploy", nginx: func(context.Context) error { return nil }, want: "invalid service"},
		{name: "prefix missing", service: "notes", setup: func(t *testing.T, store config.Store) {
			if err := store.Del("backup.s3_uri"); err != nil {
				t.Fatal(err)
			}
		}, nginx: func(context.Context) error { return nil }, want: "backup.s3_uri not set"},
		{name: "malformed prefix", service: "notes", setup: func(t *testing.T, store config.Store) {
			if err := store.Set("backup.s3_uri", "https://bucket/path"); err != nil {
				t.Fatal(err)
			}
		}, nginx: func(context.Context) error { return nil }, want: "backup.s3_uri"},
		{name: "region missing after prefix", service: "notes", setup: func(t *testing.T, store config.Store) {
			if err := store.Del("aws.region"); err != nil {
				t.Fatal(err)
			}
		}, nginx: func(context.Context) error { return nil }, want: "aws.region not set"},
		{name: "nil nginx callback", service: "notes", want: "nginx regeneration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, false)
			store := restoreConfiguredStore(t, root)
			if test.setup != nil {
				test.setup(t, store)
			}
			client := &restoreCloud{}
			executions := 0
			env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				executions++
				return host.Result{}, errors.New("unexpected execution")
			}}
			report, err := backup.Restore(context.Background(), env, cloud.Env{Open: client.open}, store, test.service, nil, "", test.nginx)
			if err == nil || !strings.Contains(err.Error(), test.want) || len(report.Steps) != 0 {
				t.Fatalf("Restore() = %+v, %v; want %q", report, err, test.want)
			}
			if len(client.opened) != 0 || len(client.listed) != 0 || len(client.got) != 0 || executions != 0 {
				t.Fatalf("preworkflow effects: client %#v executions %d", client, executions)
			}
		})
	}

	t.Run("missing cloud dependency", func(t *testing.T) {
		root := t.TempDir()
		restoreInstalled(t, root, false)
		store := restoreConfiguredStore(t, root)
		writeFile(t, root, "var/opt/ikigenba/notes/state/existing", "unchanged", 0o600)
		before := fileTreeSnapshot(t, root)
		executions := 0
		nginxCalls := 0
		report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			executions++
			return host.Result{}, errors.New("unexpected host command")
		}}, cloud.Env{}, store, "notes", nil, "", func(context.Context) error {
			nginxCalls++
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "cloud access is not configured") || len(report.Steps) != 0 {
			t.Fatalf("Restore() = %+v, %v", report, err)
		}
		if executions != 0 || nginxCalls != 0 || !reflect.DeepEqual(fileTreeSnapshot(t, root), before) {
			t.Fatalf("missing cloud dependency effects: executions %d, nginx calls %d", executions, nginxCalls)
		}
	})
}

func TestRestoreMissingSourceReportsOneFailedStepWithoutMutation(t *testing.T) {
	// R-FO3I-VU42
	tests := []struct {
		name    string
		objects []cloud.Object
		at      *time.Time
		want    string
	}{
		{name: "no backups", objects: []cloud.Object{{URI: "s3://bucket/host/notes/not-a-backup"}}, want: "no backups for notes under s3://bucket/host/notes/"},
		{name: "none at instant", objects: []cloud.Object{{URI: "s3://bucket/host/notes/2026-09-17T00:00:00Z.tar.zst"}}, at: restoreTimePointer(time.Date(2026, 9, 16, 0, 0, 0, 123000000, time.UTC)), want: "notes: no backup at or before 2026-09-16T00:00:00.123Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, false)
			store := restoreConfiguredStore(t, root)
			writeFile(t, root, "var/opt/ikigenba/notes/state/existing", "unchanged", 0o600)
			before := fileTreeSnapshot(t, root)
			client := &restoreCloud{objects: test.objects}
			env := restoreServiceHostEnv(t, root)
			executions := 0
			originalExecute := env.Execute
			env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
				executions++
				return originalExecute(ctx, command)
			}
			report, err := backup.Restore(context.Background(), env, cloud.Env{Open: client.open}, store, "notes", test.at, "", func(context.Context) error { return nil })
			if err == nil || !strings.Contains(err.Error(), test.want) || len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil {
				t.Fatalf("Restore() = %+v, %v; want %q", report, err, test.want)
			}
			var restoreErr *backup.RestoreError
			if !errors.As(err, &restoreErr) || restoreErr.Service != "notes" || restoreErr.Stage != "source" || !errors.Is(err, report.Steps[0].Err) {
				t.Fatalf("RestoreError = %#v, report %+v", restoreErr, report)
			}
			if len(client.got) != 0 || executions != 0 || !reflect.DeepEqual(fileTreeSnapshot(t, root), before) {
				t.Fatalf("missing-source effects: got %v executions %d", client.got, executions)
			}
		})
	}
}

func TestRestoreRejectsInvalidSourceBeforeHostChanges(t *testing.T) {
	// R-FQJB-NDLG R-24BQ-JTDF R-G7FZ-2AO2 R-FMVM-I2DD
	tests := []struct {
		name       string
		members    []restoreMember
		compressed []byte
		want       string
	}{
		{name: "corrupt compression", compressed: []byte("not zstd"), want: "decompress"},
		{name: "corrupt tar", compressed: rawZstandardFrame([]byte("not tar")), want: "tar archive"},
		{name: "absolute", members: []restoreMember{{name: "/etc/value", data: []byte("bad")}}, want: "unsafe archive entry"},
		{name: "traversal", members: []restoreMember{{name: "etc/../state/value", data: []byte("bad")}}, want: "unsafe archive entry"},
		{name: "outside", members: []restoreMember{{name: "cache/value", data: []byte("bad")}}, want: "outside service restore trees"},
		{name: "device", members: []restoreMember{{name: "state/device", typeflag: tar.TypeChar}}, want: "special device"},
		{name: "symlink traversal", members: []restoreMember{{name: "state/link", typeflag: tar.TypeSymlink, linkname: "../../outside"}, {name: "state/link/value", data: []byte("bad")}}, want: "traverses symlink"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, false)
			store := restoreConfiguredStore(t, root)
			writeFile(t, root, "var/opt/ikigenba/notes/state/existing", "unchanged", 0o600)
			before := fileTreeSnapshot(t, root)
			body := test.compressed
			if body == nil {
				body = hostRestoreArchive(t, test.members...)
			}
			uri := "s3://bucket/host/notes/2026-09-16T00:00:00Z.tar.zst"
			client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
			env := restoreServiceHostEnv(t, root)
			if test.name == "corrupt compression" {
				env.Execute = func(context.Context, host.Command) (host.Result, error) {
					return host.Result{Stderr: []byte("invalid zstandard frame\n"), ExitCode: 1}, nil
				}
			}
			report, err := backup.Restore(context.Background(), env, cloud.Env{Open: client.open}, store, "notes", nil, "", func(context.Context) error { return nil })
			if err == nil || !strings.Contains(err.Error(), test.want) || len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Err == nil {
				t.Fatalf("Restore() = %+v, %v; want %q", report, err, test.want)
			}
			if !reflect.DeepEqual(fileTreeSnapshot(t, root), before) || client.puts != 0 || len(client.readers) != 1 || !client.readers[0].closed {
				t.Fatalf("invalid source effects: puts %d readers %#v", client.puts, client.readers)
			}
		})
	}
}

func restoreTimePointer(value time.Time) *time.Time { return &value }

func TestRestoreUsesRootForRestoredTarget(t *testing.T) {
	// R-FDQ6-KYS5
	root := t.TempDir()
	restoreInstalled(t, root, false)
	store := restoreConfiguredStore(t, root)
	uri := "s3://bucket/host/notes/2026-09-16T00:00:00Z.tar.zst"
	body := hostRestoreArchive(t, restoreMember{name: "etc/env", data: []byte("TOKEN=secret\n")})
	client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
	report, err := backup.Restore(context.Background(), restoreServiceHostEnv(t, root), cloud.Env{Open: client.open}, store, "notes", nil, "", func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 5 {
		t.Fatalf("Restore() = %+v, %v", report, err)
	}
	if data := readHostRestoreFile(t, root, "etc/opt/ikigenba/notes/env"); strings.Contains(string(data), "TOKEN=secret") || !strings.Contains(string(data), "DRAIN_SECONDS=") {
		t.Fatalf("rooted restored target = %q", data)
	}
	if strings.Contains(fmt.Sprint(report), "TOKEN=secret") || strings.Contains(fmt.Sprint(report), root) {
		t.Fatalf("source report exposed sensitive or rooted data: %+v", report)
	}
}

func restoreInstalled(t *testing.T, root string, database bool) {
	t.Helper()
	manifest := "app = \"notes\"\n"
	if database {
		manifest += "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n"
	}
	writeFile(t, root, "opt/notes/bin/notes", "installed binary", 0700)
	writeFile(t, root, "opt/notes/etc/manifest.toml", manifest, 0600)
	writeFile(t, root, "etc/opt/ikigenba/notes/env", "OLD=value\n", 0600)
}
func restoreConfiguredStore(t *testing.T, root string) config.Store {
	t.Helper()
	store := configuredFileStore(t, root)
	if err := store.Set("host.name", "host.example.test"); err != nil {
		t.Fatal(err)
	}
	return store
}
func restoreServiceHostEnv(t *testing.T, root string) host.Env {
	t.Helper()
	executor := &restoreStageExecutor{t: t, root: root}
	return host.Env{Root: root, Execute: executor.execute, Now: func() time.Time { return time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC) }}
}
