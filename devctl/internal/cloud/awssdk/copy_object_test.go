package awssdk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type copyTransport struct {
	requests []*http.Request
	failure  bool
}

func (c *copyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, request.Clone(request.Context()))
	body := `<CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"copy"</ETag><LastModified>2026-09-12T14:22:51Z</LastModified></CopyObjectResult>`
	status := http.StatusOK
	if c.failure {
		status = http.StatusServiceUnavailable
		body = `<Error><Code>SlowDown</Code><Message>retry later</Message></Error>`
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func TestCopyObjectRequestAndErrorThroughLoader(t *testing.T) {
	// R-SL1F-5YCK R-SJTI-S6LV
	const bucket = "ikigenba.dev"
	const source = "sbx1/snapshots/crm/2026-09-12T14:22:51Z.tar.zst"
	const key = "golden/demo/crm/2026-09-12T14:22:51Z.tar.zst"
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "SlowDown"
		}
		t.Run(name, func(t *testing.T) {
			transport := &copyTransport{failure: failure}
			clients, err := OpenWithLoader(context.Background(), "profile", "us-test-1", func(context.Context, string, string) (aws.Config, error) {
				return aws.Config{Region: "us-test-1", RetryMaxAttempts: 1, Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
					return aws.Credentials{AccessKeyID: "fake", SecretAccessKey: "fake"}, nil
				}), HTTPClient: &http.Client{Transport: transport}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			err = clients.S3.CopyObject(context.Background(), bucket, source, key)
			if failure {
				var got *cloud.Error
				if !errors.As(err, &got) || got.Error() != "s3 CopyObject: SlowDown" || got.Subject != "" {
					t.Fatalf("CopyObject error = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(transport.requests) != 1 {
				t.Fatalf("requests = %d", len(transport.requests))
			}
			request := transport.requests[0]
			decoded, err := url.PathUnescape(request.Header.Get("X-Amz-Copy-Source"))
			if err != nil || decoded != bucket+"/"+source {
				t.Fatalf("copy source = %q, %v", decoded, err)
			}
			if request.Method != http.MethodPut || request.URL.Path != "/"+bucket+"/"+key || strings.HasPrefix(request.URL.Hostname(), bucket+".") {
				t.Fatalf("copy destination = %s %s", request.Method, request.URL)
			}
		})
	}
}
