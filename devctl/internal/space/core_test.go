package space

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestRunSignature(_ *testing.T) {
	accept := func(func(context.Context, []string, io.Writer, seam.Deps) error) {}
	accept(Run)
}

func TestStep(t *testing.T) {
	// R-SR5Y-JN2E
	var output bytes.Buffer
	Step(&output, "instance", "i-0c9e94542d98846a8 terminated")
	Step(&output, "init", "")
	const want = "instance: ok (i-0c9e94542d98846a8 terminated)\ninit: ok\n"
	if output.String() != want {
		t.Fatalf("Step output = %q, want %q", output.String(), want)
	}
}

func TestNamesAndConstants(t *testing.T) {
	// R-U273-JQ19
	const domain = "sbx1.ikigenba.dev"
	if got, want := RoleName(domain), domain; got != want {
		t.Errorf("RoleName() = %q, want %q", got, want)
	}
	if got, want := BackupPrefix("sbx1"), "sbx1/"; got != want {
		t.Errorf("BackupPrefix() = %q, want %q", got, want)
	}
	if got, want := RecordNames(domain), []string{domain, "*." + domain}; !reflect.DeepEqual(got, want) {
		t.Errorf("RecordNames() = %q, want %q", got, want)
	}
	if PolicyName != "space" {
		t.Errorf("PolicyName = %q, want space", PolicyName)
	}
	if RecordTTL != 60 {
		t.Errorf("RecordTTL = %d, want 60", RecordTTL)
	}
}

func TestWaitAssociateSignature(_ *testing.T) {
	// R-TRRP-P2TM
	accept := func(func(context.Context, seam.Deps, cloud.EC2, string, string) error) {}
	accept(WaitAssociate)
}

func TestWaitDeclarations(t *testing.T) {
	// R-U3EZ-XHRY
	acceptWaitState := func(func(context.Context, seam.Deps, cloud.EC2, string, cloud.InstanceState) (cloud.Instance, error)) {
	}
	acceptWaitChecks := func(func(context.Context, seam.Deps, cloud.EC2, string) error) {}
	acceptWaitLaunchReady := func(func(context.Context, seam.Deps, cloud.EC2, cloud.LaunchSpec) error) {}
	acceptWaitInsync := func(func(context.Context, seam.Deps, cloud.Route53, string) error) {}
	acceptWaitState(WaitState)
	acceptWaitChecks(WaitChecks)
	acceptWaitLaunchReady(WaitLaunchReady)
	acceptWaitInsync(WaitInsync)
	if PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %s, want 5s", PollInterval)
	}
	if PollAttempts != 60 {
		t.Errorf("PollAttempts = %d, want 60", PollAttempts)
	}
}

func TestUsageError(t *testing.T) {
	// R-SZP9-8199
	err := &UsageError{Message: "bad option", Help: "devctl space --help"}
	assertFields(t, err, struct {
		Message string
		Help    string
	}{})
	if got, want := err.Error(), "bad option"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got, want := err.Detail(), "see 'devctl space --help' for usage"; got != want {
		t.Errorf("Detail() = %q, want %q", got, want)
	}
	if got := err.ExitCode(); got != 2 {
		t.Errorf("ExitCode() = %d, want 2", got)
	}
}

func TestNotRunningError(t *testing.T) {
	// R-XQOJ-QF8R
	err := &NotRunningError{Domain: "sbx2.ikigenba.dev", State: cloud.StateStopped}
	assertFields(t, err, struct {
		Domain string
		State  cloud.InstanceState
	}{})
	if got, want := err.Error(), "'sbx2.ikigenba.dev' is stopped"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestWaitError(t *testing.T) {
	// R-T251-ZKQN
	err := &WaitError{Subject: "i-0c9e94542d98846a8", Want: "be running"}
	assertFields(t, err, struct {
		Subject string
		Want    string
	}{})
	if got, want := err.Error(), "timed out waiting for i-0c9e94542d98846a8 to be running"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestRetireStateError(t *testing.T) {
	// R-U9IH-UCHF
	err := &RetireStateError{
		ID:    "i-0c9e94542d98846a8",
		State: cloud.StateStopped,
		Label: "staging",
	}
	assertFields(t, err, struct {
		ID    string
		State cloud.InstanceState
		Label string
	}{})
	if got, want := err.Error(), "retire: instance i-0c9e94542d98846a8 is stopped"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got := err.ExitCode(); got != 1 {
		t.Errorf("ExitCode() = %d, want 1", got)
	}
	const wantDetail = "run 'devctl space start staging' first, or pass --no-backup"
	if got := err.Detail(); got != wantDetail {
		t.Errorf("Detail() = %q, want %q", got, wantDetail)
	}
}

func assertFields(t *testing.T, value, want any) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	wantType := reflect.TypeOf(want)
	if typeOf.NumField() != wantType.NumField() {
		t.Fatalf("%s has %d fields, want %d", typeOf.Name(), typeOf.NumField(), wantType.NumField())
	}
	for index := range wantType.NumField() {
		gotField := typeOf.Field(index)
		wantField := wantType.Field(index)
		if gotField.Name != wantField.Name || gotField.Type != wantField.Type {
			t.Errorf("field %d = %s %s, want %s %s", index,
				gotField.Name, gotField.Type, wantField.Name, wantField.Type)
		}
	}
}
