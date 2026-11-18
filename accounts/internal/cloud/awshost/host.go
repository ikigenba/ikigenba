// Package awshost implements the AWS operations needed by accountsd.
package awshost

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

// ConfigLoader obtains the SDK configuration for the host.
type ConfigLoader func(ctx context.Context) (aws.Config, error)

type presigner struct {
	region      string
	credentials aws.CredentialsProvider
	signer      *v4.Signer
}

type metadata struct {
	imds *imds.Client
}

// Open builds the host clients with the SDK's default configuration chain.
func Open(ctx context.Context) (cloud.Presigner, cloud.Metadata, error) {
	return OpenWithLoader(ctx, func(ctx context.Context) (aws.Config, error) {
		return config.LoadDefaultConfig(ctx)
	})
}

// OpenWithLoader builds the host clients with the configuration returned by load.
func OpenWithLoader(ctx context.Context, load ConfigLoader) (cloud.Presigner, cloud.Metadata, error) {
	cfg, err := load(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Configuration already describes the injected transport. Avoid the
	// IMDS client's independent fallback to process environment settings.
	meta := imds.NewFromConfig(cfg, func(o *imds.Options) {
		if o.ClientEnableState == imds.ClientDefaultEnableState {
			o.ClientEnableState = imds.ClientEnabled
		}
		if o.Endpoint == "" {
			o.Endpoint = "http://169.254.169.254"
			if o.EndpointMode == imds.EndpointModeStateIPv6 {
				o.Endpoint = "http://[fd00:ec2::254]"
			}
		}
	})
	region := cfg.Region
	if region == "" {
		out, err := meta.GetRegion(ctx, &imds.GetRegionInput{})
		if err != nil {
			return nil, nil, sdkError("imds", "GetRegion", err)
		}
		region = out.Region
	}
	return &presigner{region: region, credentials: cfg.Credentials, signer: v4.NewSigner()}, &metadata{imds: meta}, nil
}

func (p *presigner) PresignCallerIdentity(ctx context.Context, binding string, expires time.Duration) (string, error) {
	const op = "GetCallerIdentity"
	if expires < time.Second {
		return "", &cloud.Error{Service: "sts", Operation: op, Code: cloud.CodeBadResponse, Class: cloud.ClassRefused}
	}
	if p.credentials == nil {
		return "", &cloud.Error{Service: "sts", Operation: op, Code: cloud.CodeNoResponse, Class: cloud.ClassTransient}
	}
	credentials, err := p.credentials.Retrieve(ctx)
	if err != nil {
		return "", sdkError("sts", op, err)
	}
	target := url.URL{
		Scheme: "https", Host: "sts." + p.region + ".amazonaws.com", Path: "/",
		RawQuery: url.Values{
			"Action":        {"GetCallerIdentity"},
			"Version":       {"2011-06-15"},
			"X-Amz-Expires": {strconv.FormatInt(int64(expires/time.Second), 10)},
		}.Encode(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", sdkError("sts", op, err)
	}
	req.Header.Set(cloud.BindingHeader, binding)
	const emptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	signed, _, err := p.signer.PresignHTTP(ctx, credentials, req, emptyBodySHA256, "sts", p.region, time.Now())
	if err != nil {
		return "", sdkError("sts", op, err)
	}
	return signed, nil
}

func (m *metadata) Instance(ctx context.Context) (cloud.InstanceFacts, error) {
	out, err := m.imds.GetInstanceIdentityDocument(ctx, &imds.GetInstanceIdentityDocumentInput{})
	if err != nil {
		return cloud.InstanceFacts{}, sdkError("imds", "GetInstanceIdentityDocument", err)
	}
	return cloud.InstanceFacts{InstanceID: out.InstanceID, Type: out.InstanceType, AMI: out.ImageID, Region: out.Region}, nil
}

func sdkError(service, operation string, err error) *cloud.Error {
	code, class := cloud.CodeNoResponse, cloud.ClassTransient
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) {
		status := response.HTTPStatusCode()
		code = "HTTP " + strconv.Itoa(status)
		if status < 500 {
			class = cloud.ClassRefused
		}
	}
	return &cloud.Error{Service: service, Operation: operation, Code: code, Class: class, Err: err}
}
