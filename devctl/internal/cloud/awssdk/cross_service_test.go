package awssdk

import (
	"context"
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
	// R-YQYT-ED6X
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

	instances, err := clients.EC2.ListSpaceInstances(context.Background())
	if err != nil || !reflect.DeepEqual(instanceIDs(instances), []string{"i-one", "i-two"}) {
		t.Fatalf("ListSpaceInstances = %#v, %v; want both endpoint pages", instances, err)
	}
	parameters, err := clients.SSM.ListParameters(context.Background(), "/spaces")
	if err != nil || !reflect.DeepEqual(parameterNames(parameters), []string{"/spaces/one", "/spaces/two"}) {
		t.Fatalf("ListParameters = %#v, %v; want both endpoint pages", parameters, err)
	}
	objects, err := clients.S3.ListObjects(context.Background(), "bucket", "prefix/")
	if err != nil || !reflect.DeepEqual(objectKeys(objects), []string{"prefix/one", "prefix/two"}) {
		t.Fatalf("ListObjects = %#v, %v; want both endpoint pages", objects, err)
	}
	zones, err := clients.Route53.ListZones(context.Background())
	if err != nil || !reflect.DeepEqual(zones, []cloud.Zone{{ID: "Z1", Name: "one.example"}, {ID: "Z2", Name: "two.example"}}) {
		t.Fatalf("ListZones = %#v, %v; want both endpoint pages", zones, err)
	}
	records, err := clients.Route53.ListRecords(context.Background(), "Z1")
	if err != nil || !reflect.DeepEqual(recordNames(records), []string{"one.example", "two.example"}) {
		t.Fatalf("ListRecords = %#v, %v; want both endpoint pages", records, err)
	}

	for _, operation := range []string{"DescribeInstances", "GetParametersByPath", "ListObjectsV2", "ListHostedZones", "ListResourceRecordSets"} {
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
	calls map[string]int
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
	return &http.Response{
		StatusCode: http.StatusOK,
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
	if strings.Contains(host, ".s3.") {
		if request.URL.Query().Get("continuation-token") == "" {
			return "ListObjectsV2", `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>true</IsTruncated><NextContinuationToken>s3-next</NextContinuationToken><Contents><Key>prefix/one</Key><Size>1</Size><LastModified>2026-01-01T00:00:00Z</LastModified></Contents></ListBucketResult>`
		}
		return "ListObjectsV2", `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated><Contents><Key>prefix/two</Key><Size>2</Size><LastModified>2026-01-02T00:00:00Z</LastModified></Contents></ListBucketResult>`
	}
	if strings.HasSuffix(request.URL.Path, "/hostedzone") {
		if request.URL.Query().Get("marker") == "" {
			return "ListHostedZones", `<ListHostedZonesResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/"><HostedZones><HostedZone><Id>/hostedzone/Z1</Id><Name>one.example.</Name><CallerReference>one</CallerReference></HostedZone></HostedZones><IsTruncated>true</IsTruncated><NextMarker>zone-next</NextMarker><MaxItems>1</MaxItems></ListHostedZonesResponse>`
		}
		return "ListHostedZones", `<ListHostedZonesResponse xmlns="https://route53.amazonaws.com/doc/2013-04-01/"><HostedZones><HostedZone><Id>/hostedzone/Z2</Id><Name>two.example.</Name><CallerReference>two</CallerReference></HostedZone></HostedZones><IsTruncated>false</IsTruncated><MaxItems>1</MaxItems></ListHostedZonesResponse>`
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

func objectKeys(objects []cloud.Object) []string {
	result := make([]string, len(objects))
	for index := range objects {
		result[index] = objects[index].Key
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
