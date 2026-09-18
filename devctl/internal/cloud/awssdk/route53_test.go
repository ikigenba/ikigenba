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
	calls []string
	err   error

	listHostedZones         func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error)
	listResourceRecordSets  func(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error)
	changeResourceRecordSet func(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)
	getChange               func(*route53.GetChangeInput) (*route53.GetChangeOutput, error)
}

func (f *fakeRoute53) ListHostedZones(_ context.Context, input *route53.ListHostedZonesInput, _ ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error) {
	f.calls = append(f.calls, "ListHostedZones")
	if f.listHostedZones != nil {
		return f.listHostedZones(input)
	}
	return nil, f.err
}

func (f *fakeRoute53) ListResourceRecordSets(_ context.Context, input *route53.ListResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error) {
	f.calls = append(f.calls, "ListResourceRecordSets")
	if f.listResourceRecordSets != nil {
		return f.listResourceRecordSets(input)
	}
	return nil, f.err
}

func (f *fakeRoute53) ChangeResourceRecordSets(_ context.Context, input *route53.ChangeResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error) {
	f.calls = append(f.calls, "ChangeResourceRecordSets")
	if f.changeResourceRecordSet != nil {
		return f.changeResourceRecordSet(input)
	}
	return nil, f.err
}

func (f *fakeRoute53) GetChange(_ context.Context, input *route53.GetChangeInput, _ ...func(*route53.Options)) (*route53.GetChangeOutput, error) {
	f.calls = append(f.calls, "GetChange")
	if f.getChange != nil {
		return f.getChange(input)
	}
	return nil, f.err
}

func TestRoute53MappingsPaginationAndNormalization(t *testing.T) {
	// R-YX2B-B7WE
	// R-YYA7-OZN3
	const zoneID = "Z123"
	fake := &fakeRoute53{}
	zonePage := 0
	fake.listHostedZones = func(input *route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
		zonePage++
		switch zonePage {
		case 1:
			if input.Marker != nil {
				t.Fatalf("first zone marker = %q, want nil", aws.ToString(input.Marker))
			}
			return &route53.ListHostedZonesOutput{
				HostedZones: []types.HostedZone{{Id: aws.String("/hostedzone/" + zoneID), Name: aws.String("example.com.")}},
				IsTruncated: true,
				NextMarker:  aws.String("second-zone-page"),
			}, nil
		case 2:
			if aws.ToString(input.Marker) != "second-zone-page" {
				t.Fatalf("second zone marker = %q, want second-zone-page", aws.ToString(input.Marker))
			}
			return &route53.ListHostedZonesOutput{
				HostedZones: []types.HostedZone{{Id: aws.String("Z456"), Name: aws.String("other.example.")}},
			}, nil
		default:
			t.Fatalf("unexpected zone page %d", zonePage)
			return nil, nil
		}
	}

	recordPage := 0
	fake.listResourceRecordSets = func(input *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
		if aws.ToString(input.HostedZoneId) != zoneID {
			t.Fatalf("record zone = %q, want %q", aws.ToString(input.HostedZoneId), zoneID)
		}
		recordPage++
		switch recordPage {
		case 1:
			if input.StartRecordName != nil || input.StartRecordType != "" || input.StartRecordIdentifier != nil {
				t.Fatalf("first record cursor = %#v/%q/%#v, want empty", input.StartRecordName, input.StartRecordType, input.StartRecordIdentifier)
			}
			return &route53.ListResourceRecordSetsOutput{
				ResourceRecordSets: []types.ResourceRecordSet{{
					Name: aws.String(`\052.api.example.com.`), Type: types.RRTypeA, TTL: aws.Int64(60),
					ResourceRecords: []types.ResourceRecord{{Value: aws.String("192.0.2.1")}},
				}},
				IsTruncated:          true,
				NextRecordName:       aws.String("mail.example.com."),
				NextRecordType:       types.RRTypeMx,
				NextRecordIdentifier: aws.String("weighted-two"),
			}, nil
		case 2:
			if aws.ToString(input.StartRecordName) != "mail.example.com." || input.StartRecordType != types.RRTypeMx || aws.ToString(input.StartRecordIdentifier) != "weighted-two" {
				t.Fatalf("second record cursor = %q/%q/%q, want API continuation", aws.ToString(input.StartRecordName), input.StartRecordType, aws.ToString(input.StartRecordIdentifier))
			}
			return &route53.ListResourceRecordSetsOutput{
				ResourceRecordSets: []types.ResourceRecordSet{{
					Name: aws.String("mail.example.com."), Type: types.RRTypeMx, TTL: aws.Int64(300),
					ResourceRecords: []types.ResourceRecord{{Value: aws.String("10 mx1.example.com.")}, {Value: aws.String("20 mx2.example.com.")}},
				}},
			}, nil
		default:
			t.Fatalf("unexpected record page %d", recordPage)
			return nil, nil
		}
	}

	zones, err := (&route53Client{sdk: fake}).ListZones(context.Background())
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	wantZones := []cloud.Zone{{ID: zoneID, Name: "example.com"}, {ID: "Z456", Name: "other.example"}}
	if !reflect.DeepEqual(zones, wantZones) {
		t.Fatalf("zones = %#v, want %#v", zones, wantZones)
	}
	records, err := (&route53Client{sdk: fake}).ListRecords(context.Background(), zoneID)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	wantRecords := []cloud.Record{
		{Name: `\052.api.example.com`, Type: "A", TTL: 60, Values: []string{"192.0.2.1"}},
		{Name: "mail.example.com", Type: "MX", TTL: 300, Values: []string{"10 mx1.example.com.", "20 mx2.example.com."}},
	}
	if !reflect.DeepEqual(records, wantRecords) {
		t.Fatalf("records = %#v, want %#v", records, wantRecords)
	}
	wantCalls := []string{"ListHostedZones", "ListHostedZones", "ListResourceRecordSets", "ListResourceRecordSets"}
	if !reflect.DeepEqual(fake.calls, wantCalls) {
		t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, wantCalls)
	}
}

