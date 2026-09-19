package awssdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type fakeIAM struct {
	calls []string
	err   error

	getRole                       func(*iam.GetRoleInput) (*iam.GetRoleOutput, error)
	createRole                    func(*iam.CreateRoleInput) (*iam.CreateRoleOutput, error)
	putRolePolicy                 func(*iam.PutRolePolicyInput) (*iam.PutRolePolicyOutput, error)
	deleteRolePolicy              func(*iam.DeleteRolePolicyInput) (*iam.DeleteRolePolicyOutput, error)
	getInstanceProfile            func(*iam.GetInstanceProfileInput) (*iam.GetInstanceProfileOutput, error)
	createInstanceProfile         func(*iam.CreateInstanceProfileInput) (*iam.CreateInstanceProfileOutput, error)
	addRoleToInstanceProfile      func(*iam.AddRoleToInstanceProfileInput) (*iam.AddRoleToInstanceProfileOutput, error)
	removeRoleFromInstanceProfile func(*iam.RemoveRoleFromInstanceProfileInput) (*iam.RemoveRoleFromInstanceProfileOutput, error)
	deleteInstanceProfile         func(*iam.DeleteInstanceProfileInput) (*iam.DeleteInstanceProfileOutput, error)
	deleteRole                    func(*iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error)
	listPolicies                  func(*iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error)
}

func (f *fakeIAM) ListPolicies(_ context.Context, input *iam.ListPoliciesInput, _ ...func(*iam.Options)) (*iam.ListPoliciesOutput, error) {
	f.calls = append(f.calls, "ListPolicies")
	if f.listPolicies != nil {
		return f.listPolicies(input)
	}
	return nil, f.err
}

func (f *fakeIAM) GetRole(_ context.Context, input *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	f.calls = append(f.calls, "GetRole")
	if f.getRole != nil {
		return f.getRole(input)
	}
	return nil, f.err
}

func (f *fakeIAM) CreateRole(_ context.Context, input *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	f.calls = append(f.calls, "CreateRole")
	if f.createRole != nil {
		return f.createRole(input)
	}
	return nil, f.err
}

func (f *fakeIAM) PutRolePolicy(_ context.Context, input *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	f.calls = append(f.calls, "PutRolePolicy")
	if f.putRolePolicy != nil {
		return f.putRolePolicy(input)
	}
	return nil, f.err
}

func (f *fakeIAM) DeleteRolePolicy(_ context.Context, input *iam.DeleteRolePolicyInput, _ ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	f.calls = append(f.calls, "DeleteRolePolicy")
	if f.deleteRolePolicy != nil {
		return f.deleteRolePolicy(input)
	}
	return nil, f.err
}

func (f *fakeIAM) GetInstanceProfile(_ context.Context, input *iam.GetInstanceProfileInput, _ ...func(*iam.Options)) (*iam.GetInstanceProfileOutput, error) {
	f.calls = append(f.calls, "GetInstanceProfile")
	if f.getInstanceProfile != nil {
		return f.getInstanceProfile(input)
	}
	return nil, f.err
}

func (f *fakeIAM) CreateInstanceProfile(_ context.Context, input *iam.CreateInstanceProfileInput, _ ...func(*iam.Options)) (*iam.CreateInstanceProfileOutput, error) {
	f.calls = append(f.calls, "CreateInstanceProfile")
	if f.createInstanceProfile != nil {
		return f.createInstanceProfile(input)
	}
	return nil, f.err
}

func (f *fakeIAM) AddRoleToInstanceProfile(_ context.Context, input *iam.AddRoleToInstanceProfileInput, _ ...func(*iam.Options)) (*iam.AddRoleToInstanceProfileOutput, error) {
	f.calls = append(f.calls, "AddRoleToInstanceProfile")
	if f.addRoleToInstanceProfile != nil {
		return f.addRoleToInstanceProfile(input)
	}
	return nil, f.err
}

func (f *fakeIAM) RemoveRoleFromInstanceProfile(_ context.Context, input *iam.RemoveRoleFromInstanceProfileInput, _ ...func(*iam.Options)) (*iam.RemoveRoleFromInstanceProfileOutput, error) {
	f.calls = append(f.calls, "RemoveRoleFromInstanceProfile")
	if f.removeRoleFromInstanceProfile != nil {
		return f.removeRoleFromInstanceProfile(input)
	}
	return nil, f.err
}

