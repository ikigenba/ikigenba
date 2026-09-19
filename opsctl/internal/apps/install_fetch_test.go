package apps_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallValidatesInputsAndUsesConfiguredCloud(t *testing.T) {
	// R-WWZY-50BX R-DK7F-CAV0
	validHooks := apps.InstallHooks{
		Report:    func(string, string, bool) error { return nil },
		Configure: func(context.Context, apps.Manifest) error { return nil },
	}
	configured := installStore(t, map[string]string{"host.name": "HOST.EXAMPLE.", "aws.region": "us-east-2"})

	for _, test := range []struct {
		name  string
		uri   string
		hooks apps.InstallHooks
		store config.Store
		want  string
	}{
		{"invalid URI", "https://bucket/app.tar.xz", validHooks, configured, "install takes an s3:// URI"},
		{"missing bucket", "s3:///app.tar.xz", validHooks, configured, "install takes an s3:// URI"},
		{"missing object", "s3://bucket/", validHooks, configured, "install takes an s3:// URI"},
		{"query", "s3://bucket/app.tar.xz?version=1", validHooks, configured, "install takes an s3:// URI"},
		{"fragment", "s3://bucket/app.tar.xz#part", validHooks, configured, "install takes an s3:// URI"},
		{"nil report", "s3://bucket/app.tar.xz", apps.InstallHooks{Configure: validHooks.Configure}, configured, "install report hook not set"},
		{"nil configure", "s3://bucket/app.tar.xz", apps.InstallHooks{Report: validHooks.Report}, configured, "install configure hook not set"},
		{"missing host", "s3://bucket/app.tar.xz", validHooks, installStore(t, map[string]string{"aws.region": "us-east-2"}), "host.name not set"},
		{"empty host", "s3://bucket/app.tar.xz", validHooks, installStore(t, map[string]string{"host.name": "", "aws.region": "us-east-2"}), "host.name not set"},
		{"normalized empty host", "s3://bucket/app.tar.xz", validHooks, installStore(t, map[string]string{"host.name": ".", "aws.region": "us-east-2"}), "host.name not set"},
		{"missing region", "s3://bucket/app.tar.xz", validHooks, installStore(t, map[string]string{"host.name": "host.example"}), "aws.region not set"},
		{"empty region", "s3://bucket/app.tar.xz", validHooks, installStore(t, map[string]string{"host.name": "host.example", "aws.region": ""}), "aws.region not set"},
	} {
		t.Run(test.name, func(t *testing.T) {
			opened := false
			reported := false
			hooks := test.hooks
			if hooks.Report != nil {
				hooks.Report = func(string, string, bool) error { reported = true; return nil }
			}
			err := apps.Install(t.Context(), host.Env{}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
				opened = true
				return nil, errors.New("must not open")
			}}, test.store, test.uri, hooks)
			if err == nil || err.Error() != test.want {
				t.Fatalf("Install error = %v, want %q", err, test.want)
			}
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Cause == nil || failure.Cause.Error() == failure.Message {
				t.Fatalf("Install error = %#v, want distinct retained cause", err)
			}
			if opened || reported {
				t.Fatalf("preflight failure opened cloud = %v, reported = %v", opened, reported)
			}
		})
	}

	var missingOpenReports []installReport
	err := apps.Install(t.Context(), host.Env{}, cloud.Env{}, configured, "s3://bucket/app.tar.xz", apps.InstallHooks{
		Report: func(step, detail string, success bool) error {
			missingOpenReports = append(missingOpenReports, installReport{step, detail, success})
			return nil
		},
		Configure: func(context.Context, apps.Manifest) error { t.Fatal("Configure called"); return nil },
	})
	var missingOpenFailure *apps.InstallError
	if !errors.As(err, &missingOpenFailure) || missingOpenFailure.Code != 1 ||
		!strings.Contains(missingOpenFailure.Cause.Error(), "cloud open not configured") ||
		!reflect.DeepEqual(missingOpenReports, []installReport{{"fetch", "cloud open not configured", false}}) {
		t.Fatalf("missing Open = %#v, reports %#v", err, missingOpenReports)
	}

	var events []string
	client := &installCloudClient{get: func(ctx context.Context, uri string) (io.ReadCloser, error) {
		events = append(events, "get")
		if ctx != t.Context() || uri != "s3://artifacts/releases/notes.tar.xz" {
			t.Fatalf("GetObject(%v, %q)", ctx, uri)
		}
		return &trackedReadCloser{Reader: strings.NewReader("artifact"), onClose: func() { events = append(events, "close") }}, nil
	}}
	err = apps.Install(t.Context(), host.Env{Execute: func(context.Context, host.Command) (host.Result, error) {
		events = append(events, "validate")
		return host.Result{Stdout: emptyTar(t)}, nil
	}}, cloud.Env{Open: func(ctx context.Context, region string) (cloud.Client, error) {
		events = append(events, "open")
		if ctx != t.Context() || region != "us-east-2" {
			t.Fatalf("Open(%v, %q)", ctx, region)
		}
		return client, nil
	}}, configured, "s3://artifacts/releases/notes.tar.xz", apps.InstallHooks{
		Report: func(step, _ string, _ bool) error {
			events = append(events, "report:"+step)
			return nil
		},
		Configure: func(context.Context, apps.Manifest) error { t.Fatal("Configure called"); return nil },
	})
	var failure *apps.InstallError
	if !errors.As(err, &failure) || failure.Code != 2 {
		t.Fatalf("Install error = %#v, want file validation Code 2", err)
	}
	wantEvents := []string{"open", "get", "close", "report:fetch", "validate", "report:file"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
}

