package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type d05SSM struct {
	cloud.SSM
	getName    string
	getValue   string
	getErr     error
	listPrefix string
	parameters []cloud.Parameter
	writes     []pushWrite
	attempts   int
	failAt     int
}

func (ssm *d05SSM) GetParameter(_ context.Context, name string) (string, error) {
	ssm.getName = name
	return ssm.getValue, ssm.getErr
}

func (ssm *d05SSM) PutSecureParameter(_ context.Context, name, value string) error {
	ssm.attempts++
	if ssm.failAt != 0 && ssm.attempts == ssm.failAt {
		return errors.New("write failed")
	}
	ssm.writes = append(ssm.writes, pushWrite{name: name, value: value})
	return nil
}

func (ssm *d05SSM) ListParameters(_ context.Context, prefix string) ([]cloud.Parameter, error) {
	ssm.listPrefix = prefix
	return ssm.parameters, nil
}

type d05STS struct{ err error }

func (sts *d05STS) CallerAccountID(context.Context) (string, error) {
	return "123456789012", sts.err
}

type d05EC2 struct {
	cloud.EC2
	rootDomain string
	instances  []cloud.Instance
	err        error
	calls      int
}

func (ec2 *d05EC2) ListSpaceInstances(_ context.Context, rootDomain string) ([]cloud.Instance, error) {
	ec2.calls++
	ec2.rootDomain = rootDomain
	return ec2.instances, ec2.err
}

type d05Cloud struct {
	clients  cloud.Clients
	err      error
	profiles []string
	regions  []string
}

func (opener *d05Cloud) open(_ context.Context, profile, region string) (cloud.Clients, error) {
	opener.profiles = append(opener.profiles, profile)
	opener.regions = append(opener.regions, region)
	return opener.clients, opener.err
}

func TestPushDispatchSelectsAppsAndReportsInWriteOrder(t *testing.T) {
	// R-0PG9-9CI1
	// R-GHAJ-U7DF
	root := d05Checkout(t)
	const domain = "sbx1.ikigenba.dev"
	values := map[string]string{
		"CRM_API_KEY":         "key",
		"CRM_API_SECRET":      "secret",
		"CRM_ORG":             "org",
		"GMAIL_CLIENT_ID":     "id",
		"GMAIL_CLIENT_SECRET": "secret",
	}

	allSSM := &d05SSM{}
	allCloud := d05WorkingCloud(allSSM)
	var stdout strings.Builder
	err := Run(context.Background(), []string{"push", "sbx1"}, &stdout, d05Deps(t, root, &allCloud, values, nil))
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
	oneCloud := d05WorkingCloud(oneSSM)
	stdout.Reset()
	err = Run(context.Background(), []string{"push", domain, "crm"}, &stdout, d05Deps(t, root, &oneCloud, values, nil))
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
	const domain = "sbx1.ikigenba.dev"
	apps := []checkout.App{
		{Name: "crm", Manifest: checkout.Manifest{Secrets: []string{"ZED", "ALPHA"}}},
		{Name: "dashboard"},
	}
	ssm := &d05SSM{failAt: 2}
	entries, err := Push(context.Background(), seam.Deps{Getenv: func(name string) string { return name + "-value" }}, ssm, domain, apps)
	if err == nil {
		t.Fatal("Push returned nil error")
	}
	wantEntries := []Entry{{App: "crm", Keys: []string{"ALPHA", "ZED"}}}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("Push entries = %#v, want %#v", entries, wantEntries)
	}

	root := d05Checkout(t)
	commandSSM := &d05SSM{failAt: 2}
	opener := d05WorkingCloud(commandSSM)
	var stdout strings.Builder
	err = Run(context.Background(), []string{"push", "sbx1"}, &stdout, d05Deps(t, root, &opener, map[string]string{
		"CRM_API_KEY": "key", "CRM_API_SECRET": "secret", "CRM_ORG": "org",
		"GMAIL_CLIENT_ID": "id", "GMAIL_CLIENT_SECRET": "secret",
	}, nil))
	if err == nil {
		t.Fatal("Run returned nil error after second write failed")
	}
	if got, want := stdout.String(), "crm: ok (3 keys)\n"; got != want {
		t.Fatalf("stdout before error = %q, want %q", got, want)
	}
}

