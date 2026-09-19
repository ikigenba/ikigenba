package awssdk

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
)

func TestSDKErrorBoundary(t *testing.T) {
	// R-VV8S-ZVE7
	api := &smithy.GenericAPIError{Code: "OuterCode", Message: "outer message"}
	outer := &smithy.OperationError{ServiceID: "SDK", OperationName: "Call", Err: api}
	got := sdkError("ec2", "RunInstances", "", outer)
	if got.Code != "OuterCode" || !errors.Is(got.Err, api) || got.Err.Error() != "api error OuterCode: outer message" {
		t.Fatalf("single operation error = %#v", got)
	}

	nestedAPI := &smithy.GenericAPIError{Code: "NestedCode", Message: "nested message"}
	nested := &smithy.OperationError{ServiceID: "Other", OperationName: "Refresh", Err: nestedAPI}
	wrapped := fmt.Errorf("credential refresh: %w", nested)
	outer = &smithy.OperationError{ServiceID: "SDK", OperationName: "Call", Err: wrapped}
	got = sdkError("sts", "GetCallerIdentity", "", outer)
	if got.Code != "" || !errors.Is(got.Err, wrapped) || !errors.Is(got, nestedAPI) || got.Err.Error() != wrapped.Error() {
		t.Fatalf("nested operation error = %#v", got)
	}

	outer = &smithy.OperationError{ServiceID: "SDK", OperationName: "Call", Err: nested}
	got = sdkError("sts", "GetCallerIdentity", "", outer)
	if got.Code != "" || !errors.Is(got.Err, nested) {
		t.Fatalf("direct nested operation error = %#v", got)
	}
}
