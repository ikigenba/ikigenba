package awshost_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/awshost"
)

type wire func(*http.Request) (*http.Response, error)

func (r wire) RoundTrip(req *http.Request) (*http.Response, error) { return r(req) }

func configFor(region string, r wire) aws.Config {
	return aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider("HOSTKEY", "testing-secret", "testing-token"),
		HTTPClient:  &http.Client{Transport: r},
		Retryer:     func() aws.Retryer { return aws.NopRetryer{} },
	}
}

func open(t *testing.T, region string, r wire) (cloud.Presigner, cloud.Metadata) {
	t.Helper()
	p, m, err := awshost.OpenWithLoader(t.Context(), func(context.Context) (aws.Config, error) {
		return configFor(region, r), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, m
}

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}

func tokenReply(t *testing.T, r *http.Request) *http.Response {
	t.Helper()
	if r.Method != http.MethodPut || r.Header.Get("X-Aws-Ec2-Metadata-Token-Ttl-Seconds") == "" {
		t.Errorf("expected IMDSv2 token request, got %s %s %v", r.Method, r.URL, r.Header)
	}
	out := reply(200, "token")
	out.Header.Set("X-Aws-Ec2-Metadata-Token-Ttl-Seconds", "21600")
	return out
}

func checkIdentityRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodGet || r.URL.Path != "/latest/dynamic/instance-identity/document" ||
		r.Header.Get("X-Aws-Ec2-Metadata-Token") != "token" {
		t.Fatalf("expected IMDS identity document request, got %s %s %v", r.Method, r.URL, r.Header)
	}
}

func checkIMDSError(t *testing.T, err error, op string) {
	t.Helper()
	var got *cloud.Error
	if reflect.TypeOf(err) != reflect.TypeFor[*cloud.Error]() || !errors.As(err, &got) ||
		got.Service != "imds" || got.Operation != op {
		t.Fatalf("%s error = %T %v", op, err, err)
	}
}

func TestOpenSeams(t *testing.T) {
	// R-XSO4-V0AH R-XTW1-8S16
	type opener func(context.Context) (cloud.Presigner, cloud.Metadata, error)
	var _ opener = awshost.Open
	ctx := t.Context()
	cause := errors.New("load failed")
	var load awshost.ConfigLoader = func(got context.Context) (aws.Config, error) {
		if got != ctx {
			t.Errorf("unexpected loader context: %v", got)
		}
		return aws.Config{}, cause
	}
	p, m, err := awshost.OpenWithLoader(ctx, load)
	if p != nil || m != nil || !errors.Is(err, cause) || reflect.TypeOf(err) != reflect.TypeOf(cause) {
		t.Fatalf("loader failure = %v, %v, %v", p, m, err)
	}

	calls := 0
	p, _ = open(t, "", func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "169.254.169.254" {
			t.Fatalf("unexpected metadata host %s", r.URL.Host)
		}
		calls++
		if r.URL.Path == "/latest/api/token" {
			return tokenReply(t, r), nil
		}
		checkIdentityRequest(t, r)
		return reply(200, `{"region":"us-west-2"}`), nil
	})
	signed, err := p.PresignCallerIdentity(ctx, "host-binding", time.Minute)
	u, parseErr := url.Parse(signed)
	if err != nil || parseErr != nil || u.Host != "sts.us-west-2.amazonaws.com" || calls != 2 {
		t.Fatalf("IMDS region presign = %q, %v; calls = %d", signed, err, calls)
	}
	calls = 0
	p, _ = open(t, "us-east-2", func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected request")
	})
	signed, err = p.PresignCallerIdentity(ctx, "host-binding", time.Minute)
	u, parseErr = url.Parse(signed)
	if err != nil || parseErr != nil || u.Host != "sts.us-east-2.amazonaws.com" || calls != 0 {
		t.Fatalf("configured region presign = %q, %v; calls = %d", signed, err, calls)
	}

	_, _, err = awshost.OpenWithLoader(ctx, func(context.Context) (aws.Config, error) {
		return configFor("", func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/latest/api/token" {
				return tokenReply(t, r), nil
			}
			checkIdentityRequest(t, r)
			return reply(403, "token and credential must be hidden"), nil
		}), nil
	})
	checkIMDSError(t, err, "GetRegion")
	// R-XRG8-H8JS
	if err.Error() != "imds GetRegion: HTTP 403" {
		t.Errorf("unsafe region error: %v", err)
	}
}

