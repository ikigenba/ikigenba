package awssdk_test

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func TestFindRecords(t *testing.T) {
	// R-XMKM-Y5L0
	const name = "Acme.Hosts.TeSt"
	for _, mode := range []string{"different record on same page", "different continuation name", "absent"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/2013-04-01/hostedzone/zone-test/rrset" {
					t.Errorf("list request = %s %s", r.Method, r.URL)
				}
				if calls == 1 {
					checkForm(t, r.URL.Query(), map[string]string{"name": name})
					if mode == "absent" {
						return xmlResponse(200, `<ListResourceRecordSetsResponse><ResourceRecordSets>
							<ResourceRecordSet><Name>next.hosts.test.</Name><Type>A</Type></ResourceRecordSet>
							</ResourceRecordSets><IsTruncated>true</IsTruncated><NextRecordName>next.hosts.test.</NextRecordName></ListResourceRecordSetsResponse>`), nil
					}
					return xmlResponse(200, `<ListResourceRecordSetsResponse><ResourceRecordSets>
						<ResourceRecordSet><Name>acme.hosts.test.</Name><Type>TXT</Type><TTL>90</TTL><ResourceRecords>
						<ResourceRecord><Value>z</Value></ResourceRecord><ResourceRecord><Value>a</Value></ResourceRecord>
						</ResourceRecords></ResourceRecordSet>
						</ResourceRecordSets><IsTruncated>true</IsTruncated><NextRecordName>acme.hosts.test.</NextRecordName>
						<NextRecordType>A</NextRecordType><NextRecordIdentifier>policy-key</NextRecordIdentifier></ListResourceRecordSetsResponse>`), nil
				}
				if calls != 2 {
					t.Fatal("requested a page beyond the record name")
				}
				checkForm(t, r.URL.Query(), map[string]string{"name": "acme.hosts.test.", "type": "A", "identifier": "policy-key"})
				tail := ""
				if mode == "different record on same page" {
					tail = `<ResourceRecordSet><Name>next.hosts.test.</Name><Type>A</Type><TTL>90</TTL></ResourceRecordSet>`
				}
				return xmlResponse(200, `<ListResourceRecordSetsResponse><ResourceRecordSets>
					<ResourceRecordSet><Name>ACME.hosts.test</Name><Type>A</Type><AliasTarget><HostedZoneId>another-zone</HostedZoneId>
					<DNSName>target.test.</DNSName><EvaluateTargetHealth>false</EvaluateTargetHealth></AliasTarget></ResourceRecordSet>`+
					tail+`</ResourceRecordSets><IsTruncated>true</IsTruncated><NextRecordName>next.hosts.test.</NextRecordName></ListResourceRecordSetsResponse>`), nil
			})
			got, err := c.FindRecords(t.Context(), "zone-test", name)
			want := []cloud.Record{
				{Name: name, Type: "TXT", Values: []string{"z", "a"}, TTL: 90},
				{Name: name, Type: "A"},
			}
			wantCalls := 2
			if mode == "absent" {
				want = []cloud.Record{}
				wantCalls = 1
			}
			if err != nil || !reflect.DeepEqual(got, want) || calls != wantCalls {
				t.Errorf("FindRecords = %+v, %v (%d calls), want %+v (%d calls)", got, err, calls, want, wantCalls)
			}
		})
	}
}

