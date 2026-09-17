// Package aws implements the cloud boundary with Amazon S3 and SSM.
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
)

type s3API interface {
	GetObject(context.Context, *awss3.GetObjectInput, ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	PutObject(context.Context, *awss3.PutObjectInput, ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	ListObjectsV2(context.Context, *awss3.ListObjectsV2Input, ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error)
}

type ssmAPI interface {
	GetParameter(context.Context, *awsssm.GetParameterInput, ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error)
}

type client struct {
	s3  s3API
	ssm ssmAPI
}

type operationError struct {
	operation string
	cause     error
	sentinel  error
}

func (e *operationError) Error() string {
	return e.operation
}

func (e *operationError) Unwrap() []error {
	return []error{e.sentinel, e.cause}
}

// Open loads the default AWS configuration for region and constructs one
// cloud client backed by S3 and SSM.
func Open(ctx context.Context, region string) (cloud.Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, &operationError{operation: "load AWS configuration", cause: err}
	}
	return newClient(awss3.NewFromConfig(cfg), awsssm.NewFromConfig(cfg)), nil
}

func newClient(s3Client s3API, ssmClient ssmAPI) cloud.Client {
	return &client{s3: s3Client, ssm: ssmClient}
}

func (c *client) GetObject(ctx context.Context, uri string) (io.ReadCloser, error) {
	location, err := parseS3URI(uri, true)
	if err != nil {
		return nil, err
	}
	output, err := c.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: awssdk.String(location.bucket),
		Key:    awssdk.String(location.key),
	})
	if err != nil {
		var notFound *s3types.NoSuchKey
		if errors.As(err, &notFound) {
			return nil, &operationError{operation: "get S3 object: not found", cause: err, sentinel: cloud.ErrNotFound}
		}
		return nil, &operationError{operation: "get S3 object", cause: err}
	}
	if output == nil || output.Body == nil {
		return nil, errors.New("get S3 object: invalid response")
	}
	return output.Body, nil
}

func (c *client) PutObject(ctx context.Context, uri string, body io.Reader) error {
	location, err := parseS3URI(uri, true)
	if err != nil {
		return err
	}
	_, err = c.s3.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      awssdk.String(location.bucket),
		Key:         awssdk.String(location.key),
		Body:        body,
		IfNoneMatch: awssdk.String("*"),
	})
	if err == nil {
		return nil
	}
	if conditionalCollision(err) {
		return &operationError{operation: "put S3 object: already exists", cause: err, sentinel: cloud.ErrAlreadyExists}
	}
	return &operationError{operation: "put S3 object", cause: err}
}

func (c *client) ListObjects(ctx context.Context, prefix string) ([]cloud.Object, error) {
	location, err := parseS3URI(prefix, false)
	if err != nil {
		return nil, err
	}
	input := &awss3.ListObjectsV2Input{
		Bucket: awssdk.String(location.bucket),
		Prefix: awssdk.String(location.key),
	}
	var objects []cloud.Object
	for {
		output, listErr := c.s3.ListObjectsV2(ctx, input)
		if listErr != nil {
			return nil, &operationError{operation: "list S3 objects", cause: listErr}
		}
		if output == nil {
			return nil, errors.New("list S3 objects: invalid response")
		}
		for _, object := range output.Contents {
			if object.Key == nil || object.Size == nil || object.LastModified == nil {
				return nil, errors.New("list S3 objects: invalid response")
			}
			objects = append(objects, cloud.Object{
				URI:      canonicalS3URI(location.bucket, *object.Key),
				Size:     *object.Size,
				Modified: *object.LastModified,
			})
		}
		if !awssdk.ToBool(output.IsTruncated) {
			return objects, nil
		}
		if output.NextContinuationToken == nil || *output.NextContinuationToken == "" ||
			(input.ContinuationToken != nil && *output.NextContinuationToken == *input.ContinuationToken) {
			return nil, errors.New("list S3 objects: invalid response")
		}
		input.ContinuationToken = output.NextContinuationToken
	}
}

func (c *client) ReadSecrets(ctx context.Context, parameter string) (map[string]string, error) {
	output, err := c.ssm.GetParameter(ctx, &awsssm.GetParameterInput{
		Name:           awssdk.String(parameter),
		WithDecryption: awssdk.Bool(true),
	})
	if err != nil {
		var notFound *ssmtypes.ParameterNotFound
		if errors.As(err, &notFound) {
			return nil, &operationError{operation: fmt.Sprintf("read secret parameter %q: not found", parameter), cause: err, sentinel: cloud.ErrNotFound}
		}
		return nil, &operationError{operation: fmt.Sprintf("read secret parameter %q", parameter), cause: err}
	}
	if output == nil || output.Parameter == nil || output.Parameter.Value == nil {
		return nil, fmt.Errorf("read secret parameter %q: invalid response", parameter)
	}
	secrets, err := decodeSecrets(*output.Parameter.Value)
	if err != nil {
		return nil, fmt.Errorf("read secret parameter %q: invalid secret object", parameter)
	}
	return secrets, nil
}

type s3Location struct {
	bucket string
	key    string
}

func parseS3URI(raw string, requireKey bool) (s3Location, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "s3" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.Host == "" || strings.Contains(parsed.Host, ":") || parsed.Port() != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") {
		return s3Location{}, errors.New("invalid s3 URI")
	}
	key := parsed.Path
	if len(key) != 0 && key[0] == '/' {
		key = key[1:]
	}
	if requireKey && key == "" {
		return s3Location{}, errors.New("invalid s3 URI: object key is empty")
	}
	return s3Location{bucket: parsed.Host, key: key}, nil
}

func canonicalS3URI(bucket, key string) string {
	return (&url.URL{Scheme: "s3", Host: bucket, Path: "/" + key}).String()
}

func conditionalCollision(err error) bool {
	var apiError interface{ ErrorCode() string }
	if !errors.As(err, &apiError) {
		return false
	}
	var response interface{ HTTPStatusCode() int }
	if !errors.As(err, &response) {
		return false
	}
	return apiError.ErrorCode() == "PreconditionFailed" && response.HTTPStatusCode() == 412 ||
		apiError.ErrorCode() == "ConditionalRequestConflict" && response.HTTPStatusCode() == 409
}

func decodeSecrets(source string) (map[string]string, error) {
	if !utf8.ValidString(source) {
		return nil, errors.New("invalid UTF-8")
	}
	decoder := json.NewDecoder(strings.NewReader(source))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	secrets := make(map[string]string)
	for decoder.More() {
		nameToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, errors.New("invalid object member")
		}
		name, ok := nameToken.(string)
		if !ok {
			return nil, errors.New("invalid object member")
		}
		if _, duplicate := secrets[name]; duplicate {
			return nil, errors.New("duplicate object member")
		}
		valueToken, valueErr := decoder.Token()
		value, ok := valueToken.(string)
		if valueErr != nil || !ok {
			return nil, errors.New("object value is not a string")
		}
		secrets[name] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return nil, errors.New("unterminated object")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON data")
	}
	return secrets, nil
}
