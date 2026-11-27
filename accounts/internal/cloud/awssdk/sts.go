package awssdk

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const (
	getCallerIdentity = "GetCallerIdentity"
	assumeRole        = "AssumeRole"
)

func (a *adapter) Account(ctx context.Context) (string, error) {
	out, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", failed("sts", getCallerIdentity, "", err)
	}
	return aws.ToString(out.Account), nil
}

func (a *adapter) Identity(ctx context.Context) (cloud.Caller, error) {
	out, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return cloud.Caller{}, failed("sts", getCallerIdentity, "", err)
	}
	caller := cloud.Caller{Account: aws.ToString(out.Account), Arn: aws.ToString(out.Arn), UserID: aws.ToString(out.UserId)}
	if caller.Account == "" || caller.Arn == "" {
		return cloud.Caller{}, callerError(cloud.CodeBadResponse, cloud.ClassRefused, nil)
	}
	return caller, nil
}

func (a *adapter) AssumeRole(ctx context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
	in := &sts.AssumeRoleInput{
		RoleArn:         aws.String(r.RoleARN),
		RoleSessionName: aws.String(r.SessionName),
	}
	if r.Policy != "" {
		in.Policy = aws.String(r.Policy)
	}
	seconds := r.Duration / time.Second
	var options []func(*sts.Options)
	if seconds >= math.MinInt32 && seconds <= math.MaxInt32 {
		in.DurationSeconds = aws.Int32(int32(seconds))
	} else {
		options = append(options, sts.WithAPIOptions(encodeDurationSeconds(int64(seconds))))
	}
	out, err := a.sts.AssumeRole(ctx, in, options...)
	if err != nil {
		return cloud.Credentials{}, failed("sts", assumeRole, "", err)
	}
	if c := out.Credentials; c != nil {
		creds := cloud.Credentials{
			AccessKeyID:     aws.ToString(c.AccessKeyId),
			SecretAccessKey: aws.ToString(c.SecretAccessKey),
			SessionToken:    aws.ToString(c.SessionToken),
			Expires:         aws.ToTime(c.Expiration),
		}
		if creds.AccessKeyID != "" && creds.SecretAccessKey != "" && creds.SessionToken != "" && !creds.Expires.IsZero() {
			return creds, nil
		}
	}
	return cloud.Credentials{}, &cloud.Error{Service: "sts", Operation: assumeRole, Code: cloud.CodeBadResponse, Class: cloud.ClassRefused}
}

// encodeDurationSeconds adds the full value before the SDK hashes and signs
// the request when the SDK's typed field cannot represent it.
func encodeDurationSeconds(seconds int64) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Build.Add(middleware.BuildMiddlewareFunc("AccountsRoleDuration",
			func(ctx context.Context, in middleware.BuildInput, next middleware.BuildHandler) (middleware.BuildOutput, middleware.Metadata, error) {
				req, ok := in.Request.(*smithyhttp.Request)
				if !ok {
					return middleware.BuildOutput{}, middleware.Metadata{}, errors.New("invalid STS request transport")
				}
				var body bytes.Buffer
				if _, err := body.ReadFrom(req.GetStream()); err != nil {
					return middleware.BuildOutput{}, middleware.Metadata{}, err
				}
				body.WriteString("&DurationSeconds=")
				body.WriteString(strconv.FormatInt(seconds, 10))
				var err error
				in.Request, err = req.SetStream(bytes.NewReader(body.Bytes()))
				if err != nil {
					return middleware.BuildOutput{}, middleware.Metadata{}, err
				}
				return next.HandleBuild(ctx, in)
			}), middleware.After)
	}
}

func (a *adapter) CallerOf(ctx context.Context, presignedURL, binding string) (cloud.Caller, error) {
	ctx, cancel := context.WithTimeout(ctx, cloud.CallerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, presignedURL, nil)
	if err != nil {
		return cloud.Caller{}, callerError(cloud.CodeNoResponse, cloud.ClassTransient, err)
	}
	req.Header.Set(cloud.BindingHeader, binding)
	response, err := a.caller.Do(req)
	if err != nil {
		return cloud.Caller{}, callerError(cloud.CodeNoResponse, cloud.ClassTransient, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		code := httpCode(response.StatusCode)
		return cloud.Caller{}, callerError(code, classify(code, response.StatusCode), nil)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cloud.MaxCallerBytes+1))
	if len(body) > cloud.MaxCallerBytes {
		return cloud.Caller{}, callerError(cloud.CodeBadResponse, cloud.ClassRefused, nil)
	}
	if err != nil {
		return cloud.Caller{}, callerError(cloud.CodeNoResponse, cloud.ClassTransient, err)
	}
	result, err := readCaller(body)
	if err != nil {
		return cloud.Caller{}, callerError(cloud.CodeBadResponse, cloud.ClassRefused, err)
	}
	return result, nil
}

func callerError(code string, class cloud.Class, cause error) error {
	return &cloud.Error{Service: "sts", Operation: getCallerIdentity, Code: code, Class: class, Err: cause}
}

func readCaller(body []byte) (cloud.Caller, error) {
	var response struct {
		XMLName xml.Name `xml:"GetCallerIdentityResponse"`
		Result  struct {
			Account string
			Arn     string
			UserID  string `xml:"UserId"`
		} `xml:"GetCallerIdentityResult"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	hasRoot := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return cloud.Caller{}, err
		}
		switch token := token.(type) {
		case xml.Comment, xml.ProcInst:
			continue
		case xml.CharData:
			if len(bytes.Trim(token, " \t\r\n")) == 0 {
				continue
			}
		case xml.StartElement:
			if !hasRoot {
				if err := decoder.DecodeElement(&response, &token); err != nil {
					return cloud.Caller{}, err
				}
				hasRoot = true
				continue
			}
		}
		return cloud.Caller{}, errors.New("invalid content outside the STS XML root element")
	}
	r := response.Result
	if r.Account == "" || r.Arn == "" {
		return cloud.Caller{}, errors.New("missing identity fields in STS response")
	}
	return cloud.Caller{Account: r.Account, Arn: r.Arn, UserID: r.UserID}, nil
}
