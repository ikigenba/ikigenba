package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

const iamNoSuchEntity = "NoSuchEntity"

type iamAPI interface {
	ListPolicies(context.Context, *iam.ListPoliciesInput, ...func(*iam.Options)) (*iam.ListPoliciesOutput, error)
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
	ListAttachedRolePolicies(context.Context, *iam.ListAttachedRolePoliciesInput, ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error)
	AttachRolePolicy(context.Context, *iam.AttachRolePolicyInput, ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	DetachRolePolicy(context.Context, *iam.DetachRolePolicyInput, ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error)
	TagRole(context.Context, *iam.TagRoleInput, ...func(*iam.Options)) (*iam.TagRoleOutput, error)
	UntagRole(context.Context, *iam.UntagRoleInput, ...func(*iam.Options)) (*iam.UntagRoleOutput, error)
	ListRoleTags(context.Context, *iam.ListRoleTagsInput, ...func(*iam.Options)) (*iam.ListRoleTagsOutput, error)
	ListRoles(context.Context, *iam.ListRolesInput, ...func(*iam.Options)) (*iam.ListRolesOutput, error)
}

type iamClient struct {
	sdk iamAPI
}

var _ cloud.IAM = (*iamClient)(nil)

func (c *iamClient) PermissionsBoundary(ctx context.Context, name string) (string, error) {
	return c.findPolicy(ctx, name, "permissions boundary", false)
}

func (c *iamClient) ManagedPolicy(ctx context.Context, name string) (string, error) {
	return c.findPolicy(ctx, name, "managed policy", true)
}

func (c *iamClient) findPolicy(ctx context.Context, name, kind string, stopOnMatch bool) (string, error) {
	input := &iam.ListPoliciesInput{Scope: types.PolicyScopeTypeLocal}
	var arn string
	found := false
	for {
		output, err := c.sdk.ListPolicies(ctx, input)
		if err != nil {
			return "", iamError("ListPolicies", err)
		}
		for _, policy := range output.Policies {
			if aws.ToString(policy.PolicyName) == name {
				arn, found = aws.ToString(policy.Arn), true
				if stopOnMatch {
					return arn, nil
				}
			}
		}
		if !output.IsTruncated || output.Marker == nil || aws.ToString(output.Marker) == "" {
			if found {
				return arn, nil
			}
			return "", &cloud.NotFoundError{Kind: kind, Name: name}
		}
		input.Marker = output.Marker
	}
}

func (c *iamClient) AttachedPolicy(ctx context.Context, role, name string) (string, bool, error) {
	input := &iam.ListAttachedRolePoliciesInput{RoleName: aws.String(role)}
	for {
		output, err := c.sdk.ListAttachedRolePolicies(ctx, input)
		if iamAPIErrorCode(err) == iamNoSuchEntity {
			return "", false, nil
		}
		if err != nil {
			return "", false, iamError("ListAttachedRolePolicies", err)
		}
		for _, policy := range output.AttachedPolicies {
			if aws.ToString(policy.PolicyName) == name {
				return aws.ToString(policy.PolicyArn), true, nil
			}
		}
		if !output.IsTruncated || aws.ToString(output.Marker) == "" {
			return "", false, nil
		}
		input.Marker = output.Marker
	}
}

func (c *iamClient) AttachRolePolicy(ctx context.Context, role, arn string) error {
	_, err := c.sdk.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(role),
		PolicyArn: aws.String(arn),
	})
	return wrapIAM("AttachRolePolicy", err)
}

func (c *iamClient) DetachRolePolicy(ctx context.Context, role, arn string) error {
	_, err := c.sdk.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
		RoleName:  aws.String(role),
		PolicyArn: aws.String(arn),
	})
	return wrapIAM("DetachRolePolicy", err)
}

func (c *iamClient) TagRole(ctx context.Context, role, key, value string) error {
	_, err := c.sdk.TagRole(ctx, &iam.TagRoleInput{
		RoleName: aws.String(role),
		Tags:     []types.Tag{{Key: aws.String(key), Value: aws.String(value)}},
	})
	return wrapIAM("TagRole", err)
}

func (c *iamClient) UntagRole(ctx context.Context, role, key string) error {
	_, err := c.sdk.UntagRole(ctx, &iam.UntagRoleInput{
		RoleName: aws.String(role),
		TagKeys:  []string{key},
	})
	return wrapIAM("UntagRole", err)
}

func (c *iamClient) RoleTag(ctx context.Context, role, key string) (string, bool, error) {
	var value string
	var found bool
	exists, err := c.listRoleTags(ctx, role, func(tag types.Tag) bool {
		if aws.ToString(tag.Key) != key {
			return true
		}
		value, found = aws.ToString(tag.Value), true
		return false
	})
	if !exists || err != nil {
		return "", false, err
	}
	return value, found, nil
}

func (c *iamClient) RolesTagged(ctx context.Context, key, value string) ([]string, error) {
	input := &iam.ListRolesInput{}
	var names []string
	for {
		output, err := c.sdk.ListRoles(ctx, input)
		if err != nil {
			return nil, iamError("ListRoles", err)
		}
		for _, role := range output.Roles {
			name := aws.ToString(role.RoleName)
			found := false
			exists, err := c.listRoleTags(ctx, name, func(tag types.Tag) bool {
				if aws.ToString(tag.Key) == key && aws.ToString(tag.Value) == value {
					found = true
				}
				return true
			})
			if err != nil {
				return nil, err
			}
			if exists && found {
				names = append(names, name)
			}
		}
		if !output.IsTruncated || aws.ToString(output.Marker) == "" {
			return names, nil
		}
		input.Marker = output.Marker
	}
}

func (c *iamClient) listRoleTags(ctx context.Context, role string, each func(types.Tag) bool) (bool, error) {
	input := &iam.ListRoleTagsInput{RoleName: aws.String(role)}
	for {
		output, err := c.sdk.ListRoleTags(ctx, input)
		if iamAPIErrorCode(err) == iamNoSuchEntity {
			return false, nil
		}
		if err != nil {
			return false, iamError("ListRoleTags", err)
		}
		for _, tag := range output.Tags {
			if !each(tag) {
				return true, nil
			}
		}
		if !output.IsTruncated || aws.ToString(output.Marker) == "" {
			return true, nil
		}
		input.Marker = output.Marker
	}
}

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
	return sdkError("iam", operation, "", err)
}

func iamAPIErrorCode(err error) string {
	return apiErrorCode(err)
}
