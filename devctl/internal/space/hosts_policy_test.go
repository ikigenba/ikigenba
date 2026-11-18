package space_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func TestHostsPolicyVocabulary(t *testing.T) {
	// R-NKT5-5E6Y
	if space.HostsTagKey != "HostsOperator" || space.HostsTagValue != "true" {
		t.Fatalf("hosts tag = %q=%q", space.HostsTagKey, space.HostsTagValue)
	}
	for root, want := range map[string]string{
		"ikigenba.dev": "ikigenba.dev-hosts-operator",
		"example.test": "example.test-hosts-operator",
	} {
		if got := space.HostsPolicyName(root); got != want {
			t.Fatalf("HostsPolicyName(%q) = %q, want %q", root, got, want)
		}
	}
}

func TestDetachHostsPolicy(t *testing.T) {
	// R-S6LW-KG35 R-S7TS-Y7TU
	_ = []func(context.Context, cloud.IAM, string, string) (bool, error){space.DetachHostsPolicy}
	const root = "ikigenba.dev"
	const domain = "us.ikigenba.dev"
	const arn = "arn:aws:iam::295229566359:policy/ikigenba.dev-hosts-operator"
	lookupErr := errors.New("attachment lookup failed")
	detachErr := errors.New("detachment failed")
	for _, tc := range []struct {
		name      string
		arn       string
		attached  bool
		lookupErr error
		detachErr error
		wantErr   error
		wantOK    bool
	}{
		{name: "attached", arn: arn, attached: true, wantOK: true},
		{name: "attached with path", arn: "arn:aws:iam::123456789012:policy/operators/ikigenba.dev-hosts-operator", attached: true, wantOK: true},
		{name: "absent"},
		{name: "lookup failure", arn: arn, attached: true, lookupErr: lookupErr, wantErr: lookupErr},
		{name: "detach failure", arn: arn, attached: true, detachErr: detachErr, wantErr: detachErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &detachHostsIAM{arn: tc.arn, attached: tc.attached, lookupErr: tc.lookupErr, detachErr: tc.detachErr}
			ok, err := space.DetachHostsPolicy(t.Context(), fake, root, domain)
			if ok != tc.wantOK || !errors.Is(err, tc.wantErr) || reflect.TypeOf(err) != reflect.TypeOf(tc.wantErr) {
				t.Fatalf("detach = (%t, %v), want (%t, %v)", ok, err, tc.wantOK, tc.wantErr)
			}
			want := []string{"AttachedPolicy us.ikigenba.dev ikigenba.dev-hosts-operator"}
			if tc.attached && tc.lookupErr == nil {
				want = append(want, "DetachRolePolicy us.ikigenba.dev "+tc.arn)
			}
			if !slices.Equal(fake.calls, want) {
				t.Fatalf("IAM calls = %v, want %v", fake.calls, want)
			}
		})
	}
}

type detachHostsIAM struct {
	cloud.IAM
	arn       string
	attached  bool
	lookupErr error
	detachErr error
	calls     []string
}

func (f *detachHostsIAM) AttachedPolicy(_ context.Context, role, name string) (string, bool, error) {
	f.calls = append(f.calls, "AttachedPolicy "+role+" "+name)
	return f.arn, f.attached, f.lookupErr
}

func (f *detachHostsIAM) DetachRolePolicy(_ context.Context, role, arn string) error {
	f.calls = append(f.calls, "DetachRolePolicy "+role+" "+arn)
	return f.detachErr
}
