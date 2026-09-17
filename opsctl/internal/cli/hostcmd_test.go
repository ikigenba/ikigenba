package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantHostUsage = `Usage: opsctl host <subcommand>

Back up and restore the host's own configuration: /etc/ikigenba/ and
/etc/letsencrypt/, under 'host/' in backup.s3_uri. Nothing under /opt is
touched either way -- that is 'opsctl backup' and 'opsctl restore'.

Subcommands:
  backup    write /etc/ikigenba/ and /etc/letsencrypt/ to S3
  restore   replace them with the newest backup

'opsctl init' writes the timer that runs the backup at
backup.host_files_seconds.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func TestHostHelpIsExactAndHostIndependent(t *testing.T) {
	// R-YOW2-WOED
	for _, euid := range []int{0, 1000} {
		for _, option := range []string{"--help", "-h"} {
			deps, assertInert := inertHostCommandDeps(t, euid)
			stdout, stderr, code := invokeBackupCLI([]string{"host", option}, deps)
			assertInert()
			if code != 0 || stdout != wantHostUsage || stderr != "" {
				t.Errorf("host %s as euid %d = exit %d stdout %q stderr %q", option, euid, code, stdout, stderr)
			}
		}
	}
}

func TestHostDispatchErrorsAreExactAndHostIndependent(t *testing.T) {
	// R-YQ3Z-AG52
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"host"}, want: "opsctl: no host subcommand given\n\nsee 'opsctl host --help' for usage\n"},
		{args: []string{"host", "other"}, want: "opsctl: unknown host subcommand 'other'\n\nsee 'opsctl host --help' for usage\n"},
		{args: []string{"host", "bad\nname"}, want: "opsctl: unknown host subcommand 'bad\\nname'\n\nsee 'opsctl host --help' for usage\n"},
	}
	for _, test := range tests {
		deps, assertInert := inertHostCommandDeps(t, 1000)
		stdout, stderr, code := invokeBackupCLI(test.args, deps)
		assertInert()
		if code != 2 || stdout != "" || stderr != test.want {
			t.Errorf("%q = exit %d stdout %q stderr %q, want exit 2, empty stdout, stderr %q", test.args, code, stdout, stderr, test.want)
		}
	}
}

func TestHostBackupCommandReportsResultAndOperationalErrors(t *testing.T) {
	// R-I4KV-PSGR
	t.Run("success", func(t *testing.T) {
		root := configuredBackupRoot(t)
		client := newHostCLICloud()
		deps := hostCLIDeps(root, client, fixedSizeZstdExecute(1536))

		stdout, stderr, code := invokeBackupCLI([]string{"host", "backup"}, deps)
		if code != 0 || stdout != "host: ok (2026-09-16T12:34:56Z.tar.zst, 1.5 KiB)\n" || stderr != "" {
			t.Fatalf("host backup = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("archive result failure", func(t *testing.T) {
		root := configuredBackupRoot(t)
		client := newHostCLICloud()
		client.putErr = errors.New("upload unavailable")
		deps := hostCLIDeps(root, client, fixedSizeZstdExecute(1536))

		stdout, stderr, code := invokeBackupCLI([]string{"host", "backup"}, deps)
		want := "host: failed: upload \"s3://backups.example/host/host/2026-09-16T12:34:56Z.tar.zst\": upload unavailable\n"
		if code != 1 || stdout != want || stderr != "" {
			t.Fatalf("host backup failure = exit %d stdout %q stderr %q, want stdout %q", code, stdout, stderr, want)
		}
	})

	t.Run("preworkflow operational failure", func(t *testing.T) {
		stdout, stderr, code := invokeBackupCLI([]string{"host", "backup"}, Deps{Root: t.TempDir(), EUID: 0})
		if code != 1 || stdout != "" || stderr != "opsctl: backup.s3_uri not set\n" {
			t.Fatalf("host backup preworkflow = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("successful result with operational failure", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := renderHostBackupOutcome(&stdout, &stderr, backup.FileResult{
			Service: "host",
			Object:  "2026-09-16T12:34:56Z.tar.zst",
			Size:    1536,
		}, errors.New("finalize backup run"))
		if code != exitFail || stdout.String() != "host: ok (2026-09-16T12:34:56Z.tar.zst, 1.5 KiB)\n" || stderr.String() != "opsctl: host backup failed\n" {
			t.Fatalf("mixed host backup outcome = exit %d stdout %q stderr %q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("attempted archive interruption", func(t *testing.T) {
		root := configuredBackupRoot(t)
		client := newHostCLICloud()
		execute := func(context.Context, host.Command) (host.Result, error) {
			return host.Result{Stdout: []byte("partial output\n"), Stderr: []byte("compression stopped")}, context.Canceled
		}
		stdout, stderr, code := invokeBackupCLI([]string{"host", "backup"}, hostCLIDeps(root, client, execute))
		wantOut := "host: failed: compress \"host\" archive: context canceled\n"
		wantErr := "opsctl: host backup failed\n\n> partial output\n> compression stopped\n"
		if code != 1 || stdout != wantOut || stderr != wantErr {
			t.Fatalf("interrupted host backup = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
		if strings.Contains(stderr, "context canceled") {
			t.Fatalf("diagnostic repeated report reason: %q", stderr)
		}
	})
}

func TestHostRestoreCommandReportsCompletedAndFailedPhases(t *testing.T) {
	// R-HX9H-F60L
	t.Run("success", func(t *testing.T) {
		root := configuredBackupRoot(t)
		client := newHostCLICloud()
		basename := "2026-09-16T12:34:56Z.tar.zst"
		uri := "s3://backups.example/host/host/" + basename
		archive := makeHostCLITar(t, map[string]string{
			"etc/ikigenba/config.json":      "{}\n",
			"etc/letsencrypt/live/cert.pem": "certificate\n",
		})
		client.objects[uri] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, archive...)

		stdout, stderr, code := invokeBackupCLI([]string{"host", "restore"}, hostCLIDeps(root, client, roundTripZstdExecute(nil)))
		want := fmt.Sprintf("source: ok (host/%s, %.1f KiB)\nfiles: ok (/etc/ikigenba, /etc/letsencrypt, 2 files)\n",
			basename, float64(len(client.objects[uri]))/1024)
		if code != 0 || stdout != want || stderr != "" {
			t.Fatalf("host restore = exit %d stdout %q stderr %q, want stdout %q", code, stdout, stderr, want)
		}
	})

	t.Run("failed source phase retains command detail only in diagnostic", func(t *testing.T) {
		root := configuredBackupRoot(t)
		client := newHostCLICloud()
		uri := "s3://backups.example/host/host/2026-09-16T12:34:56Z.tar.zst"
		client.objects[uri] = []byte{0x28, 0xb5, 0x2f, 0xfd, 0}
		execute := func(context.Context, host.Command) (host.Result, error) {
			return host.Result{Stdout: []byte("partial archive\n"), Stderr: []byte("decoder stopped")}, context.Canceled
		}

		stdout, stderr, code := invokeBackupCLI([]string{"host", "restore"}, hostCLIDeps(root, client, execute))
		wantOut := "source: failed: decompress host archive: context canceled\n"
		wantErr := "opsctl: host restore failed at source\n\n> partial archive\n> decoder stopped\n"
		if code != 1 || stdout != wantOut || stderr != wantErr {
			t.Fatalf("failed host restore = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
		if strings.Contains(stdout, "partial archive") || strings.Contains(stdout, "decoder stopped") {
			t.Fatalf("captured subprocess output leaked to stdout: %q", stdout)
		}
	})

	t.Run("preworkflow failure retains specific reason", func(t *testing.T) {
		stdout, stderr, code := invokeBackupCLI([]string{"host", "restore"}, Deps{Root: t.TempDir(), EUID: 0})
		if code != 1 || stdout != "" || stderr != "opsctl: backup.s3_uri not set\n" {
			t.Fatalf("host restore preworkflow = exit %d stdout %q stderr %q", code, stdout, stderr)
		}
	})

	t.Run("files failure retains completed source", func(t *testing.T) {
		root := configuredBackupRoot(t)
		client := newHostCLICloud()
		basename := "2026-09-16T12:34:56Z.tar.zst"
		uri := "s3://backups.example/host/host/" + basename
		archive := makeHostCLITar(t, map[string]string{"etc/ikigenba/config.json": "{}\n"})
		client.objects[uri] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, archive...)
		client.getHook = func() {
			if err := os.RemoveAll(filepath.Join(root, "etc")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "etc"), []byte("blocked"), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		stdout, stderr, code := invokeBackupCLI([]string{"host", "restore"}, hostCLIDeps(root, client, roundTripZstdExecute(nil)))
		wantOut := fmt.Sprintf("source: ok (host/%s, %.1f KiB)\nfiles: failed: replace host files: destination parent /etc is not a directory\n",
			basename, float64(len(client.objects[uri]))/1024)
		if code != 1 || stdout != wantOut || stderr != "opsctl: host restore failed at files\n" {
			t.Fatalf("files failure = exit %d stdout %q stderr %q, want stdout %q", code, stdout, stderr, wantOut)
		}
	})
}

func TestHostBackupAndRestorePreserveCertificateWithoutExternalActions(t *testing.T) {
	root := configuredBackupRoot(t)
	certificate := filepath.Join(root, "etc", "letsencrypt", "live", "site", "fullchain.pem")
	if err := os.MkdirAll(filepath.Dir(certificate), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certificate, []byte("original certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	client := newHostCLICloud()
	var commands []string
	deps := hostCLIDeps(root, client, roundTripZstdExecute(&commands))
	if stdout, stderr, code := invokeBackupCLI([]string{"host", "backup"}, deps); code != 0 || stderr != "" || !strings.HasPrefix(stdout, "host: ok (") {
		t.Fatalf("host backup = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if err := os.WriteFile(certificate, []byte("replacement certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, code := invokeBackupCLI([]string{"host", "restore"}, deps); code != 0 || stderr != "" || !strings.Contains(stdout, "files: ok (") {
		t.Fatalf("host restore = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	data, err := filesystem.ReadFile("etc/letsencrypt/live/site/fullchain.pem")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original certificate\n" {
		t.Fatalf("restored certificate = %q", data)
	}
	if want := []string{"zstd", "zstd"}; fmt.Sprint(commands) != fmt.Sprint(want) {
		t.Fatalf("host backup/restore commands = %v, want only %v; certificate issuance or external project action must not run", commands, want)
	}
}

func TestHostActionsRejectInvalidGrammarBeforeHostAccess(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"host", "backup", "--force"}, want: "opsctl: unknown option '--force'\n\nsee 'opsctl host --help' for usage\n"},
		{args: []string{"host", "backup", "extra"}, want: "opsctl: host backup takes no arguments\n\nsee 'opsctl host --help' for usage\n"},
		{args: []string{"host", "restore", "-h"}, want: "opsctl: unknown option '-h'\n\nsee 'opsctl host --help' for usage\n"},
		{args: []string{"host", "restore", "extra"}, want: "opsctl: host restore takes no arguments\n\nsee 'opsctl host --help' for usage\n"},
	}
	for _, test := range tests {
		deps, assertInert := inertHostCommandDeps(t, 1000)
		stdout, stderr, code := invokeBackupCLI(test.args, deps)
		assertInert()
		if code != 2 || stdout != "" || stderr != test.want {
			t.Errorf("%q = exit %d stdout %q stderr %q, want exit 2, empty stdout, stderr %q", test.args, code, stdout, stderr, test.want)
		}
	}
}

func TestHostActionsRefuseNonRootBeforeHostAccess(t *testing.T) {
	for _, action := range []string{"backup", "restore"} {
		deps, assertInert := inertHostCommandDeps(t, 1000)
		stdout, stderr, code := invokeBackupCLI([]string{"host", action}, deps)
		assertInert()
		if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
			t.Errorf("host %s = exit %d stdout %q stderr %q", action, code, stdout, stderr)
		}
	}
}

func inertHostCommandDeps(t *testing.T, euid int) (Deps, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "host-state")
	if err := os.WriteFile(root, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	used := false
	deps := Deps{
		Root: root,
		EUID: euid,
		Getenv: func(string) string {
			used = true
			return ""
		},
		Execute: func(context.Context, host.Command) (host.Result, error) {
			used = true
			return host.Result{}, errors.New("unexpected execution")
		},
		Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			used = true
			return nil, errors.New("unexpected cloud access")
		}},
	}
	return deps, func() {
		t.Helper()
		if used {
			t.Error("host command accessed an injected host boundary")
		}
		info, err := os.Lstat(root)
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("unchanged")) {
			t.Errorf("host command changed host state: info %v error %v", info, err)
		}
	}
}

func hostCLIDeps(root string, client *hostCLICloud, execute func(context.Context, host.Command) (host.Result, error)) Deps {
	return Deps{
		Root: root, EUID: 0, Execute: execute,
		Now: func() time.Time { return time.Date(2026, 9, 16, 12, 34, 56, 0, time.UTC) },
		Cloud: cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
			if region != "us-east-2" {
				return nil, errors.New("wrong region: " + region)
			}
			return client, nil
		}},
	}
}

func fixedSizeZstdExecute(size int) func(context.Context, host.Command) (host.Result, error) {
	return func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name != "zstd" || strings.Contains(strings.Join(command.Args, " "), "--decompress") {
			return host.Result{}, errors.New("unexpected command: " + command.Name)
		}
		compressed := make([]byte, size)
		copy(compressed, []byte{0x28, 0xb5, 0x2f, 0xfd})
		return host.Result{Stdout: compressed}, nil
	}
}

func roundTripZstdExecute(commands *[]string) func(context.Context, host.Command) (host.Result, error) {
	return func(_ context.Context, command host.Command) (host.Result, error) {
		if commands != nil {
			*commands = append(*commands, command.Name)
		}
		if command.Name != "zstd" {
			return host.Result{}, errors.New("unexpected command: " + command.Name)
		}
		input, err := io.ReadAll(command.Stdin)
		if err != nil {
			return host.Result{}, err
		}
		if strings.Contains(strings.Join(command.Args, " "), "--decompress") {
			if len(input) < 4 || !bytes.Equal(input[:4], []byte{0x28, 0xb5, 0x2f, 0xfd}) {
				return host.Result{ExitCode: 1, Stderr: []byte("invalid fixture")}, nil
			}
			return host.Result{Stdout: input[4:]}, nil
		}
		return host.Result{Stdout: append([]byte{0x28, 0xb5, 0x2f, 0xfd}, input...)}, nil
	}
}

func makeHostCLITar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

type hostCLICloud struct {
	objects map[string][]byte
	putErr  error
	listErr error
	getErr  error
	getHook func()
}

func newHostCLICloud() *hostCLICloud {
	return &hostCLICloud{objects: make(map[string][]byte)}
}

func (client *hostCLICloud) GetObject(_ context.Context, uri string) (io.ReadCloser, error) {
	if client.getErr != nil {
		return nil, client.getErr
	}
	if client.getHook != nil {
		client.getHook()
	}
	data, ok := client.objects[uri]
	if !ok {
		return nil, cloud.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (client *hostCLICloud) PutObject(_ context.Context, uri string, body io.Reader) error {
	if client.putErr != nil {
		return client.putErr
	}
	if _, exists := client.objects[uri]; exists {
		return cloud.ErrAlreadyExists
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	client.objects[uri] = data
	return nil
}

func (client *hostCLICloud) ListObjects(_ context.Context, prefix string) ([]cloud.Object, error) {
	if client.listErr != nil {
		return nil, client.listErr
	}
	var uris []string
	for uri := range client.objects {
		if strings.HasPrefix(uri, prefix) {
			uris = append(uris, uri)
		}
	}
	sort.Strings(uris)
	objects := make([]cloud.Object, 0, len(uris))
	for _, uri := range uris {
		objects = append(objects, cloud.Object{URI: uri, Size: int64(len(client.objects[uri]))})
	}
	return objects, nil
}

func (*hostCLICloud) ReadSecrets(context.Context, string) (map[string]string, error) {
	panic("unexpected ReadSecrets")
}
