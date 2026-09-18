package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/deploy"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func TestDeployOperationsUseSelectedAccountAndPreserveVersion(t *testing.T) {
	// R-ZGQ0-SX28 R-ZHXX-6OSX R-FAHM-ZZOH R-FGL4-WUDY R-FHT1-AM4N R-FJ0X-ODVC
	h := newDeployHarness()
	dir, filename, artifact := h.artifact(t, "crm-v1.2.3-rc.1+build.7.tar.xz")
	operand := filepath.Join(dir, filename)
	h.manifest = "app = \"crm\"\nsecrets = [\"CRM_B\", \"CRM_A\", \"CRM_A\"]\n"
	h.secretObject = `{"EXTRA":"x","CRM_A":"a","CRM_B":"b"}`
	var stdout bytes.Buffer
	if err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "MixedCase"); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	wantOutput := "file: ok (crm v1.2.3-rc.1+build.7)\n" +
		"secrets: ok (2 keys)\n" +
		"upload: ok (-> backups/" + h.domain + "/deploy/" + filename + ")\n" +
		"install: ok (opsctl installed crm)\n"
	if stdout.String() != wantOutput {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOutput)
	}
	if got, want := h.cloudCalls, []cloudOpen{{"MixedCase", ""}, {"MixedCase", "us-test-1"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cloud calls = %#v, want %#v", got, want)
	}
	if h.spaceCalls != 1 || h.secretCalls != 1 {
		t.Fatalf("space/secret calls = %d/%d, want 1/1", h.spaceCalls, h.secretCalls)
	}
	if got, want := h.secretParameters, []string{secrets.Parameter(h.domain, "crm")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("secret parameters = %q, want %q", got, want)
	}
	wantKey := h.domain + "/deploy/" + filename
	if h.putCalls != 1 || h.putBucket != "backups" || h.putKey != wantKey || h.putSize != int64(len(artifact)) || !bytes.Equal(h.putBody, artifact) {
		t.Fatalf("put = calls %d bucket %q key %q size %d body %q", h.putCalls, h.putBucket, h.putKey, h.putSize, h.putBody)
	}
	wantCommands := []seam.Cmd{
		{Path: "tar", Args: []string{"-t", "-J", "-f", operand}, Dir: dir},
		{Path: "tar", Args: []string{"-x", "-J", "-O", "-f", operand, "etc/manifest.toml"}, Dir: dir},
		{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@192.0.2.8", "'sudo' 'opsctl' 'install' 's3://backups/" + wantKey + "'"}, Dir: dir},
	}
	if !reflect.DeepEqual(h.commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", h.commands, wantCommands)
	}
	if got, want := h.operations, []string{"tar-list", "tar-manifest", "open-bootstrap", "open-regional", "space", "secrets", "upload", "install"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %#v, want %#v", got, want)
	}
}

func TestDeployMissingSecretsAreSortedAndStopWork(t *testing.T) {
	// R-ZHXX-6OSX R-FK8U-25M1 R-FAHM-ZZOH
	h := newDeployHarness()
	dir, operand, _ := h.artifact(t, "crm-v1.2.3.tar.xz")
	h.manifest = "app = \"crm\"\nsecrets = [\"Z_LAST\", \"PRESENT\", \"A_FIRST\", \"Z_LAST\"]\n"
	h.secretObject = `{"PRESENT":"yes","IGNORED":"yes"}`
	var stdout bytes.Buffer
	err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "ProfileOne")
	var missing *deploy.MissingSecretsError
	if !errors.As(err, &missing) || missing.App != "crm" || missing.Domain != h.domain || missing.Profile != "ProfileOne" ||
		!reflect.DeepEqual(missing.Names, []string{"A_FIRST", "Z_LAST"}) {
		t.Fatalf("error = %T %#v", err, err)
	}
	if stdout.String() != "file: ok (crm v1.2.3)\n" || h.secretCalls != 1 || h.putCalls != 0 || h.installCalls != 0 {
		t.Fatalf("stdout/calls = %q secrets %d put %d install %d", stdout.String(), h.secretCalls, h.putCalls, h.installCalls)
	}
	if got, want := h.secretParameters, []string{secrets.Parameter(h.domain, "crm")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("secret parameters = %q, want %q", got, want)
	}
}

