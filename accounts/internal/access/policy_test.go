package access_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/access"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

// R-WI49-3N6O
func TestConstants(t *testing.T) {
	const (
		concurrency uint8         = access.IssueParallel
		reissue     time.Duration = access.ReissueAfter
		budget      time.Duration = access.IssueBudget
	)
	if concurrency != 8 || reissue != 15*time.Minute || budget != 8*time.Second {
		t.Fatal("incorrect concurrency, renewal age, or budget")
	}
	if _, ok := any(access.ReissueAfter).(time.Duration); !ok {
		t.Error("ReissueAfter is not typed as time.Duration")
	}
	if _, ok := any(access.IssueBudget).(time.Duration); !ok {
		t.Error("IssueBudget is not typed as time.Duration")
	}
}

// R-WJC5-HEXD
func TestPolicy(t *testing.T) {
	want := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject","s3:DeleteObject","s3:AbortMultipartUpload"],"Resource":"arn:aws:s3:::b/accounts/acme/*"},{"Effect":"Allow","Action":"s3:ListBucket","Resource":"arn:aws:s3:::b","Condition":{"StringLike":{"s3:prefix":"accounts/acme/*"}}},{"Effect":"Allow","Action":"s3:GetBucketLocation","Resource":"arn:aws:s3:::b"}]}`
	if got := access.Policy("b", "aws", "accounts/acme/"); got != want {
		t.Errorf("Policy = %q, want %q", got, want)
	}
}

// R-WKK1-V6O2
func TestPolicyBound(t *testing.T) {
	bucket := strings.Repeat("b", 63)
	partition := "aws-us-gov"
	for _, tc := range []struct {
		prefix string
		length int
	}{
		{protocol.AccountPrefix(strings.Repeat("a", 32)), 659},
		{protocol.HostPrefix("host9223372036854775807", math.MaxInt64), 675},
	} {
		if got := len(access.Policy(bucket, partition, tc.prefix)); got != tc.length || got > cloud.MaxPolicyBytes {
			t.Errorf("policy length = %d, want %d and at most %d", got, tc.length, cloud.MaxPolicyBytes)
		}
	}
	for _, b := range []string{bucket, "", strings.Repeat("é", 31)} {
		for _, p := range []string{partition, "", strings.Repeat("é", 5)} {
			for _, x := range []string{strings.Repeat("x", 50), "", strings.Repeat("é", 25)} {
				if got := len(access.Policy(b, p, x)); got > cloud.MaxPolicyBytes {
					t.Errorf("policy for lengths (%d, %d, %d) has %d bytes", len(b), len(p), len(x), got)
				}
			}
		}
	}
}

// R-WLRY-8YER
func TestRoleARN(t *testing.T) {
	h := settings.Hosts{InstanceProfile: "hosts-role"}
	if got, want := access.RoleARN("aws", "000000000000", h), "arn:aws:iam::000000000000:role/hosts-role-backup"; got != want {
		t.Errorf("RoleARN = %q, want %q", got, want)
	}
	if got, want := access.RoleARN("aws-cn", "123", h), "arn:aws-cn:iam::123:role/hosts-role-backup"; got != want {
		t.Errorf("RoleARN = %q, want %q", got, want)
	}
}

// R-WMZU-MQ5G
func TestSessionName(t *testing.T) {
	for _, tc := range []struct{ host, slug, want string }{
		{"host01", "", "backup-host01"},
		{"host01", "acme", "backup-host01-acme"},
		{"i-012345", "", "backup-i-012345"},
		{strings.Repeat("h", 56), "é", "backup-" + strings.Repeat("h", 56) + "-"},
		{strings.Repeat("h", 55), "é", "backup-" + strings.Repeat("h", 55) + "-\xc3"},
	} {
		if got := access.SessionName(tc.host, tc.slug); got != tc.want {
			t.Errorf("SessionName(%q, %q) = %q, want %q", tc.host, tc.slug, got, tc.want)
		}
	}
}
