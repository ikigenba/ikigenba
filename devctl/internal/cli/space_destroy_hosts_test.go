package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func TestDestroyHostsPolicyThroughCLI(t *testing.T) {
	// R-S1QB-1D4D R-NM11-J5XN
	const sbxID = "i-0c9e94542d98846a8"
	const stagingID = "i-0a1b2c3d4e5f60718"
	const sbxIP = "18.118.7.42"
	const stagingIP = "18.220.10.5"
	const retireOK = "retire: ok (opsctl retire)\n"
	const retireSkip = "retire: skipped (--no-backup)\n"
	const kept = "secrets: ok (kept)\nbackups: ok (kept)\n"
	detachErr := errors.New("detach failed")
	recordsErr := &cloud.Error{Service: "route53", Operation: "ChangeResourceRecordSets", Code: "Throttling"}
	for _, tc := range []struct {
		name, root, label, id, ip, retire string
		apex, tagged, skip, attached      bool
		detachErr, recordsErr             error
	}{
		{name: "apex holder", root: "ikigenba.dev", label: "sbx1", id: sbxID, ip: sbxIP, retire: retireOK, apex: true, attached: true},
		{name: "tagged", root: "ikigenba.dev", label: "staging", id: stagingID, ip: stagingIP, retire: retireOK, tagged: true, attached: true},
		{name: "untagged", root: "ikigenba.dev", label: "staging", id: stagingID, ip: stagingIP, retire: retireOK, attached: true},
		{name: "skip backup", root: "ikigenba.dev", label: "staging", id: stagingID, ip: stagingIP, retire: retireSkip, skip: true, attached: true},
		{name: "other root", root: "example.test", label: "sbx1", id: sbxID, ip: sbxIP, retire: retireOK, attached: true},
		{name: "no policy", root: "ikigenba.dev", label: "staging", id: stagingID, ip: stagingIP, retire: retireOK, tagged: true},
		{name: "detach fails", root: "ikigenba.dev", label: "staging", id: stagingID, ip: stagingIP, retire: retireOK, attached: true, detachErr: detachErr},
		{name: "records fail", root: "ikigenba.dev", label: "sbx1", id: sbxID, ip: sbxIP, retire: retireSkip, skip: true, attached: true, recordsErr: recordsErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			domain := tc.label + "." + tc.root
			name := tc.root + "-hosts-operator"
			arn := "arn:aws:iam::295229566359:policy/" + name
			f := &destroyHostsCloud{
				domain: domain, policy: name, id: tc.id, ip: tc.ip, apex: tc.apex,
				tagged: tc.tagged, detachErr: tc.detachErr, recordsErr: tc.recordsErr,
				records: []cloud.Record{
					{Name: domain, Type: "A", TTL: 60, Values: []string{tc.ip}},
					{Name: "*." + domain, Type: "A", TTL: 60, Values: []string{tc.ip}},
				},
			}
			if tc.attached {
				f.arn = arn
			}
			deps := destroyHostsDeps(t, tc.root, f)
			args := []string{"space", "destroy", tc.label}
			if tc.skip {
				args = append(args, "--no-backup")
			}
			want := tc.retire
			if tc.apex {
				want = "apex: ok (" + tc.root + " record deleted)\n" + want
			}
			want += "instance: ok (" + tc.id + " terminated)\naddress: ok (elastic ip " + tc.ip + " released)\n"

			got := invokeWithDeps(deps, args...)
			if tc.recordsErr != nil {
				assertResult(t, got, 1, want, "devctl: route53 ChangeResourceRecordSets: Throttling\n")
				if len(f.iamCalls) != 0 {
					t.Fatalf("IAM calls after records failure = %v", f.iamCalls)
				}
				return
			}
			want += "records: ok (deleted " + domain + ", *." + domain + ")\n" + kept

			wantIAM := []string{"AttachedPolicy " + domain + " " + name}
			if tc.attached {
				wantIAM = append(wantIAM, "DetachRolePolicy "+domain+" "+arn)
			}
			if tc.detachErr != nil {
				assertResult(t, got, 1, want, "devctl: detach failed\n")
			} else {
				if tc.attached {
					want += "hosts: ok (" + name + " detached)\n"
				}
				want += "role: ok (" + domain + " deleted)\n"
				assertResult(t, got, 0, want, "")
				wantIAM = append(wantIAM, "RoleExists", "InstanceProfileRoles", "DeleteRolePolicy", "RemoveRoleFromInstanceProfile", "DeleteInstanceProfile", "DeleteRole")
			}
			if !slices.Equal(f.iamCalls, wantIAM) {
				t.Fatalf("IAM calls = %v, want %v", f.iamCalls, wantIAM)
			}
			if tc.detachErr != nil {
				if !f.terminated || !f.released || len(f.records) != 0 || f.arn != arn {
					t.Fatalf("partial destroy did not leave only the attached role: %+v", f)
				}
				// Check error identity on a separate copy of the partial state.
				partial := *f
				partial.iamCalls = nil
				err := space.Run(t.Context(), args[1:], io.Discard, destroyHostsDeps(t, tc.root, &partial))
				if !errors.Is(err, tc.detachErr) || reflect.TypeOf(err) != reflect.TypeOf(tc.detachErr) {
					t.Fatalf("destroy error = %v, want original %v", err, tc.detachErr)
				}
				f.detachErr = nil
				f.iamCalls = nil
				got = invokeWithDeps(deps, args...)
				want = "retire: ok (already gone)\ninstance: ok (already gone)\naddress: ok (already gone)\nrecords: ok (already gone)\n" +
					kept + "hosts: ok (ikigenba.dev-hosts-operator detached)\nrole: ok (staging.ikigenba.dev deleted)\n"
				assertResult(t, got, 0, want, "")
				wantIAM = append(wantIAM, "RoleExists", "InstanceProfileRoles", "DeleteRolePolicy", "RemoveRoleFromInstanceProfile", "DeleteInstanceProfile", "DeleteRole")
				if !slices.Equal(f.iamCalls, wantIAM) {
					t.Fatalf("resumed IAM calls = %v, want %v", f.iamCalls, wantIAM)
				}
			}
		})
	}
}

