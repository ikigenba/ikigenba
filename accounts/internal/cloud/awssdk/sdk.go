// Package awssdk implements the accounts cloud over the AWS SDK.
package awssdk

import (
	"context"
	"net/http"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

// ConfigLoader obtains the SDK configuration for a region.
type ConfigLoader func(ctx context.Context, region string) (aws.Config, error)

type adapter struct {
	ec2    *ec2.Client
	dns    *route53.Client
	sts    *sts.Client
	caller aws.HTTPClient
}

// Open builds the cloud with the SDK's default configuration chain.
func Open(ctx context.Context, region string) (cloud.Cloud, error) {
	return OpenWithLoader(ctx, region, func(ctx context.Context, region string) (aws.Config, error) {
		return config.LoadDefaultConfig(ctx, config.WithRegion(region))
	})
}

// OpenWithLoader builds the cloud with the configuration returned by load.
func OpenWithLoader(ctx context.Context, region string, load ConfigLoader) (cloud.Cloud, error) {
	cfg, err := load(ctx, region)
	if err != nil {
		return nil, err
	}
	// The engine decides when to retry. An allocation, in particular, has
	// no client token, so retrying it here can create a second address.
	cfg.Retryer = func() aws.Retryer { return aws.NopRetryer{} }
	return &adapter{
		ec2:    ec2.NewFromConfig(cfg),
		dns:    route53.NewFromConfig(cfg, func(o *route53.Options) { o.Region = "us-east-1" }),
		sts:    sts.NewFromConfig(cfg, func(o *sts.Options) { o.Region = region }),
		caller: callerClient(cfg.HTTPClient),
	}, nil
}

func callerClient(client aws.HTTPClient) aws.HTTPClient {
	if client == nil {
		client = awshttp.NewBuildableClient()
	}
	if buildable, ok := client.(*awshttp.BuildableClient); ok {
		client = buildable.Freeze()
	}
	if standard, ok := client.(*http.Client); ok {
		single := *standard
		single.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		return &single
	}
	return client
}

func failed(service, op, subject string, err error) error {
	if err == nil {
		return nil
	}
	code, status, foundCode := errorDetails(err)
	if !foundCode {
		code = cloud.CodeNoResponse
		if status != 0 {
			code = httpCode(status)
		}
	}
	return &cloud.Error{
		Service:   service,
		Operation: op,
		Subject:   subject,
		Code:      code,
		Class:     classify(code, status),
		Err:       err,
	}
}

func errorDetails(err error) (string, int, bool) {
	// The SDK wraps each call in OperationError. Unwrap that operation only:
	// a nested operation can be an unrelated credential-provider request.
	if op, ok := any(err).(*smithy.OperationError); ok {
		err = op.Err
	}
	var code string
	var status int
	var foundCode bool
	var visit func(error)
	visit = func(err error) {
		// Inspect only this link before deciding whether to walk its children.
		link := any(err)
		if _, nested := link.(*smithy.OperationError); nested {
			return
		}
		if api, ok := link.(smithy.APIError); ok && !foundCode {
			code, foundCode = api.ErrorCode(), true
		}
		if response, ok := link.(interface{ HTTPStatusCode() int }); ok && status == 0 {
			status = response.HTTPStatusCode()
		}
		switch wrapped := link.(type) {
		case interface{ Unwrap() error }:
			visit(wrapped.Unwrap())
		case interface{ Unwrap() []error }:
			for _, next := range wrapped.Unwrap() {
				visit(next)
			}
		}
	}
	visit(err)
	return code, status, foundCode
}

func classify(code string, status int) cloud.Class {
	if status >= 500 {
		return cloud.ClassTransient
	}
	switch code {
	case "InvalidInstanceID.NotFound", "InvalidVolume.NotFound", "InvalidAllocationID.NotFound",
		"InvalidAssociationID.NotFound", "InvalidVolumeModification.NotFound", "NoSuchChange":
		return cloud.ClassNotFound
	case "IdempotentParameterMismatch":
		return cloud.ClassMismatch
	case "VolumeModificationRateExceeded", "RequestLimitExceeded", "Throttling", "ThrottlingException",
		"PriorRequestNotComplete", "InternalError", "InternalFailure", "ServiceUnavailable",
		"Unavailable", "IncorrectInstanceState", "IncorrectState", cloud.CodeNoResponse:
		return cloud.ClassTransient
	default:
		return cloud.ClassRefused
	}
}

func httpCode(status int) string {
	return "HTTP " + strconv.Itoa(status)
}

func ec2Error(op, subject, code string) *cloud.Error {
	return &cloud.Error{Service: "ec2", Operation: op, Subject: subject, Code: code, Class: classify(code, 0)}
}