func TestDeploySecretLookupFailureStopsBeforeUpload(t *testing.T) {
	// R-ZHXX-6OSX R-FAHM-ZZOH
	h := newDeployHarness()
	h.secretErr = errors.New("secret lookup failed")
	dir, operand, _ := h.artifact(t, "crm-v1.2.3.tar.xz")
	var stdout bytes.Buffer
	err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "selected")
	if !errors.Is(err, h.secretErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(h.secretErr).Pointer() {
		t.Fatalf("error = %#v, want unchanged %#v", err, h.secretErr)
	}
	if got, want := h.secretParameters, []string{secrets.Parameter(h.domain, "crm")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("secret parameters = %q, want %q", got, want)
	}
	if stdout.String() != "file: ok (crm v1.2.3)\n" || h.secretCalls != 1 || h.putCalls != 0 || h.installCalls != 0 {
		t.Fatalf("stdout/calls = %q secrets %d put %d install %d", stdout.String(), h.secretCalls, h.putCalls, h.installCalls)
	}
}

func TestDeploySpaceFailuresStopAfterFile(t *testing.T) {
	// R-ZGQ0-SX28 R-FAHM-ZZOH
	for _, test := range []struct {
		name  string
		state cloud.InstanceState
		gone  bool
	}{
		{name: "absent", gone: true},
		{name: "stopped", state: cloud.StateStopped},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newDeployHarness()
			h.gone = test.gone
			if test.state != "" {
				h.state = test.state
			}
			dir, operand, _ := h.artifact(t, "crm-v1.2.3.tar.xz")
			var stdout bytes.Buffer
			err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "selected")
			if test.gone {
				var noSpace *account.NoSpaceError
				if !errors.As(err, &noSpace) || noSpace.Domain != h.domain {
					t.Fatalf("error = %T %#v", err, err)
				}
			} else {
				var notRunning *space.NotRunningError
				if !errors.As(err, &notRunning) || notRunning.Domain != h.domain || notRunning.State != test.state {
					t.Fatalf("error = %T %#v", err, err)
				}
			}
			if stdout.String() != "file: ok (crm v1.2.3)\n" || len(h.commands) != 2 || h.secretCalls != 0 || h.putCalls != 0 || h.installCalls != 0 {
				t.Fatalf("stdout/work = %q commands %d secrets %d put %d install %d", stdout.String(), len(h.commands), h.secretCalls, h.putCalls, h.installCalls)
			}
		})
	}
}

func TestDeployReturnsSpaceLookupErrorUnchanged(t *testing.T) {
	// R-ZGQ0-SX28
	h := newDeployHarness()
	h.spaceErr = errors.New("space lookup failed")
	dir, operand, _ := h.artifact(t, "crm-v1.2.3.tar.xz")
	var stdout bytes.Buffer
	err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "selected")
	if reflect.ValueOf(err).Pointer() != reflect.ValueOf(h.spaceErr).Pointer() {
		t.Fatalf("error = %#v, want unchanged %#v", err, h.spaceErr)
	}
	if stdout.String() != "file: ok (crm v1.2.3)\n" || len(h.commands) != 2 || h.secretCalls != 0 || h.putCalls != 0 || h.installCalls != 0 {
		t.Fatalf("stdout/work = %q commands %d secrets %d put %d install %d", stdout.String(), len(h.commands), h.secretCalls, h.putCalls, h.installCalls)
	}
}

