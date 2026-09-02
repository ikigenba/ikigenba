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

func TestEndpointFieldsAreUnexported(t *testing.T) {
	// R-YEPA-QILV
	typeOfEndpoint := reflect.TypeFor[Endpoint]()
	for index := range typeOfEndpoint.NumField() {
		if typeOfEndpoint.Field(index).IsExported() {
			t.Fatalf("Endpoint field %q is assignable by consumers", typeOfEndpoint.Field(index).Name)
		}
	}
}

func TestNewEndpointValidatesRequiredInputsAndOptions(t *testing.T) {
	// R-YDHE-CQV6
	endpoint, err := NewEndpoint("https://example.test/v1", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.config.framer == nil || endpoint.config.auth == nil || endpoint.config.classifier == nil || endpoint.config.mutator == nil {
		t.Fatal("safe endpoint defaults were not installed")
	}
	invalid := []EndpointOption{
		nil,
		WithHeader("bad header", "value"),
		WithHeader("good", "bad\nvalue"),
		WithFramer(nil),
		WithClassifier(nil),
		WithMutator(nil),
		WithHTTPClient(nil),
	}
	for _, option := range invalid {
		if _, constructErr := NewEndpoint("https://example.test", authFunc(func(context.Context, *http.Request, []byte) error { return nil }), option); !errors.Is(constructErr, ErrInvalidConfig) {
			t.Fatalf("invalid option error = %v, want ErrInvalidConfig", constructErr)
		}
	}
	if _, err := NewEndpoint("", authFunc(func(context.Context, *http.Request, []byte) error { return nil })); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing URL error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewEndpoint("not a URL", authFunc(func(context.Context, *http.Request, []byte) error { return nil })); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid URL error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewEndpoint("https://example.test", nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil auth error = %v, want ErrInvalidConfig", err)
	}
}

func TestWithNameRejectsEmptyEndpointName(t *testing.T) {
	// R-O1EV-VVLP
	_, err := NewEndpoint(
		"https://example.test",
		authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
		WithName(""),
	)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty endpoint name error = %v, want ErrInvalidConfig", err)
	}
}

func TestEndpointOwnsCompleteTransportConfiguration(t *testing.T) {
	// R-YH53-I239
	framerCalls := 0
	framer := func(io.Reader) iter.Seq2[[]byte, error] {
		framerCalls++
		return func(yield func([]byte, error) bool) { yield([]byte("framed"), nil) }
	}
	classified := errors.New("classified by endpoint")
	classifier := func(status int, header http.Header, body []byte) error {
		if status == http.StatusOK && string(body) == "framed" {
			return nil
		}
		if status != http.StatusTeapot || header.Get("Retry-After") != "3" || string(body) != "failure" {
			t.Fatalf("classifier inputs = %d %v %q", status, header, body)
		}
		return classified
	}
	mutatorCalls := 0
	mutator := func(request *http.Request, body *[]byte) error {
		mutatorCalls++
		request.URL.Path += "/" + string(*body)
		*body = []byte("mutated")
		return nil
	}
	authCalls := 0
	auth := authFunc(func(_ context.Context, request *http.Request, body []byte) error {
		authCalls++
		if request.Header.Get("X-Extra") != "one" || string(body) != "mutated" {
			t.Fatalf("auth request headers=%v body=%q", request.Header, body)
		}
		return nil
	})
	clientCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}
	endpoint, err := NewEndpoint(
		"https://example.test/fixed/path",
		auth,
		WithHeader("X-Extra", "one"),
		WithFramer(framer),
		WithClassifier(classifier),
		WithMutator(mutator),
		WithHTTPClient(client),
		withModelPlacement(modelInPath),
	)
	if err != nil {
		t.Fatal(err)
	}
	config := endpoint.config
	if config.baseURL.Scheme != "https" || config.baseURL.Host != "example.test" || config.baseURL.Path != "/fixed/path" || config.headers.Get("X-Extra") != "one" || config.framer == nil || config.classifier == nil || config.mutator == nil || config.auth == nil || config.modelPlacement != modelInPath || config.client != client {
		t.Fatalf("endpoint did not retain complete transport configuration: %+v", config)
	}
	provider := newComposedProvider(&testWire{}, endpoint, Identity{})
	request, err := provider.BuildRequest(context.Background(), RequestState{Model: "model-in-path"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if request.URL.String() != "https://example.test/fixed/path/model-in-path" || string(body) != "mutated" || mutatorCalls != 1 || authCalls != 1 {
		t.Fatalf("assembled request URL=%s body=%q mutator=%d auth=%d", request.URL, body, mutatorCalls, authCalls)
	}
	var events []Event
	for event, decodeErr := range provider.Decode(context.Background(), &http.Response{Body: http.NoBody}) {
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		events = append(events, event)
	}
	want := []Event{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "framed"}}}}}
	if framerCalls != 1 || !reflect.DeepEqual(events, want) {
		t.Fatalf("framer calls=%d events=%v", framerCalls, events)
	}
	if err := provider.Classify(http.StatusTeapot, http.Header{"Retry-After": {"3"}}, []byte("failure")); !errors.Is(err, classified) {
		t.Fatalf("classifier error = %v, want %v", err, classified)
	}
	if _, err := config.client.Do(request); err != nil || clientCalls != 1 {
		t.Fatalf("endpoint client error=%v calls=%d", err, clientCalls)
	}
}

func TestEndpointModelPlacementUsesTheSingleMutator(t *testing.T) {
	// R-3ENG-USGB
	wire := &testWire{}
	bodyEndpoint, err := NewEndpoint(
		"https://body.test/messages",
		authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
		withModelPlacement(modelInBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	pathEndpoint, err := NewEndpoint(
		"https://path.test/models",
		authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
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
