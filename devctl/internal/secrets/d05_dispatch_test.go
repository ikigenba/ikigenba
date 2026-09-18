package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const d05Properties = `{
	"domain":"ikigenba.dev",
	"backup_bucket":"backups",
	"launch_template_id":"lt-test",
	"permissions_boundary_arn":"arn:test",
	"region":"us-test-1",
	"delete_secrets_on_destroy":false,
	"delete_backups_on_destroy":false,
	"backup_host_files_seconds":1,
	"backup_service_files_seconds":2,
	"backup_service_db_seconds":3,
	"backup_service_wal_seconds":4
}`

type d05SSM struct {
	cloud.SSM
	getValue   string
	parameters []cloud.Parameter
	writes     []pushWrite
	attempts   int
	failAt     int
}

func (ssm *d05SSM) GetParameter(context.Context, string) (string, error) {
	return ssm.getValue, nil
}

func (ssm *d05SSM) PutSecureParameter(_ context.Context, name, value string) error {
	ssm.attempts++
	if ssm.failAt != 0 && ssm.attempts == ssm.failAt {
		return errors.New("write failed")
	}
	ssm.writes = append(ssm.writes, pushWrite{name: name, value: value})
	return nil
}

func (ssm *d05SSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return ssm.parameters, nil
}

type d05EC2 struct {
	cloud.EC2
	domain string
}

func (ec2 *d05EC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return []cloud.Instance{{ID: "i-test", Space: ec2.domain, State: cloud.StateRunning}}, nil
}

func TestPushDispatchSelectsAppsAndReportsInWriteOrder(t *testing.T) {
	// R-GCEY-B4EN
	// R-GHAJ-U7DF
	root := d05Checkout(t)
	const domain = "foo.sbx.ikigenba.dev"
	values := map[string]string{
		"CRM_API_KEY":         "key",
		"CRM_API_SECRET":      "secret",
		"CRM_ORG":             "org",
		"GMAIL_CLIENT_ID":     "id",
		"GMAIL_CLIENT_SECRET": "secret",
	}

	allSSM := &d05SSM{}
	var stdout strings.Builder
	err := Run(context.Background(), []string{"push", domain}, &stdout, d05Deps(t, root, allSSM, values, nil), "work")
	if err != nil {
		t.Fatalf("push all returned error: %v", err)
	}
	wantOutput := "crm: ok (3 keys)\ndashboard: ok (0 keys)\ngmail: ok (2 keys)\n"
	if got := stdout.String(); got != wantOutput {
		t.Fatalf("push all stdout = %q, want %q", got, wantOutput)
	}
	wantNames := []string{Parameter(domain, "crm"), Parameter(domain, "dashboard"), Parameter(domain, "gmail")}
	if got := d05WriteNames(allSSM.writes); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("push all parameters = %q, want %q", got, wantNames)
	}

	oneSSM := &d05SSM{}
	stdout.Reset()
	err = Run(context.Background(), []string{"push", domain, "crm"}, &stdout, d05Deps(t, root, oneSSM, values, nil), "work")
	if err != nil {
		t.Fatalf("push one returned error: %v", err)
	}
	if got, want := stdout.String(), "crm: ok (3 keys)\n"; got != want {
		t.Fatalf("push one stdout = %q, want %q", got, want)
	}
	if got, want := d05WriteNames(oneSSM.writes), []string{Parameter(domain, "crm")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("push one parameters = %q, want %q", got, want)
	}
}

func TestPushReturnsAndPrintsSucceededEntriesBeforeError(t *testing.T) {
	// R-GIIG-7Z44
	const domain = "foo.sbx.ikigenba.dev"
	apps := []checkout.App{
		{Name: "crm", Manifest: checkout.Manifest{Secrets: []string{"ZED", "ALPHA"}}},
		{Name: "dashboard"},
	}
	ssm := &d05SSM{failAt: 2}
	acct := &account.Account{Clients: cloud.Clients{SSM: ssm}}
	entries, err := Push(context.Background(), seam.Deps{Getenv: func(name string) string { return name + "-value" }}, acct, domain, apps)
	if err == nil {
		t.Fatal("Push returned nil error")
	}
	wantEntries := []Entry{{App: "crm", Keys: []string{"ALPHA", "ZED"}}}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("Push entries = %#v, want %#v", entries, wantEntries)
	}

	root := d05Checkout(t)
	commandSSM := &d05SSM{failAt: 2}
	var stdout strings.Builder
	err = Run(context.Background(), []string{"push", domain}, &stdout, d05Deps(t, root, commandSSM, map[string]string{
		"CRM_API_KEY": "key", "CRM_API_SECRET": "secret", "CRM_ORG": "org",
		"GMAIL_CLIENT_ID": "id", "GMAIL_CLIENT_SECRET": "secret",
	}, nil), "work")
	if err == nil {
		t.Fatal("Run returned nil error after second write failed")
	}
	if got, want := stdout.String(), "crm: ok (3 keys)\n"; got != want {
		t.Fatalf("stdout before error = %q, want %q", got, want)
	}
}

