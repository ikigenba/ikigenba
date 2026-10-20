package candidates_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

// R-PZ5Q-AE5I
func TestResponseBodyClosedBeforeReturn(t *testing.T) {
	for _, operation := range []string{"cohort", "candidate", "prospect_list", "fallback"} {
		for _, outcome := range []string{"success", "refusal", "rpc-error", "protocol-error", "output-error"} {
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				returned := make(chan struct{})
				writeResult := make(chan error, 1)
				entry := fallbackServer(t, operation, func(w http.ResponseWriter, r *http.Request) {
					req := request(t, r)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: ")
					switch outcome {
					case "success":
						respond(w, req, `{"structuredContent":`+output(operation)+`}`)
					case "refusal":
						respond(w, req, `{"isError":true}`)
					case "rpc-error":
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"test refusal"}}`, req["id"])
					case "protocol-error":
						respond(w, req, `{"resultType":"unknown"}`)
					case "output-error":
						respond(w, req, `{"structuredContent":{}}`)
					}
					_, _ = io.WriteString(w, "\n\n")
					controller := http.NewResponseController(w)
					if err := controller.Flush(); err != nil {
						t.Errorf("flush response event: %v", err)
						writeResult <- nil
						return
					}
					// Continue only after the caller has returned, without canceling its context.
					<-returned
					chunk := []byte(strings.Repeat(":", (64<<10)-2) + "\n\n")
					for range 1024 {
						if _, err := w.Write(chunk); err != nil {
							writeResult <- err
							return
						}
						if err := controller.Flush(); err != nil {
							writeResult <- err
							return
						}
					}
					writeResult <- nil
				})
				w, _ := writer(t)
				client := candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: w})
				_, err := transportCall(context.Background(), t, client, operation, "id")
				close(returned)
				if writeErr := <-writeResult; writeErr == nil {
					t.Fatal("server wrote all 64 MiB after the call returned")
				}
				switch outcome {
				case "success":
					if err != nil {
						t.Fatal(err)
					}
				case "refusal":
					var refusal *candidates.Refusal
					if !errors.As(err, &refusal) {
						t.Fatalf("expected refusal, got %v", err)
					}
				case "rpc-error":
					var rpc *mcp.RPCError
					if !errors.As(err, &rpc) {
						t.Fatalf("expected RPC error, got %v", err)
					}
				default:
					malformed(t, err)
				}
			})
		}
	}
}