func TestDeployUploadAndInstallFailuresStopInOrder(t *testing.T) {
	// R-FAHM-ZZOH R-FGL4-WUDY R-FHT1-AM4N
	t.Run("upload", func(t *testing.T) {
		h := newDeployHarness()
		h.putErr = errors.New("upload failed")
		dir, operand, _ := h.artifact(t, "crm-v1.2.3.tar.xz")
		var stdout bytes.Buffer
		err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "selected")
		if !errors.Is(err, h.putErr) || h.putCalls != 1 || h.installCalls != 0 || strings.Contains(stdout.String(), "upload: ok") {
			t.Fatalf("error/work = %#v put %d install %d stdout %q", err, h.putCalls, h.installCalls, stdout.String())
		}
	})

	t.Run("install keeps upload", func(t *testing.T) {
		h := newDeployHarness()
		h.installStatus = 19
		dir, operand, _ := h.artifact(t, "crm-v1.2.3.tar.xz")
		var stdout bytes.Buffer
		err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "selected")
		var commandErr *host.CommandError
		if !errors.As(err, &commandErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(commandErr).Pointer() || commandErr.Step != "install" || commandErr.Status != 19 {
			t.Fatalf("error = %T %#v", err, err)
		}
		if h.putCalls != 1 || h.deleteCalls != 0 || h.installCalls != 1 || !strings.Contains(stdout.String(), "upload: ok") || strings.Contains(stdout.String(), "install: ok") {
			t.Fatalf("calls/stdout = put %d delete %d install %d %q", h.putCalls, h.deleteCalls, h.installCalls, stdout.String())
		}
	})

	t.Run("install transport keeps upload", func(t *testing.T) {
		h := newDeployHarness()
		h.installErr = errors.New("install transport failed")
		dir, operand, _ := h.artifact(t, "crm-v1.2.3.tar.xz")
		var stdout bytes.Buffer
		err := deploy.Run(context.Background(), []string{h.domain, operand}, &stdout, h.deps(dir), "selected")
		if err == nil || err.Error() != "ssh: install transport failed" || !errors.Is(err, h.installErr) ||
			reflect.ValueOf(errors.Unwrap(err)).Pointer() != reflect.ValueOf(h.installErr).Pointer() {
			t.Fatalf("error = %T %#v, want host transport error with unchanged cause %#v", err, err, h.installErr)
		}
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			t.Fatalf("error = %T %#v, unexpectedly host command exit error", err, err)
		}
		if h.putCalls != 1 || h.deleteCalls != 0 || h.installCalls != 1 || !strings.Contains(stdout.String(), "upload: ok") || strings.Contains(stdout.String(), "install: ok") {
			t.Fatalf("calls/stdout = put %d delete %d install %d %q", h.putCalls, h.deleteCalls, h.installCalls, stdout.String())
		}
	})
}

type cloudOpen struct{ profile, region string }

type deployHarness struct {
	domain           string
	state            cloud.InstanceState
	gone             bool
	members          string
	manifest         string
	secretObject     string
	cloudCalls       []cloudOpen
	spaceCalls       int
	spaceErr         error
	secretCalls      int
	secretParameters []string
	secretErr        error
	commands         []seam.Cmd
	operations       []string
	putCalls         int
	putBucket        string
	putKey           string
	putBody          []byte
	putSize          int64
	putErr           error
	deleteCalls      int
	installCalls     int
	installStatus    int
	installErr       error
}

func newDeployHarness() *deployHarness {
	return &deployHarness{
		domain:       "foo.sbx.example",
		state:        cloud.StateRunning,
		members:      "etc/manifest.toml\nbin/crm\n",
		manifest:     "app = \"crm\"\n",
		secretObject: `{}`,
	}
}

