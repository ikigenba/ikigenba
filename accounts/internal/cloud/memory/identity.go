package memory

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const amzDate = "20060102T150405Z"

type presigner struct {
	cloud *Cloud
	id    string
}

type metadata struct {
	cloud *Cloud
	id    string
}

// Presigner returns a deterministic STS presigner for the instance.
func (c *Cloud) Presigner(id string) cloud.Presigner { return presigner{c, id} }

// Metadata returns the current instance facts for the given id.
func (c *Cloud) Metadata(id string) cloud.Metadata { return metadata{c, id} }

// Account returns the sandbox account id.
func (*Cloud) Account(context.Context) (string, error) { return Account, nil }

func (m metadata) Instance(_ context.Context) (cloud.InstanceFacts, error) {
	m.cloud.mu.Lock()
	defer m.cloud.mu.Unlock()
	i, exists := m.cloud.state.Instances[m.id]
	if !exists {
		return cloud.InstanceFacts{}, ec2("DescribeInstances", m.id).reject("InvalidInstanceID.NotFound")
	}
	return cloud.InstanceFacts{InstanceID: m.id, Type: i.Value.Type, AMI: AMI, Region: m.cloud.cfg.Hosts.Region}, nil
}

func (p presigner) PresignCallerIdentity(_ context.Context, binding string, expires time.Duration) (string, error) {
	p.cloud.mu.Lock()
	defer p.cloud.mu.Unlock()
	stamp := p.cloud.cfg.Now().UTC().Format(amzDate)
	seconds := strconv.FormatInt(int64(expires/time.Second), 10)
	credential := p.id
	if subsecond := expires % time.Second; subsecond != 0 {
		credential += "/" + strconv.FormatInt(int64(subsecond), 10)
	}
	return p.cloud.identityURL(credential, stamp, seconds, binding), nil
}

func (c *Cloud) identityURL(credential, stamp, seconds, binding string) string {
	mac := hmac.New(sha256.New, c.state.Key)
	message := url.Values{
		"credential": {credential},
		"date":       {stamp},
		"expires":    {seconds},
		"binding":    {binding},
	}
	_, _ = mac.Write([]byte(message.Encode()))
	query := url.Values{
		"Action":              {"GetCallerIdentity"},
		"Version":             {"2011-06-15"},
		"X-Amz-Algorithm":     {"AWS4-HMAC-SHA256"},
		"X-Amz-SignedHeaders": {"host;" + strings.ToLower(cloud.BindingHeader)},
		"X-Amz-Credential":    {credential},
		"X-Amz-Date":          {stamp},
		"X-Amz-Expires":       {seconds},
		"X-Amz-Signature":     {hex.EncodeToString(mac.Sum(nil))},
	}
	u := url.URL{
		Scheme: "https", Host: "sts." + c.cfg.Hosts.Region + ".amazonaws.com",
		Path: "/", RawQuery: query.Encode(),
	}
	return u.String()
}

// CallerOf verifies the presigned request, then returns its sandbox role.
func (c *Cloud) CallerOf(_ context.Context, raw, binding string) (cloud.Caller, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return cloud.Caller{}, stsIdentity.reject("HTTP 403")
	}
	q := u.Query()
	credential, stamp, seconds := q.Get("X-Amz-Credential"), q.Get("X-Amz-Date"), q.Get("X-Amz-Expires")
	id, _, _ := strings.Cut(credential, "/")
	date, dateErr := time.Parse(amzDate, stamp)
	expiry, expiryErr := strconv.ParseInt(seconds, 10, 64)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	if dateErr != nil || expiryErr != nil || expiry < 0 || now.Before(date) ||
		now.After(date.Add(time.Duration(expiry)*time.Second)) {
		return cloud.Caller{}, stsIdentity.reject("HTTP 403")
	}
	i, exists := c.state.Instances[id]
	if !exists || i.at(now).State == cloud.InstanceTerminated {
		return cloud.Caller{}, stsIdentity.reject("HTTP 403")
	}
	expected := c.identityURL(credential, stamp, seconds, binding)
	if !hmac.Equal([]byte(expected), []byte(raw)) {
		return cloud.Caller{}, stsIdentity.reject("HTTP 403")
	}
	return cloud.Caller{
		Account: Account,
		Arn:     fmt.Sprintf("arn:aws:sts::%s:assumed-role/%s/%s", Account, c.cfg.Hosts.InstanceProfile, id),
		UserID:  "AROASANDBOX:" + id,
	}, nil
}
