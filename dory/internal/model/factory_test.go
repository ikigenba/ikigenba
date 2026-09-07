package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/model"
)

type capturedRequest struct {
	url           string
	authorization string
	body          map[string]any
}

type factoryToolInput struct {
	Value string `json:"value"`
}

type factoryScenario struct {
	first, second               *agentkit.Conversation
	requests                    chan capturedRequest
	firstLog, secondLog         bytes.Buffer
	firstRequest, secondRequest capturedRequest
	blockedErr                  error
	wantURL                     string
}

func TestFactoryNewAppliesEachConversationsTransportAndConfiguration(t *testing.T) {
	// R-ID6R-Q8NX
	// R-IEEO-40EM
	scenario := mustFactoryScenario(t)
	if err := scenario.exercise(); err != nil {
		t.Fatalf("exercise conversations: %v", err)
	}
	if !errors.Is(scenario.blockedErr, agentkit.ErrLimitExceeded) {
		t.Fatalf("second first-conversation Send error = %v, want context limit", scenario.blockedErr)
	}
	scenario.assertRequests(t)
	scenario.assertLogs(t)
	assertIndependentFirstRequests(t, scenario.firstRequest.body, scenario.secondRequest.body)
}

func mustFactoryScenario(t *testing.T) *factoryScenario {
	t.Helper()
	requests := make(chan capturedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decode provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- capturedRequest{
			url:           "http://" + r.Host + r.URL.RequestURI(),
			authorization: r.Header.Get("Authorization"),
			body:          decoded,
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":7}}\n\n")
	}))
	t.Cleanup(server.Close)

	cfg := openAIChatConfig(t)
	cfg.BaseURL = server.URL + "/v1/chat"
	cfg.MaxContext = 10
	cfg.Settings = map[string]string{"temperature": "0.25", "max_output_tokens": "77"}
	cfg.Getenv = func(name string) string {
		if name != "OPENAI_API_KEY" {
			t.Fatalf("Getenv(%q), want OPENAI_API_KEY", name)
		}
		return "factory-test-key"
	}
	factory, err := model.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Opening fixes the role configuration rather than retaining the caller's map.
	cfg.Settings["temperature"] = "0.9"

	alpha := mustFactoryTool(t, "alpha")
	beta := mustFactoryTool(t, "beta")
	scenario := &factoryScenario{requests: requests, wantURL: cfg.BaseURL}
	firstLog := agentkit.NewLog(&scenario.firstLog, fixedFactoryTime, "first-log")
	secondLog := agentkit.NewLog(&scenario.secondLog, fixedFactoryTime, "second-log")
	first, err := factory.New([]agentkit.Tool{alpha}, firstLog)
	if err != nil {
		t.Fatalf("New first conversation: %v", err)
	}
	second, err := factory.New([]agentkit.Tool{beta}, secondLog)
	if err != nil {
		t.Fatalf("New second conversation: %v", err)
	}
	scenario.first, scenario.second = first, second
	return scenario
}

func (s *factoryScenario) exercise() error {
	if err := consumeFactoryStream(s.first.Send(context.Background(), agentkit.Text{Text: "first user"})); err != nil {
		return fmt.Errorf("first conversation: %w", err)
	}
	blocked := s.first.Send(context.Background(), agentkit.Text{Text: "must be blocked"})
	for event := range blocked.Events() {
		_ = event
	}
	s.blockedErr = blocked.Err()
	if err := consumeFactoryStream(s.second.Send(context.Background(), agentkit.Text{Text: "second user"})); err != nil {
		return fmt.Errorf("second conversation: %w", err)
	}
	s.firstRequest = <-s.requests
	s.secondRequest = <-s.requests
	return nil
}

func (s *factoryScenario) assertRequests(t *testing.T) {
	t.Helper()
	for index, request := range []capturedRequest{s.firstRequest, s.secondRequest} {
		if request.url != s.wantURL {
			t.Errorf("request %d URL = %q, want %q", index, request.url, s.wantURL)
		}
		if request.authorization != "Bearer factory-test-key" {
			t.Errorf("request %d authorization = %q", index, request.authorization)
		}
		if request.body["temperature"] != 0.25 || request.body["max_completion_tokens"] != float64(77) {
			t.Errorf("request %d settings = temperature %#v, max_completion_tokens %#v", index, request.body["temperature"], request.body["max_completion_tokens"])
		}
	}
	assertRequestTools(t, s.firstRequest.body, []string{"alpha"})
	assertRequestTools(t, s.secondRequest.body, []string{"beta"})
}

