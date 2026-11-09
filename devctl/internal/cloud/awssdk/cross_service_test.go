package awssdk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestListMethodsPaginateThroughFakeEndpoint(t *testing.T) {
	// R-7EY7-I0O5
	transport := &paginationTransport{calls: make(map[string]int)}
	clients, err := OpenWithLoader(context.Background(), "profile", "region", func(
		context.Context, string, string,
	) (aws.Config, error) {
		return aws.Config{
			Region: "us-test-1",
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "fake", SecretAccessKey: "fake", SessionToken: "fake"}, nil
			}),
			HTTPClient: &http.Client{Transport: transport},
		}, nil
	})
	if err != nil {
		t.Fatalf("OpenWithLoader: %v", err)
	}

	instances, err := clients.EC2.ListSpaceInstances(context.Background(), "root.example")
	if err != nil || !reflect.DeepEqual(instanceIDs(instances), []string{"i-one", "i-two"}) {
		t.Fatalf("ListSpaceInstances = %#v, %v; want both endpoint pages", instances, err)
	}
	parameters, err := clients.SSM.ListParameters(context.Background(), "/spaces")
	if err != nil || !reflect.DeepEqual(parameterNames(parameters), []string{"/spaces/one", "/spaces/two"}) {
		t.Fatalf("ListParameters = %#v, %v; want both endpoint pages", parameters, err)
	}
	records, err := clients.Route53.ListRecords(context.Background(), "Z1")
	if err != nil || !reflect.DeepEqual(recordNames(records), []string{"one.example", "two.example"}) {
		t.Fatalf("ListRecords = %#v, %v; want both endpoint pages", records, err)
	}

	boundary, err := clients.IAM.PermissionsBoundary(context.Background(), "target")
	if err != nil || boundary != "arn:target" {
		t.Fatalf("PermissionsBoundary = %q, %v; want arn:target, nil", boundary, err)
	}

	for _, operation := range []string{"DescribeInstances", "GetParametersByPath", "ListResourceRecordSets", "ListPolicies"} {
		if transport.calls[operation] != 2 {
			t.Errorf("%s endpoint calls = %d; want 2", operation, transport.calls[operation])
		}
	}
}

func TestDeleteMissingResourcesSucceeds(t *testing.T) {
	// R-YS6P-S4XM
	missingEC2 := func(code string) *ec2Client {
		return &ec2Client{sdk: &fakeEC2{err: &smithy.GenericAPIError{Code: code, Message: "missing"}}}
	}
	if err := missingEC2("InvalidAssociationID.NotFound").DisassociateAddress(context.Background(), "eipassoc-missing"); err != nil {
		t.Errorf("DisassociateAddress missing: %v", err)
	}
	if err := missingEC2("InvalidAllocationID.NotFound").ReleaseAddress(context.Background(), "eipalloc-missing"); err != nil {
		t.Errorf("ReleaseAddress missing: %v", err)
	}
	if err := (&ssmClient{sdk: &fakeSSM{err: &smithy.GenericAPIError{Code: "ParameterNotFound", Message: "missing"}}}).DeleteParameter(context.Background(), "/missing"); err != nil {
		t.Errorf("DeleteParameter missing: %v", err)
	}

	missingIAM := &smithy.GenericAPIError{Code: iamNoSuchEntity, Message: "missing"}
	iamDeletes := []struct {
		name string
		call func(*iamClient) error
	}{
		{"DeleteRolePolicy", func(client *iamClient) error { return client.DeleteRolePolicy(context.Background(), "role", "policy") }},
		{"RemoveRoleFromInstanceProfile", func(client *iamClient) error {
			return client.RemoveRoleFromInstanceProfile(context.Background(), "profile", "role")
		}},
		{"DeleteInstanceProfile", func(client *iamClient) error { return client.DeleteInstanceProfile(context.Background(), "profile") }},
		{"DeleteRole", func(client *iamClient) error { return client.DeleteRole(context.Background(), "role") }},
	}
	for _, test := range iamDeletes {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(&iamClient{sdk: &fakeIAM{err: missingIAM}}); err != nil {
				t.Errorf("missing resource: %v", err)
			}
		})
	}

	fakeObjects := &fakeS3{deleteObjects: func(*s3.DeleteObjectsInput) (*s3.DeleteObjectsOutput, error) {
		return &s3.DeleteObjectsOutput{Errors: []s3types.Error{{Code: aws.String("NoSuchKey"), Key: aws.String("missing")}}}, nil
	}}
	if err := (&s3Client{sdk: fakeObjects}).DeleteObjects(context.Background(), "bucket", []string{"missing"}); err != nil {
		t.Errorf("DeleteObjects missing: %v", err)
	}
}

type paginationTransport struct {
	calls           map[string]int
	boundaryOnFirst bool
	failIAMSecond   bool
}

func (p *paginationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil {
		var err error
		body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
	}
	operation, response := p.response(request, string(body))
	p.calls[operation]++
	status := http.StatusOK
	if operation == "ListPolicies" && p.failIAMSecond && p.calls[operation] == 2 {
		status = http.StatusForbidden
		response = `<ErrorResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><Error><Type>Sender</Type><Code>AccessDenied</Code><Message>denied</Message></Error></ErrorResponse>`
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(response)),
		Request:    request,
	}, nil
}