func (f *fakeIAM) DeleteInstanceProfile(_ context.Context, input *iam.DeleteInstanceProfileInput, _ ...func(*iam.Options)) (*iam.DeleteInstanceProfileOutput, error) {
	f.calls = append(f.calls, "DeleteInstanceProfile")
	if f.deleteInstanceProfile != nil {
		return f.deleteInstanceProfile(input)
	}
	return nil, f.err
}

func (f *fakeIAM) DeleteRole(_ context.Context, input *iam.DeleteRoleInput, _ ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	f.calls = append(f.calls, "DeleteRole")
	if f.deleteRole != nil {
		return f.deleteRole(input)
	}
	return nil, f.err
}

func TestIAMPermissionsBoundary(t *testing.T) {
	// R-QW4W-UBBI R-QXCT-8327 R-QL5T-EDN9
	page := 0
	fake := &fakeIAM{listPolicies: func(in *iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error) {
		if in.Scope != types.PolicyScopeTypeLocal {
			t.Fatalf("scope = %q, want Local", in.Scope)
		}
		page++
		if page == 1 {
			if in.Marker != nil {
				t.Fatalf("first marker = %q", aws.ToString(in.Marker))
			}
			return &iam.ListPoliciesOutput{Policies: []types.Policy{{PolicyName: aws.String("other")}}, IsTruncated: true, Marker: aws.String("next")}, nil
		}
		if aws.ToString(in.Marker) != "next" {
			t.Fatalf("second marker = %q", aws.ToString(in.Marker))
		}
		return &iam.ListPoliciesOutput{Policies: []types.Policy{{PolicyName: aws.String("boundary"), Arn: aws.String("arn:boundary")}}}, nil
	}}
	arn, err := (&iamClient{sdk: fake}).PermissionsBoundary(context.Background(), "boundary")
	if err != nil || arn != "arn:boundary" {
		t.Fatalf("PermissionsBoundary = %q, %v", arn, err)
	}
	if want := []string{"ListPolicies", "ListPolicies"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}

	missing := &fakeIAM{listPolicies: func(*iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error) { return &iam.ListPoliciesOutput{}, nil }}
	_, err = (&iamClient{sdk: missing}).PermissionsBoundary(context.Background(), "missing")
	var notFound *cloud.NotFoundError
	if !errors.As(err, &notFound) || notFound.Kind != "permissions boundary" || notFound.Name != "missing" {
		t.Fatalf("missing error = %#v", err)
	}
}

