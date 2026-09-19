package awssdk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type fakeS3 struct {
	calls []string
	err   error

	listObjectsV2 func(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)
	putObject     func(*s3.PutObjectInput) (*s3.PutObjectOutput, error)
	deleteObjects func(*s3.DeleteObjectsInput) (*s3.DeleteObjectsOutput, error)
}

func (f *fakeS3) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	f.calls = append(f.calls, "ListObjectsV2")
	if f.listObjectsV2 != nil {
		return f.listObjectsV2(input)
	}
	return nil, f.err
}

func (f *fakeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.calls = append(f.calls, "PutObject")
	if f.putObject != nil {
		return f.putObject(input)
	}
	return nil, f.err
}

func (f *fakeS3) DeleteObjects(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	f.calls = append(f.calls, "DeleteObjects")
	if f.deleteObjects != nil {
		return f.deleteObjects(input)
	}
	return nil, f.err
}

func TestS3MappingPaginationAndFields(t *testing.T) {
	// R-CEU6-HHNG
	const (
		bucket = "backup-bucket"
		prefix = "spaces/example/backups/"
		key    = prefix + "archive.tar"
	)
	firstModified := time.Date(2026, time.January, 2, 3, 4, 5, 6, time.UTC)
	secondModified := time.Date(2026, time.February, 3, 4, 5, 6, 7, time.FixedZone("offset", 3600))
	page := 0
	fake := &fakeS3{}
	fake.listObjectsV2 = func(input *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
		if aws.ToString(input.Bucket) != bucket || aws.ToString(input.Prefix) != prefix {
			t.Fatalf("ListObjectsV2 bucket/prefix = %q/%q, want %q/%q", aws.ToString(input.Bucket), aws.ToString(input.Prefix), bucket, prefix)
		}
		page++
		switch page {
		case 1:
			if input.ContinuationToken != nil {
				t.Fatalf("first continuation token = %q, want nil", aws.ToString(input.ContinuationToken))
			}
			return &s3.ListObjectsV2Output{
				Contents:    []types.Object{{Key: aws.String(prefix + "one"), Size: aws.Int64(11), LastModified: aws.Time(firstModified)}},
				IsTruncated: aws.Bool(true), NextContinuationToken: aws.String("second-page"),
			}, nil
		case 2:
			if aws.ToString(input.ContinuationToken) != "second-page" {
				t.Fatalf("second continuation token = %q, want second-page", aws.ToString(input.ContinuationToken))
			}
			return &s3.ListObjectsV2Output{
				Contents:    []types.Object{{Key: aws.String(prefix + "two"), Size: aws.Int64(22), LastModified: aws.Time(secondModified)}},
				IsTruncated: aws.Bool(false),
			}, nil
		default:
			t.Fatalf("unexpected list page %d", page)
			return nil, nil
		}
	}
	fake.putObject = func(input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
		if aws.ToString(input.Bucket) != bucket || aws.ToString(input.Key) != key {
			t.Fatalf("PutObject bucket/key = %q/%q, want %q/%q", aws.ToString(input.Bucket), aws.ToString(input.Key), bucket, key)
		}
		if aws.ToInt64(input.ContentLength) != 13 {
			t.Fatalf("PutObject content length = %d, want 13", aws.ToInt64(input.ContentLength))
		}
		body, err := io.ReadAll(input.Body)
		if err != nil || string(body) != "payload-bytes" {
			t.Fatalf("PutObject body = %q, %v; want payload-bytes, nil", body, err)
		}
		return &s3.PutObjectOutput{}, nil
	}

	client := &s3Client{sdk: fake}
	objects, err := client.ListObjects(context.Background(), bucket, prefix)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	wantObjects := []cloud.Object{
		{Key: prefix + "one", Size: 11, Modified: firstModified},
		{Key: prefix + "two", Size: 22, Modified: secondModified},
	}
	if !reflect.DeepEqual(objects, wantObjects) {
		t.Fatalf("objects = %#v, want %#v", objects, wantObjects)
	}
	if err := client.PutObject(context.Background(), bucket, key, bytes.NewBufferString("payload-bytes"), 13); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	wantCalls := []string{"ListObjectsV2", "ListObjectsV2", "PutObject"}
	if !reflect.DeepEqual(fake.calls, wantCalls) {
		t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, wantCalls)
	}
}

