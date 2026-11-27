package awssdk_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/awssdk"
)

const region = "us-east-2"

type wire func(*http.Request) (*http.Response, error)

func (r wire) RoundTrip(req *http.Request) (*http.Response, error) {
	return r(req)
}

type endpointSource string

func (s endpointSource) GetServiceBaseEndpoint(context.Context, string) (string, bool, error) {
	return string(s), true, nil
}

func configFor(r wire) aws.Config {
	return aws.Config{
		Region:      region,
		Credentials: testCredentials(),
		HTTPClient:  &http.Client{Transport: r},
	}
}

func testCredentials() credentials.StaticCredentialsProvider {
	return credentials.NewStaticCredentialsProvider("TESTKEY", "testsecret", "")
}

func checkSignature(t *testing.T, r *http.Request, service, signingRegion string) {
	t.Helper()
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Body.Close(); err != nil {
			t.Fatal(err)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	digest := sha256.Sum256(body)
	payload := hex.EncodeToString(digest[:])
	headerPayload := r.Header.Get("X-Amz-Content-Sha256")
	if service == "s3" && headerPayload == "UNSIGNED-PAYLOAD" {
		payload = headerPayload
	}
	if headerPayload != "" && headerPayload != payload {
		t.Fatalf("%s request carried a payload hash that differs from its body", service)
	}
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		t.Fatal(err)
	}
	creds, err := testCredentials().Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	auth := r.Header.Get("Authorization")
	_, tail, found := strings.Cut(auth, "SignedHeaders=")
	names, _, hasSignature := strings.Cut(tail, ", Signature=")
	if !found || !hasSignature {
		t.Fatalf("%s request is missing a SigV4 authorization header", service)
	}
	unsigned := r.Clone(t.Context())
	unsigned.Header = make(http.Header)
	for _, name := range strings.Split(names, ";") {
		canonical := http.CanonicalHeaderKey(name)
		if values := r.Header[canonical]; len(values) > 0 {
			unsigned.Header[canonical] = values
		}
	}
	signer := v4.NewSigner(func(options *v4.SignerOptions) { options.DisableURIPathEscaping = service == "s3" })
	if err := signer.SignHTTP(t.Context(), creds, unsigned, payload, service, signingRegion, at); err != nil {
		t.Fatal(err)
	}
	if unsigned.Header.Get("Authorization") != auth {
		t.Errorf("%s authorization does not match the signature for this request, region and credentials", service)
	}
}

func open(t *testing.T, r wire) cloud.Cloud {
	t.Helper()
	return openConfig(t, configFor(r))
}