func destroyHostsDeps(t *testing.T, root string, f *destroyHostsCloud) seam.Deps {
	t.Helper()
	deps := checkoutDeps(t, fmt.Sprintf(`{"domain": "%s", "region": "eu-west-1"}`, root))
	exec := deps.Exec
	deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
		if cmd.Path == "git" {
			return exec(ctx, cmd)
		}
		if cmd.Path != "ssh" || cmd.Args[len(cmd.Args)-2] != "ec2-user@"+f.domain ||
			cmd.Args[len(cmd.Args)-1] != "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'opsctl' 'retire'" {
			t.Fatalf("unexpected host command %+v", cmd)
		}
		return seam.Result{}, nil
	}
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		return cloud.Clients{EC2: f, Route53: f, IAM: f, STS: &cliSTS{}}, nil
	}
	return deps
}

type destroyHostsCloud struct {
	cloud.EC2
	cloud.IAM
	cloud.Route53
	domain, policy, arn, id, ip string
	apex, tagged                bool
	terminated, released        bool
	records                     []cloud.Record
	detachErr, recordsErr       error
	iamCalls                    []string
}

func (f *destroyHostsCloud) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	if f.terminated {
		return nil, nil
	}
	return []cloud.Instance{{ID: f.id, State: cloud.StateRunning, Space: f.domain}}, nil
}

func (f *destroyHostsCloud) ListSpaceAddresses(context.Context, string) ([]cloud.Address, error) {
	if f.released {
		return nil, nil
	}
	return []cloud.Address{{IP: f.ip, AllocationID: "eipalloc-01", Space: f.domain}}, nil
}

func (f *destroyHostsCloud) TerminateInstance(_ context.Context, id string) error {
	if f.terminated || id != f.id {
		return fmt.Errorf("unexpected instance termination: %s", id)
	}
	f.terminated = true
	return nil
}

