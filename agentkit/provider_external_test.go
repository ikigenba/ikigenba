package agentkit_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

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