func openConfig(t *testing.T, cfg aws.Config) cloud.Cloud {
	t.Helper()
	c, err := awssdk.OpenWithLoader(t.Context(), region, func(context.Context, string) (aws.Config, error) {
		return cfg, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func xmlResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{"Content-Type": {"text/xml"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func form(t *testing.T, r *http.Request, action string) url.Values {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	got := r.Form.Get("Action")
	if got != action {
		t.Fatalf("request action = %q, want %q", got, action)
	}
	r.Form.Del("Action")
	r.Form.Del("Version")
	return r.Form
}

func checkForm(t *testing.T, got url.Values, want map[string]string) {
	t.Helper()
	values := make(url.Values, len(want))
	for k, v := range want {
		values.Set(k, v)
	}
	if !reflect.DeepEqual(got, values) {
		t.Errorf("request parameters = %v, want %v", got, values)
	}
}

func checkError(t *testing.T, err error, service, op, subject, code string, class cloud.Class) {
	t.Helper()
	var got *cloud.Error
	if reflect.TypeOf(err) != reflect.TypeFor[*cloud.Error]() || !errors.As(err, &got) {
		t.Fatalf("error = %T %v, want *cloud.Error", err, err)
	}
	if got.Service != service || got.Operation != op || got.Subject != subject || got.Code != code || got.Class != class {
		t.Errorf("error = %+v, want %s %s %q: %s (class %v), got class %v", got, service, op, subject, code, class, got.Class)
	}
}

func TestOpenAndAccount(t *testing.T) {
	// R-X328-TTPW R-YJ87-CXA2 R-XP0F-PP2E
	var opener cloud.Opener = awssdk.Open
	if opener == nil {
		t.Fatal("missing default opener")
	}
	ctx := t.Context()
	loadFailure := errors.New("loader failed")
	loads := 0
	var load awssdk.ConfigLoader = func(gotCtx context.Context, gotRegion string) (aws.Config, error) {
		loads++
		if gotCtx != ctx || gotRegion != "custom-region" {
			t.Errorf("loader received %v and %q", gotCtx, gotRegion)
		}
		return aws.Config{}, loadFailure
	}
	c, err := awssdk.OpenWithLoader(ctx, "custom-region", load)
	if c != nil || !errors.Is(err, loadFailure) || reflect.TypeOf(err) != reflect.TypeOf(loadFailure) || loads != 1 {
		t.Fatalf("loader failure = %v, %v", c, err)
	}

	const targetRegion = "us-west-2"
	const bucket = "wire-test-bucket"
	tests := []struct {
		serviceRegion, service, url, action, body string
	}{
		{targetRegion, "ec2", "https://ec2." + targetRegion + ".amazonaws.com/", "StopInstances", `<StopInstancesResponse/>`},
		{"us-east-1", "route53", "", "", `<GetChangeResponse><ChangeInfo><Status>INSYNC</Status></ChangeInfo></GetChangeResponse>`},
		{targetRegion, "sts", "https://sts." + targetRegion + ".amazonaws.com/", "GetCallerIdentity", `<GetCallerIdentityResponse><GetCallerIdentityResult><Account>123456789012</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`},
		{targetRegion, "sts", "https://sts." + targetRegion + ".amazonaws.com/", "AssumeRole", roleResponse(roleFields)},
		{targetRegion, "s3", "https://" + bucket + ".s3." + targetRegion + ".amazonaws.com/?list-type=2&max-keys=1&prefix=backups%2F", "", `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`},
	}
	calls := 0
	cfg := configFor(func(r *http.Request) (*http.Response, error) {
		if calls >= len(tests) {
			t.Fatalf("extra request %s %s", r.Method, r.URL)
		}
		step := tests[calls]
		calls++
		if step.url != "" && r.URL.String() != step.url {
			t.Errorf("%s URL = %s, want %s", step.service, r.URL, step.url)
		}
		checkSignature(t, r, step.service, step.serviceRegion)
		if step.action != "" {
			params := form(t, r, step.action)
			if step.action == "GetCallerIdentity" {
				checkForm(t, params, nil)
			}
		}
		return xmlResponse(200, step.body), nil
	})
	cfg.BaseEndpoint = aws.String("https://base-endpoint.invalid/")
	cfg.ConfigSources = []any{endpointSource("https://service-endpoint.invalid/")}
	c, err = awssdk.OpenWithLoader(ctx, targetRegion, func(gotCtx context.Context, gotRegion string) (aws.Config, error) {
		loads++
		if gotCtx != ctx || gotRegion != targetRegion {
			t.Errorf("loader received context %v and region %q", gotCtx, gotRegion)
		}
		return cfg, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StopInstance(ctx, "i-test"); err != nil {
		t.Fatal(err)
	}
	inSync, err := c.ChangeInsync(ctx, "change-1")
	if !inSync || err != nil {
		t.Fatalf("ChangeInsync = %v, %v", inSync, err)
	}
	id, err := c.Account(ctx)
	if id != "123456789012" || err != nil {
		t.Fatalf("Account = %q, %v", id, err)
	}
	if _, err := c.AssumeRole(ctx, cloud.RoleRequest{RoleARN: "test-role", SessionName: "test-session", Duration: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.HasKeys(ctx, bucket, "backups/"); err != nil {
		t.Fatal(err)
	}
	if calls != len(tests) || loads != 2 {
		t.Fatalf("requests = %d, loader calls = %d", calls, loads)
	}
}

func failureXML(code string) string {
	return `<Response><Errors><Error><Code>` + code + `</Code><Message>private AWS message</Message></Error></Errors></Response>`
}

func TestErrorCodesAndClasses(t *testing.T) {
	// R-YKG3-QP0R R-YI0A-Z5JD R-XRG8-H8JS
	tests := []struct {
		code  string
		class cloud.Class
	}{
		{"InvalidInstanceID.NotFound", cloud.ClassNotFound},
		{"InvalidVolume.NotFound", cloud.ClassNotFound},
		{"InvalidAllocationID.NotFound", cloud.ClassNotFound},
		{"InvalidAssociationID.NotFound", cloud.ClassNotFound},
		{"InvalidVolumeModification.NotFound", cloud.ClassNotFound},
		{"NoSuchChange", cloud.ClassNotFound},
		{"NoSuchBucket", cloud.ClassNotFound},
		{"IdempotentParameterMismatch", cloud.ClassMismatch},
		{"VolumeModificationRateExceeded", cloud.ClassTransient},
		{"RequestLimitExceeded", cloud.ClassTransient},
		{"Throttling", cloud.ClassTransient},
		{"ThrottlingException", cloud.ClassTransient},
		{"PriorRequestNotComplete", cloud.ClassTransient},
		{"InternalError", cloud.ClassTransient},
		{"InternalFailure", cloud.ClassTransient},
		{"ServiceUnavailable", cloud.ClassTransient},
		{"Unavailable", cloud.ClassTransient},
		{"IncorrectInstanceState", cloud.ClassTransient},
		{"IncorrectState", cloud.ClassTransient},
		{"SlowDown", cloud.ClassTransient},
		{"RequestTimeout", cloud.ClassTransient},
		{"AccessDenied", cloud.ClassRefused},
		{"ExpiredToken", cloud.ClassRefused},
		{"UnauthorizedOperation", cloud.ClassRefused},
		{cloud.CodeTagContract, cloud.ClassRefused},
		{cloud.CodeBadResponse, cloud.ClassRefused},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			requests := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				requests++
				form(t, r, "StopInstances")
				return xmlResponse(400, failureXML(test.code)), nil
			})
			err := c.StopInstance(t.Context(), "i-test")
			checkError(t, err, "ec2", "StopInstances", "i-test", test.code, test.class)
			if requests != 1 || err.Error() != "ec2 StopInstances i-test: "+test.code {
				t.Errorf("calls = %d, error = %v", requests, err)
			}
		})
	}
	t.Run("coded service failure", func(t *testing.T) {
		for _, code := range []string{"InvalidInstanceID.NotFound", "IdempotentParameterMismatch", "AccessDenied"} {
			c := open(t, func(*http.Request) (*http.Response, error) {
				return xmlResponse(503, failureXML(code)), nil
			})
			checkError(t, c.StopInstance(t.Context(), "i-test"), "ec2", "StopInstances", "i-test", code, cloud.ClassTransient)
		}
	})
	t.Run("uncoded service failure", func(t *testing.T) {
		c := open(t, func(*http.Request) (*http.Response, error) {
			return xmlResponse(502, "<Response"), nil
		})
		checkError(t, c.StopInstance(t.Context(), "i-test"), "ec2", "StopInstances", "i-test", "HTTP 502", cloud.ClassTransient)
	})
	t.Run("transport failure", func(t *testing.T) {
		cause := errors.New("transport message with a URL")
		c := open(t, func(*http.Request) (*http.Response, error) { return nil, cause })
		err := c.StopInstance(t.Context(), "i-test")
		checkError(t, err, "ec2", "StopInstances", "i-test", cloud.CodeNoResponse, cloud.ClassTransient)
		if err.Error() != "ec2 StopInstances i-test: NoResponse" {
			t.Errorf("transport error text = %v", err)
		}
	})
	t.Run("first code", func(t *testing.T) {
		cfg := configFor(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport called with failed credentials")
			return nil, nil
		})
		cfg.Credentials = aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{}, errors.Join(
				&smithy.GenericAPIError{Code: "InternalFailure"},
				&smithy.GenericAPIError{Code: "UnauthorizedOperation"},
			)
		})
		c := openConfig(t, cfg)
		checkError(t, c.StopInstance(t.Context(), "i-test"), "ec2", "StopInstances", "i-test", "InternalFailure", cloud.ClassTransient)
	})
	t.Run("nested SDK operation", func(t *testing.T) {
		cfg := configFor(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport called with failed credentials")
			return nil, nil
		})
		cfg.Credentials = aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{}, fmt.Errorf("credentials: %w", &smithy.OperationError{
				ServiceID: "STS", OperationName: "AssumeRole",
				Err: &smithy.GenericAPIError{Code: "UnauthorizedOperation"},
			})
		})
		c := openConfig(t, cfg)
		checkError(t, c.StopInstance(t.Context(), "i-test"), "ec2", "StopInstances", "i-test", cloud.CodeNoResponse, cloud.ClassTransient)
	})
}
