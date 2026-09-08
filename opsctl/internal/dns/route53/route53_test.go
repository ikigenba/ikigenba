package route53_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns/route53"
)

const xmlNamespace = `https://route53.amazonaws.com/doc/2013-04-01/`

func setAWSEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
}

func writeResponse(t *testing.T, response http.ResponseWriter, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/xml")
	if _, err := io.WriteString(response, body); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func listResponse(records, pagination string) string {
	return `<ListResourceRecordSetsResponse xmlns="` + xmlNamespace + `">` +
		`<ResourceRecordSets>` + records + `</ResourceRecordSets>` + pagination +
		`<MaxItems>300</MaxItems></ListResourceRecordSetsResponse>`
}

func recordSet(name, typ, ttl, values string) string {
	return `<ResourceRecordSet><Name>` + name + `</Name><Type>` + typ + `</Type>` +
		`<TTL>` + ttl + `</TTL><ResourceRecords>` + values +
		`</ResourceRecords></ResourceRecordSet>`
}

func resourceRecord(value string) string {
	return `<ResourceRecord><Value>` + value + `</Value></ResourceRecord>`
}

func changeResponse(status string) string {
	return `<ChangeResourceRecordSetsResponse xmlns="` + xmlNamespace + `"><ChangeInfo>` +
		`<Id>/change/C1</Id><Status>` + status + `</Status>` +
		`<SubmittedAt>2026-01-01T00:00:00Z</SubmittedAt>` +
		`</ChangeInfo></ChangeResourceRecordSetsResponse>`
}

func getChangeResponse(status string) string {
	return `<GetChangeResponse xmlns="` + xmlNamespace + `"><ChangeInfo>` +
		`<Id>/change/C1</Id><Status>` + status + `</Status>` +
		`<SubmittedAt>2026-01-01T00:00:00Z</SubmittedAt>` +
		`</ChangeInfo></GetChangeResponse>`
}

// R-L7UV-RKHW
func TestExportsAndNew(t *testing.T) {
	setAWSEnvironment(t)
	if route53.Region != "us-east-1" {
		t.Fatalf("Region = %q", route53.Region)
	}
	provider, err := route53.New(context.Background(), "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if provider == nil {
		t.Fatal("New returned a nil provider")
	}
}

// R-L92S-5C8L
func TestOpenRegistry(t *testing.T) {
	setAWSEnvironment(t)
	provider, err := route53.Open(context.Background(), "route53")
	if err != nil || provider == nil {
		t.Fatalf("Open(route53) = %v, %v", provider, err)
	}
	_, err = route53.Open(context.Background(), "other")
	if !errors.Is(err, dns.ErrUnknownProvider) || !strings.Contains(err.Error(), "other") {
		t.Fatalf("Open(other) error = %v", err)
	}
}

// R-LAAO-J3ZA
func TestNewUsesEndpointDefaultCredentialsAndRegion(t *testing.T) {
	setAWSEnvironment(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/2013-04-01/hostedzone/Z1/rrset" {
			t.Errorf("path = %q", request.URL.Path)
		}
		authorization := request.Header.Get("Authorization")
		if !strings.Contains(authorization, "Credential=test-access-key/") {
			t.Errorf("Authorization does not use default-chain environment credentials: %q", authorization)
		}
		if !strings.Contains(authorization, "/"+route53.Region+"/route53/aws4_request") {
			t.Errorf("Authorization does not use %s: %q", route53.Region, authorization)
		}
		writeResponse(t, response, listResponse("", `<IsTruncated>false</IsTruncated>`))
	}))
	defer server.Close()

	provider, err := route53.New(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := provider.Records(context.Background(), "Z1"); err != nil {
		t.Fatalf("Records: %v", err)
	}
}

// R-LBIK-WVPZ
func TestRecordsNormalizesValuesAndPaginates(t *testing.T) {
	setAWSEnvironment(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			records := recordSet(`\052.example.test.`, "TXT", "60", resourceRecord(`&quot;one&quot;`))
			pagination := `<IsTruncated>true</IsTruncated><NextRecordName>example.test.</NextRecordName><NextRecordType>NS</NextRecordType>`
			writeResponse(t, response, listResponse(records, pagination))
			return
		}
		if request.URL.Query().Get("name") != "example.test." || request.URL.Query().Get("type") != "NS" {
			t.Errorf("pagination query = %q", request.URL.RawQuery)
		}
		records := recordSet("example.test.", "NS", "172800", resourceRecord("ns-2.example."))
		writeResponse(t, response, listResponse(records, `<IsTruncated>false</IsTruncated>`))
	}))
	defer server.Close()

	provider, err := route53.New(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	records, err := provider.Records(context.Background(), "Z1")
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if requests != 2 {
		t.Fatalf("request count = %d", requests)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v", records)
	}
	if got := records[0]; got.Name != "*.example.test" || got.Type != "TXT" || got.TTL != 60 || len(got.Values) != 1 || got.Values[0] != "one" {
		t.Errorf("first record = %#v", got)
	}
	if got := records[1]; got.Name != "example.test" || got.Values[0] != "ns-2.example." {
		t.Errorf("second record = %#v", got)
	}
}

type changeServer struct {
	t            *testing.T
	listing      string
	mu           sync.Mutex
	changeBodies []string
	getStatuses  []string
}

