package apps_test

import (
	"archive/tar"
	"bytes"
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

func TestInstallValidatesCompleteArchiveLayout(t *testing.T) {
	// R-UG7M-Y36C
	manifest := []byte("app = \"notes\"\n")
	tests := []struct {
		name    string
		entries []installTarEntry
		want    string
	}{
		{"non executable app", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/notes", []byte("app"), 0o644)}, "no executable bin/notes"},
		{"outside top level", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/notes", []byte("app"), 0o755), regularEntry("state/data", nil, 0o600)}, "outside bin, etc, and share"},
		{"duplicate", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/notes", []byte("one"), 0o755), regularEntry("bin/notes", []byte("two"), 0o755)}, "duplicate archive path bin/notes"},
		{"file ancestor", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/notes", []byte("app"), 0o755), regularEntry("share/item", []byte("file"), 0o644), regularEntry("share/item/child", []byte("child"), 0o644)}, "file ancestor"},
		{"file descendant before ancestor", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/notes", []byte("app"), 0o755), regularEntry("share/item/child", []byte("child"), 0o644), regularEntry("share/item", []byte("file"), 0o644)}, "file ancestor"},
		{"generated environment", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/notes", []byte("app"), 0o755), regularEntry("etc/env", []byte("TOKEN=evil"), 0o644)}, "etc/env is reserved"},
		{"absolute", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("/bin/notes", nil, 0o755)}, "invalid archive path"},
		{"dot component", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/./notes", nil, 0o755)}, "invalid archive path"},
		{"dotdot component", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), regularEntry("bin/../notes", nil, 0o755)}, "invalid archive path"},
		{"link", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), {header: tar.Header{Name: "bin/notes", Typeflag: tar.TypeSymlink, Linkname: "/tmp/target", Mode: 0o777}}}, "not a regular file or directory"},
		{"special", []installTarEntry{regularEntry("etc/manifest.toml", manifest, 0o644), {header: tar.Header{Name: "bin/notes", Typeflag: tar.TypeChar, Mode: 0o755}}}, "not a regular file or directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			reports, _, err := runInstallArchive(t, root, tarEntries(t, test.entries), nil, nil)
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != 2 || !strings.Contains(reports[len(reports)-1].detail, test.want) {
				t.Fatalf("failure = %#v, reports = %#v, want %q", err, reports, test.want)
			}
			if got := reports[len(reports)-1]; got.step != "file" || got.success {
				t.Fatalf("last report = %#v", got)
			}
			if _, statErr := os.Stat(filepath.Join(root, "opt")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("archive rejection changed installed host state: %v", statErr)
			}
		})
	}
}

func TestInstallAcceptsOptionalShareAndAdditionalFiles(t *testing.T) {
	// R-UG7M-Y36C
	root := t.TempDir()
	archive := tarEntries(t, []installTarEntry{
		regularEntry("etc/manifest.toml", []byte("app = \"notes\"\n"), 0o644),
		regularEntry("etc/extra.conf", []byte("configuration"), 0o640),
		regularEntry("bin/notes", []byte("binary"), 0o755),
		regularEntry("bin/helper", []byte("helper"), 0o750),
		regularEntry("share/public/index.html", []byte("page"), 0o644),
	})
	_, _, _ = runInstallArchive(t, root, archive, nil, nil)
	assertFile(t, filepath.Join(root, "opt", "notes", "etc", "extra.conf"), "configuration")
	assertFile(t, filepath.Join(root, "opt", "notes", "bin", "helper"), "helper")
	assertFile(t, filepath.Join(root, "opt", "notes", "share", "public", "index.html"), "page")
}

func TestInstallDiscoversDefaultsBeforeSecretsOrMutation(t *testing.T) {
	// R-OUAD-BPMU
	archive := validInstallTar(t, "app = \"notes\"\ndefault = true\n")
	for _, test := range []struct {
		name     string
		service  string
		manifest string
		want     string
	}{
		{"competing default", "web", "app = \"web\"\ndefault = true\n", "notes: web is already the default app"},
		{"bad service manifest", "web", "app = [\n", "web:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, filepath.Join(root, "opt", test.service, "etc", "manifest.toml"), []byte(test.manifest), 0o600)
			secretRead := false
			reports, _, err := runInstallArchive(t, root, archive, func(context.Context, string) (map[string]string, error) {
				secretRead = true
				return nil, nil
			}, nil)
			if err == nil || reports[len(reports)-1].step != "file" || reports[len(reports)-1].success ||
				!strings.Contains(reports[len(reports)-1].detail, test.want) {
				t.Fatalf("error = %v, reports = %#v", err, reports)
			}
			if secretRead {
				t.Fatal("discovery failure read secrets")
			}
			assertFile(t, filepath.Join(root, "opt", test.service, "etc", "manifest.toml"), test.manifest)
			if _, statErr := os.Stat(filepath.Join(root, "opt", "notes")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("discovery failure changed incoming app state: %v", statErr)
			}
		})
	}
}

func TestInstallReadsDistinctSecretsAndReportsMissingInOrder(t *testing.T) {
	// R-WY7U-IS2M
	// R-5RIZ-EOX9
	archive := validInstallTar(t, "app = \"notes\"\nsecrets = [\"FIRST\", \"SECOND\", \"FIRST\"]\n")
	root := t.TempDir()
	var parameters []string
	reports, _, err := runInstallArchive(t, root, archive, func(_ context.Context, parameter string) (map[string]string, error) {
		parameters = append(parameters, parameter)
		return map[string]string{"UNREQUESTED": "omit", "SECOND": "present"}, nil
	}, nil)
	if err == nil || len(parameters) != 1 || parameters[0] != "/host.example/notes" {
		t.Fatalf("error = %v, parameters = %v", err, parameters)
	}
	want := "notes: no value for 'FIRST' in /host.example/notes"
	if got := reports[len(reports)-1]; got != (installReport{"secrets", want, false}) {
		t.Fatalf("last report = %#v", got)
	}

	root = t.TempDir()
	reports, _, err = runInstallArchive(t, root, archive, func(context.Context, string) (map[string]string, error) {
		return nil, cloud.ErrNotFound
	}, nil)
	if err == nil || reports[len(reports)-1].detail != want {
		t.Fatalf("missing parameter error = %v, reports = %#v", err, reports)
	}

	for _, operational := range []error{errors.New("access denied"), errors.New("transport failed")} {
		root = t.TempDir()
		reports, _, err = runInstallArchive(t, root, archive, func(context.Context, string) (map[string]string, error) {
			return nil, operational
		}, nil)
		var failure *apps.InstallError
		if !errors.As(err, &failure) || failure.Code != 1 || !errors.Is(failure.Cause, operational) ||
			errors.Is(failure.Cause, cloud.ErrNotFound) || reports[len(reports)-1].detail != operational.Error() {
			t.Fatalf("operational secret error = %#v, reports = %#v", err, reports)
		}
	}

	emptyArchive := validInstallTar(t, "app = \"notes\"\n")
	root = t.TempDir()
	reports, _, _ = runInstallArchive(t, root, emptyArchive, func(context.Context, string) (map[string]string, error) {
		t.Fatal("empty secret list called ReadSecrets")
		return nil, nil
	}, nil)
	if !containsReport(reports, installReport{"secrets", "0 keys", true}) {
		t.Fatalf("reports = %#v", reports)
	}
}

func TestInstallValidatesEnvironmentWithoutExposingValues(t *testing.T) {
	// R-UINF-PMNQ
	secretValue := "top-secret\nsecond-line"
	tests := []struct {
		name     string
		manifest string
		secrets  map[string]string
	}{
		{"invalid secret name", "secrets = [\"BAD-NAME\"]\n", map[string]string{"BAD-NAME": "x"}},
		{"invalid secret start", "secrets = [\"9BAD\"]\n", map[string]string{"9BAD": "x"}},
		{"invalid setting name", "[env]\n\"BAD-NAME\" = \"x\"\n", nil},
		{"secret drain", "secrets = [\"DRAIN_SECONDS\"]\n", map[string]string{"DRAIN_SECONDS": "x"}},
		{"setting drain", "[env]\nDRAIN_SECONDS = \"x\"\n", nil},
		{"overlap", "secrets = [\"TOKEN\"]\n[env]\nTOKEN = \"plain\"\n", map[string]string{"TOKEN": "value-not-for-diagnostics"}},
		{"secret newline", "secrets = [\"TOKEN\"]\n", map[string]string{"TOKEN": secretValue}},
		{"secret nul", "secrets = [\"TOKEN\"]\n", map[string]string{"TOKEN": "bad\x00value"}},
		{"setting carriage return", "[env]\nTOKEN = \"bad\\rvalue\"\n", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			archive := validInstallTar(t, "app = \"notes\"\n"+test.manifest)
			reports, _, err := runInstallArchive(t, root, archive, func(context.Context, string) (map[string]string, error) {
				return test.secrets, nil
			}, nil)
			if err == nil || reports[len(reports)-1].step != "secrets" || reports[len(reports)-1].success {
				t.Fatalf("error = %v, reports = %#v", err, reports)
			}
			all := err.Error()
			for _, report := range reports {
				all += report.detail
			}
			for _, value := range test.secrets {
				if value != "x" && strings.Contains(all, value) {
					t.Fatalf("diagnostics exposed environment value %q: %q", value, all)
				}
			}
		})
	}
}

