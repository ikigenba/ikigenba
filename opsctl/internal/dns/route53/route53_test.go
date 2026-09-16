package route53_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// R-7JWE-YKBM R-ERWS-0X5E R-EUCK-SGMS
func TestExportsAndNew(t *testing.T) {
	newSignature := reflect.TypeFor[func(context.Context, string) (dns.Provider, error)]()
	if got := reflect.TypeOf(route53.New); got != newSignature {
		t.Fatalf("New signature = %s, want %s", got, newSignature)
	}
	openSignature := reflect.TypeFor[func(context.Context, string) (dns.Provider, error)]()
	if got := reflect.TypeOf(route53.Open); got != openSignature {
		t.Fatalf("Open signature = %s, want %s", got, openSignature)
	}
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
	requests := 0
	recordExists := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		authorization := request.Header.Get("Authorization")
		if !strings.Contains(authorization, "Credential=test-access-key/") {
			t.Errorf("Authorization does not use default-chain environment credentials: %q", authorization)
		}
		if !strings.Contains(authorization, "/"+route53.Region+"/route53/aws4_request") {
			t.Errorf("Authorization does not use %s: %q", route53.Region, authorization)
		}

		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/2013-04-01/hostedzone/Z1/rrset":
			records := ""
			if recordExists {
				records = recordSet("new.example.test.", "TXT", "60", resourceRecord(`&quot;token&quot;`))
			}
			writeResponse(t, response, listResponse(records, `<IsTruncated>false</IsTruncated>`))
		case request.Method == http.MethodPost && request.URL.Path == "/2013-04-01/hostedzone/Z1/rrset":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read change request: %v", err)
			}
			recordExists = !strings.Contains(string(body), "<Action>DELETE</Action>")
			writeResponse(t, response, changeResponse("PENDING"))
		case request.Method == http.MethodGet && request.URL.Path == "/2013-04-01/change/C1":
			writeResponse(t, response, getChangeResponse("INSYNC"))
		default:
			t.Errorf("unexpected request %d: %s %s", requests, request.Method, request.URL.Path)
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider, err := route53.New(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := provider.Records(context.Background(), "Z1"); err != nil {
		t.Fatalf("Records: %v", err)
	}
	if err := provider.Add(context.Background(), "Z1", "new.example.test", "TXT", 60, "token"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := provider.Remove(context.Background(), "Z1", "new.example.test", "TXT", "token"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if requests != 7 {
		t.Fatalf("request count = %d", requests)
	}
}

// R-EWSD-K046 R-XUL6-VKTK
func TestRecordsNormalizesValuesAndPaginates(t *testing.T) {
	setAWSEnvironment(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodGet {
			t.Errorf("Records request method = %s, want GET", request.Method)
		}
		if request.URL.Query().Get("name") == "" {
			records := recordSet(`\052.Example.TEST.`, "TXT", "60", resourceRecord(`&quot;one \&quot;quoted\&quot; &quot; &quot;\\tail&quot;`))
			pagination := `<IsTruncated>true</IsTruncated><NextRecordName>example.test.</NextRecordName>` +
				`<NextRecordType>NS</NextRecordType><NextRecordIdentifier>weighted-a</NextRecordIdentifier>`
			writeResponse(t, response, listResponse(records, pagination))
			return
		}
		if request.URL.Query().Get("name") != "example.test." || request.URL.Query().Get("type") != "NS" ||
			request.URL.Query().Get("identifier") != "weighted-a" {
			t.Errorf("pagination query = %q", request.URL.RawQuery)
		}
		records := recordSet("Example.TEST.", "NS", "172800", resourceRecord("NS-2.Example.")) +
			recordSet("Example.TEST.", "SOA", "900", resourceRecord("ns.example. hostmaster.example. 1 2 3 4 5"))
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
	if len(records) != 3 {
		t.Fatalf("records = %#v", records)
	}
	if got := records[0]; got.Name != "*.example.test" || got.Type != "TXT" || got.TTL != 60 || len(got.Values) != 1 || got.Values[0] != `one "quoted" \tail` {
		t.Errorf("first record = %#v", got)
	}
	if got := records[1]; got.Name != "example.test" || got.Type != "NS" || got.TTL != 172800 || got.Values[0] != "NS-2.Example" {
		t.Errorf("second record = %#v", got)
	}
	if got := records[2]; got.Name != "example.test" || got.Type != "SOA" || got.Values[0] != "ns.example. hostmaster.example. 1 2 3 4 5" {
		t.Errorf("third record = %#v", got)
	}
}

type changeServer struct {
	t            *testing.T
	listing      string
	mu           sync.Mutex
	changeBodies []string
	getStatuses  []string
	getCalls     int
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
		fake.getCalls++
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
	if strings.Index(body, "&#34;old&#34;") > strings.Index(body, "&#34;new&#34;") {
		t.Errorf("new value was not appended after existing value: %s", body)
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
	for _, want := range []string{
		"<Action>UPSERT</Action>",
		"<Name>new.example.test</Name>",
		"<Type>TXT</Type>",
		"<TTL>60</TTL>",
		"&#34;token&#34;",
	} {
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

// R-FIQK-FVGO R-XUL6-VKTK
func TestProviderBoundaryPreservesUnrequestedData(t *testing.T) {
	values := resourceRecord("NS1.Example.") + resourceRecord("NS2.Example.")
	target := recordSet("Delegated.Example.TEST.", "NS", "600", values)
	unrelated := recordSet("other.example.test.", "A", "30", resourceRecord("192.0.2.9"))
	fake := &changeServer{t: t, listing: target + unrelated}
	provider, closeServer := newChangeProvider(t, fake)
	defer closeServer()

	records, err := provider.Records(t.Context(), "Z1")
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(fake.changeBodies) != 0 || fake.getCalls != 0 {
		t.Fatal("Records performed a write or change-status poll")
	}
	if len(records) != 2 || records[0].Name != "delegated.example.test" ||
		records[0].Type != "NS" || !reflect.DeepEqual(records[0].Values, []string{"NS1.Example", "NS2.Example"}) {
		t.Fatalf("Records = %#v", records)
	}

	if err := provider.Add(t.Context(), "Z1", records[0].Name, records[0].Type, 99, "NS3.Example"); err != nil {
		t.Fatalf("Add listed representation: %v", err)
	}
	if len(fake.changeBodies) != 1 {
		t.Fatalf("Add changes = %d", len(fake.changeBodies))
	}
	body := fake.changeBodies[0]
	for _, want := range []string{
		"<Name>Delegated.Example.TEST.</Name>", "<Type>NS</Type>", "<TTL>600</TTL>",
		"<Value>NS1.Example.</Value>", "<Value>NS2.Example.</Value>", "<Value>NS3.Example.</Value>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Add body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "other.example.test") || strings.Contains(body, "192.0.2.9") {
		t.Errorf("Add changed an unrelated record: %s", body)
	}

	fake.listing = recordSet("Delegated.Example.TEST.", "NS", "600", values+resourceRecord("NS3.Example.")) + unrelated
	if err := provider.Remove(t.Context(), "Z1", records[0].Name, records[0].Type, records[0].Values[0]); err != nil {
		t.Fatalf("Remove listed representation: %v", err)
	}
	if len(fake.changeBodies) != 2 {
		t.Fatalf("Remove changes = %d", len(fake.changeBodies))
	}
	body = fake.changeBodies[1]
	if strings.Contains(body, "NS1.Example.") || !strings.Contains(body, "NS2.Example.") ||
		!strings.Contains(body, "NS3.Example.") || !strings.Contains(body, "<TTL>600</TTL>") {
		t.Errorf("Remove did not preserve the rest of the set: %s", body)
	}
	if strings.Contains(body, "other.example.test") || strings.Contains(body, "192.0.2.9") {
		t.Errorf("Remove changed an unrelated record: %s", body)
	}
}

// R-EWSD-K046 R-XUL6-VKTK
func TestTXTLogicalValuesRoundTrip(t *testing.T) {
	providerValue := `&quot;part \&quot;quoted\&quot; &quot; &quot;\\tail&quot;`
	logicalValue := `part "quoted" \tail`
	fake := &changeServer{
		t:       t,
		listing: recordSet("txt.example.test.", "TXT", "120", resourceRecord(providerValue)),
	}
	provider, closeServer := newChangeProvider(t, fake)
	defer closeServer()

	records, err := provider.Records(t.Context(), "Z1")
	if err != nil || len(records) != 1 || !reflect.DeepEqual(records[0].Values, []string{logicalValue}) {
		t.Fatalf("Records = %#v, %v", records, err)
	}
	if err := provider.Add(t.Context(), "Z1", records[0].Name, records[0].Type, 99, records[0].Values[0]); err != nil {
		t.Fatalf("idempotent Add of listed TXT: %v", err)
	}
	if len(fake.changeBodies) != 0 {
		t.Fatal("Add did not recognize the listed logical TXT value")
	}

	newValue := `next "value" \path`
	if err := provider.Add(t.Context(), "Z1", records[0].Name, records[0].Type, 99, newValue); err != nil {
		t.Fatalf("Add escaped TXT: %v", err)
	}
	if len(fake.changeBodies) != 1 {
		t.Fatalf("Add changes = %d", len(fake.changeBodies))
	}
	body := fake.changeBodies[0]
	if !strings.Contains(body, `&#34;next \&#34;value\&#34; \\path&#34;`) {
		t.Errorf("Add did not encode TXT quotes and backslashes: %s", body)
	}

	newProviderValue := `&quot;next \&quot;value\&quot; \\path&quot;`
	fake.listing = recordSet("txt.example.test.", "TXT", "120", resourceRecord(providerValue)+resourceRecord(newProviderValue))
	if err := provider.Remove(t.Context(), "Z1", records[0].Name, records[0].Type, records[0].Values[0]); err != nil {
		t.Fatalf("Remove listed TXT: %v", err)
	}
	if len(fake.changeBodies) != 2 {
		t.Fatalf("Remove changes = %d", len(fake.changeBodies))
	}
	body = fake.changeBodies[1]
	if strings.Contains(body, "part") || !strings.Contains(body, `next \&#34;value\&#34; \\path`) ||
		!strings.Contains(body, "<TTL>120</TTL>") {
		t.Errorf("Remove did not preserve only the other logical TXT value: %s", body)
	}
}

// R-EY09-XRUV
func TestChangesWaitForINSYNCAndWrapContextError(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
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
		if len(fake.changeBodies) != 2 {
			t.Fatalf("Add cancellation submitted rollback: %d change requests", len(fake.changeBodies))
		}

		fake.listing = recordSet("b.example.test.", "A", "30", resourceRecord("192.0.2.2"))
		getCalls := fake.getCalls
		if err := provider.Add(context.Background(), "Z1", "b.example.test", "A", 99, "192.0.2.2"); err != nil {
			t.Fatalf("idempotent Add: %v", err)
		}
		if fake.getCalls != getCalls || len(fake.changeBodies) != 2 {
			t.Fatal("idempotent Add submitted or waited for a change")
		}
	})

	t.Run("Remove", func(t *testing.T) {
		record := recordSet("a.example.test.", "A", "30", resourceRecord("192.0.2.1"))
		fake := &changeServer{t: t, listing: record, getStatuses: []string{"PENDING", "INSYNC"}}
		provider, closeServer := newChangeProvider(t, fake)
		defer closeServer()
		if err := provider.Remove(context.Background(), "Z1", "a.example.test", "A", "192.0.2.1"); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if len(fake.getStatuses) != 0 {
			t.Fatal("Remove returned before INSYNC")
		}

		fake.getStatuses = make([]string, 100)
		for index := range fake.getStatuses {
			fake.getStatuses[index] = "PENDING"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		err := provider.Remove(ctx, "Z1", "a.example.test", "A", "192.0.2.1")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Remove cancellation error = %v", err)
		}
		if len(fake.changeBodies) != 2 {
			t.Fatalf("Remove cancellation submitted rollback: %d change requests", len(fake.changeBodies))
		}

		fake.listing = ""
		getCalls := fake.getCalls
		if err := provider.Remove(context.Background(), "Z1", "a.example.test", "A", "192.0.2.1"); err != nil {
			t.Fatalf("idempotent Remove: %v", err)
		}
		if fake.getCalls != getCalls || len(fake.changeBodies) != 2 {
			t.Fatal("idempotent Remove submitted or waited for a change")
		}
	})
}
