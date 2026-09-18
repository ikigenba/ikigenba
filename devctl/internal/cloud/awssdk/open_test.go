package awssdk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2sdk "github.com/aws/aws-sdk-go-v2/service/ec2"
	iamsdk "github.com/aws/aws-sdk-go-v2/service/iam"
	route53sdk "github.com/aws/aws-sdk-go-v2/service/route53"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	ssmsdk "github.com/aws/aws-sdk-go-v2/service/ssm"
	stssdk "github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestOpenWithLoaderUsesOnlySuppliedConfiguration(t *testing.T) {
	// R-CG22-V9E5 R-CH9Z-914U
	poisonDefaultConfiguration(t)

	const (
		profile   = "  MiXeD Profile  "
		region    = "caller-region-1"
		cfgRegion = "supplied-region-2"
	)
	transport := &recordingTransport{}
	called := 0
	loader := func(ctx context.Context, gotProfile, gotRegion string) (aws.Config, error) {
		called++
		if ctx != context.Background() {
			t.Fatal("OpenWithLoader changed the context")
		}
		if gotProfile != profile || gotRegion != region {
			t.Fatalf("loader arguments = %q, %q; want %q, %q", gotProfile, gotRegion, profile, region)
		}
		return aws.Config{
			Region: cfgRegion,
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     "fake-access",
					SecretAccessKey: "fake-secret",
					SessionToken:    "fake-token",
				}, nil
			}),
			HTTPClient: &http.Client{Transport: transport},
		}, nil
	}

	clients, err := OpenWithLoader(context.Background(), profile, region, loader)
	if err != nil {
		t.Fatalf("OpenWithLoader: %v", err)
	}
	if called != 1 {
		t.Fatalf("loader calls = %d; want 1", called)
	}
	assertClientsPresent(t, clients)
	assertClientRegions(t, clients, cfgRegion)
	exerciseClients(t, clients)
	if transport.requests != 6 {
		t.Fatalf("transport requests = %d; want 6", transport.requests)
	}
}