func TestChangeRecords(t *testing.T) {
	// R-XNSJ-BXBP
	type record struct {
		Name   string
		Type   string
		TTL    int64
		Values []string `xml:"ResourceRecords>ResourceRecord>Value"`
	}
	type change struct {
		Action string
		Record record `xml:"ResourceRecordSet"`
	}
	const name, ip, zone, changeID = "acme.hosts.test", "192.0.2.22", "zone-test", "/change/change-1"
	for _, action := range []string{cloud.RecordCreate, cloud.RecordDelete} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/2013-04-01/hostedzone/"+zone+"/rrset" {
					t.Errorf("change request = %s %s", r.Method, r.URL)
				}
				var in struct {
					XMLName xml.Name `xml:"ChangeResourceRecordSetsRequest"`
					Changes []change `xml:"ChangeBatch>Changes>Change"`
				}
				if err := xml.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Fatal(err)
				}
				want := []change{{Action: action, Record: record{Name: name, Type: "A", TTL: 25, Values: []string{ip}}}}
				if !reflect.DeepEqual(in.Changes, want) {
					t.Errorf("changes = %+v, want %+v", in.Changes, want)
				}
				return xmlResponse(200, `<ChangeResourceRecordSetsResponse><ChangeInfo><Id>`+changeID+`</Id></ChangeInfo></ChangeResourceRecordSetsResponse>`), nil
			})
			got, err := c.ChangeRecord(t.Context(), zone, cloud.RecordChange{Action: action, Name: name, IP: ip, TTL: 25})
			if got != changeID || err != nil || calls != 1 {
				t.Fatalf("ChangeRecord = %q, %v (%d calls)", got, err, calls)
			}
			_, err = c.ChangeRecord(t.Context(), zone, cloud.RecordChange{Action: "UPSERT", Name: name, IP: ip, TTL: 25})
			var invalid *cloud.Error
			if !errors.As(err, &invalid) || invalid.Class != cloud.ClassRefused || calls != 1 {
				t.Fatalf("invalid record change = %v (%d calls)", err, calls)
			}
		})
	}
	for _, status := range []string{"PENDING", "INSYNC"} {
		t.Run(status, func(t *testing.T) {
			c := open(t, func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/2013-04-01/change/change-1" {
					t.Errorf("change status request = %s %s", r.Method, r.URL)
				}
				return xmlResponse(200, `<GetChangeResponse><ChangeInfo><Status>`+status+`</Status></ChangeInfo></GetChangeResponse>`), nil
			})
			got, err := c.ChangeInsync(t.Context(), changeID)
			if err != nil || got != (status == "INSYNC") {
				t.Fatalf("ChangeInsync %s = %v, %v", status, got, err)
			}
		})
	}
}

func TestCallerOf(t *testing.T) {
	// R-KLRD-EYLQ
	const (
		target  = "https://sts.us-east-2.amazonaws.com/?X-Amz-Signature=private%2Fsignature&Action=GetCallerIdentity"
		binding = "accounts.space.test"
		success = `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
			<GetCallerIdentityResult><Account>123456789012</Account><Arn>test-arn</Arn><UserId>test-user</UserId></GetCallerIdentityResult>
			<ResponseMetadata><RequestId>private</RequestId></ResponseMetadata></GetCallerIdentityResponse>`
	)
	for _, test := range []struct {
		name      string
		status    int
		body      string
		code      string
		class     cloud.Class
		transport error
	}{
		{name: "success", status: 200, body: `<?xml version="1.0"?>` + success},
		{name: "unknown response", status: 200, body: "<different/>", code: cloud.CodeBadResponse, class: cloud.ClassRefused},
		{name: "leading text", status: 200, body: "text" + success, code: cloud.CodeBadResponse, class: cloud.ClassRefused},
		{name: "malformed preamble", status: 200, body: "<!garbage>" + success, code: cloud.CodeBadResponse, class: cloud.ClassRefused},
		{name: "trailing XML", status: 200, body: success + "<different/>", code: cloud.CodeBadResponse, class: cloud.ClassRefused},
		{name: "missing fields", status: 200, body: `<GetCallerIdentityResponse><GetCallerIdentityResult><Account>123</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`, code: cloud.CodeBadResponse, class: cloud.ClassRefused},
		{name: "known excess length", status: 200, body: success + strings.Repeat(" ", cloud.MaxCallerBytes), code: cloud.CodeBadResponse, class: cloud.ClassRefused},
		{name: "redirect", status: 307, body: "private URL", code: "HTTP 307", class: cloud.ClassRefused},
		{name: "no content", status: 204, code: "HTTP 204", class: cloud.ClassRefused},
		{name: "denied", status: 403, body: "private credentials", code: "HTTP 403", class: cloud.ClassRefused},
		{name: "server error", status: 503, body: "private binding", code: "HTTP 503", class: cloud.ClassTransient},
		{name: "transport error", transport: errors.New(target), code: cloud.CodeNoResponse, class: cloud.ClassTransient},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != target || r.Method != http.MethodGet || r.Header.Get(cloud.BindingHeader) != binding {
					t.Fatalf("unmatched signed request: %s %s %v", r.Method, r.URL, r.Header)
				}
				if test.transport != nil {
					return nil, test.transport
				}
				out := xmlResponse(test.status, test.body)
				out.Header.Set("Location", "/redirect-target")
				return out, nil
			})
			got, err := c.CallerOf(t.Context(), target, binding)
			if calls != 1 {
				t.Errorf("transport calls = %d, want 1", calls)
			}
			if test.code != "" {
				// R-XRG8-H8JS
				checkError(t, err, "sts", "GetCallerIdentity", "", test.code, test.class)
				if err.Error() != "sts GetCallerIdentity: "+test.code {
					t.Errorf("unsafe caller error = %v", err)
				}
			} else if err != nil || got != (cloud.Caller{Account: "123456789012", Arn: "test-arn", UserID: "test-user"}) {
				t.Errorf("CallerOf = %+v, %v", got, err)
			}
		})
	}
}

