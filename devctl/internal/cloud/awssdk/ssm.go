package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type ssmAPI interface {
	GetParameter(context.Context, *ssm.GetParameterInput, ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	PutParameter(context.Context, *ssm.PutParameterInput, ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	GetParametersByPath(context.Context, *ssm.GetParametersByPathInput, ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
	DeleteParameter(context.Context, *ssm.DeleteParameterInput, ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
}

type ssmClient struct {
	sdk ssmAPI
}

func (c *ssmClient) GetParameter(ctx context.Context, name string) (string, error) {
	output, err := c.sdk.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", ssmError("GetParameter", name, err)
	}
	if output.Parameter == nil {
		return "", nil
	}
	return aws.ToString(output.Parameter.Value), nil
}

func (c *ssmClient) PutSecureParameter(ctx context.Context, name, value string) error {
	_, err := c.sdk.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(name),
		Value:     aws.String(value),
		Type:      types.ParameterTypeSecureString,
		Overwrite: aws.Bool(true),
	})
	return wrapSSM("PutParameter", name, err)
}

func (c *ssmClient) ListParameters(ctx context.Context, prefix string) ([]cloud.Parameter, error) {
	input := &ssm.GetParametersByPathInput{
		Path:           aws.String(prefix),
		Recursive:      aws.Bool(true),
		WithDecryption: aws.Bool(true),
	}
	var parameters []cloud.Parameter
	for {
		output, err := c.sdk.GetParametersByPath(ctx, input)
		if err != nil {
			return nil, ssmError("GetParametersByPath", prefix, err)
		}
		for _, parameter := range output.Parameters {
			parameters = append(parameters, cloud.Parameter{
				Name:  aws.ToString(parameter.Name),
				Value: aws.ToString(parameter.Value),
			})
		}
		if output.NextToken == nil || *output.NextToken == "" {
			return parameters, nil
		}
		input.NextToken = output.NextToken
	}
}

func (c *ssmClient) DeleteParameter(ctx context.Context, name string) error {
	_, err := c.sdk.DeleteParameter(ctx, &ssm.DeleteParameterInput{Name: aws.String(name)})
	if ssmAPIErrorCode(err) == "ParameterNotFound" {
		return nil
	}
	return wrapSSM("DeleteParameter", name, err)
}

func wrapSSM(operation, subject string, err error) error {
	if err == nil {
		return nil
	}
	return ssmError(operation, subject, err)
}

func ssmError(operation, subject string, err error) *cloud.Error {
	return sdkError("ssm", operation, subject, err)
}

func ssmAPIErrorCode(err error) string {
	return apiErrorCode(err)
}
