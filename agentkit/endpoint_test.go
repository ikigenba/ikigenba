package agentkit

import (
	"context"
	"errors"
	"io"
	"iter"
	"net/http"
	"reflect"
	"testing"
)

func TestEndpointIsOpaqueOptionBuiltAndValidatesOptions(t *testing.T) {
	typeOfEndpoint := reflect.TypeFor[Endpoint]()
	for index := range typeOfEndpoint.NumField() {
		if typeOfEndpoint.Field(index).IsExported() {
			t.Fatalf("Endpoint field %q is assignable by consumers", typeOfEndpoint.Field(index).Name)
		}
	}
	endpoint, err := newEndpoint(WithBaseURL("https://example.test/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.config.framer == nil || endpoint.config.auth == nil || endpoint.config.classifier == nil || endpoint.config.mutator == nil {
		t.Fatal("safe endpoint defaults were not installed")
	}
	invalid := []EndpointOption{
		nil,
		WithBaseURL("not a URL"),
		WithHeader("bad header", "value"),
		WithHeader("good", "bad\nvalue"),
		WithFramer(nil),
		WithClassifier(nil),
		WithMutator(nil),
		withAuth(nil),
	}
	for _, option := range invalid {
		if _, constructErr := newEndpoint(WithBaseURL("https://example.test"), option); !errors.Is(constructErr, ErrInvalidConfig) {
			t.Fatalf("invalid option error = %v, want ErrInvalidConfig", constructErr)
		}
	}
	if _, err := newEndpoint(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing URL error = %v, want ErrInvalidConfig", err)
	}
}

func TestEndpointOwnsCompleteTransportConfiguration(t *testing.T) {
	framer := func(io.Reader) iter.Seq2[[]byte, error] { return func(func([]byte, error) bool) {} }
	classifier := func(int, http.Header, []byte) error { return nil }
	mutator := func(*http.Request, *[]byte) error { return nil }
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := newEndpoint(
		WithBaseURL("https://example.test/fixed/path"),
		WithHeader("X-Extra", "one"),
		WithFramer(framer),
		WithClassifier(classifier),
		WithMutator(mutator),
		withAuth(auth),
		withModelPlacement(modelInPath),
	)
	if err != nil {
		t.Fatal(err)
	}
	config := endpoint.config
	if config.baseURL.String() != "https://example.test/fixed/path" || config.headers.Get("X-Extra") != "one" || config.framer == nil || config.classifier == nil || config.mutator == nil || config.auth == nil || config.modelPlacement != modelInPath {
		t.Fatalf("endpoint did not retain complete transport configuration: %+v", config)
	}
}

func TestEndpointModelPlacementUsesTheSingleMutator(t *testing.T) {
	// R-3ENG-USGB
	wire := &testWire{}
	bodyEndpoint, err := newEndpoint(
		WithBaseURL("https://body.test/messages"),
		withModelPlacement(modelInBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	pathEndpoint, err := newEndpoint(
		WithBaseURL("https://path.test/models"),
		withModelPlacement(modelInPath),
		WithMutator(func(request *http.Request, body *[]byte) error {
			request.URL.Path += "/" + string(*body)
			*body = []byte("history-only")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	state := RequestState{Model: "chosen-model"}
	bodyRequest, err := newComposedProvider(wire, bodyEndpoint, Identity{}).BuildRequest(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes, _ := io.ReadAll(bodyRequest.Body)
	if bodyRequest.URL.String() != "https://body.test/messages" || string(bodyBytes) != "chosen-model" || bodyEndpoint.config.modelPlacement != modelInBody {
		t.Fatalf("body placement URL=%s body=%q", bodyRequest.URL, bodyBytes)
	}
	pathRequest, err := newComposedProvider(wire, pathEndpoint, Identity{}).BuildRequest(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	pathBytes, _ := io.ReadAll(pathRequest.Body)
	if pathRequest.URL.String() != "https://path.test/models/chosen-model" || string(pathBytes) != "history-only" || pathEndpoint.config.modelPlacement != modelInPath {
		t.Fatalf("path placement URL=%s body=%q", pathRequest.URL, pathBytes)
	}
}
