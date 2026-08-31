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
		yield("complete", nil)
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
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	unknownModel := "external-model-not-known-to-agentkit"
	provider := &externalProvider{url: server.URL, model: unknownModel}
	conversation := agentkit.NewConversation(provider, server.Client())

	conversation.Send(context.Background(), agentkit.Text{Text: "hello"})
	if provider.buildCalls != 1 || provider.decodeCalls != 1 || provider.classifyCalls != 0 {
		t.Fatalf("provider calls: build=%d decode=%d classify=%d", provider.buildCalls, provider.decodeCalls, provider.classifyCalls)
	}
	if provider.received.Model != unknownModel {
		t.Fatalf("provider received model %q, want verbatim %q", provider.received.Model, unknownModel)
	}
}
