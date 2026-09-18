package space

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestRunSignature(_ *testing.T) {
	// R-SPY2-5VBP
	accept := func(func(context.Context, []string, io.Writer, seam.Deps, string) error) {}
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
	// R-SSDU-XET3
	const domain = "foo.sbx.ikigenba.dev"
	if got, want := RoleName(domain), "ikigenba-space-foo.sbx.ikigenba.dev"; got != want {
		t.Errorf("RoleName() = %q, want %q", got, want)
	}
	if got, want := BackupPrefix(domain), "foo.sbx.ikigenba.dev/"; got != want {
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

func TestWaitDeclarations(t *testing.T) {
	// R-STLR-B6JS
	acceptWaitState := func(func(context.Context, seam.Deps, *account.Account, string, cloud.InstanceState) (cloud.Instance, error)) {
	}
	acceptWaitChecks := func(func(context.Context, seam.Deps, *account.Account, string) error) {}
	acceptWaitState(WaitState)
	acceptWaitChecks(WaitChecks)
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
	assertFields(t, err, []string{"Message", "Help"})
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
	// R-T0X5-LSZY
	err := &NotRunningError{Domain: "bar.sbx.ikigenba.dev", State: cloud.StateStopped}
	assertFields(t, err, []string{"Domain", "State"})
	if got, want := err.Error(), "'bar.sbx.ikigenba.dev' is stopped"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestWaitError(t *testing.T) {
	// R-T251-ZKQN
	err := &WaitError{Subject: "i-0c9e94542d98846a8", Want: "be running"}
	assertFields(t, err, []string{"Subject", "Want"})
	if got, want := err.Error(), "timed out waiting for i-0c9e94542d98846a8 to be running"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestRetireStateError(t *testing.T) {
	// R-DXPN-P60P
	err := &RetireStateError{
		ID:      "i-0c9e94542d98846a8",
		State:   cloud.StateStopped,
		Domain:  "foo.sbx.ikigenba.dev",
		Profile: "sandbox",
	}
	assertFields(t, err, []string{"ID", "State", "Domain", "Profile"})
	if got, want := err.Error(), "retire: instance i-0c9e94542d98846a8 is stopped"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got := err.ExitCode(); got != 1 {
		t.Errorf("ExitCode() = %d, want 1", got)
	}
	const wantDetail = "run 'devctl --account sandbox space start foo.sbx.ikigenba.dev' first, or pass --no-backup"
	if got := err.Detail(); got != wantDetail {
		t.Errorf("Detail() = %q, want %q", got, wantDetail)
	}
}

func assertFields(t *testing.T, value any, want []string) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typeOf.Name(), typeOf.NumField(), len(want))
	}
	for index, name := range want {
		if got := typeOf.Field(index).Name; got != name {
			t.Errorf("field %d = %s, want %s", index, got, name)
		}
	}
}
