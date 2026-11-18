package awssdk

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const getCallerIdentity = "GetCallerIdentity"

func (a *adapter) Account(ctx context.Context) (string, error) {
	out, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", failed("sts", getCallerIdentity, "", err)
	}
	return aws.ToString(out.Account), nil
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
	if response.ContentLength > cloud.MaxCallerBytes {
		return cloud.Caller{}, callerError(cloud.CodeBadResponse, cloud.ClassRefused, nil)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cloud.MaxCallerBytes))
	if err != nil {
		return cloud.Caller{}, callerError(cloud.CodeNoResponse, cloud.ClassTransient, err)
	}
	// If the limit was reached and framing did not establish the complete
	// length, a prefix of an oversized response could look like valid XML.
	if len(body) == cloud.MaxCallerBytes && response.ContentLength != cloud.MaxCallerBytes {
		return cloud.Caller{}, callerError(cloud.CodeBadResponse, cloud.ClassRefused, nil)
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
	if err := decoder.Decode(&response); err != nil {
		return cloud.Caller{}, err
	}
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
			if len(bytes.TrimSpace(token)) == 0 {
				continue
			}
		}
		return cloud.Caller{}, errors.New("invalid content after STS XML document")
	}
	r := response.Result
	if r.Account == "" || r.Arn == "" {
		return cloud.Caller{}, errors.New("missing identity fields in STS response")
	}
	return cloud.Caller{Account: r.Account, Arn: r.Arn, UserID: r.UserID}, nil
}