func (fake *changeServer) handler(response http.ResponseWriter, request *http.Request) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	switch {
	case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/rrset"):
		writeResponse(fake.t, response, listResponse(fake.listing, `<IsTruncated>false</IsTruncated>`))
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/rrset"):
		body, err := io.ReadAll(request.Body)
		if err != nil {
			fake.t.Errorf("read change request: %v", err)
		}
		fake.changeBodies = append(fake.changeBodies, string(body))
		writeResponse(fake.t, response, changeResponse("PENDING"))
	case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/change/"):
		status := "INSYNC"
		if len(fake.getStatuses) != 0 {
			status = fake.getStatuses[0]
			fake.getStatuses = fake.getStatuses[1:]
		}
		writeResponse(fake.t, response, getChangeResponse(status))
	default:
		fake.t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
		response.WriteHeader(http.StatusNotFound)
	}
}

func newChangeProvider(t *testing.T, fake *changeServer) (dns.Provider, func()) {
	t.Helper()
	setAWSEnvironment(t)
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	provider, err := route53.New(context.Background(), server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("New: %v", err)
	}
	return provider, server.Close
}

// R-LDYD-OF7D
func TestAddPreservesSetAndQuotesTXT(t *testing.T) {
	existing := recordSet("_acme.example.test.", "TXT", "120", resourceRecord(`&quot;old&quot;`))
	fake := &changeServer{t: t, listing: existing}
	provider, closeServer := newChangeProvider(t, fake)
	defer closeServer()

	if err := provider.Add(context.Background(), "Z1", "_acme.example.test", "TXT", 60, "new"); err != nil {
		t.Fatalf("Add existing: %v", err)
	}
	if len(fake.changeBodies) != 1 {
		t.Fatalf("changes = %d", len(fake.changeBodies))
	}
	body := fake.changeBodies[0]
	for _, want := range []string{"<Action>UPSERT</Action>", "<TTL>120</TTL>", "&#34;old&#34;", "&#34;new&#34;"} {
		if !strings.Contains(body, want) {
			t.Errorf("change body missing %q: %s", want, body)
		}
	}

	fake.listing = existing
	fake.changeBodies = nil
	if err := provider.Add(context.Background(), "Z1", "_acme.example.test", "TXT", 999, "old"); err != nil {
		t.Fatalf("Add duplicate: %v", err)
	}
	if len(fake.changeBodies) != 0 {
		t.Fatalf("duplicate submitted %d changes", len(fake.changeBodies))
	}

	fake.listing = ""
	if err := provider.Add(context.Background(), "Z1", "new.example.test", "TXT", 60, "token"); err != nil {
		t.Fatalf("Add absent: %v", err)
	}
	body = fake.changeBodies[0]
	for _, want := range []string{"<TTL>60</TTL>", "&#34;token&#34;"} {
		if !strings.Contains(body, want) {
			t.Errorf("create body missing %q: %s", want, body)
		}
	}
}

// R-LF6A-26Y2
func TestRemoveUpdatesDeletesAndSkipsAbsent(t *testing.T) {
	values := resourceRecord(`&quot;one&quot;`) + resourceRecord(`&quot;two&quot;`)
	fake := &changeServer{t: t, listing: recordSet("_acme.example.test.", "TXT", "60", values)}
	provider, closeServer := newChangeProvider(t, fake)
	defer closeServer()

	if err := provider.Remove(context.Background(), "Z1", "_acme.example.test", "TXT", "one"); err != nil {
		t.Fatalf("Remove from set: %v", err)
	}
	body := fake.changeBodies[0]
	if !strings.Contains(body, "<Action>UPSERT</Action>") || strings.Contains(body, "&#34;one&#34;") || !strings.Contains(body, "&#34;two&#34;") {
		t.Errorf("update body = %s", body)
	}

	fake.listing = recordSet("_acme.example.test.", "TXT", "60", resourceRecord(`&quot;two&quot;`))
	if err := provider.Remove(context.Background(), "Z1", "_acme.example.test", "TXT", "two"); err != nil {
		t.Fatalf("Remove last: %v", err)
	}
	if !strings.Contains(fake.changeBodies[1], "<Action>DELETE</Action>") {
		t.Errorf("delete body = %s", fake.changeBodies[1])
	}

	fake.listing = ""
	before := len(fake.changeBodies)
	if err := provider.Remove(context.Background(), "Z1", "missing.example.test", "TXT", "x"); err != nil {
		t.Fatalf("Remove absent set: %v", err)
	}
	fake.listing = recordSet("_acme.example.test.", "TXT", "60", resourceRecord(`&quot;two&quot;`))
	if err := provider.Remove(context.Background(), "Z1", "_acme.example.test", "TXT", "missing"); err != nil {
		t.Fatalf("Remove absent value: %v", err)
	}
	if len(fake.changeBodies) != before {
		t.Fatalf("absent removals submitted changes: before %d after %d", before, len(fake.changeBodies))
	}
}

// R-LGE6-FYOR
func TestChangesWaitForINSYNCAndWrapContextError(t *testing.T) {
	fake := &changeServer{t: t, getStatuses: []string{"PENDING", "INSYNC"}}
	provider, closeServer := newChangeProvider(t, fake)
	defer closeServer()
	if err := provider.Add(context.Background(), "Z1", "a.example.test", "A", 30, "192.0.2.1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(fake.getStatuses) != 0 {
		t.Fatal("Add returned before INSYNC")
	}

	fake.listing = ""
	fake.getStatuses = make([]string, 100)
	for index := range fake.getStatuses {
		fake.getStatuses[index] = "PENDING"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := provider.Add(ctx, "Z1", "b.example.test", "A", 30, "192.0.2.2")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Add cancellation error = %v", err)
	}
}
