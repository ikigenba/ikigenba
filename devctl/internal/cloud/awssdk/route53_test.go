package awssdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type fakeRoute53 struct {
	calls       []string
	err         error
	listZones   func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error)
	listRecords func(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error)
	change      func(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)
	getChange   func(*route53.GetChangeInput) (*route53.GetChangeOutput, error)
}

func (f *fakeRoute53) ListHostedZonesByName(_ context.Context, in *route53.ListHostedZonesByNameInput, _ ...func(*route53.Options)) (*route53.ListHostedZonesByNameOutput, error) {
	f.calls = append(f.calls, "ListHostedZonesByName")
	if f.listZones != nil {
		return f.listZones(in)
	}
	return nil, f.err
}
func (f *fakeRoute53) ListResourceRecordSets(_ context.Context, in *route53.ListResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error) {
	f.calls = append(f.calls, "ListResourceRecordSets")
	if f.listRecords != nil {
		return f.listRecords(in)
	}
	return nil, f.err
}
func (f *fakeRoute53) ChangeResourceRecordSets(_ context.Context, in *route53.ChangeResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error) {
	f.calls = append(f.calls, "ChangeResourceRecordSets")
	if f.change != nil {
		return f.change(in)
	}
	return nil, f.err
}
func (f *fakeRoute53) GetChange(_ context.Context, in *route53.GetChangeInput, _ ...func(*route53.Options)) (*route53.GetChangeOutput, error) {
	f.calls = append(f.calls, "GetChange")
	if f.getChange != nil {
		return f.getChange(in)
	}
	return nil, f.err
}

func TestRoute53ChangeBatchAndStatus(t *testing.T) {
	// R-QR9B-B8CQ R-BH55-J2NI
	changes := []cloud.RecordChange{
		{Action: cloud.ChangeDelete, Record: cloud.Record{Name: `\052.api.root.example`, Type: "A", TTL: 60, Values: []string{"192.0.2.1"}}},
		{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "www.root.example", Type: "AAAA", TTL: 300, Values: []string{"2001:db8::1", "2001:db8::2"}}},
	}
	wantChanges := []types.Change{
		{Action: types.ChangeActionDelete, ResourceRecordSet: &types.ResourceRecordSet{Name: aws.String(`\052.api.root.example`), Type: types.RRTypeA, TTL: aws.Int64(60), ResourceRecords: []types.ResourceRecord{{Value: aws.String("192.0.2.1")}}}},
		{Action: types.ChangeActionUpsert, ResourceRecordSet: &types.ResourceRecordSet{Name: aws.String("www.root.example"), Type: types.RRTypeAaaa, TTL: aws.Int64(300), ResourceRecords: []types.ResourceRecord{{Value: aws.String("2001:db8::1")}, {Value: aws.String("2001:db8::2")}}}},
	}
	fake := &fakeRoute53{}
	fake.change = func(in *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
		if aws.ToString(in.HostedZoneId) != "Z123" {
			t.Fatalf("HostedZoneId = %q, want Z123", aws.ToString(in.HostedZoneId))
		}
		if in.ChangeBatch == nil || !reflect.DeepEqual(in.ChangeBatch.Changes, wantChanges) {
			t.Fatalf("change batch = %#v, want %#v", in.ChangeBatch, wantChanges)
		}
		return &route53.ChangeResourceRecordSetsOutput{ChangeInfo: &types.ChangeInfo{Id: aws.String("/change/C456")}}, nil
	}
	fake.getChange = func(in *route53.GetChangeInput) (*route53.GetChangeOutput, error) {
		if aws.ToString(in.Id) != "/change/C456" {
			t.Fatalf("change id = %q, want /change/C456", aws.ToString(in.Id))
		}
		return &route53.GetChangeOutput{ChangeInfo: &types.ChangeInfo{Status: types.ChangeStatusInsync}}, nil
	}

	client := &route53Client{sdk: fake}
	changeID, err := client.ChangeRecords(context.Background(), "Z123", changes)
	if err != nil || changeID != "/change/C456" {
		t.Fatalf("ChangeRecords = %q, %v; want /change/C456, nil", changeID, err)
	}
	status, err := client.ChangeStatus(context.Background(), changeID)
	if err != nil || status != cloud.ChangeInsync {
		t.Fatalf("ChangeStatus = %q, %v; want %q, nil", status, err, cloud.ChangeInsync)
	}
	if want := []string{"ChangeResourceRecordSets", "GetChange"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want exactly %v", fake.calls, want)
	}
}