func TestRoute53ChangeBatchAndStatusRoundTrip(t *testing.T) {
	const (
		zoneID   = "Z123"
		changeID = "/change/C456"
	)
	changes := []cloud.RecordChange{
		{Action: cloud.ChangeDelete, Record: cloud.Record{Name: `\052.api.example.com`, Type: "A", TTL: 60, Values: []string{"192.0.2.1"}}},
		{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "www.example.com", Type: "AAAA", TTL: 120, Values: []string{"2001:db8::1", "2001:db8::2"}}},
	}
	fake := &fakeRoute53{}
	fake.changeResourceRecordSet = func(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
		if aws.ToString(input.HostedZoneId) != zoneID {
			t.Fatalf("change zone = %q, want %q", aws.ToString(input.HostedZoneId), zoneID)
		}
		want := []types.Change{
			{Action: types.ChangeActionDelete, ResourceRecordSet: &types.ResourceRecordSet{Name: aws.String(`\052.api.example.com`), Type: types.RRTypeA, TTL: aws.Int64(60), ResourceRecords: []types.ResourceRecord{{Value: aws.String("192.0.2.1")}}}},
			{Action: types.ChangeActionUpsert, ResourceRecordSet: &types.ResourceRecordSet{Name: aws.String("www.example.com"), Type: types.RRTypeAaaa, TTL: aws.Int64(120), ResourceRecords: []types.ResourceRecord{{Value: aws.String("2001:db8::1")}, {Value: aws.String("2001:db8::2")}}}},
		}
		if input.ChangeBatch == nil || !reflect.DeepEqual(input.ChangeBatch.Changes, want) {
			t.Fatalf("change batch = %#v, want one batch %#v", input.ChangeBatch, want)
		}
		return &route53.ChangeResourceRecordSetsOutput{ChangeInfo: &types.ChangeInfo{Id: aws.String(changeID), Status: types.ChangeStatusPending}}, nil
	}
	fake.getChange = func(input *route53.GetChangeInput) (*route53.GetChangeOutput, error) {
		if aws.ToString(input.Id) != changeID {
			t.Fatalf("change id = %q, want unchanged %q", aws.ToString(input.Id), changeID)
		}
		return &route53.GetChangeOutput{ChangeInfo: &types.ChangeInfo{Id: aws.String(changeID), Status: types.ChangeStatusInsync}}, nil
	}

	client := &route53Client{sdk: fake}
	gotID, err := client.ChangeRecords(context.Background(), zoneID, changes)
	if err != nil || gotID != changeID {
		t.Fatalf("ChangeRecords = %q, %v; want %q, nil", gotID, err, changeID)
	}
	status, err := client.ChangeStatus(context.Background(), gotID)
	if err != nil || status != cloud.ChangeInsync {
		t.Fatalf("ChangeStatus = %q, %v; want %q, nil", status, err, cloud.ChangeInsync)
	}
	wantCalls := []string{"ChangeResourceRecordSets", "GetChange"}
	if !reflect.DeepEqual(fake.calls, wantCalls) {
		t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, wantCalls)
	}
}