func (h *deployHarness) artifact(t *testing.T, name string) (string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	artifact := []byte("complete artifact bytes\x00\xff")
	if err := os.WriteFile(filepath.Join(dir, name), artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, name, artifact
}

func (h *deployHarness) deps(dir string) seam.Deps {
	return seam.Deps{
		Dir: dir,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			h.cloudCalls = append(h.cloudCalls, cloudOpen{profile, region})
			if region == "" {
				h.operations = append(h.operations, "open-bootstrap")
				return cloud.Clients{SSM: deploySSM{h: h, bootstrap: true}}, nil
			}
			h.operations = append(h.operations, "open-regional")
			return cloud.Clients{EC2: deployEC2{h}, SSM: deploySSM{h: h}, S3: deployS3{h}}, nil
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			h.commands = append(h.commands, command)
			switch command.Path {
			case "tar":
				if command.Args[0] == "-t" {
					h.operations = append(h.operations, "tar-list")
					return seam.Result{Stdout: []byte(h.members)}, nil
				}
				h.operations = append(h.operations, "tar-manifest")
				return seam.Result{Stdout: []byte(h.manifest)}, nil
			case "ssh":
				h.operations = append(h.operations, "install")
				h.installCalls++
				if h.installErr != nil {
					return seam.Result{}, h.installErr
				}
				return seam.Result{ExitCode: h.installStatus, Stdout: []byte("discard me")}, nil
			default:
				return seam.Result{}, errors.New("unexpected command")
			}
		},
	}
}

func (h *deployHarness) properties() string {
	return `{"domain":"sbx.example","backup_bucket":"backups","launch_template_id":"lt-1","permissions_boundary_arn":"arn:boundary","region":"us-test-1","delete_secrets_on_destroy":false,"delete_backups_on_destroy":false,"backup_host_files_seconds":1,"backup_service_files_seconds":2,"backup_service_db_seconds":3,"backup_service_wal_seconds":4}`
}

type deploySSM struct {
	h         *deployHarness
	bootstrap bool
}

func (s deploySSM) GetParameter(_ context.Context, name string) (string, error) {
	if s.bootstrap {
		if name != account.PropertiesParameter {
			return "", errors.New("unexpected bootstrap parameter")
		}
		return s.h.properties(), nil
	}
	s.h.operations = append(s.h.operations, "secrets")
	s.h.secretCalls++
	s.h.secretParameters = append(s.h.secretParameters, name)
	if s.h.secretErr != nil {
		return "", s.h.secretErr
	}
	return s.h.secretObject, nil
}
func (deploySSM) PutSecureParameter(context.Context, string, string) error { return nil }
func (deploySSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, nil
}
func (deploySSM) DeleteParameter(context.Context, string) error { return nil }

type deployEC2 struct{ h *deployHarness }

func (f deployEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	f.h.operations = append(f.h.operations, "space")
	f.h.spaceCalls++
	if f.h.spaceErr != nil {
		return nil, f.h.spaceErr
	}
	if f.h.gone {
		return nil, nil
	}
	return []cloud.Instance{{ID: "i-1", Space: f.h.domain, State: f.h.state, Address: "192.0.2.8"}}, nil
}
func (deployEC2) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (deployEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (deployEC2) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) { return false, nil }
func (deployEC2) StartInstance(context.Context, string) error                 { return nil }
func (deployEC2) StopInstance(context.Context, string) error                  { return nil }
func (deployEC2) TerminateInstance(context.Context, string) error             { return nil }
func (deployEC2) InstanceChecksPassed(context.Context, string) (bool, error)  { return false, nil }
func (deployEC2) ListSpaceAddresses(context.Context) ([]cloud.Address, error) { return nil, nil }
func (deployEC2) AllocateAddress(context.Context, string) (cloud.Address, error) {
	return cloud.Address{}, nil
}
func (deployEC2) AssociateAddress(context.Context, string, string) error { return nil }
func (deployEC2) DisassociateAddress(context.Context, string) error      { return nil }
func (deployEC2) ReleaseAddress(context.Context, string) error           { return nil }

type deployS3 struct{ h *deployHarness }

func (f deployS3) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	return nil, nil
}
func (f deployS3) PutObject(_ context.Context, bucket, key string, body io.Reader, size int64) error {
	f.h.operations = append(f.h.operations, "upload")
	f.h.putCalls++
	f.h.putBucket, f.h.putKey, f.h.putSize = bucket, key, size
	f.h.putBody, _ = io.ReadAll(body)
	return f.h.putErr
}
func (f deployS3) DeleteObjects(context.Context, string, []string) error {
	f.h.deleteCalls++
	return nil
}
