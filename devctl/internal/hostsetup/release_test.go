package hostsetup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestReleaseDiscoveryContract(t *testing.T) {
	// R-FXNQ-9MRO
	releaseType := reflect.TypeOf(Release{})
	if releaseType.NumField() != 2 || releaseType.Field(0).Name != "Version" || releaseType.Field(0).Type != reflect.TypeFor[string]() ||
		releaseType.Field(1).Name != "InstallerURL" || releaseType.Field(1).Type != reflect.TypeFor[string]() {
		t.Fatalf("Release fields = %#v", reflect.VisibleFields(releaseType))
	}

	// R-G1BF-EXZR
	if ReleasesURL != "https://api.github.com/repos/ikigenba/ikigenba/releases" ||
		InstallerPath != "/tmp/opsctl-install" ||
		SavedInstaller != "/usr/local/share/ikigenba/opsctl-install.sh" || DNSProvider != "route53" {
		t.Fatalf("release constants = %q, %q, %q, %q", ReleasesURL, InstallerPath, SavedInstaller, DNSProvider)
	}

	// R-GCAI-UVO0
	requireLatestSignature(Latest)
}

func TestLatestPaginatesAndSelectsNewestPublishedRelease(t *testing.T) {
	// R-YJ1K-MA17
	firstPage := make([]githubRelease, 100)
	for i := range firstPage {
		firstPage[i] = githubRelease{TagName: "opsctl/v1.0.0", Draft: true}
	}
	secondPage := []githubRelease{
		{TagName: "opsctl/v9.0.0", Draft: true, PublishedAt: "2026-09-17T12:00:00Z"},
		{TagName: "opsctl/v8.0.0", Prerelease: true, PublishedAt: "2026-09-17T12:00:00Z"},
		{TagName: "opsctl/not-a-version", PublishedAt: "2026-09-17T12:00:00Z"},
		{TagName: "other/v7.0.0", PublishedAt: "2026-09-17T12:00:00Z"},
		{TagName: "opsctl/v2.0.0", PublishedAt: "2026-09-16T12:00:00Z", Assets: []githubAsset{{Name: "install.sh", BrowserDownloadURL: "https://old.invalid/install"}}},
		{TagName: "opsctl/v3.0.1", PublishedAt: "2026-09-17T12:00:00Z", Assets: []githubAsset{{Name: "install.sh", BrowserDownloadURL: "https://winner.invalid/install"}}},
		{TagName: "opsctl/v3.0.2", PublishedAt: "2026-09-17T12:00:00Z", Assets: []githubAsset{{Name: "install.sh", BrowserDownloadURL: "https://tie.invalid/install"}}},
	}
	pages := [][]githubRelease{firstPage, secondPage}
	var commands []seam.Cmd
	deps := seam.Deps{Dir: "/work/tree", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		body, err := json.Marshal(pages[len(commands)-1])
		if err != nil {
			t.Fatal(err)
		}
		return seam.Result{Stdout: body}, nil
	}}

	got, err := Latest(context.Background(), deps)
	if err != nil {
		t.Fatalf("Latest(): %v", err)
	}
	want := Release{Version: "v3.0.1", InstallerURL: "https://winner.invalid/install"}
	if got != want {
		t.Fatalf("Latest() = %#v, want %#v", got, want)
	}
	if len(commands) != 2 {
		t.Fatalf("curl command count = %d, want 2", len(commands))
	}
	for i, command := range commands {
		wantArgs := []string{"-fsSL", ReleasesURL + "?per_page=100&page=" + string(rune('1'+i))}
		if command.Path != "curl" || command.Dir != deps.Dir || !reflect.DeepEqual(command.Args, wantArgs) {
			t.Fatalf("command %d = %#v, want curl %#v in %q", i, command, wantArgs, deps.Dir)
		}
	}
}