func TestListDispatchPrintsEntriesWithoutCheckout(t *testing.T) {
	// R-GJQC-LQUT
	// R-GR1Q-WDAZ
	const domain = "foo.sbx.ikigenba.dev"
	parameters := []cloud.Parameter{
		{Name: Parameter(domain, "gmail"), Value: `{"GMAIL_CLIENT_SECRET":"s","GMAIL_CLIENT_ID":"i"}`},
		{Name: Parameter(domain, "dashboard"), Value: `{}`},
		{Name: Parameter(domain, "crm"), Value: `{"CRM_ORG":"o","CRM_API_SECRET":"s","CRM_API_KEY":"k"}`},
	}
	ssm := &d05SSM{parameters: parameters}
	var commands []seam.Cmd
	var stdout strings.Builder
	err := Run(context.Background(), []string{"list", domain}, &stdout, d05Deps(t, "", ssm, nil, &commands), "work")
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	want := "crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG\ndashboard -\ngmail GMAIL_CLIENT_ID,GMAIL_CLIENT_SECRET\n"
	if got := stdout.String(); got != want {
		t.Fatalf("list stdout = %q, want %q", got, want)
	}
	if len(commands) != 0 {
		t.Fatalf("list process commands = %#v, want none", commands)
	}

	emptySSM := &d05SSM{}
	stdout.Reset()
	err = Run(context.Background(), []string{"list", domain}, &stdout, d05Deps(t, "", emptySSM, nil, &commands), "work")
	if err != nil {
		t.Fatalf("empty list returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("empty list stdout = %q, want empty", stdout.String())
	}
}

func TestListOneDispatchPrintsNames(t *testing.T) {
	// R-GKY8-ZILI
	const domain = "foo.sbx.ikigenba.dev"
	ssm := &d05SSM{getValue: `{"CRM_ORG":"o","CRM_API_SECRET":"s","CRM_API_KEY":"k"}`}
	var stdout strings.Builder
	err := Run(context.Background(), []string{"list", domain, "crm"}, &stdout, d05Deps(t, "", ssm, nil, nil), "work")
	if err != nil {
		t.Fatalf("list one returned error: %v", err)
	}
	if got, want := stdout.String(), "crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG\n"; got != want {
		t.Fatalf("list one stdout = %q, want %q", got, want)
	}
}

func d05Deps(t *testing.T, root string, regional *d05SSM, values map[string]string, commands *[]seam.Cmd) seam.Deps {
	t.Helper()
	return seam.Deps{
		Dir: root,
		Getenv: func(name string) string {
			return values[name]
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if commands != nil {
				*commands = append(*commands, command)
			}
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			if profile != "work" {
				t.Fatalf("cloud profile = %q, want work", profile)
			}
			if region == "" {
				return cloud.Clients{SSM: &d05SSM{getValue: d05Properties}}, nil
			}
			if region != "us-test-1" {
				t.Fatalf("cloud region = %q, want us-test-1", region)
			}
			return cloud.Clients{SSM: regional, EC2: &d05EC2{domain: "foo.sbx.ikigenba.dev"}}, nil
		},
	}
}

func d05Checkout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	d05WriteApp(t, root, "gmail", []string{"GMAIL_CLIENT_SECRET", "GMAIL_CLIENT_ID"})
	d05WriteApp(t, root, "crm", []string{"CRM_ORG", "CRM_API_KEY", "CRM_API_SECRET"})
	d05WriteApp(t, root, "dashboard", nil)
	return root
}

func d05WriteApp(t *testing.T, root, name string, secrets []string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var manifest strings.Builder
	manifest.WriteString("app = \"")
	manifest.WriteString(name)
	manifest.WriteString("\"\nsecrets = [")
	for index, secret := range secrets {
		if index != 0 {
			manifest.WriteString(", ")
		}
		manifest.WriteString("\"")
		manifest.WriteString(secret)
		manifest.WriteString("\"")
	}
	manifest.WriteString("]\n")
	if err := os.WriteFile(filepath.Join(dir, checkout.ManifestFile), []byte(manifest.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func d05WriteNames(writes []pushWrite) []string {
	names := make([]string, len(writes))
	for index, write := range writes {
		names[index] = write.name
	}
	return names
}
