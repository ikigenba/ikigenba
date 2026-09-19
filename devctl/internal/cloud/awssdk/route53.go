package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type route53API interface {
	ListHostedZonesByName(context.Context, *route53.ListHostedZonesByNameInput, ...func(*route53.Options)) (*route53.ListHostedZonesByNameOutput, error)
	ListResourceRecordSets(context.Context, *route53.ListResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error)
	ChangeResourceRecordSets(context.Context, *route53.ChangeResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error)
	GetChange(context.Context, *route53.GetChangeInput, ...func(*route53.Options)) (*route53.GetChangeOutput, error)
}

type route53Client struct {
	sdk route53API
}

var _ cloud.Route53 = (*route53Client)(nil)

func (c *route53Client) Zone(ctx context.Context, name string) (cloud.Zone, error) {
	output, err := c.sdk.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{DNSName: aws.String(name)})
	if err != nil {
		return cloud.Zone{}, route53Error("ListHostedZonesByName", err)
	}
	for _, zone := range output.HostedZones {
		zoneName := strings.TrimSuffix(aws.ToString(zone.Name), ".")
		if zoneName == name {
			return cloud.Zone{ID: strings.TrimPrefix(aws.ToString(zone.Id), "/hostedzone/"), Name: zoneName}, nil
		}
	}
	return cloud.Zone{}, &cloud.NotFoundError{Kind: "hosted zone", Name: name}
}

func (c *route53Client) ListRecords(ctx context.Context, zoneID string) ([]cloud.Record, error) {
	input := &route53.ListResourceRecordSetsInput{HostedZoneId: aws.String(zoneID)}
	var records []cloud.Record
	for {
		output, err := c.sdk.ListResourceRecordSets(ctx, input)
		if err != nil {
			return nil, route53Error("ListResourceRecordSets", err)
		}
		for _, record := range output.ResourceRecordSets {
			records = append(records, cloudRecord(record))
		}
		if !output.IsTruncated {
			return records, nil
		}
		input.StartRecordName = output.NextRecordName
		input.StartRecordType = output.NextRecordType
		input.StartRecordIdentifier = output.NextRecordIdentifier
	}
}

func (c *route53Client) FindRecord(ctx context.Context, zoneID, name, recordType string) (cloud.Record, bool, error) {
	output, err := c.sdk.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID), StartRecordName: aws.String(name),
		StartRecordType: types.RRType(recordType), MaxItems: aws.Int32(1),
	})
	if err != nil {
		return cloud.Record{}, false, route53Error("ListResourceRecordSets", err)
	}
	if len(output.ResourceRecordSets) == 0 {
		return cloud.Record{}, false, nil
	}
	record := cloudRecord(output.ResourceRecordSets[0])
	comparableName := record.Name
	if strings.HasPrefix(comparableName, `\052`) {
		comparableName = "*" + strings.TrimPrefix(comparableName, `\052`)
	}
	if comparableName != name || record.Type != recordType {
		return cloud.Record{}, false, nil
	}
	return record, true, nil
}

func cloudRecord(record types.ResourceRecordSet) cloud.Record {
	values := make([]string, len(record.ResourceRecords))
	for i, value := range record.ResourceRecords {
		values[i] = aws.ToString(value.Value)
	}
	return cloud.Record{Name: strings.TrimSuffix(aws.ToString(record.Name), "."), Type: string(record.Type), TTL: aws.ToInt64(record.TTL), Values: values}
}

func (c *route53Client) ChangeRecords(ctx context.Context, zoneID string, changes []cloud.RecordChange) (string, error) {
	sdkChanges := make([]types.Change, len(changes))
	for i, change := range changes {
		values := make([]types.ResourceRecord, len(change.Record.Values))
		for j, value := range change.Record.Values {
			values[j] = types.ResourceRecord{Value: aws.String(value)}
		}
		sdkChanges[i] = types.Change{
			Action: types.ChangeAction(change.Action),
			ResourceRecordSet: &types.ResourceRecordSet{
				Name:            aws.String(change.Record.Name),
				Type:            types.RRType(change.Record.Type),
				TTL:             aws.Int64(change.Record.TTL),
				ResourceRecords: values,
			},
		}
	}

	output, err := c.sdk.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  &types.ChangeBatch{Changes: sdkChanges},
	})
	if err != nil {
		return "", route53Error("ChangeResourceRecordSets", err)
	}
	if output.ChangeInfo == nil {
		return "", nil
	}
	return aws.ToString(output.ChangeInfo.Id), nil
}

func (c *route53Client) ChangeStatus(ctx context.Context, changeID string) (cloud.ChangeStatus, error) {
	output, err := c.sdk.GetChange(ctx, &route53.GetChangeInput{Id: aws.String(changeID)})
	if err != nil {
		return "", route53Error("GetChange", err)
	}
	if output.ChangeInfo == nil {
		return "", nil
	}
	return cloud.ChangeStatus(output.ChangeInfo.Status), nil
}

func route53Error(operation string, err error) *cloud.Error {
	return sdkError("route53", operation, "", err)
}