func (p *paginationTransport) response(request *http.Request, body string) (string, string) {
	host := request.URL.Hostname()
	if strings.HasPrefix(host, "ec2.") {
		values, _ := url.ParseQuery(body)
		if values.Get("NextToken") == "" {
			return "DescribeInstances", `<DescribeInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"><reservationSet><item><instancesSet><item><instanceId>i-one</instanceId><instanceState><name>running</name></instanceState></item></instancesSet></item></reservationSet><nextToken>ec2-next</nextToken></DescribeInstancesResponse>`
		}
		return "DescribeInstances", `<DescribeInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"><reservationSet><item><instancesSet><item><instanceId>i-two</instanceId><instanceState><name>stopped</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`
	}
	if strings.HasPrefix(host, "ssm.") {
		if !strings.Contains(body, `"NextToken"`) {
			return "GetParametersByPath", `{"Parameters":[{"Name":"/spaces/one","Value":"one"}],"NextToken":"ssm-next"}`
		}
		return "GetParametersByPath", `{"Parameters":[{"Name":"/spaces/two","Value":"two"}]}`
	}
	if strings.HasPrefix(host, "iam.") {
		values, _ := url.ParseQuery(body)
		if values.Get("Marker") == "" {
			response := `<ListPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListPoliciesResult><Policies><member><PolicyName>other</PolicyName><Arn>arn:other</Arn></member></Policies><IsTruncated>true</IsTruncated><Marker>iam-next</Marker></ListPoliciesResult></ListPoliciesResponse>`
			if p.boundaryOnFirst {
				response = strings.ReplaceAll(response, "other", "target")
			}
			return "ListPolicies", response
		}
		response := `<ListPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListPoliciesResult><Policies><member><PolicyName>target</PolicyName><Arn>arn:target</Arn></member></Policies><IsTruncated>false</IsTruncated></ListPoliciesResult></ListPoliciesResponse>`
		if p.boundaryOnFirst {
			response = strings.ReplaceAll(response, "target", "other")
		}
		return "ListPolicies", response
	}
	if strings.HasSuffix(request.URL.Path, "/rrset") {
		if request.URL.Query().Get("name") == "" {
			return "ListResourceRecordSets", `<ListResourceRecordSetsResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/"><ResourceRecordSets><ResourceRecordSet><Name>one.example.</Name><Type>A</Type><TTL>60</TTL><ResourceRecords><ResourceRecord><Value>192.0.2.1</Value></ResourceRecord></ResourceRecords></ResourceRecordSet></ResourceRecordSets><IsTruncated>true</IsTruncated><NextRecordName>two.example.</NextRecordName><NextRecordType>A</NextRecordType><MaxItems>1</MaxItems></ListResourceRecordSetsResponse>`
		}
		return "ListResourceRecordSets", `<ListResourceRecordSetsResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/"><ResourceRecordSets><ResourceRecordSet><Name>two.example.</Name><Type>A</Type><TTL>60</TTL><ResourceRecords><ResourceRecord><Value>192.0.2.2</Value></ResourceRecord></ResourceRecords></ResourceRecordSet></ResourceRecordSets><IsTruncated>false</IsTruncated><MaxItems>1</MaxItems></ListResourceRecordSetsResponse>`
	}
	return "unknown", ""
}

func instanceIDs(instances []cloud.Instance) []string {
	result := make([]string, len(instances))
	for index := range instances {
		result[index] = instances[index].ID
	}
	return result
}

func parameterNames(parameters []cloud.Parameter) []string {
	result := make([]string, len(parameters))
	for index := range parameters {
		result[index] = parameters[index].Name
	}
	return result
}

func recordNames(records []cloud.Record) []string {
	result := make([]string, len(records))
	for index := range records {
		result[index] = records[index].Name
	}
	return result
}

func TestPermissionsBoundaryExhaustsPagesAfterMatch(t *testing.T) {
	// R-7EY7-I0O5 R-VV8S-ZVE7
	for _, failure := range []bool{false, true} {
		name := "later page"
		if failure {
			name = "later error"
		}
		t.Run(name, func(t *testing.T) {
			transport := &paginationTransport{calls: make(map[string]int), boundaryOnFirst: true, failIAMSecond: failure}
			clients, err := OpenWithLoader(t.Context(), "profile", "us-test-1", func(context.Context, string, string) (aws.Config, error) {
				return aws.Config{
					Region: "us-test-1", RetryMaxAttempts: 1,
					Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
						return aws.Credentials{AccessKeyID: "fake", SecretAccessKey: "fake"}, nil
					}),
					HTTPClient: &http.Client{Transport: transport},
				}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			arn, err := clients.IAM.PermissionsBoundary(t.Context(), "target")
			if failure {
				var got *cloud.Error
				if arn != "" || !errors.As(err, &got) || got.Service != "iam" || got.Operation != "ListPolicies" || got.Subject != "" || got.Code != "AccessDenied" {
					t.Fatalf("PermissionsBoundary = %q, %#v; want later ListPolicies error", arn, err)
				}
			} else if err != nil || arn != "arn:target" {
				t.Fatalf("PermissionsBoundary = %q, %v; want first page match", arn, err)
			}
			if transport.calls["ListPolicies"] != 2 {
				t.Fatalf("ListPolicies calls = %d; want 2", transport.calls["ListPolicies"])
			}
		})
	}
}