func TestRoute53EscapedListDeleteRoundTrip(t *testing.T) {
	// R-QUX0-GJKT R-BH55-J2NI
	const escapedName = `\052.api.root.example`
	wantRecord := cloud.Record{Name: escapedName, Type: "A", TTL: 60, Values: []string{"192.0.2.1", "192.0.2.2"}}
	fake := &fakeRoute53{}
	fake.listRecords = func(in *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
		if aws.ToString(in.HostedZoneId) != "Z1" {
			t.Fatalf("HostedZoneId = %q, want Z1", aws.ToString(in.HostedZoneId))
		}
		return &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []types.ResourceRecordSet{{
			Name: aws.String(escapedName + "."), Type: types.RRTypeA, TTL: aws.Int64(60),
			ResourceRecords: []types.ResourceRecord{{Value: aws.String("192.0.2.1")}, {Value: aws.String("192.0.2.2")}},
		}}}, nil
	}
	fake.change = func(in *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
		want := []types.Change{{
			Action: types.ChangeActionDelete,
			ResourceRecordSet: &types.ResourceRecordSet{
				Name: aws.String(escapedName), Type: types.RRTypeA, TTL: aws.Int64(60),
				ResourceRecords: []types.ResourceRecord{{Value: aws.String("192.0.2.1")}, {Value: aws.String("192.0.2.2")}},
			},
		}}
		if aws.ToString(in.HostedZoneId) != "Z1" || in.ChangeBatch == nil || !reflect.DeepEqual(in.ChangeBatch.Changes, want) {
			t.Fatalf("delete input = %#v, want zone Z1 and change %#v", in, want)
		}
		return &route53.ChangeResourceRecordSetsOutput{ChangeInfo: &types.ChangeInfo{Id: aws.String("C1")}}, nil
	}

	client := &route53Client{sdk: fake}
	records, err := client.ListRecords(context.Background(), "Z1")
	if err != nil || !reflect.DeepEqual(records, []cloud.Record{wantRecord}) {
		t.Fatalf("ListRecords = %#v, %v; want %#v, nil", records, err, []cloud.Record{wantRecord})
	}
	if _, err := client.ChangeRecords(context.Background(), "Z1", []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: records[0]}}); err != nil {
		t.Fatalf("ChangeRecords: %v", err)
	}
	if want := []string{"ListResourceRecordSets", "ChangeResourceRecordSets"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want exactly %v", fake.calls, want)
	}
}

func TestRoute53ZoneSelectionAbsenceAndError(t *testing.T) {
	// R-QSH7-P03F R-BH55-J2NI
	t.Run("first matching zone after nonmatches", func(t *testing.T) {
		fake := &fakeRoute53{listZones: func(in *route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			if aws.ToString(in.DNSName) != "root.example" {
				t.Fatalf("DNSName = %q, want root.example", aws.ToString(in.DNSName))
			}
			return &route53.ListHostedZonesByNameOutput{HostedZones: []types.HostedZone{
				{Id: aws.String("/hostedzone/Z0"), Name: aws.String("other.example.")},
				{Id: aws.String("/hostedzone/Z1"), Name: aws.String("root.example.")},
				{Id: aws.String("/hostedzone/Z2"), Name: aws.String("root.example.")},
			}}, nil
		}}
		zone, err := (&route53Client{sdk: fake}).Zone(context.Background(), "root.example")
		if err != nil || zone != (cloud.Zone{ID: "Z1", Name: "root.example"}) {
			t.Fatalf("Zone = %#v, %v; want first matching zone, nil", zone, err)
		}
		if want := []string{"ListHostedZonesByName"}; !reflect.DeepEqual(fake.calls, want) {
			t.Fatalf("calls = %v, want exactly %v", fake.calls, want)
		}
	})

	t.Run("absence", func(t *testing.T) {
		fake := &fakeRoute53{listZones: func(*route53.ListHostedZonesByNameInput) (*route53.ListHostedZonesByNameOutput, error) {
			return &route53.ListHostedZonesByNameOutput{HostedZones: []types.HostedZone{{Id: aws.String("Z0"), Name: aws.String("other.example.")}}}, nil
		}}
		_, err := (&route53Client{sdk: fake}).Zone(context.Background(), "missing.example")
		var missing *cloud.NotFoundError
		if !errors.As(err, &missing) || missing.Kind != "hosted zone" || missing.Name != "missing.example" {
			t.Fatalf("Zone error = %#v", err)
		}
	})

	t.Run("SDK error", func(t *testing.T) {
		boom := &smithy.GenericAPIError{Code: "Throttling", Message: "slow"}
		_, err := (&route53Client{sdk: &fakeRoute53{err: boom}}).Zone(context.Background(), "root.example")
		var cloudErr *cloud.Error
		if !errors.As(err, &cloudErr) || cloudErr.Service != "route53" || cloudErr.Operation != "ListHostedZonesByName" || cloudErr.Subject != "" || cloudErr.Code != "Throttling" || !errors.Is(cloudErr, boom) {
			t.Fatalf("Zone error = %#v", err)
		}
	})
}