func (f *destroyHostsCloud) DescribeInstance(context.Context, string) (cloud.Instance, error) {
	return cloud.Instance{ID: f.id, State: cloud.StateTerminated}, nil
}

func (f *destroyHostsCloud) ReleaseAddress(_ context.Context, id string) error {
	if f.released || id != "eipalloc-01" {
		return fmt.Errorf("unexpected address release: %s", id)
	}
	f.released = true
	return nil
}

func (*destroyHostsCloud) Zone(_ context.Context, root string) (cloud.Zone, error) {
	return cloud.Zone{ID: "ZROOT", Name: root}, nil
}

func (f *destroyHostsCloud) FindRecord(_ context.Context, _, name, kind string) (cloud.Record, bool, error) {
	return cloud.Record{Name: name, Type: kind, TTL: 60, Values: []string{f.ip}}, f.apex, nil
}

func (f *destroyHostsCloud) ListRecords(context.Context, string) ([]cloud.Record, error) {
	return f.records, nil
}

func (f *destroyHostsCloud) ChangeRecords(_ context.Context, _ string, changes []cloud.RecordChange) (string, error) {
	if f.recordsErr != nil {
		return "", f.recordsErr
	}
	for _, change := range changes {
		if change.Action != cloud.ChangeDelete {
			return "", fmt.Errorf("unexpected record change: %+v", change)
		}
		f.records = slices.DeleteFunc(f.records, func(record cloud.Record) bool {
			return record.Name == change.Record.Name && record.Type == change.Record.Type
		})
		if change.Record.Name != f.domain && change.Record.Name != "*."+f.domain {
			f.apex = false
		}
	}
	return "", nil
}

func (f *destroyHostsCloud) AttachedPolicy(_ context.Context, role, name string) (string, bool, error) {
	f.iamCalls = append(f.iamCalls, "AttachedPolicy "+role+" "+name)
	return f.arn, f.arn != "" && role == f.domain && name == f.policy, nil
}

func (f *destroyHostsCloud) DetachRolePolicy(_ context.Context, role, arn string) error {
	f.iamCalls = append(f.iamCalls, "DetachRolePolicy "+role+" "+arn)
	if f.detachErr != nil {
		return f.detachErr
	}
	f.arn = ""
	return nil
}

func (f *destroyHostsCloud) RoleTag(context.Context, string, string) (string, bool, error) {
	f.iamCalls = append(f.iamCalls, "RoleTag")
	if f.tagged {
		return "true", true, nil
	}
	return "", false, nil
}

func (f *destroyHostsCloud) TagRole(context.Context, string, string, string) error {
	f.iamCalls = append(f.iamCalls, "TagRole")
	return nil
}

func (f *destroyHostsCloud) UntagRole(context.Context, string, string) error {
	f.iamCalls = append(f.iamCalls, "UntagRole")
	return nil
}

func (f *destroyHostsCloud) RoleExists(context.Context, string) (bool, error) {
	f.iamCalls = append(f.iamCalls, "RoleExists")
	return true, nil
}

func (f *destroyHostsCloud) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	f.iamCalls = append(f.iamCalls, "InstanceProfileRoles")
	return []string{f.domain}, true, nil
}

func (f *destroyHostsCloud) DeleteRolePolicy(context.Context, string, string) error {
	f.iamCalls = append(f.iamCalls, "DeleteRolePolicy")
	return nil
}

func (f *destroyHostsCloud) RemoveRoleFromInstanceProfile(context.Context, string, string) error {
	f.iamCalls = append(f.iamCalls, "RemoveRoleFromInstanceProfile")
	return nil
}

func (f *destroyHostsCloud) DeleteInstanceProfile(context.Context, string) error {
	f.iamCalls = append(f.iamCalls, "DeleteInstanceProfile")
	return nil
}

func (f *destroyHostsCloud) DeleteRole(context.Context, string) error {
	f.iamCalls = append(f.iamCalls, "DeleteRole")
	return nil
}
