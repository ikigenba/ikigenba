package awssdk_test

import (
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const roleFields = `<AccessKeyId>issued-key</AccessKeyId><SecretAccessKey>issued-secret</SecretAccessKey>
	<SessionToken>issued-token</SessionToken><Expiration>2031-02-03T04:05:06Z</Expiration>`

func roleResponse(fields string) string {
	return `<AssumeRoleResponse><AssumeRoleResult><Credentials>` + fields + `</Credentials></AssumeRoleResult></AssumeRoleResponse>`
}

func TestIdentity(t *testing.T) {
	// R-YO3S-W08U
	identity := cloud.Caller{Account: "123456789012", Arn: "test-arn", UserID: "test-user"}
	withoutUserID := cloud.Caller{Account: identity.Account, Arn: identity.Arn}
	coreFields := "<Account>" + identity.Account + "</Account><Arn>" + identity.Arn + "</Arn>"
	for _, test := range []struct {
		name, fields string
		want         cloud.Caller
	}{
		{"complete", coreFields + "<UserId>" + identity.UserID + "</UserId>", identity},
		{"missing user id", coreFields, withoutUserID},
		{"missing account", "<Arn>" + identity.Arn + "</Arn>", cloud.Caller{}},
		{"missing ARN", "<Account>" + identity.Account + "</Account>", cloud.Caller{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				checkSTSSignature(t, r)
				checkForm(t, form(t, r, "GetCallerIdentity"), nil)
				return xmlResponse(200, "<GetCallerIdentityResponse><GetCallerIdentityResult>"+test.fields+
					"</GetCallerIdentityResult></GetCallerIdentityResponse>"), nil
			})
			got, err := c.Identity(t.Context())
			if calls != 1 {
				t.Errorf("Identity sent %d requests, want 1", calls)
			}
			if test.want != (cloud.Caller{}) {
				if err != nil || got != test.want {
					t.Errorf("Identity = %+v, %v, want %+v", got, err, test.want)
				}
				return
			}
			checkError(t, err, "sts", "GetCallerIdentity", "", cloud.CodeBadResponse, cloud.ClassRefused)
			if got != (cloud.Caller{}) {
				t.Error("Identity returned a caller with an error")
			}
		})
	}
}

func TestAssumeRole(t *testing.T) {
	// R-YPBP-9RZJ
	want := cloud.Credentials{
		AccessKeyID: "issued-key", SecretAccessKey: "issued-secret", SessionToken: "issued-token",
		Expires: time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC),
	}
	const policy = "{\n  \"example\": \"preserve + whitespace\"\n}"
	for _, test := range []struct {
		policy   string
		duration time.Duration
		seconds  string
	}{
		{"", 53*time.Minute + 900*time.Millisecond, "3180"},
		{policy, time.Hour, "3600"},
		{policy, (math.MaxInt32+1)*time.Second + time.Millisecond, "2147483648"},
		{"", (math.MinInt32-1)*time.Second - time.Millisecond, "-2147483649"},
	} {
		req := cloud.RoleRequest{
			RoleARN: "arn:aws:iam::123456789012:role/test-role", SessionName: "test-session",
			Policy: test.policy, Duration: test.duration,
		}
		calls := 0
		c := open(t, func(r *http.Request) (*http.Response, error) {
			calls++
			params := map[string]string{"RoleArn": req.RoleARN, "RoleSessionName": req.SessionName, "DurationSeconds": test.seconds}
			if test.policy != "" {
				params["Policy"] = test.policy
			}
			checkSTSSignature(t, r)
			checkForm(t, form(t, r, "AssumeRole"), params)
			return xmlResponse(200, roleResponse(roleFields)), nil
		})
		got, err := c.AssumeRole(t.Context(), req)
		if err != nil || got != want || calls != 1 {
			t.Errorf("AssumeRole did not return the issued credentials in one request: error %v, requests %d", err, calls)
		}
	}
}

func TestAssumeRoleErrors(t *testing.T) {
	// R-YQJL-NJQ8
	const policy = "test-policy-text"
	request := cloud.RoleRequest{RoleARN: "test-role", SessionName: "test-session", Policy: policy, Duration: time.Hour}
	testFields := []string{
		"<AccessKeyId>issued-key</AccessKeyId>",
		"<SecretAccessKey>issued-secret</SecretAccessKey>",
		"<SessionToken>issued-token</SessionToken>",
		"<Expiration>2031-02-03T04:05:06Z</Expiration>",
	}
	for _, field := range testFields {
		c := open(t, func(*http.Request) (*http.Response, error) {
			return xmlResponse(200, roleResponse(strings.Replace(roleFields, field, "", 1))), nil
		})
		got, err := c.AssumeRole(t.Context(), request)
		checkError(t, err, "sts", "AssumeRole", "", cloud.CodeBadResponse, cloud.ClassRefused)
		if got != (cloud.Credentials{}) || err.Error() != "sts AssumeRole: BadResponse" {
			t.Errorf("invalid credentials returned nonzero credentials or unexpected error %q", err)
		}
	}
	c := open(t, func(*http.Request) (*http.Response, error) {
		return xmlResponse(403, "<ErrorResponse><Error><Code>AccessDenied</Code><Message>"+
			policy+" issued-key issued-secret issued-token</Message></Error></ErrorResponse>"), nil
	})
	got, err := c.AssumeRole(t.Context(), request)
	checkError(t, err, "sts", "AssumeRole", "", "AccessDenied", cloud.ClassRefused)
	if got != (cloud.Credentials{}) || err.Error() != "sts AssumeRole: AccessDenied" {
		t.Errorf("STS failure returned nonzero credentials or unexpected error %q", err)
	}
}

func checkSTSSignature(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodPost || r.URL.String() != "https://sts."+region+".amazonaws.com/" {
		t.Errorf("STS request = %s %s", r.Method, r.URL)
	}
	checkSignature(t, r, "sts", region)
}