func (s *factoryScenario) assertLogs(t *testing.T) {
	t.Helper()
	firstRecords := decodeFactoryLog(t, s.firstLog.Bytes())
	assertFactoryLogRecords(t, firstRecords, "first-log", 2, 3, 2)
	var foundLimit bool
	for _, record := range firstRecords {
		if record.Type == agentkit.RecordLimit && record.Limit != nil && record.Limit.Max == 10 && record.Limit.Actual == 19 {
			foundLimit = true
		}
	}
	if !foundLimit {
		t.Fatalf("first log has no installed max-context refusal: %#v", firstRecords)
	}
	assertFactoryLogRecords(t, decodeFactoryLog(t, s.secondLog.Bytes()), "second-log", 1, 2, 1)
}

func openAIChatConfig(t *testing.T) model.Config {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if offering.ID != agentkit.OfferingOpenAIChat {
				continue
			}
			return model.Config{
				Provider:   string(offering.Host),
				Model:      entry.Model,
				Wire:       string(offering.WireName),
				Auth:       string(agentkit.AuthModeAPIKey),
				MaxContext: -1,
				Home:       t.TempDir(),
			}
		}
	}
	t.Fatal("catalog has no OpenAI Chat offering")
	return model.Config{}
}

func mustFactoryTool(t *testing.T, name string) agentkit.Tool {
	t.Helper()
	tool, err := agentkit.NewTool(name, name+" description", func(context.Context, factoryToolInput) (string, error) {
		return "unused", nil
	})
	if err != nil {
		t.Fatalf("NewTool(%q): %v", name, err)
	}
	return tool
}

func consumeFactoryStream(stream *agentkit.Stream) error {
	for event := range stream.Events() {
		_ = event
	}
	return stream.Err()
}

func fixedFactoryTime() time.Time {
	return time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
}

func assertRequestTools(t *testing.T, body map[string]any, want []string) {
	t.Helper()
	rawTools, ok := body["tools"].([]any)
	if !ok {
		t.Fatalf("request tools = %#v", body["tools"])
	}
	got := make([]string, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			t.Fatalf("tool = %#v", rawTool)
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatalf("tool function = %#v", tool["function"])
		}
		got = append(got, fmt.Sprint(function["name"]))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("advertised tools = %q, want exactly %q", got, want)
	}
}

func decodeFactoryLog(t *testing.T, data []byte) []agentkit.LogRecord {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []agentkit.LogRecord
	for {
		var record agentkit.LogRecord
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			return records
		} else if err != nil {
			t.Fatalf("decode log: %v", err)
		}
		records = append(records, record)
	}
}

func assertFactoryLogRecords(t *testing.T, records []agentkit.LogRecord, id string, turns, messages, ends int) {
	t.Helper()
	counts := make(map[agentkit.RecordType]int)
	for _, record := range records {
		if record.ID != id {
			t.Errorf("log record ID = %q, want %q", record.ID, id)
		}
		counts[record.Type]++
	}
	if counts[agentkit.RecordTurnStart] != turns || counts[agentkit.RecordMessage] != messages || counts[agentkit.RecordTurnEnd] != ends {
		t.Errorf("log %q turn/message records = %#v", id, counts)
	}
}

func assertIndependentFirstRequests(t *testing.T, first, second map[string]any) {
	t.Helper()
	firstMessages, ok := first["messages"].([]any)
	if !ok || len(firstMessages) != 1 {
		t.Fatalf("first request messages = %#v, want one first user message", first["messages"])
	}
	secondMessages, ok := second["messages"].([]any)
	if !ok || len(secondMessages) != 1 {
		t.Fatalf("second request messages = %#v, want one independent user message", second["messages"])
	}
	message, ok := secondMessages[0].(map[string]any)
	if !ok || message["role"] != "user" || message["content"] != "second user" {
		t.Fatalf("second conversation first message = %#v", secondMessages[0])
	}
	encoded, err := json.Marshal(secondMessages)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("first user")) || bytes.Contains(encoded, []byte("hello")) {
		t.Fatalf("second conversation inherited first history: %s", encoded)
	}
}