func TestCallerTimeout(t *testing.T) {
	// R-KLRD-EYLQ
	synctest.Test(t, func(t *testing.T) {
		c := open(t, func(r *http.Request) (*http.Response, error) {
			if _, ok := r.Context().Deadline(); !ok {
				t.Fatal("request has no deadline")
			}
			<-r.Context().Done()
			err := r.Context().Err()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("request context ended with %v, want DeadlineExceeded", err)
			}
			return nil, err
		})
		start := time.Now()
		_, err := c.CallerOf(t.Context(), "https://sts.us-east-2.amazonaws.com/", "test-binding")
		if elapsed := time.Since(start); elapsed != cloud.CallerTimeout {
			t.Errorf("CallerOf gave up after %v, want %v", elapsed, cloud.CallerTimeout)
		}
		checkError(t, err, "sts", "GetCallerIdentity", "", cloud.CodeNoResponse, cloud.ClassTransient)
	})
}

type countedBody struct {
	reader io.Reader
	read   int
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (*countedBody) Close() error { return nil }

func TestCallerReadLimit(t *testing.T) {
	// R-KLRD-EYLQ
	const value = `<GetCallerIdentityResponse><GetCallerIdentityResult><Account>123</Account><Arn>test</Arn></GetCallerIdentityResult></GetCallerIdentityResponse>`
	for _, test := range []struct {
		name string
		size int
	}{
		{name: "exact limit", size: cloud.MaxCallerBytes},
		{name: "one byte over", size: cloud.MaxCallerBytes + 1},
		{name: "more than one byte over", size: cloud.MaxCallerBytes + 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &countedBody{reader: strings.NewReader(value + strings.Repeat(" ", test.size-len(value)))}
			c := open(t, func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: -1}, nil
			})
			got, err := c.CallerOf(t.Context(), "https://sts.us-east-2.amazonaws.com/", "test-binding")
			if test.size > cloud.MaxCallerBytes {
				checkError(t, err, "sts", "GetCallerIdentity", "", cloud.CodeBadResponse, cloud.ClassRefused)
			} else if err != nil || got != (cloud.Caller{Account: "123", Arn: "test"}) {
				t.Errorf("CallerOf = %+v, %v at the limit", got, err)
			}
			if want := min(test.size, cloud.MaxCallerBytes+1); body.read != want {
				t.Errorf("read %d bytes from STS, want %d", body.read, want)
			}
		})
	}
}
