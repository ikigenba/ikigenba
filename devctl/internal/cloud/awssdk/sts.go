package awssdk

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

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
	var apiErr smithy.APIError
	code := ""
	if errors.As(err, &apiErr) {
		code = apiErr.ErrorCode()
	}
	return &cloud.Error{
		Service:   "sts",
		Operation: "GetCallerIdentity",
		Code:      code,
		Err:       err,
	}
}
