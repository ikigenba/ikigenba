package apps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func TestReadReleaseSecretsOnlyRequestedKeys(t *testing.T) {
	// R-AANM-R65B R-ABVJ-4XW0 R-APAF-CF1N
	ctx := t.Context()
	result, err := apps.ReadSecrets(ctx, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
		t.Fatal("opened cloud for no secrets")
		return nil, nil
	}}, "region", "host", apps.Manifest{})
	if err != nil || len(result) != 0 || result == nil {
		t.Fatalf("empty secrets %v %v", result, err)
	}
	failure := errors.New("cloud failure")
	for _, test := range []struct {
		name             string
		values           map[string]string
		openErr, readErr error
		want             map[string]string
		wantErr          string
		cause            error
	}{
		{name: "ordered distinct", values: map[string]string{"A": "", "B": "secret", "OTHER": "not requested"}, want: map[string]string{"A": "", "B": "secret"}},
		{name: "first missing", values: map[string]string{"OTHER": "value"}, wantErr: "notes: no value for 'A' in /host/notes"},
		{name: "parameter absent", readErr: cloud.ErrNotFound, wantErr: "notes: no value for 'A' in /host/notes"},
		{name: "open failure", openErr: failure, cause: failure},
		{name: "read failure", readErr: failure, cause: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			before := snapshotTree(t, root)
			opens, reads := 0, 0
			remote := cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
				opens++
				if region != "region" {
					t.Fatalf("region %q", region)
				}
				if test.openErr != nil {
					return nil, test.openErr
				}
				return &installCloudClient{readSecrets: func(_ context.Context, parameter string) (map[string]string, error) {
					reads++
					if parameter != "/host/notes" {
						t.Fatalf("parameter %q", parameter)
					}
					return test.values, test.readErr
				}}, nil
			}}
			result, err := apps.ReadSecrets(t.Context(), remote, "region", "host", apps.Manifest{App: "notes", Secrets: []string{"A", "B", "A"}})
			switch {
			case test.wantErr != "":
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("error %v", err)
				}
			case test.cause != nil:
				if !errors.Is(err, test.cause) {
					t.Fatalf("cause %v", err)
				}
			case err != nil || !reflect.DeepEqual(result, test.want):
				t.Fatalf("result %v %v", result, err)
			}
			wantReads := 1
			if test.openErr != nil {
				wantReads = 0
			}
			if opens != 1 || reads != wantReads {
				t.Fatalf("calls %d %d", opens, reads)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, root)) {
				t.Fatal("read changed filesystem")
			}
		})
	}
}

func TestReleaseEnvironmentExactEncodingAndPurity(t *testing.T) {
	// R-AD3F-IPMP R-AFJ8-A943 R-APAF-CF1N
	r := release.Release{SHA: strings.Repeat("a", 40), Label: "candidate"}
	m := apps.Manifest{App: "notes", Secrets: []string{"B", "A", "B"}, Env: map[string]string{"Z": "tail", "C": "quote\" slash\\"}}
	secrets := map[string]string{"A": "", "B": "private", "OTHER": "ignored"}
	for _, label := range []string{r.Label, ""} {
		r.Label = label
		data, err := apps.ReleaseEnv(m, secrets, apps.Timeouts{DrainSeconds: 7, StopSeconds: 9}, r)
		want := "B=\"private\"\nA=\"\"\nC=\"quote\\\" slash\\\\\"\nZ=\"tail\"\nDRAIN_SECONDS=7\nIKIGENBA_SERVICES=/run/ikigenba/services.json\nIKIGENBA_COMMIT=" + r.SHA + "\n"
		if label != "" {
			want += "IKIGENBA_RELEASE=" + label + "\n"
		}
		if err != nil || string(data) != want {
			t.Fatalf("environment %q %v want %q", data, err, want)
		}
		again, err := apps.ReleaseEnv(m, secrets, apps.Timeouts{DrainSeconds: 7, StopSeconds: 9}, r)
		if err != nil || string(again) != want || m.Secrets[0] != "B" || secrets["OTHER"] != "ignored" {
			t.Fatal("not pure")
		}
	}
}

