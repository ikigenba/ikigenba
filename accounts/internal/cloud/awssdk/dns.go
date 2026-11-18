package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func (a *adapter) FindRecords(ctx context.Context, zoneID, name string) ([]cloud.Record, error) {
	in := &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID), StartRecordName: aws.String(name),
	}
	records := []cloud.Record{}
	for {
		out, err := a.dns.ListResourceRecordSets(ctx, in)
		if err != nil {
			return nil, failed("route53", "ListResourceRecordSets", name, err)
		}
		for _, item := range out.ResourceRecordSets {
			if !sameRecordName(aws.ToString(item.Name), name) {
				return records, nil
			}
			record := cloud.Record{Name: name, Type: string(item.Type), TTL: aws.ToInt64(item.TTL)}
			if item.AliasTarget == nil {
				for _, v := range item.ResourceRecords {
					record.Values = append(record.Values, aws.ToString(v.Value))
				}
			}
			records = append(records, record)
		}
		if len(out.ResourceRecordSets) == 0 || !out.IsTruncated || !sameRecordName(aws.ToString(out.NextRecordName), name) {
			return records, nil
		}
		in.StartRecordName = out.NextRecordName
		in.StartRecordType = out.NextRecordType
		in.StartRecordIdentifier = out.NextRecordIdentifier
	}
}

func sameRecordName(a, b string) bool {
	a, b = strings.TrimSuffix(a, "."), strings.TrimSuffix(b, ".")
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if asciiLower(a[i]) != asciiLower(b[i]) {
			return false
		}
	}
	return true
}

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func (a *adapter) ChangeRecord(ctx context.Context, zoneID string, c cloud.RecordChange) (string, error) {
	const op = "ChangeResourceRecordSets"
	if c.Action != cloud.RecordCreate && c.Action != cloud.RecordDelete {
		return "", &cloud.Error{Service: "route53", Operation: op, Subject: c.Name, Code: cloud.CodeTagContract, Class: cloud.ClassRefused}
	}
	out, err := a.dns.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch: &types.ChangeBatch{Changes: []types.Change{{
			Action: types.ChangeAction(c.Action),
			ResourceRecordSet: &types.ResourceRecordSet{
				Name: aws.String(c.Name), Type: types.RRTypeA, TTL: aws.Int64(c.TTL),
				ResourceRecords: []types.ResourceRecord{{Value: aws.String(c.IP)}},
			},
		}}},
	})
	if err != nil {
		return "", failed("route53", op, c.Name, err)
	}
	if out.ChangeInfo == nil {
		return "", &cloud.Error{Service: "route53", Operation: op, Subject: c.Name, Code: cloud.CodeBadResponse, Class: cloud.ClassRefused}
	}
	return aws.ToString(out.ChangeInfo.Id), nil
}

func (a *adapter) ChangeInsync(ctx context.Context, changeID string) (bool, error) {
	out, err := a.dns.GetChange(ctx, &route53.GetChangeInput{Id: aws.String(changeID)})
	if err != nil {
		return false, failed("route53", "GetChange", changeID, err)
	}
	return out.ChangeInfo != nil && out.ChangeInfo.Status == types.ChangeStatusInsync, nil
}
