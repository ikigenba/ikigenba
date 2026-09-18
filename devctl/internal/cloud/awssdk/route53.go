package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type route53API interface {
	ListHostedZones(context.Context, *route53.ListHostedZonesInput, ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error)
	ListResourceRecordSets(context.Context, *route53.ListResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error)
	ChangeResourceRecordSets(context.Context, *route53.ChangeResourceRecordSetsInput, ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error)
	GetChange(context.Context, *route53.GetChangeInput, ...func(*route53.Options)) (*route53.GetChangeOutput, error)
}

type route53Client struct {
	sdk route53API
}

func (c *route53Client) ListZones(ctx context.Context) ([]cloud.Zone, error) {
	input := &route53.ListHostedZonesInput{}
	var zones []cloud.Zone
	for {
		output, err := c.sdk.ListHostedZones(ctx, input)
		if err != nil {
			return nil, route53Error("ListHostedZones", err)
		}
		for _, zone := range output.HostedZones {
			zones = append(zones, cloud.Zone{
				ID:   strings.TrimPrefix(aws.ToString(zone.Id), "/hostedzone/"),
				Name: strings.TrimSuffix(aws.ToString(zone.Name), "."),
			})
		}
		if !output.IsTruncated {
			return zones, nil
		}
		input.Marker = output.NextMarker
	}
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
			values := make([]string, len(record.ResourceRecords))
			for i, value := range record.ResourceRecords {
				values[i] = aws.ToString(value.Value)
			}
			records = append(records, cloud.Record{
				Name:   strings.TrimSuffix(aws.ToString(record.Name), "."),
				Type:   string(record.Type),
				TTL:    aws.ToInt64(record.TTL),
				Values: values,
			})
		}
		if !output.IsTruncated {
			return records, nil
		}
		input.StartRecordName = output.NextRecordName
		input.StartRecordType = output.NextRecordType
		input.StartRecordIdentifier = output.NextRecordIdentifier
	}
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
	var apiErr smithy.APIError
	code := ""
	if errors.As(err, &apiErr) {
		code = apiErr.ErrorCode()
	}
	return &cloud.Error{
		Service:   "route53",
		Operation: operation,
		Code:      code,
		Err:       err,
	}
}