func TestRoute53EscapedNameReadDeleteRoundTrip(t *testing.T) {
	const escapedName = `\052.api.example.com`
	fake := &fakeRoute53{}
	fake.listResourceRecordSets = func(_ *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
		return &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []types.ResourceRecordSet{{
			Name: aws.String(escapedName + "."), Type: types.RRTypeA, TTL: aws.Int64(60),
			ResourceRecords: []types.ResourceRecord{{Value: aws.String("192.0.2.1")}},
		}}}, nil
	}
	fake.changeResourceRecordSet = func(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
		got := input.ChangeBatch.Changes[0].ResourceRecordSet
		if aws.ToString(got.Name) != escapedName {
			t.Fatalf("delete name = %q, want escaped name unchanged %q", aws.ToString(got.Name), escapedName)
		}
		return &route53.ChangeResourceRecordSetsOutput{ChangeInfo: &types.ChangeInfo{Id: aws.String("C1")}}, nil
	}

	client := &route53Client{sdk: fake}
	records, err := client.ListRecords(context.Background(), "Z1")
	if err != nil || len(records) != 1 {
		t.Fatalf("ListRecords = %#v, %v; want one record, nil", records, err)
	}
	_, err = client.ChangeRecords(context.Background(), "Z1", []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: records[0]}})
	if err != nil {
		t.Fatalf("ChangeRecords: %v", err)
	}
	if want := []string{"ListResourceRecordSets", "ChangeResourceRecordSets"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
	}
}

func TestRoute53ErrorMappingAndExactDispatch(t *testing.T) {
	// R-YOJ0-MTPJ R-YPQX-0LG8
	boom := &smithy.GenericAPIError{Code: "Throttling", Message: "slow down"}
	tests := []struct {
		name      string
		operation string
		call      func(*route53Client) error
	}{
		{"ListZones", "ListHostedZones", func(client *route53Client) error { _, err := client.ListZones(context.Background()); return err }},
		{"ListRecords", "ListResourceRecordSets", func(client *route53Client) error {
			_, err := client.ListRecords(context.Background(), "Z1")
			return err
		}},
		{"ChangeRecords", "ChangeResourceRecordSets", func(client *route53Client) error {
			_, err := client.ChangeRecords(context.Background(), "Z1", nil)
			return err
		}},
		{"ChangeStatus", "GetChange", func(client *route53Client) error {
			_, err := client.ChangeStatus(context.Background(), "C1")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeRoute53{err: boom}
			var got *cloud.Error
			if err := test.call(&route53Client{sdk: fake}); !errors.As(err, &got) {
				t.Fatalf("error = %v, want *cloud.Error", err)
			}
			if got.Service != "route53" || got.Operation != test.operation || got.Subject != "" || got.Code != boom.ErrorCode() || !errors.Is(got, boom) {
				t.Fatalf("error = %#v, want route53 %s with no subject and code %s wrapping SDK error", got, test.operation, boom.ErrorCode())
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}
}

func TestRoute53NonAPIErrorHasEmptyCode(t *testing.T) {
	boom := errors.New("transport failed")
	fake := &fakeRoute53{err: boom}
	_, err := (&route53Client{sdk: fake}).ListZones(context.Background())
	var got *cloud.Error
	if !errors.As(err, &got) || got.Code != "" || !errors.Is(got, boom) {
		t.Fatalf("error = %#v, want code-empty *cloud.Error wrapping transport error", err)
	}
}