func TestInstallFetchLifecycleAndOutcomes(t *testing.T) {
	// R-EKWN-JXML, R-EOKC-P8UO
	openFailure := errors.New("credentials unavailable")
	getFailure := errors.New("download denied")
	readFailure := errors.New("connection reset during read")
	closeFailure := errors.New("response close failed")
	incompleteReader := &trackedReadCloser{Reader: errorReader{data: []byte("partial"), err: io.ErrUnexpectedEOF}}
	readAndCloseReader := &trackedReadCloser{Reader: errorReader{err: readFailure}, closeErr: closeFailure}
	closeErrorReader := &trackedReadCloser{Reader: strings.NewReader("complete"), closeErr: closeFailure}
	getErrorReader := &trackedReadCloser{Reader: strings.NewReader("unused")}

	tests := []struct {
		name       string
		open       func(context.Context, string) (cloud.Client, error)
		wantCode   int
		wantDetail string
		wantCauses []error
		reader     *trackedReadCloser
	}{
		{name: "open", open: func(context.Context, string) (cloud.Client, error) { return nil, openFailure }, wantCode: 1, wantDetail: openFailure.Error(), wantCauses: []error{openFailure}},
		{name: "get", open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) { return nil, getFailure }), wantCode: 1, wantDetail: "bundle.tar.xz: " + getFailure.Error(), wantCauses: []error{getFailure}},
		{name: "missing", open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) { return nil, cloud.ErrNotFound }), wantCode: 2, wantDetail: "bundle.tar.xz: no such object", wantCauses: []error{cloud.ErrNotFound}},
		{name: "incomplete read", open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) {
			return incompleteReader, nil
		}), wantCode: 1, wantDetail: "bundle.tar.xz: unexpected EOF", wantCauses: []error{io.ErrUnexpectedEOF}, reader: incompleteReader},
		{name: "read and close", open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) {
			return readAndCloseReader, nil
		}), wantCode: 1, wantDetail: "bundle.tar.xz: connection reset during read\\nresponse close failed", wantCauses: []error{readFailure, closeFailure}, reader: readAndCloseReader},
		{name: "close", open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) {
			return closeErrorReader, nil
		}), wantCode: 1, wantDetail: "bundle.tar.xz: " + closeFailure.Error(), wantCauses: []error{closeFailure}, reader: closeErrorReader},
		{name: "get reader and error", open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) {
			return getErrorReader, getFailure
		}), wantCode: 1, wantDetail: "bundle.tar.xz: " + getFailure.Error(), wantCauses: []error{getFailure}, reader: getErrorReader},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := installStore(t, map[string]string{"host.name": "host.example", "aws.region": "eu-west-1"})
			before := treeSnapshot(t, store.Root)
			var reports []installReport
			validated := false
			err := apps.Install(t.Context(), host.Env{Execute: func(context.Context, host.Command) (host.Result, error) {
				validated = true
				return host.Result{}, nil
			}}, cloud.Env{Open: test.open}, store, "s3://bucket/path/bundle.tar.xz", apps.InstallHooks{
				Report: func(step, detail string, success bool) error {
					reports = append(reports, installReport{step, detail, success})
					return nil
				},
				Configure: func(context.Context, apps.Manifest) error { t.Fatal("Configure called"); return nil },
			})
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != test.wantCode || failure.Message != "artifact download failed" {
				t.Fatalf("Install error = %#v, want download Code %d", err, test.wantCode)
			}
			for _, cause := range test.wantCauses {
				if !errors.Is(failure.Cause, cause) {
					t.Errorf("Cause = %v, want errors.Is(_, %v)", failure.Cause, cause)
				}
			}
			if len(reports) != 1 || reports[0] != (installReport{"fetch", test.wantDetail, false}) {
				t.Fatalf("reports = %#v", reports)
			}
			if validated {
				t.Fatal("failed fetch reached artifact validation")
			}
			if test.reader != nil && !test.reader.closed {
				t.Fatal("object reader was not closed")
			}
			if after := treeSnapshot(t, store.Root); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed fetch changed host state:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

