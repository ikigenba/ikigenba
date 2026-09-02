package agentkit_test

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

type externalProvider struct {
	url           string
	model         string
	received      agentkit.RequestState
	buildCalls    int
	decodeCalls   int
	classifyCalls int
}

var _ agentkit.Provider = (*externalProvider)(nil)

func (p *externalProvider) BuildRequest(ctx context.Context, state agentkit.RequestState) (*http.Request, error) {
	p.buildCalls++
	p.received = state
	return http.NewRequestWithContext(ctx, http.MethodPost, p.url, nil)
}

func (p *externalProvider) Decode(context.Context, *http.Response) iter.Seq2[agentkit.Event, error] {
	p.decodeCalls++
	return func(yield func(agentkit.Event, error) bool) {
		yield(agentkit.ToolCall{Use: agentkit.ToolUse{Name: "complete"}}, nil)
	}
}

func (p *externalProvider) Classify(int, http.Header, []byte) error {
	p.classifyCalls++
	return errors.New("classified")
}

func (p *externalProvider) Identity() agentkit.Identity {
	return agentkit.Identity{Endpoint: "external", AuthMode: "custom", Model: p.model}
}

func TestExternalProviderDrivesSend(t *testing.T) {
	// R-1VRZ-N432
	// R-3H39-MBXP
	// R-SN1U-XZMS
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	unknownModel := "external-model-not-known-to-agentkit"
	provider := &externalProvider{url: server.URL, model: unknownModel}
	clientCalls := 0
	serverClient := server.Client()
	client := &http.Client{Transport: externalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		clientCalls++
		return serverClient.Transport.RoundTrip(request)
	})}
	conversation := agentkit.NewConversation(provider, client, agentkit.Config{})

	for event := range conversation.Send(context.Background(), agentkit.Text{Text: "hello"}).Events() {
		_ = event
	}
	if provider.buildCalls != 1 || provider.decodeCalls != 1 || provider.classifyCalls != 0 {
		t.Fatalf("provider calls: build=%d decode=%d classify=%d", provider.buildCalls, provider.decodeCalls, provider.classifyCalls)
	}
	if provider.received.Model != unknownModel {
		t.Fatalf("provider received model %q, want verbatim %q", provider.received.Model, unknownModel)
	}
	if clientCalls != 1 {
		t.Fatalf("supplied HTTP client calls = %d, want 1", clientCalls)
	}
}

type externalRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip externalRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestEndpointHookSignaturesArePublic(t *testing.T) {
	var auth agentkit.AuthApplier = externalAuth{}
	var mutator agentkit.RequestMutator = func(*http.Request, *[]byte) error { return nil }
	var classifier agentkit.ErrorClassifier = func(int, http.Header, []byte) error { return nil }
	options := []agentkit.EndpointOption{
		agentkit.WithHeader("X-External", "yes"),
		agentkit.WithFramer(agentkit.SSEFrames),
		agentkit.WithClassifier(classifier),
		agentkit.WithMutator(mutator),
		agentkit.WithHTTPClient(http.DefaultClient),
	}
	if _, err := agentkit.NewEndpoint("https://example.test/v1", auth, options...); err != nil {
		t.Fatal(err)
	}
	if len(options) != 5 {
		t.Fatal("exported endpoint hook vocabulary is unavailable")
	}
}

type externalAuth struct{}

func (externalAuth) Apply(context.Context, *http.Request, []byte) error { return nil }
