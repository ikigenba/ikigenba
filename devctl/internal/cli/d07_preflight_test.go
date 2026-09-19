package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const testDomain = "sbx1.ikigenba.dev"

const expectedCreateUsage = `Usage: devctl space create <space> --acme-email <address>

Create the space: push its secrets, make its role, launch its instance with an
Elastic IP, write its records, install the newest published opsctl, set its
ten host keys, restore its own host backup when the bucket holds one, and run
opsctl init. Completed steps remain on failure; use space destroy to clean up.

Options:
  --acme-email <address>  where the CA sends the space's expiry warnings; required
`

func TestCreateCLIUsagePrecedesCheckout(t *testing.T) {
	// R-8UTA-21TK R-8X92-TLAY R-8YGZ-7D1N
	deps := seam.Deps{Dir: t.TempDir(), EUID: 1, Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected exec")
		return seam.Result{}, nil
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud")
		return cloud.Clients{}, nil
	}}
	for _, tc := range []struct {
		args           []string
		code           int
		stdout, stderr string
	}{
		{[]string{"space", "create"}, 2, "", "devctl: space create needs <space>\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "sbx1"}, 2, "", "devctl: space create needs --acme-email <address>\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "sbx1", "--acme-email"}, 2, "", "devctl: option '--acme-email' requires a value\n\nsee 'devctl space --help' for usage\n"},
		{[]string{"space", "create", "--help"}, 0, expectedCreateUsage, ""},
		{[]string{"space", "create", "-h"}, 0, expectedCreateUsage, ""},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr, deps); code != tc.code || stdout.String() != tc.stdout || stderr.String() != tc.stderr {
			t.Fatalf("Run(%q) = %d, %q, %q", tc.args, code, stdout.String(), stderr.String())
		}
	}
}

// commandHarness remains shared with the existing secret-safety regression.
type commandHarness struct {
	t             *testing.T
	checkoutRoot  string
	mutations     []string
	createRoleErr error
}

func newCommandHarness(t *testing.T) *commandHarness {
	h := &commandHarness{t: t, checkoutRoot: t.TempDir()}
	h.write(filepath.Join("infra", "terraform.tfvars.json"), `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	return h
}

func (h *commandHarness) write(relative, value string) {
	h.t.Helper()
	path := filepath.Join(h.checkoutRoot, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		h.t.Fatal(err)
	}
}
func (h *commandHarness) addAppWithSecrets(name string, names ...string) {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "\"" + n + "\""
	}
	h.write(filepath.Join(name, "cmd", name, "main.go"), "package main\n")
	h.write(filepath.Join(name, "etc", "manifest.toml"), "app = \""+name+"\"\nsecrets = ["+strings.Join(quoted, ",")+"]\n")
}
func (h *commandHarness) deps() seam.Deps {
	return seam.Deps{Dir: h.checkoutRoot, EUID: 1, Getenv: func(string) string { return "" }, After: immediateD07After, Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		if c.Path == "git" {
			return seam.Result{Stdout: []byte(h.checkoutRoot + "\n")}, nil
		}
		return seam.Result{}, nil
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: h, SSM: h, Route53: h, S3: h, IAM: h, STS: h}, nil
	}}
}

func immediateD07After(time.Duration) <-chan time.Time {
	ready := make(chan time.Time)
	close(ready)
	return ready
}

func (h *commandHarness) CallerAccountID(context.Context) (string, error)        { return "123", nil }
func (h *commandHarness) LaunchTemplate(context.Context, string) (string, error) { return "lt", nil }
func (h *commandHarness) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return nil, nil
}
func (h *commandHarness) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (h *commandHarness) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	return cloud.Instance{}, nil
}
func (h *commandHarness) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	return true, nil
}
func (h *commandHarness) StartInstance(context.Context, string) error     { return nil }
func (h *commandHarness) StopInstance(context.Context, string) error      { return nil }
func (h *commandHarness) TerminateInstance(context.Context, string) error { return nil }
func (h *commandHarness) InstanceChecksPassed(context.Context, string) (bool, error) {
	return true, nil
}
func (h *commandHarness) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	return nil, nil
}
func (h *commandHarness) AllocateAddress(context.Context, string, string) (cloud.Address, error) {
	return cloud.Address{}, nil
}
func (h *commandHarness) AssociateAddress(context.Context, string, string) error { return nil }
func (h *commandHarness) DisassociateAddress(context.Context, string) error      { return nil }
func (h *commandHarness) ReleaseAddress(context.Context, string) error           { return nil }
func (h *commandHarness) GetParameter(context.Context, string) (string, error)   { return "", nil }
func (h *commandHarness) PutSecureParameter(_ context.Context, name, _ string) error {
	h.mutations = append(h.mutations, "ssm:put:"+filepath.Base(name))
	return nil
}
func (h *commandHarness) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, nil
}
func (h *commandHarness) DeleteParameter(context.Context, string) error { return nil }
func (h *commandHarness) Zone(context.Context, string) (cloud.Zone, error) {
	return cloud.Zone{ID: "Z", Name: "ikigenba.dev"}, nil
}
func (h *commandHarness) ListRecords(context.Context, string) ([]cloud.Record, error) {
	return nil, nil
}
func (h *commandHarness) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	return cloud.Record{}, false, nil
}
func (h *commandHarness) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	return "c", nil
}
func (h *commandHarness) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	return cloud.ChangeInsync, nil
}
func (h *commandHarness) ListObjects(context.Context, string, string) ([]cloud.Object, error) {
	return nil, nil
}
func (h *commandHarness) PutObject(context.Context, string, string, io.Reader, int64) error {
	return nil
}
func (h *commandHarness) DeleteObjects(context.Context, string, []string) error { return nil }
func (h *commandHarness) PermissionsBoundary(context.Context, string) (string, error) {
	return "arn", nil
}
func (h *commandHarness) RoleExists(context.Context, string) (bool, error) { return false, nil }
func (h *commandHarness) CreateRole(context.Context, cloud.RoleSpec) error {
	h.mutations = append(h.mutations, "iam:create-role")
	return h.createRoleErr
}
func (h *commandHarness) PutRolePolicy(context.Context, string, string, string) error { return nil }
func (h *commandHarness) DeleteRolePolicy(context.Context, string, string) error      { return nil }
func (h *commandHarness) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	return nil, false, nil
}
func (h *commandHarness) CreateInstanceProfile(context.Context, string) error            { return nil }
func (h *commandHarness) AddRoleToInstanceProfile(context.Context, string, string) error { return nil }
func (h *commandHarness) RemoveRoleFromInstanceProfile(context.Context, string, string) error {
	return nil
}
func (h *commandHarness) DeleteInstanceProfile(context.Context, string) error { return nil }
func (h *commandHarness) DeleteRole(context.Context, string) error            { return nil }