func TestPresignCallerIdentity(t *testing.T) {
	// R-XV3X-MJRV
	const region, binding = "us-west-2", "accounts.test"
	never := func(*http.Request) (*http.Response, error) {
		t.Fatal("presigning reached the transport")
		return nil, nil
	}
	p, _ := open(t, region, never)
	for _, expires := range []time.Duration{0, time.Second - 1, -time.Minute} {
		if got, err := p.PresignCallerIdentity(t.Context(), binding, expires); err == nil || got != "" {
			t.Errorf("PresignCallerIdentity(%v) = %q, %v", expires, got, err)
		}
	}
	signed, err := p.PresignCallerIdentity(t.Context(), binding, time.Minute+time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "sts."+region+".amazonaws.com" || u.Path != "/" {
		t.Fatalf("presigned target = %s", u)
	}
	q := u.Query()
	for name, want := range map[string]string{
		"Action":               "GetCallerIdentity",
		"Version":              "2011-06-15",
		"X-Amz-Algorithm":      "AWS4-HMAC-SHA256",
		"X-Amz-Expires":        "60",
		"X-Amz-SignedHeaders":  "host;" + strings.ToLower(cloud.BindingHeader),
		"X-Amz-Security-Token": "testing-token",
	} {
		if q.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, q.Get(name), want)
		}
	}
	at, err := time.Parse("20060102T150405Z", q.Get("X-Amz-Date"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "HOSTKEY/" + at.Format("20060102") + "/" + region + "/sts/aws4_request"; q.Get("X-Amz-Credential") != want {
		t.Errorf("X-Amz-Credential = %q, want %q", q.Get("X-Amz-Credential"), want)
	}
	unsigned := *u
	unsigned.RawQuery = url.Values{
		"Action":        {"GetCallerIdentity"},
		"Version":       {"2011-06-15"},
		"X-Amz-Expires": {"60"},
	}.Encode()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, unsigned.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(cloud.BindingHeader, binding)
	creds, err := configFor(region, never).Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	const emptyPayload = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	signer := v4.NewSigner()
	expected, _, err := signer.PresignHTTP(t.Context(), creds, req, emptyPayload, "sts", region, at)
	if err != nil {
		t.Fatal(err)
	}
	want, err := url.Parse(expected)
	if err != nil || want.Query().Get("X-Amz-Signature") == "" ||
		want.Query().Get("X-Amz-Signature") != q.Get("X-Amz-Signature") {
		t.Fatal("presigned URL differs from SDK signature for the requested binding")
	}
}

func TestInstanceFacts(t *testing.T) {
	// R-XWBU-0BIK
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		_, m := open(t, "us-east-2", func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/latest/api/token" {
				return tokenReply(t, r), nil
			}
			checkIdentityRequest(t, r)
			return reply(status, `{"instanceId":"i-test","instanceType":"t4g.medium","imageId":"ami-test","region":"us-west-2"}`), nil
		})
		got, err := m.Instance(t.Context())
		if status == http.StatusForbidden {
			checkIMDSError(t, err, "GetInstanceIdentityDocument")
			continue
		}
		want := cloud.InstanceFacts{InstanceID: "i-test", Type: "t4g.medium", AMI: "ami-test", Region: "us-west-2"}
		if got != want || err != nil {
			t.Errorf("Instance() = %+v, %v, want %+v", got, err, want)
		}
	}
}