func TestReleaseEnvironmentRejectsInvalidNamesAndValues(t *testing.T) {
	// R-AEBB-WHDE
	for _, name := range []string{"", "9KEY", "bad-name", "DRAIN_SECONDS", "PORT", "IKIGENBA_SERVICES", "IKIGENBA_COMMIT", "IKIGENBA_RELEASE"} {
		for _, secret := range []bool{false, true} {
			m := apps.Manifest{App: "notes"}
			values := map[string]string{}
			if secret {
				m.Secrets = []string{name}
				values[name] = "private-value"
			} else {
				m.Env = map[string]string{name: "private-value"}
			}
			_, err := apps.ReleaseEnv(m, values, apps.Timeouts{}, release.Release{})
			if err == nil || !strings.Contains(err.Error(), "notes") || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("error %v", err)
			}
		}
	}
	for _, value := range []string{"private\x00value", "private\rvalue", "private\nvalue"} {
		for _, secret := range []bool{false, true} {
			m := apps.Manifest{App: "notes"}
			values := map[string]string{}
			if secret {
				m.Secrets = []string{"KEY"}
				values["KEY"] = value
			} else {
				m.Env = map[string]string{"KEY": value}
			}
			_, err := apps.ReleaseEnv(m, values, apps.Timeouts{}, release.Release{})
			if err == nil || !strings.Contains(err.Error(), "notes") || !strings.Contains(err.Error(), "KEY") || strings.Contains(err.Error(), "private") {
				t.Fatalf("error %v", err)
			}
		}
	}
	_, err := apps.ReleaseEnv(apps.Manifest{App: "notes", Secrets: []string{"KEY"}, Env: map[string]string{"KEY": "private"}}, map[string]string{"KEY": "private"}, apps.Timeouts{}, release.Release{})
	if err == nil || !strings.Contains(err.Error(), "notes") || !strings.Contains(err.Error(), "KEY") || strings.Contains(err.Error(), "private") {
		t.Fatalf("overlap error %v", err)
	}
}

func TestReleaseEnvironmentErrorIsPure(t *testing.T) {
	// R-APAF-CF1N
	m := apps.Manifest{App: "notes", Env: map[string]string{"IKIGENBA_COMMIT": "hidden", "IKIGENBA_RELEASE": "hidden"}}
	_, first := apps.ReleaseEnv(m, nil, apps.Timeouts{}, release.Release{})
	if first == nil {
		t.Fatal("reserved env accepted")
	}
	for range 32 {
		_, err := apps.ReleaseEnv(m, nil, apps.Timeouts{}, release.Release{})
		if err == nil || err.Error() != first.Error() {
			t.Fatalf("same arguments changed error: %v vs %v", first, err)
		}
	}
}

func TestWriteReleaseEnvironmentAtomicAndNameValidation(t *testing.T) {
	// R-AGR4-O0US R-SV82-4IER
	mask := syscall.Umask(0o077)
	defer syscall.Umask(mask)
	root := t.TempDir()
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("WriteEnv executed command")
		return host.Result{}, nil
	}}
	before := snapshotTree(t, root)
	if apps.WriteEnv(env, "bad/name", []byte("bad")) == nil || !reflect.DeepEqual(before, snapshotTree(t, root)) {
		t.Fatal("invalid name touched filesystem")
	}
	if err := apps.WriteEnv(env, "notes", []byte("FIRST=one\n")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, apps.EnvRoot, "notes", "env")
	for _, dir := range []string{"etc/opt", "etc/opt/ikigenba", "etc/opt/ikigenba/notes"} {
		assertMode(t, filepath.Join(root, dir), 0755)
	}
	old, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	if err := filesystem.Chmod("etc/opt/ikigenba/notes", 0o711); err != nil {
		t.Fatal(err)
	}
	if err := apps.WriteEnv(env, "notes", []byte("SECOND=two\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(destination)
	if err != nil || os.SameFile(old, info) {
		t.Fatal("environment not replaced atomically")
	}
	assertFile(t, destination, "SECOND=two\n")
	assertMode(t, destination, 0600)
	assertMode(t, filepath.Dir(destination), 0711)
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("left temporary file: %v %v", entries, err)
	}
}
