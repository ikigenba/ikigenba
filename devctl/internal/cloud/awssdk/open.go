// Package awssdk implements the cloud boundary with the AWS SDK.
package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

// ConfigLoader loads AWS configuration for a profile and optional region.
type ConfigLoader func(ctx context.Context, profile, region string) (aws.Config, error)

var _ cloud.Opener = Open

// Open loads shared AWS configuration and constructs cloud clients.
func Open(ctx context.Context, profile, region string) (cloud.Clients, error) {
	return OpenWithLoader(ctx, profile, region, loadSharedConfig)
}

// OpenWithLoader constructs cloud clients from configuration returned by load.
func OpenWithLoader(
	ctx context.Context,
	profile, region string,
	load ConfigLoader,
) (cloud.Clients, error) {
	cfg, err := load(ctx, profile, region)
	if err != nil {
		return cloud.Clients{}, err
	}

	return cloud.Clients{
		EC2:     &ec2Client{sdk: ec2.NewFromConfig(cfg)},
		SSM:     &ssmClient{sdk: ssm.NewFromConfig(cfg)},
		Route53: &route53Client{sdk: route53.NewFromConfig(cfg)},
		S3:      &s3Client{sdk: s3.NewFromConfig(cfg)},
		IAM:     &iamClient{sdk: iam.NewFromConfig(cfg)},
		STS:     &stsClient{sdk: sts.NewFromConfig(cfg)},
	}, nil
}

func loadSharedConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	options := []func(*config.LoadOptions) error{
		config.WithSharedConfigProfile(profile),
	}
	if region != "" {
		options = append(options, config.WithRegion(region))
	}
	return config.LoadDefaultConfig(ctx, options...)
}
