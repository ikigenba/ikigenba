package awssdk_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
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

func configFor(r wire) aws.Config {
	return aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider("TESTKEY", "testsecret", ""),
		HTTPClient:  &http.Client{Transport: r},
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
	// R-X328-TTPW R-X4A5-7LGL R-XP0F-PP2E
	var opener cloud.Opener = awssdk.Open
	if opener == nil {
		t.Fatal("missing default opener")
	}
	ctx := t.Context()
	loadFailure := errors.New("loader failed")
	var load awssdk.ConfigLoader = func(gotCtx context.Context, gotRegion string) (aws.Config, error) {
		if gotCtx != ctx || gotRegion != "custom-region" {
			t.Errorf("loader received %v and %q", gotCtx, gotRegion)
		}
		return aws.Config{}, loadFailure
	}
	c, err := awssdk.OpenWithLoader(ctx, "custom-region", load)
	if c != nil || !errors.Is(err, loadFailure) || reflect.TypeOf(err) != reflect.TypeOf(loadFailure) {
		t.Fatalf("loader failure = %v, %v", c, err)
	}

	calls := 0
	cfg := configFor(func(r *http.Request) (*http.Response, error) {
		calls++
		auth := r.Header.Get("Authorization")
		if calls == 1 {
			checkForm(t, form(t, r, "GetCallerIdentity"), nil)
			if r.URL.String() != "https://sts."+region+".amazonaws.com/" || !strings.Contains(auth, "Credential=TESTKEY/") ||
				!strings.Contains(auth, "/"+region+"/sts/aws4_request") {
				t.Errorf("STS request = %s, %s", r.URL, auth)
			}
			return xmlResponse(200, `<GetCallerIdentityResponse><GetCallerIdentityResult><Account>123456789012</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`), nil
		}
		if !strings.Contains(auth, "Credential=TESTKEY/") || !strings.Contains(auth, "/us-east-1/route53/aws4_request") {
			t.Errorf("Route 53 signing = %s", auth)
		}
		return xmlResponse(200, `<GetChangeResponse><ChangeInfo><Status>INSYNC</Status></ChangeInfo></GetChangeResponse>`), nil
	})
	c = openConfig(t, cfg)
	id, err := c.Account(ctx)
	if id != "123456789012" || err != nil {
		t.Fatalf("Account = %q, %v", id, err)
	}
	inSync, err := c.ChangeInsync(ctx, "change-1")
	if !inSync || err != nil || calls != 2 {
		t.Fatalf("ChangeInsync = %v, %v; calls = %d", inSync, err, calls)
	}
}

func failureXML(code string) string {
	return `<Response><Errors><Error><Code>` + code + `</Code><Message>private AWS message</Message></Error></Errors></Response>`
}

func TestErrorCodesAndClasses(t *testing.T) {
	// R-X5I1-LD7A R-X6PX-Z4XZ R-XRG8-H8JS
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
		c := open(t, func(*http.Request) (*http.Response, error) {
			return xmlResponse(503, failureXML("Unrecognized")), nil
		})
		checkError(t, c.StopInstance(t.Context(), "i-test"), "ec2", "StopInstances", "i-test", "Unrecognized", cloud.ClassTransient)
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