func TestInstallReplacesFilesPublishesEnvironmentAndPreservesData(t *testing.T) {
	// R-UG7M-Y36C
	// R-OUAD-BPMU
	// R-WY7U-IS2M
	// R-UINF-PMNQ
	// R-OXY2-H0UX
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "opt", "notes", "bin", "stale"), []byte("old"), 0o700)
	writeFixture(t, filepath.Join(root, "opt", "notes", "etc", "stale"), []byte("old"), 0o600)
	writeFixture(t, filepath.Join(root, "opt", "notes", "etc", "manifest.toml"), []byte("app = \"notes\"\ndefault = true\n"), 0o600)
	writeFixture(t, filepath.Join(root, "opt", "notes", "share", "stale"), []byte("old"), 0o600)
	writeFixture(t, filepath.Join(root, "opt", "notes", "state", "db"), []byte("state"), 0o600)
	writeFixture(t, filepath.Join(root, "opt", "notes", "cache", "item"), []byte("cache"), 0o600)
	writeFixture(t, filepath.Join(root, "opt", "other", "etc", "manifest.toml"), []byte("app = \"other\"\n"), 0o600)
	writeFixture(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-other.service"), []byte("other unit"), 0o600)

	manifest := "app = \"notes\"\ndefault = true\nsecrets = [\"TOKEN\", \"EMPTY\", \"TOKEN\"]\n[env]\nMODE = \"production\"\nQUOTED = \"a\\\"b\\\\c\"\n"
	archive := tarEntries(t, []installTarEntry{
		regularEntry("etc/manifest.toml", []byte(manifest), 0o644),
		regularEntry("etc/config", []byte("new config"), 0o640),
		regularEntry("bin/notes", []byte("new binary"), 0o751),
		regularEntry("bin/helper", []byte("helper"), 0o755),
	})
	phase4 := errors.New("phase 4 account lookup stopped")
	reports, commands, err := runInstallArchive(t, root, archive, func(_ context.Context, parameter string) (map[string]string, error) {
		if parameter != "/host.example/notes" {
			t.Fatalf("parameter = %q", parameter)
		}
		return map[string]string{"TOKEN": "a value", "EMPTY": "", "EXTRA": "omit"}, nil
	}, func(command host.Command) (host.Result, error) {
		if command.Name == "id" {
			return host.Result{}, phase4
		}
		if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show" {
			return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=\n")}, nil
		}
		return host.Result{ExitCode: 3}, nil
	})
	if !errors.Is(err, phase4) {
		t.Fatalf("Install error = %v, want phase 4 seam", err)
	}
	wantPrefix := []installReport{
		{"fetch", "bundle.tar.xz, 0.0 MiB", true},
		{"file", "notes, default", true},
		{"secrets", "2 keys", true},
		{"unpack", "/opt/notes", true},
	}
	if !reflect.DeepEqual(reports[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("reports = %#v, want prefix %#v", reports, wantPrefix)
	}
	if len(commands) != 4 || commands[1].Name != "systemctl" || !reflect.DeepEqual(commands[1].Args, []string{"is-active", "ikigenba-notes.service"}) || commands[2].Name != "systemctl" || commands[3].Name != "id" {
		t.Fatalf("commands = %#v", commands)
	}
	assertFile(t, filepath.Join(root, "opt", "notes", "bin", "notes"), "new binary")
	assertFile(t, filepath.Join(root, "opt", "notes", "bin", "helper"), "helper")
	assertFile(t, filepath.Join(root, "opt", "notes", "etc", "config"), "new config")
	assertFile(t, filepath.Join(root, "opt", "notes", "state", "db"), "state")
	assertFile(t, filepath.Join(root, "opt", "notes", "cache", "item"), "cache")
	assertFile(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-other.service"), "other unit")
	for _, stale := range []string{"bin/stale", "etc/stale", "share"} {
		if _, statErr := os.Stat(filepath.Join(root, "opt", "notes", filepath.FromSlash(stale))); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("stale path %s still exists: %v", stale, statErr)
		}
	}
	envPath := filepath.Join(root, "opt", "notes", "etc", "env")
	assertFile(t, envPath, "TOKEN=\"a value\"\nEMPTY=\"\"\nMODE=\"production\"\nQUOTED=\"a\\\"b\\\\c\"\nDRAIN_SECONDS=5\n")
	info, statErr := os.Stat(envPath)
	if statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("env mode = %v, error = %v", info.Mode().Perm(), statErr)
	}
}

