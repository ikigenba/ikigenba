package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

func collectParityStream(stream *agentkit.Stream) ([]agentkit.Event, error) {
	var events []agentkit.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events, stream.Err()
}

type capturingCredential struct {
	url *url.URL
	err error
}

func (credential *capturingCredential) apply(_ context.Context, request *http.Request, _ []byte) error {
	capturedURL := *request.URL
	credential.url = &capturedURL
	return credential.err
}

func (*capturingCredential) isGeminiCredential() {}

func TestNewPlacesEscapedModelInDefaultURLPath(t *testing.T) {
	// R-TDVN-CXY2
	terminal := errors.New("stop after URL capture")
	credential := &capturingCredential{err: terminal}
	model := "vendor/model:latest"
	conversation, err := New(credential, model)
	if err != nil {
		t.Fatal(err)
	}
	stream := conversation.Send(context.Background(), agentkit.Text{Text: "hello"})
	for event := range stream.Events() {
		_ = event
	}
	if !errors.Is(stream.Err(), terminal) {
		t.Fatalf("stream error = %v, want sentinel %v", stream.Err(), terminal)
	}
	if credential.url == nil {
		t.Fatal("credential did not capture the request URL")
	}
	wantPath := "/v1beta/models/vendor%2Fmodel:latest:streamGenerateContent"
	if credential.url.EscapedPath() != wantPath {
		t.Fatalf("escaped request path = %q, want %q", credential.url.EscapedPath(), wantPath)
	}
	if credential.url.Query().Get("alt") != "sse" {
		t.Fatalf("request query = %q, want alt=sse", credential.url.RawQuery)
	}
}

func TestNewNamesGeminiEndpointAndUsesCatalogPricing(t *testing.T) {
	// R-EKRG-9Y2W
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"done\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":3}}\n\n")
	}))
	defer server.Close()

	var output bytes.Buffer
	conversation, err := New(APIKey("key"), "gemini-2.5-flash",
		WithBaseURL(server.URL),
		WithConfig(agentkit.Config{Log: agentkit.NewLog(&output, nil)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, streamErr := collectParityStream(conversation.Send(context.Background(), agentkit.Text{Text: "hello"}))
	if streamErr != nil {
		t.Fatal(streamErr)
	}
	const wantCost = agentkit.Cost(2*300 + 3*2_500)
	assertGeminiTurnIdentityAndCost(t, output.Bytes(), agentkit.ProviderGemini, wantCost)
}

func assertGeminiTurnIdentityAndCost(t *testing.T, data []byte, provider agentkit.ProviderID, want agentkit.Cost) {
	t.Helper()
	var identity *agentkit.Identity
	var cost *agentkit.Cost
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var record agentkit.LogRecord
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record.Type == agentkit.RecordTurnStart {
			identity = record.Identity
		}
		if record.Type == agentkit.RecordUsage {
			cost = record.Cost
		}
	}
	if identity == nil || identity.Endpoint != string(provider) {
		t.Fatalf("turn identity = %+v, want endpoint %q", identity, provider)
	}
	if cost == nil || *cost != want {
		t.Fatalf("turn cost = %v, want catalog amount %d", cost, want)
	}
}
