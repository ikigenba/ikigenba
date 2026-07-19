package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fixture struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

func TestExtractJSONCarvesFencedAndBareValues(t *testing.T) {
	// R-J8QP-BETB
	for name, input := range map[string]string{
		"json fence": "```json\n{\"title\":\"ok\",\"count\":1}\n```",
		"bare fence": "```\n[{\"title\":\"ok\",\"count\":1}]\n```",
		"bare JSON":  " {\"title\":\"ok\",\"count\":1} ",
	} {
		t.Run(name, func(t *testing.T) {
			got := ExtractJSON(input)
			var value any
			if err := json.Unmarshal([]byte(got), &value); err != nil {
				t.Fatalf("ExtractJSON(%q) = %q: %v", input, got, err)
			}
		})
	}
}

func TestExtractJSONCarvesDecoratedValues(t *testing.T) {
	// R-4BCC-0EHJ
	for name, input := range map[string]string{
		"prose":       "Here it is: {\"title\":\"ok\"} thanks",
		"extra ticks": "````json\n{\"title\":\"ok\"}\n````",
		"stray tick":  "`{\"title\":\"ok\"}",
	} {
		t.Run(name, func(t *testing.T) {
			if got := ExtractJSON(input); got != `{"title":"ok"}` {
				t.Fatalf("ExtractJSON() = %q", got)
			}
		})
	}
}

func TestJSONReturnsValidatedFencedResponse(t *testing.T) {
	// R-J9YL-P6K0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, "```json\n{\"title\":\"ok\",\"count\":2}\n```", 1)
	}))
	defer server.Close()
	validated := false
	got, err := JSON(context.Background(), New(server.URL), CallSite{Stage: "extract"}, Attribution{Origin: "service:wiki"}, "prompt", func(v *fixture) error {
		validated = true
		if v.Title == "" {
			return errors.New("title required")
		}
		return nil
	})
	if err != nil || got.Title != "ok" || got.Count != 2 || !validated {
		t.Fatalf("JSON() = %#v, %v, validated=%v", got, err, validated)
	}
}

func TestJSONRetriesBadThenGoodAndErrorsWhenAlwaysBad(t *testing.T) {
	// R-JCEE-GQ1E
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			writeResponse(t, w, "not json", 1)
			return
		}
		writeResponse(t, w, `{"title":"fixed","count":3}`, 1)
	}))
	got, err := JSON(context.Background(), New(server.URL), CallSite{MaxParseRetries: 1}, Attribution{}, "prompt", nilFixture)
	server.Close()
	if err != nil || got.Title != "fixed" || calls != 2 {
		t.Fatalf("bad then good = %#v, %v, calls=%d", got, err, calls)
	}

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, "still bad", 1)
	}))
	defer server.Close()
	got, err = JSON(context.Background(), New(server.URL), CallSite{MaxParseRetries: 1}, Attribution{}, "prompt", nilFixture)
	if err == nil || got != (fixture{}) {
		t.Fatalf("always bad = %#v, %v; want zero and error", got, err)
	}
}

func TestJSONCarriesCompleteRequestConfiguration(t *testing.T) {
	// R-0X4N-U0XB
	// R-MSKH-GPX5
	temp, thinking := 0.25, false
	var got completeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		writeResponse(t, w, `{"title":"ok"}`, 1)
	}))
	defer server.Close()
	site := CallSite{Stage: "compile", System: "system", Config: Config{Model: "model", Provider: "provider", Temperature: &temp, MaxTokens: 321, Effort: "low", Thinking: &thinking}}
	if _, err := JSON(context.Background(), New(server.URL), site, Attribution{Origin: "user:a@example.com", GroupID: "group-1"}, "prompt", nilFixture); err != nil {
		t.Fatal(err)
	}
	if got.Name != "wiki.compile" || got.Origin != "user:a@example.com" || got.GroupID != "group-1" || got.Attempt != 1 || got.Model != "model" || got.Provider != "provider" || got.System != "system" {
		t.Fatalf("request envelope = %+v", got)
	}
	if got.Config.Temperature == nil || *got.Config.Temperature != temp || got.Config.MaxTokens != 321 || got.Config.Effort != "low" || got.Config.Thinking == nil || *got.Config.Thinking {
		t.Fatalf("request config = %+v", got.Config)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" || got.Messages[0].Content != "prompt" {
		t.Fatalf("messages = %+v", got.Messages)
	}
}

func TestJSONRetryReplaysFullStatelessHistory(t *testing.T) {
	// R-0ZKG-LKEP
	var requests []completeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req completeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, req)
		if len(requests) == 1 {
			writeResponse(t, w, "bad reply", 1)
			return
		}
		writeResponse(t, w, `{"title":"fixed"}`, 1)
	}))
	defer server.Close()
	if _, err := JSON(context.Background(), New(server.URL), CallSite{MaxParseRetries: 1}, Attribution{}, "original", nilFixture); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].Attempt != 1 || len(requests[0].Messages) != 1 || requests[1].Attempt != 2 || len(requests[1].Messages) != 3 {
		t.Fatalf("requests = %+v", requests)
	}
	roles := []string{requests[1].Messages[0].Role, requests[1].Messages[1].Role, requests[1].Messages[2].Role}
	if strings.Join(roles, ",") != "user,assistant,user" || requests[1].Messages[0].Content != "original" || requests[1].Messages[1].Content != "bad reply" {
		t.Fatalf("retry messages = %+v", requests[1].Messages)
	}
}

func TestJSONReturns400BodyWithoutRetry(t *testing.T) {
	// R-10SC-ZC5E
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad model"}`))
	}))
	defer server.Close()
	_, err := JSON[fixture](context.Background(), New(server.URL), CallSite{MaxParseRetries: 3}, Attribution{}, "prompt", nilFixture)
	if err == nil || !strings.Contains(err.Error(), "bad model") || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestJSONReturnsProviderAndTransportFailuresWithoutRetry(t *testing.T) {
	// R-1209-D3W3
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"provider unavailable"}`))
	}))
	_, err := JSON[fixture](context.Background(), New(server.URL), CallSite{MaxParseRetries: 2}, Attribution{}, "prompt", nilFixture)
	server.Close()
	if err == nil || calls != 1 {
		t.Fatalf("502 error=%v calls=%d", err, calls)
	}
	_, err = JSON[fixture](context.Background(), New(server.URL), CallSite{MaxParseRetries: 2}, Attribution{}, "prompt", nilFixture)
	if err == nil || !strings.Contains(err.Error(), "prompts /complete") {
		t.Fatalf("transport error=%v", err)
	}
}

func TestJSONReportsTruncationDistinctlyAndDoesNotRetry(t *testing.T) {
	// R-MTSD-UHNU
	// R-MV0A-89EJ
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writeResponse(t, w, `{"title":"partial"`, 10)
	}))
	defer server.Close()
	_, err := JSON[fixture](context.Background(), New(server.URL), CallSite{Config: Config{MaxTokens: 10}, MaxParseRetries: 3}, Attribution{}, "prompt", nilFixture)
	if !errors.Is(err, ErrTruncated) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func writeResponse(t *testing.T, w http.ResponseWriter, text string, output int) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"text": text, "usage": map[string]any{"output": output}}); err != nil {
		t.Fatal(err)
	}
}

func nilFixture(*fixture) error { return nil }
