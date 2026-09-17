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
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestNginxRegeneratorContractAndPreflight(t *testing.T) {
	// R-1NHS-71WK R-1OPO-KTN9
	typeOf := reflect.TypeFor[backup.NginxRegenerator]()
	if typeOf.Name() != "NginxRegenerator" || typeOf.Kind() != reflect.Func || typeOf.NumIn() != 1 || typeOf.In(0) != reflect.TypeFor[context.Context]() || typeOf.NumOut() != 1 || typeOf.Out(0) != reflect.TypeFor[error]() {
		t.Fatalf("NginxRegenerator type = %v", typeOf)
	}

	root := t.TempDir()
	store := configuredFileStore(t, root)
	used := false
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		used = true
		return host.Result{}, errors.New("unexpected process access")
	}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
		used = true
		return nil, errors.New("unexpected cloud access")
	}}, store, "notes", nil, nil)
	if err == nil || err.Error() != "restore nginx regeneration is not configured" || len(report.Steps) != 0 || used {
		t.Fatalf("nil regenerator = %+v, %v, boundary used %v", report, err, used)
	}
}

func TestRestoreRunsNginxOnceAfterPublicationAndBeforeStarts(t *testing.T) {
	// R-1OPO-KTN9
	for _, test := range []struct {
		name      string
		service   string
		installed bool
		active    bool
	}{
		{name: "no database app", service: "notes", installed: true, active: true},
		{name: "data only", service: "backup-host"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("published"), uid: os.Getuid(), gid: os.Getgid()})
			executor := &restoreStageExecutor{t: t, root: root, installed: test.installed, active: test.active}
			calls := 0
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientForService(t, body, test.service).open}, store, test.service, nil, func(context.Context) error {
				calls++
				if got := string(readHostRestoreFile(t, root, "opt/"+test.service+"/state/value")); got != "published" {
					t.Fatalf("nginx ran before restored files were published: %q", got)
				}
				if strings.Contains(strings.Join(executor.commands, "\n"), "systemctl start") {
					t.Fatalf("unit started before nginx callback: %v", executor.commands)
				}
				return nil
			})
			if err != nil || calls != 1 || len(report.Steps) != 4 || report.Steps[3].Name != "start" {
				t.Fatalf("Restore() = %+v, %v, nginx calls %d", report, err, calls)
			}
		})
	}
}

func TestRestoreNginxFailurePreservesCauseAndStopsWorkflow(t *testing.T) {
	// R-1OPO-KTN9 R-G7FZ-2AO2 R-RX15-3IAF
	root := t.TempDir()
	store := configuredFileStore(t, root)
	body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("published"), uid: os.Getuid(), gid: os.Getgid()})
	executor := &restoreStageExecutor{t: t, root: root, installed: true, active: true}
	cause := errors.New("nginx publication unavailable")
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error { return cause })
	var failure *backup.RestoreError
	if err == nil || !errors.Is(err, cause) || !errors.As(err, &failure) || failure.Stage != "nginx regeneration" || !reflect.DeepEqual(failure.Stopped, []string{"ikigenba-notes.service"}) {
		t.Fatalf("Restore() error = %#v", err)
	}
	if len(report.Steps) != 3 || report.Steps[0].Name != "source" || report.Steps[1].Name != "stop" || report.Steps[2].Name != "files" {
		t.Fatalf("report = %+v", report.Steps)
	}
	if strings.Contains(strings.Join(executor.commands, "\n"), "systemctl start") {
		t.Fatalf("nginx failure allowed a later start: %v", executor.commands)
	}
	if got := string(readHostRestoreFile(t, root, "opt/notes/state/value")); got != "published" {
		t.Fatalf("nginx failure rolled back published files: %q", got)
	}
	if _, statErr := os.Stat(filepath.Join(root, "run/opsctl/restore/notes.active")); statErr != nil {
		t.Fatalf("nginx failure removed activation intent: %v", statErr)
	}
}