func TestIAMMappingInputsAndResults(t *testing.T) {
	// R-QW4W-UBBI
	const (
		role     = "ikigenba-role"
		profile  = "ikigenba-profile"
		policy   = "ikigenba-policy"
		document = `{"Version":"2012-10-17"}`
		boundary = "arn:aws:iam::123456789012:policy/boundary"
	)
	fake := &fakeIAM{}
	fake.getRole = func(input *iam.GetRoleInput) (*iam.GetRoleOutput, error) {
		assertIAMString(t, "GetRole role", input.RoleName, role)
		return &iam.GetRoleOutput{}, nil
	}
	fake.createRole = func(input *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
		want := &iam.CreateRoleInput{
			RoleName:                 aws.String(role),
			AssumeRolePolicyDocument: aws.String(document),
			PermissionsBoundary:      aws.String(boundary),
		}
		if !reflect.DeepEqual(input, want) {
			t.Fatalf("CreateRole input = %#v, want exactly %#v", input, want)
		}
		return &iam.CreateRoleOutput{}, nil
	}
	fake.putRolePolicy = func(input *iam.PutRolePolicyInput) (*iam.PutRolePolicyOutput, error) {
		assertIAMString(t, "PutRolePolicy role", input.RoleName, role)
		assertIAMString(t, "PutRolePolicy policy", input.PolicyName, policy)
		assertIAMString(t, "PutRolePolicy document", input.PolicyDocument, document)
		return &iam.PutRolePolicyOutput{}, nil
	}
	fake.deleteRolePolicy = func(input *iam.DeleteRolePolicyInput) (*iam.DeleteRolePolicyOutput, error) {
		assertIAMString(t, "DeleteRolePolicy role", input.RoleName, role)
		assertIAMString(t, "DeleteRolePolicy policy", input.PolicyName, policy)
		return &iam.DeleteRolePolicyOutput{}, nil
	}
	fake.getInstanceProfile = func(input *iam.GetInstanceProfileInput) (*iam.GetInstanceProfileOutput, error) {
		assertIAMString(t, "GetInstanceProfile profile", input.InstanceProfileName, profile)
		return &iam.GetInstanceProfileOutput{InstanceProfile: &types.InstanceProfile{
			Roles: []types.Role{{RoleName: aws.String("role-one")}, {RoleName: aws.String("role-two")}},
		}}, nil
	}
	fake.createInstanceProfile = func(input *iam.CreateInstanceProfileInput) (*iam.CreateInstanceProfileOutput, error) {
		assertIAMString(t, "CreateInstanceProfile profile", input.InstanceProfileName, profile)
		return &iam.CreateInstanceProfileOutput{}, nil
	}
	fake.addRoleToInstanceProfile = func(input *iam.AddRoleToInstanceProfileInput) (*iam.AddRoleToInstanceProfileOutput, error) {
		assertIAMString(t, "AddRoleToInstanceProfile profile", input.InstanceProfileName, profile)
		assertIAMString(t, "AddRoleToInstanceProfile role", input.RoleName, role)
		return &iam.AddRoleToInstanceProfileOutput{}, nil
	}
	fake.removeRoleFromInstanceProfile = func(input *iam.RemoveRoleFromInstanceProfileInput) (*iam.RemoveRoleFromInstanceProfileOutput, error) {
		assertIAMString(t, "RemoveRoleFromInstanceProfile profile", input.InstanceProfileName, profile)
		assertIAMString(t, "RemoveRoleFromInstanceProfile role", input.RoleName, role)
		return &iam.RemoveRoleFromInstanceProfileOutput{}, nil
	}
	fake.deleteInstanceProfile = func(input *iam.DeleteInstanceProfileInput) (*iam.DeleteInstanceProfileOutput, error) {
		assertIAMString(t, "DeleteInstanceProfile profile", input.InstanceProfileName, profile)
		return &iam.DeleteInstanceProfileOutput{}, nil
	}
	fake.deleteRole = func(input *iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error) {
		assertIAMString(t, "DeleteRole role", input.RoleName, role)
		return &iam.DeleteRoleOutput{}, nil
	}

	client := &iamClient{sdk: fake}
	exists, err := client.RoleExists(context.Background(), role)
	if err != nil || !exists {
		t.Fatalf("RoleExists = %v, %v; want true, nil", exists, err)
	}
	if err := client.CreateRole(context.Background(), cloud.RoleSpec{
		Name: role, AssumeRolePolicy: document, PermissionsBoundaryARN: boundary,
	}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := client.PutRolePolicy(context.Background(), role, policy, document); err != nil {
		t.Fatalf("PutRolePolicy: %v", err)
	}
	if err := client.DeleteRolePolicy(context.Background(), role, policy); err != nil {
		t.Fatalf("DeleteRolePolicy: %v", err)
	}
	roles, exists, err := client.InstanceProfileRoles(context.Background(), profile)
	if err != nil || !exists || !reflect.DeepEqual(roles, []string{"role-one", "role-two"}) {
		t.Fatalf("InstanceProfileRoles = %v, %v, %v; want role names, true, nil", roles, exists, err)
	}
	if err := client.CreateInstanceProfile(context.Background(), profile); err != nil {
		t.Fatalf("CreateInstanceProfile: %v", err)
	}
	if err := client.AddRoleToInstanceProfile(context.Background(), profile, role); err != nil {
		t.Fatalf("AddRoleToInstanceProfile: %v", err)
	}
	if err := client.RemoveRoleFromInstanceProfile(context.Background(), profile, role); err != nil {
		t.Fatalf("RemoveRoleFromInstanceProfile: %v", err)
	}
	if err := client.DeleteInstanceProfile(context.Background(), profile); err != nil {
		t.Fatalf("DeleteInstanceProfile: %v", err)
	}
	if err := client.DeleteRole(context.Background(), role); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}

	wantCalls := []string{
		"GetRole", "CreateRole", "PutRolePolicy", "DeleteRolePolicy", "GetInstanceProfile",
		"CreateInstanceProfile", "AddRoleToInstanceProfile", "RemoveRoleFromInstanceProfile",
		"DeleteInstanceProfile", "DeleteRole",
	}
	if !reflect.DeepEqual(fake.calls, wantCalls) {
		t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, wantCalls)
	}
}

