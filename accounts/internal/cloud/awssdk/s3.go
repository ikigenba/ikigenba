package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const (
	listObjects   = "ListObjectsV2"
	deleteObjects = "DeleteObjects"
)

func (a *adapter) ListKeys(ctx context.Context, bucket, prefix string, each func([]string) bool) error {
	return a.listObjects(ctx, bucket, prefix, cloud.MaxKeysPerPage, func(objects []types.Object) bool {
		keys := make([]string, len(objects))
		for i, object := range objects {
			keys[i] = aws.ToString(object.Key)
		}
		return each(keys)
	})
}

func (a *adapter) HasKeys(ctx context.Context, bucket, prefix string) (bool, error) {
	hasKeys := false
	err := a.listObjects(ctx, bucket, prefix, 1, func([]types.Object) bool {
		hasKeys = true
		return false
	})
	return hasKeys, err
}

func (a *adapter) listObjects(ctx context.Context, bucket, prefix string, maxKeys int32, each func([]types.Object) bool) error {
	in := &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(maxKeys)}
	for {
		out, err := a.s3.ListObjectsV2(ctx, in)
		if err != nil {
			return failed("s3", listObjects, bucket, err)
		}
		if len(out.Contents) != 0 && !each(out.Contents) {
			return nil
		}
		if !aws.ToBool(out.IsTruncated) {
			return nil
		}
		in.ContinuationToken = out.NextContinuationToken
	}
}

func (a *adapter) DeleteKeys(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 || len(keys) > cloud.MaxKeysPerPage {
		return s3DeleteError(bucket, "MalformedXML")
	}
	objects := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objects[i].Key = aws.String(key)
	}
	out, err := a.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return failed("s3", deleteObjects, bucket, err)
	}
	if len(out.Errors) > 0 {
		return s3DeleteError(bucket, aws.ToString(out.Errors[0].Code))
	}
	return nil
}

func s3DeleteError(bucket, code string) *cloud.Error {
	return &cloud.Error{Service: "s3", Operation: deleteObjects, Subject: bucket, Code: code, Class: classify(code, 0)}
}
