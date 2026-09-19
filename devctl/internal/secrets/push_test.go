package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/keyring"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type pushWrite struct {
	name  string
	value string
}

type pushSSM struct {
	writes []pushWrite
}

func (*pushSSM) GetParameter(context.Context, string) (string, error) { return "", nil }

func (ssm *pushSSM) PutSecureParameter(_ context.Context, name, value string) error {
	ssm.writes = append(ssm.writes, pushWrite{name: name, value: value})
	return nil
}

func (*pushSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) { return nil, nil }
func (*pushSSM) DeleteParameter(context.Context, string) error                     { return nil }

// R-GEUR-2NW1
func TestPushGathersEveryValueBeforeWriting(t *testing.T) {
	t.Parallel()

	apps := []checkout.App{
		{Name: "crm", Manifest: checkout.Manifest{Secrets: []string{"CRM_TOKEN"}}},
		{Name: "dashboard", Manifest: checkout.Manifest{Secrets: []string{"DASHBOARD_TOKEN"}}},
		{Name: "gmail", Manifest: checkout.Manifest{Secrets: []string{"GMAIL_TOKEN"}}},
	}
	values := map[string]string{
		"CRM_TOKEN":       "crm-value",
		"DASHBOARD_TOKEN": "dashboard-value",
	}
	var lookedUp []string
	deps := seam.Deps{
		Dir: t.TempDir(),
		Getenv: func(name string) string {
			lookedUp = append(lookedUp, name)
			return values[name]
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{ExitCode: 1}, nil
		},
	}
	ssm := &pushSSM{}

	entries, err := Push(context.Background(), deps, ssm, "foo.sbx.ikigenba.dev", apps)
	if err == nil {
		t.Fatal("Push returned nil error")
	}
	if len(entries) != 0 {
		t.Fatalf("Push entries = %#v, want none", entries)
	}
	if len(ssm.writes) != 0 {
		t.Fatalf("PutSecureParameter calls = %#v, want none", ssm.writes)
	}
	if want := []string{"CRM_TOKEN", "DASHBOARD_TOKEN", "GMAIL_TOKEN"}; !reflect.DeepEqual(lookedUp, want) {
		t.Fatalf("lookups = %#v, want %#v", lookedUp, want)
	}
	var noValue *keyring.NoValueError
	if !errors.As(err, &noValue) {
		t.Fatalf("error %T does not wrap *keyring.NoValueError: %v", err, err)
	}
	if !strings.HasPrefix(err.Error(), "gmail: ") {
		t.Fatalf("error = %q, want gmail prefix", err)
	}
}

// R-GG2N-GFMQ
func TestPushWritesOneExactJSONObjectPerApp(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"ALPHA": "line one\nline two",
		"OMEGA": `quotes " and \\ stay exact`,
	}
	deps := seam.Deps{Getenv: func(name string) string { return values[name] }}
	ssm := &pushSSM{}
	apps := []checkout.App{
		{Name: "crm", Manifest: checkout.Manifest{Secrets: []string{"OMEGA", "ALPHA", "ALPHA"}}},
		{Name: "dashboard", Manifest: checkout.Manifest{}},
	}

	_, err := Push(context.Background(), deps, ssm, "foo.sbx.ikigenba.dev", apps)
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	wantNames := []string{
		"/foo.sbx.ikigenba.dev/crm",
		"/foo.sbx.ikigenba.dev/dashboard",
	}
	if len(ssm.writes) != len(wantNames) {
		t.Fatalf("PutSecureParameter call count = %d, want %d", len(ssm.writes), len(wantNames))
	}
	for i, want := range wantNames {
		if ssm.writes[i].name != want {
			t.Errorf("write %d name = %q, want %q", i, ssm.writes[i].name, want)
		}
	}
	var object map[string]string
	if err := json.Unmarshal([]byte(ssm.writes[0].value), &object); err != nil {
		t.Fatalf("first value is not JSON: %v", err)
	}
	if !reflect.DeepEqual(object, values) {
		t.Errorf("first object = %#v, want %#v", object, values)
	}
	if count := strings.Count(ssm.writes[0].value, `"ALPHA":`); count != 1 {
		t.Errorf("first object contains ALPHA %d times, want 1: %s", count, ssm.writes[0].value)
	}
	if ssm.writes[1].value != "{}" {
		t.Errorf("empty object = %q, want %q", ssm.writes[1].value, "{}")
	}
}
