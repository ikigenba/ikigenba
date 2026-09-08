// Package route53 implements the DNS provider seam with Amazon Route 53.
package route53

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsroute53 "github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

// Region is the signing region for Route 53.
const Region = "us-east-1"

const changePollInterval = time.Second

type provider struct {
	client *awsroute53.Client
}

// New constructs the Route 53 provider. A non-empty endpoint overrides the
// service URL while retaining normal SDK signing and credential behavior.
func New(ctx context.Context, endpoint string) (dns.Provider, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	options := func(o *awsroute53.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}
	return &provider{client: awsroute53.NewFromConfig(cfg, options)}, nil
}

// Open resolves a provider name from the process registry.
func Open(ctx context.Context, name string) (dns.Provider, error) {
	if name != "route53" {
		return nil, fmt.Errorf("%w: %s", dns.ErrUnknownProvider, name)
	}
	return New(ctx, "")
}

func (p *provider) Records(ctx context.Context, zoneID string) ([]dns.Record, error) {
	sets, err := p.recordSets(ctx, zoneID)
	if err != nil {
		return nil, err
	}

	records := make([]dns.Record, 0, len(sets))
	for _, set := range sets {
		values := make([]string, 0, len(set.ResourceRecords))
		for _, record := range set.ResourceRecords {
			value := aws.ToString(record.Value)
			if set.Type == types.RRTypeTxt {
				value = unquoteTXT(value)
			}
			values = append(values, value)
		}
		records = append(records, dns.Record{
			Name:   normalizeName(aws.ToString(set.Name)),
			Type:   string(set.Type),
			TTL:    int(aws.ToInt64(set.TTL)),
			Values: values,
		})
	}
	return records, nil
}

func (p *provider) Add(ctx context.Context, zoneID, name, typ string, ttl int, value string) error {
	typ = strings.ToUpper(typ)
	set, found, err := p.findRecordSet(ctx, zoneID, name, typ)
	if err != nil {
		return err
	}
	encoded := encodeValue(typ, value)
	if found {
		for _, record := range set.ResourceRecords {
			if aws.ToString(record.Value) == encoded {
				return nil
			}
		}
		set.ResourceRecords = append(set.ResourceRecords, types.ResourceRecord{Value: aws.String(encoded)})
	} else {
		set = types.ResourceRecordSet{
			Name:            aws.String(name),
			Type:            types.RRType(typ),
			TTL:             aws.Int64(int64(ttl)),
			ResourceRecords: []types.ResourceRecord{{Value: aws.String(encoded)}},
		}
	}
	return p.change(ctx, zoneID, types.ChangeActionUpsert, &set)
}

func (p *provider) Remove(ctx context.Context, zoneID, name, typ, value string) error {
	typ = strings.ToUpper(typ)
	set, found, err := p.findRecordSet(ctx, zoneID, name, typ)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	encoded := encodeValue(typ, value)
	remaining := make([]types.ResourceRecord, 0, len(set.ResourceRecords))
	found = false
	for _, record := range set.ResourceRecords {
		if aws.ToString(record.Value) == encoded {
			found = true
			continue
		}
		remaining = append(remaining, record)
	}
	if !found {
		return nil
	}

	action := types.ChangeActionUpsert
	if len(remaining) == 0 {
		action = types.ChangeActionDelete
	} else {
		set.ResourceRecords = remaining
	}
	return p.change(ctx, zoneID, action, &set)
}

func (p *provider) recordSets(ctx context.Context, zoneID string) ([]types.ResourceRecordSet, error) {
	input := &awsroute53.ListResourceRecordSetsInput{HostedZoneId: aws.String(zoneID)}
	var sets []types.ResourceRecordSet
	for {
		output, err := p.client.ListResourceRecordSets(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("list Route 53 records: %w", err)
		}
		sets = append(sets, output.ResourceRecordSets...)
		if !output.IsTruncated {
			return sets, nil
		}
		input.StartRecordName = output.NextRecordName
		input.StartRecordType = output.NextRecordType
		input.StartRecordIdentifier = output.NextRecordIdentifier
	}
}

func (p *provider) findRecordSet(
	ctx context.Context,
	zoneID, name, typ string,
) (types.ResourceRecordSet, bool, error) {
	sets, err := p.recordSets(ctx, zoneID)
	if err != nil {
		return types.ResourceRecordSet{}, false, err
	}
	name = normalizeName(name)
	for _, set := range sets {
		if normalizeName(aws.ToString(set.Name)) == name && string(set.Type) == typ {
			return set, true, nil
		}
	}
	return types.ResourceRecordSet{}, false, nil
}

func (p *provider) change(
	ctx context.Context,
	zoneID string,
	action types.ChangeAction,
	set *types.ResourceRecordSet,
) error {
	output, err := p.client.ChangeResourceRecordSets(ctx, &awsroute53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch: &types.ChangeBatch{Changes: []types.Change{{
			Action:            action,
			ResourceRecordSet: set,
		}}},
	})
	if err != nil {
		return fmt.Errorf("change Route 53 records: %w", err)
	}
	if output.ChangeInfo == nil || output.ChangeInfo.Id == nil {
		return fmt.Errorf("change Route 53 records: response has no change id")
	}

	changeID := output.ChangeInfo.Id
	for {
		status, getErr := p.client.GetChange(ctx, &awsroute53.GetChangeInput{Id: changeID})
		if getErr != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("wait for Route 53 change: %w", ctx.Err())
			}
			return fmt.Errorf("get Route 53 change: %w", getErr)
		}
		if status.ChangeInfo != nil && status.ChangeInfo.Status == types.ChangeStatusInsync {
			return nil
		}

		timer := time.NewTimer(changePollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for Route 53 change: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func normalizeName(name string) string {
	return strings.ReplaceAll(strings.TrimSuffix(name, "."), `\052`, "*")
}

func unquoteTXT(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	return value
}

func encodeValue(typ, value string) string {
	if typ == string(types.RRTypeTxt) {
		return `"` + value + `"`
	}
	return value
}
