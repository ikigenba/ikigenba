package apps_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallChecksIconBeforeDiscoveryWithoutMutation(t *testing.T) {
	// R-2KGK-SCJS
	tests := []struct {
		name    string
		entries []installTarEntry
		want    error
	}{
		{"invalid XML", []installTarEntry{regularEntry(apps.IconPath, []byte("<svg/>trailing"), 0o644)}, apps.ErrIconNotSVG},
		{"too large", []installTarEntry{regularEntry(apps.IconPath, bytes.Repeat([]byte("x"), 65537), 0o644)}, apps.ErrIconTooLarge},
		{"directory", []installTarEntry{{header: tar.Header{Name: apps.IconPath + "/", Typeflag: tar.TypeDir, Mode: 0o755}}}, apps.ErrIconNotSVG},
		{"symlink", []installTarEntry{{header: tar.Header{Name: apps.IconPath, Typeflag: tar.TypeSymlink, Linkname: "public.svg"}}}, apps.ErrIconNotSVG},
		{"hard link", []installTarEntry{{header: tar.Header{Name: apps.IconPath, Typeflag: tar.TypeLink, Linkname: "share/public.svg"}}}, apps.ErrIconNotSVG},
		{"FIFO", []installTarEntry{{header: tar.Header{Name: apps.IconPath, Typeflag: tar.TypeFifo}}}, apps.ErrIconNotSVG},
		{"device", []installTarEntry{{header: tar.Header{Name: apps.IconPath, Typeflag: tar.TypeChar}}}, apps.ErrIconNotSVG},
		{"child only", []installTarEntry{regularEntry(apps.IconPath+"/child", []byte("<svg/>"), 0o644)}, apps.ErrIconNotSVG},
		{"regular with child", []installTarEntry{regularEntry(apps.IconPath, []byte("<svg/>"), 0o644), regularEntry(apps.IconPath+"/child", nil, 0o644)}, apps.ErrIconNotSVG},
		{"child link", []installTarEntry{{header: tar.Header{Name: apps.IconPath + "/child", Typeflag: tar.TypeSymlink, Linkname: "outside"}}}, apps.ErrIconNotSVG},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			// Discovery would reject this installed manifest if it ran before icon validation.
			marker := filepath.Join(root, "opt", "old", "etc", "manifest.toml")
			writeFixture(t, marker, []byte("app = [\n"), 0o600)
			fixture := newCompletedInstallFixture(t, root, false)
			entries := []installTarEntry{regularEntry("etc/manifest.toml", []byte("app = \"notes\"\n"), 0o644), regularEntry("bin/notes", []byte("binary"), 0o755)}
			fixture.archive = tarEntries(t, append(entries, test.entries...))
			err := fixture.run()
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != 2 || !errors.Is(failure.Cause, test.want) {
				t.Fatalf("failure = %#v", err)
			}
			wantReports := []installReport{{"fetch", "notes-from-uri-v0.tar.xz, 0.0 MiB", true}, {"file", test.want.Error(), false}}
			if !reflect.DeepEqual(fixture.reports, wantReports) || fixture.configureCalls != 0 {
				t.Fatalf("reports = %#v, Configure = %d", fixture.reports, fixture.configureCalls)
			}
			if !reflect.DeepEqual(fixture.commands, []commandCall{{"xz", []string{"--decompress", "--stdout"}}}) {
				t.Fatalf("commands = %#v", fixture.commands)
			}
			assertFile(t, marker, "app = [\n")
			if _, err := os.Stat(filepath.Join(root, "opt", "notes")); !os.IsNotExist(err) {
				t.Fatalf("incoming app changed: %v", err)
			}
		})
	}
}

func TestInstallPreservesPassingIconAndSetsServicesEnvironment(t *testing.T) {
	// R-2KGK-SCJS R-UQEV-85OK
	icon := append([]byte{0xef, 0xbb, 0xbf}, []byte("<svg><!-- unchanged bytes --></svg>\n")...)
	for _, data := range [][]byte{nil, icon, []byte("<svg>" + strings.Repeat(" ", 65536-len("<svg></svg>")) + "</svg>")} {
		fixture := newCompletedInstallFixture(t, t.TempDir(), false)
		entries := []installTarEntry{regularEntry("etc/manifest.toml", []byte("app = \"notes\"\n"), 0o644), regularEntry("bin/notes", []byte("binary"), 0o755)}
		if data != nil {
			entries = append(entries, regularEntry(apps.IconPath, data, 0o644))
		}
		fixture.archive = tarEntries(t, entries)
		if err := fixture.run(); err != nil {
			t.Fatal(err)
		}
		if data != nil {
			assertFile(t, filepath.Join(fixture.root, "opt", "notes", "share", "icon.svg"), string(data))
		}
		assertFile(t, filepath.Join(fixture.root, "opt", "notes", "etc", "env"), "DRAIN_SECONDS=5\n"+apps.ServicesEnv+"="+apps.ServicesPath+"\n")
	}
}

func TestInstallChecksCompleteArtifactBeforeIcon(t *testing.T) {
	// R-2KGK-SCJS R-2LOH-64AH
	for _, entries := range [][]installTarEntry{
		{regularEntry("bin/notes", nil, 0o755)},
		{regularEntry("etc/manifest.toml", []byte("app = [\n"), 0o644), regularEntry("bin/notes", nil, 0o755)},
		{regularEntry("etc/manifest.toml", []byte("app = \"notes\"\n"), 0o644), regularEntry("bin/notes", nil, 0o644)},
		{regularEntry("etc/manifest.toml", []byte("app = \"notes\"\n"), 0o644), regularEntry("bin/notes", nil, 0o755), regularEntry("state/late", nil, 0o644)},
	} {
		entries = append([]installTarEntry{regularEntry(apps.IconPath, []byte("invalid"), 0o644)}, entries...)
		reports, _, err := runInstallArchive(t, t.TempDir(), tarEntries(t, entries), nil, nil)
		var failure *apps.InstallError
		if !errors.As(err, &failure) || failure.Code != 2 || errors.Is(failure.Cause, apps.ErrIconNotSVG) || reports[len(reports)-1].detail == apps.ErrIconNotSVG.Error() {
			t.Fatalf("failure = %v, reports = %#v", err, reports)
		}
	}
}

func TestInstallPreservesEnsureAccountFailure(t *testing.T) {
	// R-USUN-ZP5Y
	root := t.TempDir()
	cause := errors.New("account transport unavailable")
	reports, commands, err := runInstallArchive(t, root, validInstallTar(t, "app = \"notes\"\n"), nil, func(command host.Command) (host.Result, error) {
		if command.Name == "id" {
			return host.Result{}, cause
		}
		if command.Name == "systemctl" && command.Args[0] == "show" {
			return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=\n")}, nil
		}
		if command.Name == "chown" {
			return host.Result{}, nil
		}
		return host.Result{ExitCode: 3}, nil
	})
	var failure *apps.InstallError
	if !errors.As(err, &failure) || !errors.Is(failure.Cause, cause) || reports[len(reports)-1].step != "unit" || reports[len(reports)-1].success {
		t.Fatalf("failure = %v, reports = %#v", err, reports)
	}
	if commands[len(commands)-1].Name != "id" {
		t.Fatalf("commands = %#v", commands)
	}
}