func TestListDispatchPrintsEntriesWithoutInstanceLookup(t *testing.T) {
	// R-0WRN-JYY7
	// R-0VJR-677I
	// R-D209-R4QN
	root := d05Checkout(t)
	const domain = "sbx1.ikigenba.dev"
	parameters := []cloud.Parameter{
		{Name: Parameter(domain, "gmail"), Value: `{"GMAIL_CLIENT_SECRET":"s","GMAIL_CLIENT_ID":"i"}`},
		{Name: Parameter(domain, "dashboard"), Value: `{}`},
		{Name: Parameter(domain, "crm"), Value: `{"CRM_ORG":"o","CRM_API_SECRET":"s","CRM_API_KEY":"k"}`},
	}
	ssm := &d05SSM{parameters: parameters}
	ec2 := &d05EC2{err: errors.New("instance lookup must not occur")}
	opener := &d05Cloud{clients: cloud.Clients{SSM: ssm, EC2: ec2, STS: &d05STS{}}}
	var commands []seam.Cmd
	var stdout strings.Builder
	err := Run(context.Background(), []string{"list", "sbx1"}, &stdout, d05Deps(t, root, opener, nil, &commands))
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	want := "crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG\ndashboard -\ngmail GMAIL_CLIENT_ID,GMAIL_CLIENT_SECRET\n"
	if got := stdout.String(); got != want {
		t.Fatalf("list stdout = %q, want %q", got, want)
	}
	if len(commands) != 1 || commands[0].Path != "git" {
		t.Fatalf("list process commands = %#v, want one git command", commands)
	}
	if ec2.calls != 0 {
		t.Fatalf("EC2 calls = %d, want none", ec2.calls)
	}

	emptySSM := &d05SSM{}
	emptyCloud := d05WorkingCloud(emptySSM)
	stdout.Reset()
	err = Run(context.Background(), []string{"list", domain}, &stdout, d05Deps(t, root, &emptyCloud, nil, nil))
	if err != nil {
		t.Fatalf("empty list returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("empty list stdout = %q, want empty", stdout.String())
	}
}

func TestListOneDispatchPrintsNames(t *testing.T) {
	// R-0XZJ-XQOW
	root := d05Checkout(t)
	const domain = "sbx1.ikigenba.dev"
	ssm := &d05SSM{getValue: `{"CRM_ORG":"o","CRM_API_SECRET":"s","CRM_API_KEY":"k"}`}
	opener := d05WorkingCloud(ssm)
	var stdout strings.Builder
	err := Run(context.Background(), []string{"list", "sbx1", "crm"}, &stdout, d05Deps(t, root, &opener, nil, nil))
	if err != nil {
		t.Fatalf("list one returned error: %v", err)
	}
	if got, want := stdout.String(), "crm CRM_API_KEY,CRM_API_SECRET,CRM_ORG\n"; got != want {
		t.Fatalf("list one stdout = %q, want %q", got, want)
	}
	if ssm.getName != Parameter(domain, "crm") {
		t.Fatalf("GetParameter name = %q", ssm.getName)
	}
}

func TestPushResolvesAppBeforeRootAndCloud(t *testing.T) {
	// R-0N0G-HT0N
	root := d05Checkout(t)
	if err := os.Remove(filepath.Join(root, checkout.RootFilePath)); err != nil {
		t.Fatal(err)
	}
	opener := &d05Cloud{}
	var commands []seam.Cmd
	err := Run(context.Background(), []string{"push", "sbx1", "bogus"}, &strings.Builder{}, d05Deps(t, root, opener, nil, &commands))
	var noApp *checkout.NoAppError
	if !errors.As(err, &noApp) || noApp.Name != "bogus" {
		t.Fatalf("Run error = %T %v, want no app bogus", err, err)
	}
	if len(opener.profiles) != 0 {
		t.Fatalf("cloud calls = %d, want none", len(opener.profiles))
	}
	if len(commands) != 1 || commands[0].Path != "git" {
		t.Fatalf("process commands = %#v, want checkout.Open git only", commands)
	}
}

func TestCommandsParseSpaceAndConnectFromRootFile(t *testing.T) {
	// R-0Z7G-BIFL
	root := d05Checkout(t)
	const domain = "sbx1.ikigenba.dev"
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "push label", args: []string{"push", "sbx1", "dashboard"}},
		{name: "list domain", args: []string{"list", domain}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ssm := &d05SSM{}
			opener := d05WorkingCloud(ssm)
			if err := Run(context.Background(), test.args, &strings.Builder{}, d05Deps(t, root, &opener, nil, nil)); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if !reflect.DeepEqual(opener.profiles, []string{"ikigenba.dev"}) || !reflect.DeepEqual(opener.regions, []string{"us-east-2"}) {
				t.Fatalf("connect calls = profiles %q regions %q", opener.profiles, opener.regions)
			}
		})
	}

	for _, subcommand := range []string{"push", "list"} {
		opener := &d05Cloud{}
		err := Run(context.Background(), []string{subcommand, "crm.sbx1"}, &strings.Builder{}, d05Deps(t, root, opener, nil, nil))
		if err == nil || err.Error() != "'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'" {
			t.Fatalf("%s invalid space error = %v", subcommand, err)
		}
		if len(opener.profiles) != 0 {
			t.Fatalf("%s cloud calls = %d, want none", subcommand, len(opener.profiles))
		}
	}

	connectErr := errors.New("connect failed")
	for _, subcommand := range []string{"push", "list"} {
		opener := &d05Cloud{err: connectErr}
		err := Run(context.Background(), []string{subcommand, "sbx1"}, &strings.Builder{}, d05Deps(t, root, opener, nil, nil))
		if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(connectErr).Pointer() {
			t.Fatalf("%s connect error = %T %v, want original", subcommand, err, err)
		}
		if len(opener.profiles) != 1 {
			t.Fatalf("%s connect calls = %d, want one", subcommand, len(opener.profiles))
		}
	}
}

