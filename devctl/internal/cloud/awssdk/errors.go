package awssdk

import (
	"errors"
	"reflect"

	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func sdkError(service, operation, subject string, err error) *cloud.Error {
	cause := err
	var operationError *smithy.OperationError
	if errors.As(err, &operationError) && sameError(err, operationError) {
		cause = operationError.Err
	}

	var code string
	for current := cause; current != nil; current = errors.Unwrap(current) {
		operationError = nil
		if errors.As(current, &operationError) && sameError(current, operationError) {
			break
		}
		var apiError smithy.APIError
		if errors.As(current, &apiError) && sameError(current, apiError) {
			code = apiError.ErrorCode()
			break
		}
	}

	return &cloud.Error{
		Service: service, Operation: operation, Subject: subject,
		Code: code, Err: cause,
	}
}

func sameError(left, right error) bool {
	return reflect.ValueOf(left) == reflect.ValueOf(right)
}

func apiErrorCode(err error) string {
	apiError := boundedAPIError(err)
	if apiError != nil {
		return apiError.ErrorCode()
	}
	return ""
}

func boundedAPIError(err error) smithy.APIError {
	for current := err; current != nil; current = errors.Unwrap(current) {
		var operationError *smithy.OperationError
		if errors.As(current, &operationError) && sameError(current, operationError) && !sameError(current, err) {
			return nil
		}
		var apiError smithy.APIError
		if errors.As(current, &apiError) && sameError(current, apiError) {
			return apiError
		}
	}
	return nil
}
