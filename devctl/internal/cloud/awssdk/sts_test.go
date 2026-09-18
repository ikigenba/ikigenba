package awssdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type fakeSTS struct {
	calls []string
	err   error
}

func (f *fakeSTS) GetCallerIdentity(_ context.Context, input *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	f.calls = append(f.calls, "GetCallerIdentity")
	if f.err != nil {
		return nil, f.err
	}
	if input == nil {
		panic("nil GetCallerIdentity input")
	}
	return &sts.GetCallerIdentityOutput{Account: aws.String("123456789012")}, nil
}

func TestSTSCallerAccountID(t *testing.T) {
	// R-Z35T-82LV
	t.Run("account", func(t *testing.T) {
		fake := &fakeSTS{}
		account, err := (&stsClient{sdk: fake}).CallerAccountID(context.Background())
		if err != nil || account != "123456789012" {
			t.Fatalf("CallerAccountID = %q, %v; want 123456789012, nil", account, err)
		}
		if want := []string{"GetCallerIdentity"}; !reflect.DeepEqual(fake.calls, want) {
			t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
		}
	})

	t.Run("error", func(t *testing.T) {
		boom := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
		fake := &fakeSTS{err: boom}
		account, err := (&stsClient{sdk: fake}).CallerAccountID(context.Background())
		var got *cloud.Error
		if account != "" || !errors.As(err, &got) {
			t.Fatalf("CallerAccountID = %q, %v; want empty account and *cloud.Error", account, err)
		}
		if got.Service != "sts" || got.Operation != "GetCallerIdentity" || got.Subject != "" || got.Code != boom.ErrorCode() || !errors.Is(got, boom) {
			t.Fatalf("error = %#v, want sts GetCallerIdentity with no subject, code %s, wrapping SDK error", got, boom.ErrorCode())
		}
		if want := []string{"GetCallerIdentity"}; !reflect.DeepEqual(fake.calls, want) {
			t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
		}
	})
}
