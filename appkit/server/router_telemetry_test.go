package server_test

import (
	"appkit/server"
	"appkit/telemetry"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRouterRecorderReturnsServeWiredRecorder(t *testing.T) {
	// R-XSOU-MECH
	recorder, sink, cancel := phase20Recorder(t)
	_, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Service:    testService,
		Recorder:   recorder,
		Register: func(rt *server.Router) error {
			rt.Recorder().Record(telemetry.Record{Kind: telemetry.KindRoot, Op: "ledger:worker-cycle"})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	records := phase20Finish(t, recorder, sink, cancel, 1)
	if len(records) != 1 || records[0].Kind != telemetry.KindRoot || records[0].Op != "ledger:worker-cycle" {
		t.Fatalf("sink records = %#v, want the root emitted through Router.Recorder", records)
	}
}

func TestRouterHTTPClientUsesTimeoutAndServeWiredRecorder(t *testing.T) {
	// R-XTWR-0636
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "peer response")
	}))
	defer peer.Close()
	recorder, sink, cancel := phase20Recorder(t)
	var client *http.Client
	_, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Service:    testService,
		Recorder:   recorder,
		Register: func(rt *server.Router) error {
			client = rt.HTTPClient(3 * time.Second)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if client == nil || client.Timeout != 3*time.Second {
		t.Fatalf("HTTPClient timeout = %v, want 3s", client.Timeout)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, peer.URL+"/probe", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}

	records := phase20Finish(t, recorder, sink, cancel, 1)
	if len(records) != 1 || records[0].Kind != telemetry.KindOutbound {
		t.Fatalf("sink records = %#v, want exactly one outbound record", records)
	}
}
