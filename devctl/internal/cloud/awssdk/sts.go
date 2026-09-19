package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type stsAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

type stsClient struct {
	sdk stsAPI
}

func (c *stsClient) CallerAccountID(ctx context.Context) (string, error) {
	output, err := c.sdk.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", stsError(err)
	}
	if output == nil {
		return "", nil
	}
	return aws.ToString(output.Account), nil
}

func stsError(err error) *cloud.Error {
	return sdkError("sts", "GetCallerIdentity", "", err)
}