func TestIAMErrorMapping(t *testing.T) {
	// R-QW4W-UBBI R-VV8S-ZVE7 R-YPQX-0LG8
	boom := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
	tests := []struct {
		method    string
		operation string
		call      func(*iamClient) error
	}{
		{"PermissionsBoundary", "ListPolicies", func(client *iamClient) error {
			_, err := client.PermissionsBoundary(context.Background(), "boundary")
			return err
		}},
		{"RoleExists", "GetRole", func(client *iamClient) error {
			_, err := client.RoleExists(context.Background(), "role")
			return err
		}},
		{"CreateRole", "CreateRole", func(client *iamClient) error {
			return client.CreateRole(context.Background(), cloud.RoleSpec{Name: "role"})
		}},
		{"PutRolePolicy", "PutRolePolicy", func(client *iamClient) error {
			return client.PutRolePolicy(context.Background(), "role", "policy", "document")
		}},
		{"DeleteRolePolicy", "DeleteRolePolicy", func(client *iamClient) error {
			return client.DeleteRolePolicy(context.Background(), "role", "policy")
		}},
		{"InstanceProfileRoles", "GetInstanceProfile", func(client *iamClient) error {
			_, _, err := client.InstanceProfileRoles(context.Background(), "profile")
			return err
		}},
		{"CreateInstanceProfile", "CreateInstanceProfile", func(client *iamClient) error {
			return client.CreateInstanceProfile(context.Background(), "profile")
		}},
		{"AddRoleToInstanceProfile", "AddRoleToInstanceProfile", func(client *iamClient) error {
			return client.AddRoleToInstanceProfile(context.Background(), "profile", "role")
		}},
		{"RemoveRoleFromInstanceProfile", "RemoveRoleFromInstanceProfile", func(client *iamClient) error {
			return client.RemoveRoleFromInstanceProfile(context.Background(), "profile", "role")
		}},
		{"DeleteInstanceProfile", "DeleteInstanceProfile", func(client *iamClient) error {
			return client.DeleteInstanceProfile(context.Background(), "profile")
		}},
		{"DeleteRole", "DeleteRole", func(client *iamClient) error {
			return client.DeleteRole(context.Background(), "role")
		}},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			fake := &fakeIAM{err: boom}
			var got *cloud.Error
			if err := test.call(&iamClient{sdk: fake}); !errors.As(err, &got) {
				t.Fatalf("error = %v, want *cloud.Error", err)
			}
			if got.Service != "iam" || got.Operation != test.operation || got.Subject != "" ||
				got.Code != boom.ErrorCode() || !errors.Is(got, boom) {
				t.Fatalf("error = %#v, want iam %s with no subject and code %s wrapping SDK error", got, test.operation, boom.ErrorCode())
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}
}

func TestIAMMissingEntitiesSucceedOnlyWhereRequired(t *testing.T) {
	// R-YS6P-S4XM
	missing := &smithy.GenericAPIError{Code: iamNoSuchEntity, Message: "missing"}

	fake := &fakeIAM{err: missing}
	exists, err := (&iamClient{sdk: fake}).RoleExists(context.Background(), "role")
	if err != nil || exists {
		t.Fatalf("RoleExists missing = %v, %v; want false, nil", exists, err)
	}
	if want := []string{"GetRole"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("RoleExists SDK calls = %v, want exactly %v", fake.calls, want)
	}

	fake = &fakeIAM{err: missing}
	roles, exists, err := (&iamClient{sdk: fake}).InstanceProfileRoles(context.Background(), "profile")
	if err != nil || exists || roles != nil {
		t.Fatalf("InstanceProfileRoles missing = %v, %v, %v; want nil, false, nil", roles, exists, err)
	}
	if want := []string{"GetInstanceProfile"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("InstanceProfileRoles SDK calls = %v, want exactly %v", fake.calls, want)
	}

	deleteTests := []struct {
		method    string
		operation string
		call      func(*iamClient) error
	}{
		{"DeleteRolePolicy", "DeleteRolePolicy", func(client *iamClient) error {
			return client.DeleteRolePolicy(context.Background(), "role", "policy")
		}},
		{"RemoveRoleFromInstanceProfile", "RemoveRoleFromInstanceProfile", func(client *iamClient) error {
			return client.RemoveRoleFromInstanceProfile(context.Background(), "profile", "role")
		}},
		{"DeleteInstanceProfile", "DeleteInstanceProfile", func(client *iamClient) error {
			return client.DeleteInstanceProfile(context.Background(), "profile")
		}},
		{"DeleteRole", "DeleteRole", func(client *iamClient) error {
			return client.DeleteRole(context.Background(), "role")
		}},
	}
	for _, test := range deleteTests {
		t.Run(test.method, func(t *testing.T) {
			fake := &fakeIAM{err: missing}
			if err := test.call(&iamClient{sdk: fake}); err != nil {
				t.Fatalf("missing error = %v, want nil", err)
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}
}

func assertIAMString(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want pointer to %q", field, got, want)
	}
}