func TestInstallFetchSuccessReadsCompleteObjectBeforeReporting(t *testing.T) {
	// R-EKWN-JXML R-DK7F-CAV0
	data := bytes.Repeat([]byte{'x'}, 1572864)
	reader := &trackedReadCloser{Reader: bytes.NewReader(data)}
	store := installStore(t, map[string]string{"host.name": "host.example", "aws.region": "ap-south-1"})
	var events []string
	err := apps.Install(t.Context(), host.Env{Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		events = append(events, "validate")
		got, readErr := io.ReadAll(command.Stdin)
		if readErr != nil || !bytes.Equal(got, data) {
			t.Fatalf("validator input = %d bytes, %v", len(got), readErr)
		}
		return host.Result{Stdout: emptyTar(t)}, nil
	}}, cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
		events = append(events, "open:"+region)
		return &installCloudClient{get: func(context.Context, string) (io.ReadCloser, error) {
			events = append(events, "get")
			reader.onClose = func() { events = append(events, "close") }
			return reader, nil
		}}, nil
	}}, store, "s3://bucket/releases/bundle.tar.xz", apps.InstallHooks{
		Report: func(step, detail string, success bool) error {
			events = append(events, step+":"+detail)
			if step == "fetch" && !success {
				t.Fatal("successful fetch reported failure")
			}
			return nil
		},
		Configure: func(context.Context, apps.Manifest) error { t.Fatal("Configure called"); return nil },
	})
	if err == nil {
		t.Fatal("missing manifest unexpectedly succeeded")
	}
	wantPrefix := []string{"open:ap-south-1", "get", "close", "fetch:bundle.tar.xz, 1.5 MiB", "validate"}
	if !reflect.DeepEqual(events[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("events = %v, want prefix %v", events, wantPrefix)
	}
	if !reader.closed {
		t.Fatal("object reader was not closed")
	}
}