func TestPushLooksUpSpaceBeforeSecrets(t *testing.T) {
	// R-0O8C-VKRC
	// R-D209-R4QN
	root := d05Checkout(t)
	ssm := &d05SSM{}
	ec2 := &d05EC2{}
	opener := &d05Cloud{clients: cloud.Clients{SSM: ssm, EC2: ec2, STS: &d05STS{}}}
	lookups := 0
	deps := d05Deps(t, root, opener, nil, nil)
	deps.Getenv = func(string) string {
		lookups++
		return "must-not-be-read"
	}
	err := Run(context.Background(), []string{"push", "gone", "crm"}, &strings.Builder{}, deps)
	var noSpace *cloud.NoSpaceError
	if !errors.As(err, &noSpace) || noSpace.Domain != "gone.ikigenba.dev" {
		t.Fatalf("Run error = %T %v, want NoSpaceError", err, err)
	}
	if ec2.calls != 1 || ec2.rootDomain != "ikigenba.dev" {
		t.Fatalf("space lookup = calls %d root %q", ec2.calls, ec2.rootDomain)
	}
	if lookups != 0 || ssm.attempts != 0 {
		t.Fatalf("before failed space lookup: keyring calls %d, writes %d", lookups, ssm.attempts)
	}

	lookupErr := errors.New("space lookup failed")
	failingEC2 := &d05EC2{err: lookupErr}
	failingCloud := &d05Cloud{clients: cloud.Clients{SSM: ssm, EC2: failingEC2, STS: &d05STS{}}}
	deps = d05Deps(t, root, failingCloud, nil, nil)
	deps.Getenv = func(string) string {
		lookups++
		return "must-not-be-read"
	}
	err = Run(context.Background(), []string{"push", "gone", "crm"}, &strings.Builder{}, deps)
	if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(lookupErr).Pointer() {
		t.Fatalf("Run lookup error = %T %v, want original", err, err)
	}
	if lookups != 0 || ssm.attempts != 0 {
		t.Fatalf("before errored space lookup: keyring calls %d, writes %d", lookups, ssm.attempts)
	}
}

func d05WorkingCloud(ssm *d05SSM) d05Cloud {
	return d05Cloud{clients: cloud.Clients{
		SSM: ssm,
		EC2: &d05EC2{instances: []cloud.Instance{{ID: "i-test", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning}}},
		STS: &d05STS{},
	}}
}

func d05Deps(t *testing.T, root string, opener *d05Cloud, values map[string]string, commands *[]seam.Cmd) seam.Deps {
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
		Cloud: opener.open,
	}
}

func d05Checkout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	d05WriteRootFile(t, root)
	d05WriteApp(t, root, "gmail", []string{"GMAIL_CLIENT_SECRET", "GMAIL_CLIENT_ID"})
	d05WriteApp(t, root, "crm", []string{"CRM_ORG", "CRM_API_KEY", "CRM_API_SECRET"})
	d05WriteApp(t, root, "dashboard", nil)
	return root
}

func d05WriteRootFile(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, checkout.RootFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"domain":"ikigenba.dev","region":"us-east-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func d05WriteApp(t *testing.T, root, name string, secrets []string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd", name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", name, "main.go"), []byte("package main\n"), 0o600); err != nil {
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