func TestInstallRejectsDestinationSymlinkWithoutFollowingIt(t *testing.T) {
	// R-UG7M-Y36C
	root := t.TempDir()
	outside := t.TempDir()
	writeFixture(t, filepath.Join(outside, "marker"), []byte("unchanged"), 0o600)
	if err := os.MkdirAll(filepath.Join(root, "opt"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "opt", "notes")); err != nil {
		t.Fatal(err)
	}
	reports, _, err := runInstallArchive(t, root, validInstallTar(t, "app = \"notes\"\n"), nil, nil)
	if err == nil || reports[len(reports)-1].step != "unpack" || !strings.Contains(reports[len(reports)-1].detail, "symbolic link") {
		t.Fatalf("error = %v, reports = %#v", err, reports)
	}
	assertFile(t, filepath.Join(outside, "marker"), "unchanged")
}

type installTarEntry struct {
	header tar.Header
	data   []byte
}

func regularEntry(name string, data []byte, mode int64) installTarEntry {
	return installTarEntry{header: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(data))}, data: data}
}

func tarEntries(t *testing.T, entries []installTarEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for _, entry := range entries {
		if err := writer.WriteHeader(&entry.header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func validInstallTar(t *testing.T, manifest string) []byte {
	t.Helper()
	return tarEntries(t, []installTarEntry{
		regularEntry("etc/manifest.toml", []byte(manifest), 0o644),
		regularEntry("bin/notes", []byte("binary"), 0o755),
	})
}

func runInstallArchive(
	t *testing.T,
	root string,
	archive []byte,
	readSecrets func(context.Context, string) (map[string]string, error),
	afterXZ func(host.Command) (host.Result, error),
) ([]installReport, []host.Command, error) {
	t.Helper()
	store := installStoreAt(t, root, map[string]string{"host.name": "HOST.EXAMPLE.", "aws.region": "us-east-1"})
	client := &installCloudClient{
		get: func(context.Context, string) (io.ReadCloser, error) {
			return &trackedReadCloser{Reader: strings.NewReader("compressed")}, nil
		},
		readSecrets: readSecrets,
	}
	var reports []installReport
	var commands []host.Command
	err := apps.Install(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		if command.Name == "xz" {
			return host.Result{Stdout: archive}, nil
		}
		if afterXZ != nil {
			return afterXZ(command)
		}
		if command.Name == "systemctl" {
			if len(command.Args) > 0 && command.Args[0] == "show" {
				return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=\n")}, nil
			}
			return host.Result{ExitCode: 3}, nil
		}
		return host.Result{}, errors.New("phase 4 account lookup stopped")
	}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
		return client, nil
	}}, store, "s3://bucket/releases/bundle.tar.xz", apps.InstallHooks{
		Report: func(step, detail string, success bool) error {
			reports = append(reports, installReport{step, detail, success})
			return nil
		},
		Configure: func(context.Context, apps.Manifest) error {
			return errors.New("configuration reached after phase 3 boundary")
		},
	})
	return reports, commands, err
}

func containsReport(reports []installReport, want installReport) bool {
	for _, report := range reports {
		if report == want {
			return true
		}
	}
	return false
}

func writeFixture(t *testing.T, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, mode); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, name, want string) {
	t.Helper()
	filesystem, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		t.Fatalf("open root for %s: %v", name, err)
	}
	defer func() { _ = filesystem.Close() }()
	data, err := filesystem.ReadFile(filepath.Base(name))
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", name, data, err, want)
	}
}
