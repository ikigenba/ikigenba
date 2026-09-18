package awssdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type fakeSSM struct {
	calls []string
	err   error

	getParameter        func(*ssm.GetParameterInput) (*ssm.GetParameterOutput, error)
	putParameter        func(*ssm.PutParameterInput) (*ssm.PutParameterOutput, error)
	getParametersByPath func(*ssm.GetParametersByPathInput) (*ssm.GetParametersByPathOutput, error)
	deleteParameter     func(*ssm.DeleteParameterInput) (*ssm.DeleteParameterOutput, error)
}

func (f *fakeSSM) GetParameter(_ context.Context, input *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	f.calls = append(f.calls, "GetParameter")
	if f.getParameter != nil {
		return f.getParameter(input)
	}
	return nil, f.err
}

func (f *fakeSSM) PutParameter(_ context.Context, input *ssm.PutParameterInput, _ ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	f.calls = append(f.calls, "PutParameter")
	if f.putParameter != nil {
		return f.putParameter(input)
	}
	return nil, f.err
}

func (f *fakeSSM) GetParametersByPath(_ context.Context, input *ssm.GetParametersByPathInput, _ ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	f.calls = append(f.calls, "GetParametersByPath")
	if f.getParametersByPath != nil {
		return f.getParametersByPath(input)
	}
	return nil, f.err
}

func (f *fakeSSM) DeleteParameter(_ context.Context, input *ssm.DeleteParameterInput, _ ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	f.calls = append(f.calls, "DeleteParameter")
	if f.deleteParameter != nil {
		return f.deleteParameter(input)
	}
	return nil, f.err
}

func TestSSMMappingAndInputs(t *testing.T) {
	// R-YVUE-XG5P
	const (
		name   = "/ikigenba/account"
		prefix = "/ikigenba/spaces"
	)
	fake := &fakeSSM{}
	fake.getParameter = func(input *ssm.GetParameterInput) (*ssm.GetParameterOutput, error) {
		if aws.ToString(input.Name) != name || !aws.ToBool(input.WithDecryption) {
			t.Fatalf("GetParameter input = %#v, want name %q with decryption", input, name)
		}
		return &ssm.GetParameterOutput{Parameter: &types.Parameter{Value: aws.String("account-value")}}, nil
	}
	fake.putParameter = func(input *ssm.PutParameterInput) (*ssm.PutParameterOutput, error) {
		if aws.ToString(input.Name) != name || aws.ToString(input.Value) != "new-value" {
			t.Fatalf("PutParameter name/value = %q/%q, want %q/new-value", aws.ToString(input.Name), aws.ToString(input.Value), name)
		}
		if input.Type != types.ParameterTypeSecureString || !aws.ToBool(input.Overwrite) {
			t.Fatalf("PutParameter type/overwrite = %q/%v, want SecureString/true", input.Type, aws.ToBool(input.Overwrite))
		}
		return &ssm.PutParameterOutput{}, nil
	}
	page := 0
	fake.getParametersByPath = func(input *ssm.GetParametersByPathInput) (*ssm.GetParametersByPathOutput, error) {
		if aws.ToString(input.Path) != prefix || !aws.ToBool(input.Recursive) || !aws.ToBool(input.WithDecryption) {
			t.Fatalf("GetParametersByPath input = %#v, want path %q recursive with decryption", input, prefix)
		}
		page++
		switch page {
		case 1:
			if input.NextToken != nil {
				t.Fatalf("first next token = %q, want nil", aws.ToString(input.NextToken))
			}
			return &ssm.GetParametersByPathOutput{
				Parameters: []types.Parameter{{Name: aws.String(prefix + "/one"), Value: aws.String("secret-one")}},
				NextToken:  aws.String("page-two"),
			}, nil
		case 2:
			if aws.ToString(input.NextToken) != "page-two" {
				t.Fatalf("second next token = %q, want page-two", aws.ToString(input.NextToken))
			}
			return &ssm.GetParametersByPathOutput{
				Parameters: []types.Parameter{{Name: aws.String(prefix + "/two"), Value: aws.String("secret-two")}},
			}, nil
		default:
			t.Fatalf("unexpected page %d", page)
			return nil, nil
		}
	}
	fake.deleteParameter = func(input *ssm.DeleteParameterInput) (*ssm.DeleteParameterOutput, error) {
		if aws.ToString(input.Name) != name {
			t.Fatalf("DeleteParameter name = %q, want %q", aws.ToString(input.Name), name)
		}
		return &ssm.DeleteParameterOutput{}, nil
	}

	client := &ssmClient{sdk: fake}
	value, err := client.GetParameter(context.Background(), name)
	if err != nil || value != "account-value" {
		t.Fatalf("GetParameter = %q, %v; want account-value, nil", value, err)
	}
	if err := client.PutSecureParameter(context.Background(), name, "new-value"); err != nil {
		t.Fatalf("PutSecureParameter: %v", err)
	}
	parameters, err := client.ListParameters(context.Background(), prefix)
	if err != nil {
		t.Fatalf("ListParameters: %v", err)
	}
	wantParameters := []cloud.Parameter{
		{Name: prefix + "/one", Value: "secret-one"},
		{Name: prefix + "/two", Value: "secret-two"},
	}
	if !reflect.DeepEqual(parameters, wantParameters) {
		t.Fatalf("parameters = %#v, want %#v", parameters, wantParameters)
	}
	if err := client.DeleteParameter(context.Background(), name); err != nil {
		t.Fatalf("DeleteParameter: %v", err)
	}
	wantCalls := []string{"GetParameter", "PutParameter", "GetParametersByPath", "GetParametersByPath", "DeleteParameter"}
	if !reflect.DeepEqual(fake.calls, wantCalls) {
		t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, wantCalls)
	}
}

func TestSSMErrorMapping(t *testing.T) {
	const name = "/ikigenba/secret"
	boom := &smithy.GenericAPIError{Code: "ThrottlingException", Message: "slow down"}
	tests := []struct {
		method    string
		operation string
		call      func(*ssmClient) error
	}{
		{"GetParameter", "GetParameter", func(client *ssmClient) error {
			_, err := client.GetParameter(context.Background(), name)
			return err
		}},
		{"PutSecureParameter", "PutParameter", func(client *ssmClient) error {
			return client.PutSecureParameter(context.Background(), name, "value")
		}},
		{"ListParameters", "GetParametersByPath", func(client *ssmClient) error {
			_, err := client.ListParameters(context.Background(), name)
			return err
		}},
		{"DeleteParameter", "DeleteParameter", func(client *ssmClient) error {
			return client.DeleteParameter(context.Background(), name)
		}},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			fake := &fakeSSM{err: boom}
			var got *cloud.Error
			if err := test.call(&ssmClient{sdk: fake}); !errors.As(err, &got) {
				t.Fatalf("error = %v, want *cloud.Error", err)
			}
			if got.Service != "ssm" || got.Operation != test.operation || got.Subject != name || got.Code != boom.ErrorCode() || !errors.Is(got, boom) {
				t.Fatalf("error = %#v, want ssm %s %s with code %s wrapping SDK error", got, test.operation, name, boom.ErrorCode())
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}
}

func TestSSMDeleteMissingParameterSucceeds(t *testing.T) {
	fake := &fakeSSM{err: &smithy.GenericAPIError{Code: "ParameterNotFound", Message: "missing"}}
	if err := (&ssmClient{sdk: fake}).DeleteParameter(context.Background(), "/missing"); err != nil {
		t.Fatalf("DeleteParameter missing error = %v, want nil", err)
	}
	if want := []string{"DeleteParameter"}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
	}
}
