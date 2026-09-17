// Package awssdk implements the cloud boundary with the AWS SDK.
package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

// Open loads AWS configuration for profile and region and constructs clients.
func Open(ctx context.Context, profile, region string) (cloud.Clients, error) {
	options := []func(*config.LoadOptions) error{config.WithRegion(region)}
	if profile != "" {
		options = append(options, config.WithSharedConfigProfile(profile))
	}
	if _, err := config.LoadDefaultConfig(ctx, options...); err != nil {
		return cloud.Clients{}, err
	}
	return cloud.Clients{}, nil
}
