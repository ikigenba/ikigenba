package awssdk

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

const maxDeleteObjects = 1000

type s3API interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObjects(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

type s3Client struct {
	sdk s3API
}

func (c *s3Client) ListObjects(ctx context.Context, bucket, prefix string) ([]cloud.Object, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	var objects []cloud.Object
	for {
		output, err := c.sdk.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, s3Error("ListObjectsV2", bucket+"/"+prefix, err)
		}
		for _, object := range output.Contents {
			objects = append(objects, cloud.Object{
				Key:      aws.ToString(object.Key),
				Size:     aws.ToInt64(object.Size),
				Modified: aws.ToTime(object.LastModified),
			})
		}
		if !aws.ToBool(output.IsTruncated) {
			return objects, nil
		}
		input.ContinuationToken = output.NextContinuationToken
	}
}

func (c *s3Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error {
	_, err := c.sdk.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	return wrapS3("PutObject", bucket+"/"+key, err)
}

func (c *s3Client) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	for start := 0; start < len(keys); start += maxDeleteObjects {
		end := min(start+maxDeleteObjects, len(keys))
		identifiers := make([]types.ObjectIdentifier, 0, end-start)
		for _, key := range keys[start:end] {
			identifiers = append(identifiers, types.ObjectIdentifier{Key: aws.String(key)})
		}
		_, err := c.sdk.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{Objects: identifiers},
		})
		if err != nil {
			return s3Error("DeleteObjects", bucket, err)
		}
	}
	return nil
}

func wrapS3(operation, subject string, err error) error {
	if err == nil {
		return nil
	}
	return s3Error(operation, subject, err)
}

func s3Error(operation, subject string, err error) *cloud.Error {
	return sdkError("s3", operation, subject, err)
}