func TestRoute53FindRecordMatchMissesAndError(t *testing.T) {
	// R-QTP4-2RU4 R-BH55-J2NI
	const escapedName = `\052.api.root.example`
	wantRecord := cloud.Record{Name: escapedName, Type: "A", TTL: 60, Values: []string{"192.0.2.1"}}
	tests := []struct {
		name   string
		output *route53.ListResourceRecordSetsOutput
		err    error
		want   cloud.Record
		found  bool
	}{
		{name: "match", output: &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []types.ResourceRecordSet{{Name: aws.String(escapedName + "."), Type: types.RRTypeA, TTL: aws.Int64(60), ResourceRecords: []types.ResourceRecord{{Value: aws.String("192.0.2.1")}}}}}, want: wantRecord, found: true},
		{name: "empty", output: &route53.ListResourceRecordSetsOutput{}},
		{name: "nonempty name mismatch", output: &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []types.ResourceRecordSet{{Name: aws.String("other.root.example."), Type: types.RRTypeA, TTL: aws.Int64(60)}}}},
		{name: "type mismatch", output: &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []types.ResourceRecordSet{{Name: aws.String(escapedName + "."), Type: types.RRTypeAaaa, TTL: aws.Int64(60)}}}},
		{name: "SDK error", err: &smithy.GenericAPIError{Code: "Throttling", Message: "slow"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeRoute53{listRecords: func(in *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
				if aws.ToString(in.HostedZoneId) != "Z1" || aws.ToString(in.StartRecordName) != "*.api.root.example" || in.StartRecordType != types.RRTypeA || aws.ToInt32(in.MaxItems) != 1 {
					t.Fatalf("find input = %#v", in)
				}
				return test.output, test.err
			}}
			got, found, err := (&route53Client{sdk: fake}).FindRecord(context.Background(), "Z1", "*.api.root.example", "A")
			if want := []string{"ListResourceRecordSets"}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("calls = %v, want exactly %v", fake.calls, want)
			}
			if test.err != nil {
				var cloudErr *cloud.Error
				if !errors.As(err, &cloudErr) || cloudErr.Service != "route53" || cloudErr.Operation != "ListResourceRecordSets" || cloudErr.Subject != "" || cloudErr.Code != "Throttling" || !errors.Is(cloudErr, test.err) {
					t.Fatalf("FindRecord error = %#v", err)
				}
				return
			}
			if err != nil || found != test.found || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("FindRecord = %#v, %v, %v; want %#v, %v, nil", got, found, err, test.want, test.found)
			}
		})
	}
}

func TestRoute53ErrorMapping(t *testing.T) {
	// R-BFX9-5AWT R-BH55-J2NI
	ctx := t.Context()
	cause := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
	for _, test := range []struct {
		name      string
		operation string
		call      func(cloud.Route53) error
	}{
		{"Zone", "ListHostedZonesByName", func(client cloud.Route53) error {
			_, err := client.Zone(ctx, "root.example")
			return err
		}},
		{"ListRecords", "ListResourceRecordSets", func(client cloud.Route53) error {
			_, err := client.ListRecords(ctx, "Z1")
			return err
		}},
		{"FindRecord", "ListResourceRecordSets", func(client cloud.Route53) error {
			_, _, err := client.FindRecord(ctx, "Z1", "root.example", "A")
			return err
		}},
		{"ChangeRecords", "ChangeResourceRecordSets", func(client cloud.Route53) error {
			_, err := client.ChangeRecords(ctx, "Z1", []cloud.RecordChange{{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "root.example", Type: "A", TTL: 60, Values: []string{"192.0.2.1"}}}})
			return err
		}},
		{"ChangeStatus", "GetChange", func(client cloud.Route53) error {
			_, err := client.ChangeStatus(ctx, "C1")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeRoute53{err: &smithy.OperationError{ServiceID: "Route 53", OperationName: test.operation, Err: cause}}
			err := test.call(&route53Client{sdk: fake})
			var got *cloud.Error
			if !errors.As(err, &got) || got.Service != "route53" || got.Operation != test.operation ||
				got.Subject != "" || got.Code != cause.ErrorCode() || !errors.Is(got.Err, cause) || got.Err.Error() != cause.Error() {
				t.Fatalf("error = %#v, want route53 %s with no subject and code %s wrapping the SDK cause", err, test.operation, cause.ErrorCode())
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}
}