func TestLatestRejectsUnusableResponses(t *testing.T) {
	// R-YJ1K-MA17
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "no matching release", body: `[{"tag_name":"other/v1.0.0"}]`, want: "no published opsctl release"},
		{name: "missing installer", body: `[{"tag_name":"opsctl/v1.0.0","published_at":"2026-01-01T00:00:00Z","assets":[]}]`, want: "no install.sh asset"},
		{name: "malformed JSON", body: `[`, want: "decode GitHub releases"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps := seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				return seam.Result{Stdout: []byte(tc.body)}, nil
			}}
			if _, err := Latest(context.Background(), deps); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Latest() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestLatestClassifiesCommandFailures(t *testing.T) {
	// R-G9UQ-3C6M
	t.Run("command stdout remains empty", func(t *testing.T) {
		const workDir = "/release-work"
		deps := seam.Deps{Dir: workDir, Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Dir != workDir {
				t.Fatalf("command Dir = %q, want %q", command.Dir, workDir)
			}
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v1.0.0","published_at":"2026-01-01T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://release.invalid/install"}]}]`)}, nil
		}}

		var (
			release Release
			err     error
		)
		stdout := captureStdout(t, func() {
			release, err = Latest(context.Background(), deps)
		})
		if err != nil {
			t.Fatalf("Latest(): %v", err)
		}
		if release.Version != "v1.0.0" {
			t.Fatalf("Latest() release = %#v", release)
		}
		if stdout != "" {
			t.Fatalf("command stdout = %q, want empty", stdout)
		}
	})

	t.Run("nonzero exit", func(t *testing.T) {
		deps := seam.Deps{Dir: "/release-work", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Dir != "/release-work" {
				t.Fatalf("command Dir = %q", command.Dir)
			}
			return seam.Result{ExitCode: 22, Stderr: []byte("download failed\n")}, nil
		}}
		_, err := Latest(context.Background(), deps)
		var processErr *ProcessError
		if !errors.As(err, &processErr) || processErr.Label != "curl" || processErr.Status != 22 || processErr.Stderr != "download failed\n" {
			t.Fatalf("Latest() error = %#v", err)
		}
	})

	t.Run("process start", func(t *testing.T) {
		cause := errors.New("start sentinel")
		deps := seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{}, cause
		}}
		_, err := Latest(context.Background(), deps)
		if !errors.Is(err, cause) || err.Error() == cause.Error() {
			t.Fatalf("Latest() error = %#v, want wrapped sentinel", err)
		}
	})

	t.Run("decode", func(t *testing.T) {
		deps := seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{Stdout: []byte("not json")}, nil
		}}
		_, err := Latest(context.Background(), deps)
		var syntaxErr *json.SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Fatalf("Latest() error = %#v, want wrapped JSON error", err)
		}
	})
}

func requireLatestSignature(func(context.Context, seam.Deps) (Release, error)) {}

func TestProcessErrorContract(t *testing.T) {
	// R-GB2M-H3XB
	typ := reflect.TypeOf(ProcessError{})
	if typ.NumField() != 3 || typ.Field(0).Name != "Label" || typ.Field(0).Type != reflect.TypeFor[string]() ||
		typ.Field(1).Name != "Status" || typ.Field(1).Type != reflect.TypeFor[int]() ||
		typ.Field(2).Name != "Stderr" || typ.Field(2).Type != reflect.TypeFor[string]() {
		t.Fatalf("ProcessError fields = %#v", reflect.VisibleFields(typ))
	}
	err := &ProcessError{Label: "curl releases", Status: 7, Stderr: "first\nsecond\n"}
	if err.Error() != "curl releases: exit status 7" || err.Detail() != "> first\n> second" || err.ExitCode() != 1 {
		t.Fatalf("ProcessError methods = %q, %q, %d", err.Error(), err.Detail(), err.ExitCode())
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("close stdout reader: %v", err)
		}
	}()

	saved := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = saved
	}()

	run()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout capture: %v", err)
	}
	stdout, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout capture: %v", err)
	}
	return string(stdout)
}