func TestInstallFetchReportFailuresPreserveCauses(t *testing.T) {
	// R-EKWN-JXML, R-EM4J-XPDA
	downloadErr := errors.New("download failed")
	reportErr := errors.New("report failed")
	store := installStore(t, map[string]string{"host.name": "host.example", "aws.region": "us-west-2"})
	reportCalls := 0
	err := apps.Install(t.Context(), host.Env{}, cloud.Env{Open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) {
		return nil, downloadErr
	})}, store, "s3://bucket/bundle.tar.xz", apps.InstallHooks{
		Report:    func(string, string, bool) error { reportCalls++; return reportErr },
		Configure: func(context.Context, apps.Manifest) error { return nil },
	})
	var failure *apps.InstallError
	if !errors.As(err, &failure) || failure.Code != 1 || failure.Message != "artifact download failed" ||
		!errors.Is(failure.Cause, downloadErr) || !errors.Is(failure.Cause, reportErr) || reportCalls != 1 {
		t.Fatalf("failure = %#v cause = %v reports = %d", failure, failure.Cause, reportCalls)
	}

	reportCalls = 0
	err = apps.Install(t.Context(), host.Env{Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("validation reached after fetch report failure")
		return host.Result{}, nil
	}}, cloud.Env{Open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) {
		return &trackedReadCloser{Reader: strings.NewReader("artifact")}, nil
	})}, store, "s3://bucket/bundle.tar.xz", apps.InstallHooks{
		Report:    func(string, string, bool) error { reportCalls++; return reportErr },
		Configure: func(context.Context, apps.Manifest) error { return nil },
	})
	if !errors.As(err, &failure) || failure.Code != 1 || failure.Message != "install failed" ||
		!errors.Is(failure.Cause, reportErr) || reportCalls != 1 {
		t.Fatalf("success-report failure = %#v reports = %d", err, reportCalls)
	}
}

func TestInstallReportsSpecifiedManifestFailures(t *testing.T) {
	// R-ENCG-BH3Z
	malformed := []byte("app = \"notes\"\nport = [\n")
	tests := []struct {
		name      string
		archive   []byte
		want      string
		wantCause string
	}{
		{"missing", emptyTar(t), "bundle.tar.xz: no etc/manifest.toml in the file", "no etc/manifest.toml in the file"},
		{"malformed", tarWithManifest(t, malformed), "bundle.tar.xz: etc/manifest.toml: invalid manifest: line 3: missing value", "invalid manifest: line 3: missing value"},
		{"unusable name", tarWithManifest(t, []byte("app = \"bad/name\"\nport = 3000\n")), "'bad/name' is not a usable app name", "invalid manifest: unusable app name \"bad/name\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := installStoreAt(t, root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"})
			before := treeSnapshot(t, root)
			var reports []installReport
			client := &installCloudClient{get: func(context.Context, string) (io.ReadCloser, error) {
				return &trackedReadCloser{Reader: strings.NewReader("compressed artifact")}, nil
			}}
			err := apps.Install(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				if command.Name != "xz" || !reflect.DeepEqual(command.Args, []string{"--decompress", "--stdout"}) {
					t.Fatalf("decompress command = %#v", command)
				}
				return host.Result{Stdout: test.archive}, nil
			}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }}, store,
				"s3://bucket/releases/bundle.tar.xz", apps.InstallHooks{
					Report: func(step, detail string, success bool) error {
						reports = append(reports, installReport{step, detail, success})
						return nil
					},
					Configure: func(context.Context, apps.Manifest) error { t.Fatal("Configure called"); return nil },
				})
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != 2 || failure.Message != "install failed" ||
				failure.Cause.Error() != test.wantCause {
				t.Fatalf("Install error = %#v, cause %v", err, failure.Cause)
			}
			wantReports := []installReport{{"fetch", "bundle.tar.xz, 0.0 MiB", true}, {"file", test.want, false}}
			if !reflect.DeepEqual(reports, wantReports) {
				t.Fatalf("reports = %#v, want %#v", reports, wantReports)
			}
			if after := treeSnapshot(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("host state changed:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

func TestInstallContinuesFileStageAfterManifestInspection(t *testing.T) {
	root := t.TempDir()
	store := installStoreAt(t, root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"})
	before := treeSnapshot(t, root)
	archive := tarWithManifest(t, []byte("app = \"notes\"\nport = 3000\n"))
	var reports []installReport
	configured := false

	err := apps.Install(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name != "xz" || !reflect.DeepEqual(command.Args, []string{"--decompress", "--stdout"}) {
			t.Fatalf("unexpected later command: %#v", command)
		}
		return host.Result{Stdout: archive}, nil
	}}, cloud.Env{Open: cloudWithGet(func(context.Context, string) (io.ReadCloser, error) {
		return &trackedReadCloser{Reader: strings.NewReader("compressed artifact")}, nil
	})}, store, "s3://bucket/releases/bundle.tar.xz", apps.InstallHooks{
		Report: func(step, detail string, success bool) error {
			reports = append(reports, installReport{step, detail, success})
			return nil
		},
		Configure: func(context.Context, apps.Manifest) error {
			configured = true
			return nil
		},
	})

	var failure *apps.InstallError
	if !errors.As(err, &failure) || failure.Code != 2 || failure.Message != "install failed" {
		t.Fatalf("Install error = %#v, want file-stage Code 2", err)
	}
	wantReports := []installReport{
		{"fetch", "bundle.tar.xz, 0.0 MiB", true},
		{"file", "bundle.tar.xz: no executable bin/notes in the file", false},
	}
	if !reflect.DeepEqual(reports, wantReports) {
		t.Fatalf("reports = %#v, want %#v", reports, wantReports)
	}
	if configured {
		t.Fatal("configuration ran before the file stage completed")
	}
	if after := treeSnapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("host state changed:\nbefore %v\nafter  %v", before, after)
	}
}