func TestS3DeleteObjectsBatchBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 999, 1000, 1001, 2000, 2001} {
		t.Run(fmt.Sprintf("keys_%d", count), func(t *testing.T) {
			keys := make([]string, count)
			for index := range keys {
				keys[index] = fmt.Sprintf("object-%04d", index)
			}
			var deleted []string
			var batchSizes []int
			fake := &fakeS3{deleteObjects: func(input *s3.DeleteObjectsInput) (*s3.DeleteObjectsOutput, error) {
				if aws.ToString(input.Bucket) != "bucket" || input.Delete == nil {
					t.Fatalf("DeleteObjects input = %#v, want bucket and delete payload", input)
				}
				if len(input.Delete.Objects) == 0 || len(input.Delete.Objects) > 1000 {
					t.Fatalf("DeleteObjects batch size = %d, want 1..1000", len(input.Delete.Objects))
				}
				batchSizes = append(batchSizes, len(input.Delete.Objects))
				for _, object := range input.Delete.Objects {
					deleted = append(deleted, aws.ToString(object.Key))
				}
				return &s3.DeleteObjectsOutput{}, nil
			}}

			if err := (&s3Client{sdk: fake}).DeleteObjects(context.Background(), "bucket", keys); err != nil {
				t.Fatalf("DeleteObjects: %v", err)
			}
			if !slices.Equal(deleted, keys) {
				t.Fatalf("deleted %d keys in order, want %d exact keys", len(deleted), len(keys))
			}
			wantBatches := (count + 999) / 1000
			if len(batchSizes) != wantBatches || len(fake.calls) != wantBatches {
				t.Fatalf("batch sizes/calls = %v/%v, want %d batches", batchSizes, fake.calls, wantBatches)
			}
			for _, call := range fake.calls {
				if call != "DeleteObjects" {
					t.Fatalf("SDK call = %q, want only DeleteObjects", call)
				}
			}
		})
	}
}

func TestS3ErrorMapping(t *testing.T) {
	// R-VV8S-ZVE7 R-YPQX-0LG8
	boom := &smithy.GenericAPIError{Code: "SlowDown", Message: "retry later"}
	tests := []struct {
		method    string
		operation string
		subject   string
		call      func(*s3Client) error
	}{
		{"ListObjects", "ListObjectsV2", "bucket/prefix/", func(client *s3Client) error {
			_, err := client.ListObjects(context.Background(), "bucket", "prefix/")
			return err
		}},
		{"PutObject", "PutObject", "bucket/key", func(client *s3Client) error {
			return client.PutObject(context.Background(), "bucket", "key", bytes.NewReader(nil), 0)
		}},
		{"DeleteObjects", "DeleteObjects", "bucket", func(client *s3Client) error {
			return client.DeleteObjects(context.Background(), "bucket", []string{"key"})
		}},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			fake := &fakeS3{err: boom}
			var got *cloud.Error
			if err := test.call(&s3Client{sdk: fake}); !errors.As(err, &got) {
				t.Fatalf("error = %v, want *cloud.Error", err)
			}
			if got.Service != "s3" || got.Operation != test.operation || got.Subject != test.subject || got.Code != boom.ErrorCode() || !errors.Is(got, boom) {
				t.Fatalf("error = %#v, want s3 %s %s with code %s wrapping SDK error", got, test.operation, test.subject, boom.ErrorCode())
			}
			if want := []string{test.operation}; !reflect.DeepEqual(fake.calls, want) {
				t.Fatalf("SDK calls = %v, want exactly %v", fake.calls, want)
			}
		})
	}
}
