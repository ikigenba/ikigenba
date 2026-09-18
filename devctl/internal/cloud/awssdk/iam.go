package awssdk

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

const iamNoSuchEntity = "NoSuchEntity"

type iamAPI interface {
	GetRole(context.Context, *iam.GetRoleInput, ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	CreateRole(context.Context, *iam.CreateRoleInput, ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	PutRolePolicy(context.Context, *iam.PutRolePolicyInput, ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	DeleteRolePolicy(context.Context, *iam.DeleteRolePolicyInput, ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	GetInstanceProfile(context.Context, *iam.GetInstanceProfileInput, ...func(*iam.Options)) (*iam.GetInstanceProfileOutput, error)
	CreateInstanceProfile(context.Context, *iam.CreateInstanceProfileInput, ...func(*iam.Options)) (*iam.CreateInstanceProfileOutput, error)
	AddRoleToInstanceProfile(context.Context, *iam.AddRoleToInstanceProfileInput, ...func(*iam.Options)) (*iam.AddRoleToInstanceProfileOutput, error)
	RemoveRoleFromInstanceProfile(context.Context, *iam.RemoveRoleFromInstanceProfileInput, ...func(*iam.Options)) (*iam.RemoveRoleFromInstanceProfileOutput, error)
	DeleteInstanceProfile(context.Context, *iam.DeleteInstanceProfileInput, ...func(*iam.Options)) (*iam.DeleteInstanceProfileOutput, error)
	DeleteRole(context.Context, *iam.DeleteRoleInput, ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
}

type iamClient struct {
	sdk iamAPI
}

var _ cloud.IAM = (*iamClient)(nil)

func (c *iamClient) RoleExists(ctx context.Context, name string) (bool, error) {
	_, err := c.sdk.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
	if iamAPIErrorCode(err) == iamNoSuchEntity {
		return false, nil
	}
	if err != nil {
		return false, iamError("GetRole", err)
	}
	return true, nil
}

func (c *iamClient) CreateRole(ctx context.Context, spec cloud.RoleSpec) error {
	_, err := c.sdk.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(spec.Name),
		AssumeRolePolicyDocument: aws.String(spec.AssumeRolePolicy),
		PermissionsBoundary:      aws.String(spec.PermissionsBoundaryARN),
	})
	return wrapIAM("CreateRole", err)
}

func (c *iamClient) PutRolePolicy(ctx context.Context, role, policy, document string) error {
	_, err := c.sdk.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       aws.String(role),
		PolicyName:     aws.String(policy),
		PolicyDocument: aws.String(document),
	})
	return wrapIAM("PutRolePolicy", err)
}

func (c *iamClient) DeleteRolePolicy(ctx context.Context, role, policy string) error {
	_, err := c.sdk.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
		RoleName:   aws.String(role),
		PolicyName: aws.String(policy),
	})
	return wrapIAMDelete("DeleteRolePolicy", err)
}

func (c *iamClient) InstanceProfileRoles(ctx context.Context, name string) ([]string, bool, error) {
	output, err := c.sdk.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	if iamAPIErrorCode(err) == iamNoSuchEntity {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, iamError("GetInstanceProfile", err)
	}

	var roles []string
	if output.InstanceProfile != nil {
		roles = make([]string, 0, len(output.InstanceProfile.Roles))
		for _, role := range output.InstanceProfile.Roles {
			roles = append(roles, aws.ToString(role.RoleName))
		}
	}
	return roles, true, nil
}

func (c *iamClient) CreateInstanceProfile(ctx context.Context, name string) error {
	_, err := c.sdk.CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	return wrapIAM("CreateInstanceProfile", err)
}

func (c *iamClient) AddRoleToInstanceProfile(ctx context.Context, profile, role string) error {
	_, err := c.sdk.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: aws.String(profile),
		RoleName:            aws.String(role),
	})
	return wrapIAM("AddRoleToInstanceProfile", err)
}

func (c *iamClient) RemoveRoleFromInstanceProfile(ctx context.Context, profile, role string) error {
	_, err := c.sdk.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(profile),
		RoleName:            aws.String(role),
	})
	return wrapIAMDelete("RemoveRoleFromInstanceProfile", err)
}

func (c *iamClient) DeleteInstanceProfile(ctx context.Context, name string) error {
	_, err := c.sdk.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	return wrapIAMDelete("DeleteInstanceProfile", err)
}

func (c *iamClient) DeleteRole(ctx context.Context, name string) error {
	_, err := c.sdk.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: aws.String(name)})
	return wrapIAMDelete("DeleteRole", err)
}

func wrapIAM(operation string, err error) error {
	if err == nil {
		return nil
	}
	return iamError(operation, err)
}

func wrapIAMDelete(operation string, err error) error {
	if iamAPIErrorCode(err) == iamNoSuchEntity {
		return nil
	}
	return wrapIAM(operation, err)
}

func iamError(operation string, err error) *cloud.Error {
	return &cloud.Error{
		Service:   "iam",
		Operation: operation,
		Code:      iamAPIErrorCode(err),
		Err:       err,
	}
}

func iamAPIErrorCode(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}
	return ""
}