type installReport struct {
	step    string
	detail  string
	success bool
}

type installCloudClient struct {
	get         func(context.Context, string) (io.ReadCloser, error)
	readSecrets func(context.Context, string) (map[string]string, error)
}

func (client *installCloudClient) GetObject(ctx context.Context, uri string) (io.ReadCloser, error) {
	return client.get(ctx, uri)
}

func (*installCloudClient) PutObject(context.Context, string, io.Reader) error {
	panic("unexpected PutObject")
}

func (*installCloudClient) ListObjects(context.Context, string) ([]cloud.Object, error) {
	panic("unexpected ListObjects")
}

func (client *installCloudClient) ReadSecrets(ctx context.Context, parameter string) (map[string]string, error) {
	if client.readSecrets == nil {
		panic("unexpected ReadSecrets")
	}
	return client.readSecrets(ctx, parameter)
}

type trackedReadCloser struct {
	io.Reader
	closeErr error
	closed   bool
	onClose  func()
}

func (reader *trackedReadCloser) Close() error {
	reader.closed = true
	if reader.onClose != nil {
		reader.onClose()
	}
	return reader.closeErr
}

type errorReader struct {
	data []byte
	err  error
}

func (reader errorReader) Read(buffer []byte) (int, error) {
	return copy(buffer, reader.data), reader.err
}

func cloudWithGet(get func(context.Context, string) (io.ReadCloser, error)) func(context.Context, string) (cloud.Client, error) {
	return func(context.Context, string) (cloud.Client, error) { return &installCloudClient{get: get}, nil }
}

func installStore(t *testing.T, values map[string]string) config.Store {
	t.Helper()
	return installStoreAt(t, t.TempDir(), values)
}

func installStoreAt(t *testing.T, root string, values map[string]string) config.Store {
	t.Helper()
	store := config.Store{Root: root}
	for key, value := range values {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func emptyTar(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func tarWithManifest(t *testing.T, manifest []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	if err := writer.WriteHeader(&tar.Header{Name: "etc/manifest.toml", Mode: 0o644, Size: int64(len(manifest))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			snapshot[relative] = "dir"
			return nil
		}
		data, err := fs.ReadFile(os.DirFS(root), relative)
		if err != nil {
			return err
		}
		snapshot[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
