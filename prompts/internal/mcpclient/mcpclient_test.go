package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInitializeInstructionsPresentAndAbsent(t *testing.T) {
	// R-9JNO-RZM2
	tests := []struct {
		name   string
		result map[string]any
		want   string
	}{
		{name: "present", result: map[string]any{"instructions": "Use describe first."}, want: "Use describe first."},
		{name: "absent", result: map[string]any{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params struct {
						ProtocolVersion string `json:"protocolVersion"`
						ClientInfo      struct {
							Name string `json:"name"`
						} `json:"clientInfo"`
					} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if req.Method != "initialize" {
					t.Fatalf("method = %q, want initialize", req.Method)
				}
				if req.Params.ProtocolVersion == "" {
					t.Fatalf("initialize params missing protocolVersion")
				}
				if req.Params.ClientInfo.Name != "prompts" {
					t.Fatalf("clientInfo.name = %q, want prompts", req.Params.ClientInfo.Name)
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      json.RawMessage(req.ID),
					"result":  tt.result,
				}); err != nil {
					t.Fatalf("encode response: %v", err)
				}
			}))
			t.Cleanup(srv.Close)

			got, err := New(srv.URL, nil, time.Second).Initialize(context.Background())
			if err != nil {
				t.Fatalf("Initialize returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Initialize returned %q, want %q", got, tt.want)
			}
		})
	}
}