func TestOpenWithLoaderFailureReturnsNoClients(t *testing.T) {
	// R-CG22-V9E5
	poisonDefaultConfiguration(t)
	wantErr := errors.New("configuration failed")
	clients, err := OpenWithLoader(context.Background(), "profile", "region", func(
		context.Context, string, string,
	) (aws.Config, error) {
		return aws.Config{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v; want %v", err, wantErr)
	}
	assertClientsAbsent(t, clients)
}

func TestOpenLoadsExactProfileAndRegion(t *testing.T) {
	// R-YM37-VA85 R-YNB4-91YU R-CH9Z-914U
	const profile = "MiXeD Profile"
	configPath := filepath.Join(t.TempDir(), "config")
	contents := "[profile " + profile + "]\nregion = profile-region-1\n" +
		"[profile mixed profile]\nregion = wrong-region-9\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	isolateProductionConfiguration(t, configPath)

	clients, err := Open(context.Background(), profile, "")
	if err != nil {
		t.Fatalf("Open with profile region: %v", err)
	}
	assertClientsPresent(t, clients)
	assertClientRegions(t, clients, "profile-region-1")

	clients, err = Open(context.Background(), profile, "override-region-2")
	if err != nil {
		t.Fatalf("Open with explicit region: %v", err)
	}
	assertClientRegions(t, clients, "override-region-2")
}

func TestOpenConfigurationFailureReturnsNoClients(t *testing.T) {
	// R-YNB4-91YU
	configPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(configPath, []byte("[profile malformed\nregion = nowhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	isolateProductionConfiguration(t, configPath)

	clients, err := Open(context.Background(), "malformed", "")
	if err == nil {
		t.Fatal("Open returned a nil error for malformed shared configuration")
	}
	assertClientsAbsent(t, clients)
}

func assertClientsPresent(t *testing.T, clients cloud.Clients) {
	t.Helper()
	if clients.EC2 == nil || clients.SSM == nil || clients.Route53 == nil ||
		clients.S3 == nil || clients.IAM == nil || clients.STS == nil {
		t.Fatalf("Open returned nil client fields: %#v", clients)
	}
}

func assertClientsAbsent(t *testing.T, clients cloud.Clients) {
	t.Helper()
	if clients.EC2 != nil || clients.SSM != nil || clients.Route53 != nil ||
		clients.S3 != nil || clients.IAM != nil || clients.STS != nil {
		t.Fatalf("Open returned clients with an error: %#v", clients)
	}
}

func assertClientRegions(t *testing.T, clients cloud.Clients, want string) {
	t.Helper()
	regions := map[string]string{
		"ec2":     clients.EC2.(*ec2Client).sdk.(*ec2sdk.Client).Options().Region,
		"ssm":     clients.SSM.(*ssmClient).sdk.(*ssmsdk.Client).Options().Region,
		"route53": clients.Route53.(*route53Client).sdk.(*route53sdk.Client).Options().Region,
		"s3":      clients.S3.(*s3Client).sdk.(*s3sdk.Client).Options().Region,
		"iam":     clients.IAM.(*iamClient).sdk.(*iamsdk.Client).Options().Region,
		"sts":     clients.STS.(*stsClient).sdk.(*stssdk.Client).Options().Region,
	}
	for service, got := range regions {
		if got != want {
			t.Errorf("%s client region = %q; want %q", service, got, want)
		}
	}
}

func exerciseClients(t *testing.T, clients cloud.Clients) {
	t.Helper()
	ctx := context.Background()
	if err := clients.EC2.StartInstance(ctx, "i-test"); err != nil {
		t.Errorf("EC2: %v", err)
	}
	if _, err := clients.SSM.GetParameter(ctx, "/test"); err != nil {
		t.Errorf("SSM: %v", err)
	}
	if _, err := clients.Route53.ListZones(ctx); err != nil {
		t.Errorf("Route53: %v", err)
	}
	if _, err := clients.S3.ListObjects(ctx, "bucket", "prefix"); err != nil {
		t.Errorf("S3: %v", err)
	}
	if _, err := clients.IAM.RoleExists(ctx, "role"); err != nil {
		t.Errorf("IAM: %v", err)
	}
	if _, err := clients.STS.CallerAccountID(ctx); err != nil {
		t.Errorf("STS: %v", err)
	}
}

type recordingTransport struct {
	requests int
}

func (r *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	r.requests++
	if !strings.Contains(request.Header.Get("Authorization"), "Credential=fake-access/") {
		return nil, errors.New("request was not signed with supplied fake credentials")
	}

	body := responseBody(request)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func responseBody(request *http.Request) string {
	host := request.URL.Hostname()
	switch {
	case strings.HasPrefix(host, "ec2."):
		return `<StartInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"><requestId>x</requestId><instancesSet/></StartInstancesResponse>`
	case strings.HasPrefix(host, "ssm."):
		return `{"Parameter":{"Name":"/test","Type":"SecureString","Value":"value"}}`
	case strings.HasPrefix(host, "route53."):
		return `<ListHostedZonesResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/"><HostedZones/><IsTruncated>false</IsTruncated><MaxItems>100</MaxItems></ListHostedZonesResponse>`
	case strings.HasPrefix(host, "s3.") || strings.HasPrefix(host, "bucket.s3."):
		return `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListBucketResult>`
	case strings.HasPrefix(host, "iam."):
		return `<GetRoleResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><GetRoleResult><Role><Path>/</Path><RoleName>role</RoleName><RoleId>id</RoleId><Arn>arn:aws:iam::1:role/role</Arn><CreateDate>2020-01-01T00:00:00Z</CreateDate></Role></GetRoleResult></GetRoleResponse>`
	case strings.HasPrefix(host, "sts."):
		return `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><GetCallerIdentityResult><Account>123456789012</Account><Arn>arn</Arn><UserId>user</UserId></GetCallerIdentityResult></GetCallerIdentityResponse>`
	default:
		return ""
	}
}

func poisonDefaultConfiguration(t *testing.T) {
	t.Helper()
	badConfig := filepath.Join(t.TempDir(), "must-not-be-read")
	if err := os.WriteFile(badConfig, []byte("[malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(t.TempDir(), "no-home"))
	t.Setenv("AWS_CONFIG_FILE", badConfig)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", badConfig)
	t.Setenv("AWS_PROFILE", "must-not-be-read")
	t.Setenv("AWS_ACCESS_KEY_ID", "must-not-be-read")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "must-not-be-read")
	t.Setenv("AWS_SESSION_TOKEN", "must-not-be-read")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func isolateProductionConfiguration(t *testing.T, configPath string) {
	t.Helper()
	credentialsPath := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(credentialsPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(t.TempDir(), "no-home"))
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)
	t.Setenv("AWS_PROFILE", "wrong-environment-profile")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}
