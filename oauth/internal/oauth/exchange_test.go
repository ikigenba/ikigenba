package oauth_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

type capturedTokenRequest struct {
	method  string
	url     string
	body    string
	headers http.Header
}

func tokenEndpoint(t *testing.T) (*http.Client, *url.URL, <-chan capturedTokenRequest) {
	t.Helper()

	requests := make(chan capturedTokenRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return
		}
		requests <- capturedTokenRequest{
			method:  request.Method,
			url:     "http://" + request.Host + request.URL.RequestURI(),
			body:    string(body),
			headers: request.Header.Clone(),
		}
		_, _ = writer.Write([]byte(`{"access_token":"token"}`))
	}))
	t.Cleanup(server.Close)

	tokenURL, err := url.Parse(server.URL + "/oauth/token?tenant=example")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	return server.Client(), tokenURL, requests
}

func requireSuccessfulExchange(
	t *testing.T,
	client oauth.Client,
	hc *http.Client,
	session oauth.Session,
	code string,
	extra, headers []oauth.Param,
	requests <-chan capturedTokenRequest,
) capturedTokenRequest {
	t.Helper()

	if _, err := client.Exchange(context.Background(), hc, session, code, extra, headers); err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}

	return <-requests
}

// R-JIJM-L0PC
func TestExchangePostsRequiredFormFieldsToTokenURL(t *testing.T) {
	hc, tokenURL, requests := tokenEndpoint(t)
	client := oauth.Client{
		TokenURL:    tokenURL,
		ClientID:    "client id",
		RedirectURI: "https://app.example.com/oauth/callback?source=test",
	}
	session := oauth.Session{CodeVerifier: "verifier-._~+value"}
	code := "authorization code+value"

	request := requireSuccessfulExchange(t, client, hc, session, code, nil, nil, requests)
	if request.method != http.MethodPost {
		t.Errorf("request method = %q, want %q", request.method, http.MethodPost)
	}
	if request.url != tokenURL.String() {
		t.Errorf("request URL = %q, want %q", request.url, tokenURL.String())
	}
	form, err := url.ParseQuery(request.body)
	if err != nil {
		t.Fatalf("url.ParseQuery(request body) error = %v", err)
	}
	want := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": session.CodeVerifier,
		"redirect_uri":  client.RedirectURI,
		"client_id":     client.ClientID,
	}
	for key, wantValue := range want {
		if got := form.Get(key); got != wantValue {
			t.Errorf("form.Get(%q) = %q, want %q", key, got, wantValue)
		}
	}
}

// R-JJRI-YSG1
func TestExchangeIncludesClientSecretOnlyWhenNonEmpty(t *testing.T) {
	tests := []struct {
		name         string
		clientSecret string
		wantPresent  bool
	}{
		{name: "non-empty", clientSecret: "secret + value", wantPresent: true},
		{name: "empty", clientSecret: "", wantPresent: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hc, tokenURL, requests := tokenEndpoint(t)
			client := oauth.Client{TokenURL: tokenURL, ClientSecret: test.clientSecret}

			request := requireSuccessfulExchange(t, client, hc, oauth.Session{}, "code", nil, nil, requests)
			form, err := url.ParseQuery(request.body)
			if err != nil {
				t.Fatalf("url.ParseQuery(request body) error = %v", err)
			}
			values, present := form["client_secret"]
			if present != test.wantPresent {
				t.Fatalf("client_secret presence = %t, want %t; values = %q", present, test.wantPresent, values)
			}
			if test.wantPresent && !slices.Equal(values, []string{test.clientSecret}) {
				t.Errorf("client_secret values = %q, want [%q]", values, test.clientSecret)
			}
		})
	}
}

// R-JKZF-CK6Q
func TestExchangeSetsFormContentType(t *testing.T) {
	hc, tokenURL, requests := tokenEndpoint(t)

	request := requireSuccessfulExchange(t, oauth.Client{TokenURL: tokenURL}, hc, oauth.Session{}, "code", nil, nil, requests)
	if got, want := request.headers.Get("Content-Type"), "application/x-www-form-urlencoded"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
}

// R-JM7B-QBXF
func TestExchangeAddsCallerHeadersAlongsideContentType(t *testing.T) {
	hc, tokenURL, requests := tokenEndpoint(t)
	headers := []oauth.Param{
		{Key: "Authorization", Value: "Basic Y2xpZW50OnNlY3JldA=="},
		{Key: "X-Provider-Tenant", Value: "tenant-123"},
	}

	request := requireSuccessfulExchange(t, oauth.Client{TokenURL: tokenURL}, hc, oauth.Session{}, "code", nil, headers, requests)
	if got := request.headers.Get("Authorization"); got != headers[0].Value {
		t.Errorf("Authorization = %q, want %q", got, headers[0].Value)
	}
	if got := request.headers.Get("X-Provider-Tenant"); got != headers[1].Value {
		t.Errorf("X-Provider-Tenant = %q, want %q", got, headers[1].Value)
	}
	if got, want := request.headers.Get("Content-Type"), "application/x-www-form-urlencoded"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
}

// R-JNF8-43O4
func TestExchangeAppendsOrderedExtrasWithoutCollapsingRepeatedKeys(t *testing.T) {
	hc, tokenURL, requests := tokenEndpoint(t)
	client := oauth.Client{
		TokenURL:     tokenURL,
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURI:  "https://app.example.com/callback",
	}
	extra := []oauth.Param{
		{Key: "audience", Value: "user profile"},
		{Key: "resource+name", Value: "api/value"},
		{Key: "audience", Value: "admin+write"},
	}

	request := requireSuccessfulExchange(t, client, hc, oauth.Session{CodeVerifier: "verifier"}, "code", extra, nil, requests)
	const extraSuffix = "audience=user+profile&resource%2Bname=api%2Fvalue&audience=admin%2Bwrite"
	if !strings.HasSuffix(request.body, "&"+extraSuffix) {
		t.Errorf("request body = %q, want ordered extra suffix %q", request.body, extraSuffix)
	}
	requiredPrefix := strings.TrimSuffix(request.body, "&"+extraSuffix)
	for _, requiredKey := range []string{"grant_type=", "code=", "code_verifier=", "redirect_uri=", "client_id=", "client_secret="} {
		if !strings.Contains(requiredPrefix, requiredKey) {
			t.Errorf("required prefix %q does not contain %q", requiredPrefix, requiredKey)
		}
	}
	form, err := url.ParseQuery(request.body)
	if err != nil {
		t.Fatalf("url.ParseQuery(request body) error = %v", err)
	}
	if got, want := form["audience"], []string{"user profile", "admin+write"}; !slices.Equal(got, want) {
		t.Errorf("audience values = %q, want %q", got, want)
	}
}

// R-JON4-HVET
func TestReservedTokenParamDefinedKeysAndRepresentativeOthers(t *testing.T) {
	tests := []struct {
		key      string
		reserved bool
	}{
		{key: "grant_type", reserved: true},
		{key: "code", reserved: true},
		{key: "code_verifier", reserved: true},
		{key: "redirect_uri", reserved: true},
		{key: "client_id", reserved: true},
		{key: "client_secret", reserved: true},
		{key: "", reserved: false},
		{key: "grant-type", reserved: false},
		{key: "code-verifier", reserved: false},
		{key: "Client_Secret", reserved: false},
		{key: "client_secret ", reserved: false},
		{key: "scope", reserved: false},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			if got := oauth.ReservedTokenParam(test.key); got != test.reserved {
				t.Errorf("ReservedTokenParam(%q) = %t, want %t", test.key, got, test.reserved)
			}
		})
	}
}
