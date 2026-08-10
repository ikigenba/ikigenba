package suite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"eventplane/correlation"
	"prompts/internal/mcpclient"
	"registry"
)

func TestDiscoverMapsInventoryToBarePeers(t *testing.T) {
	// R-ORZ1-QIWX
	// R-ZDZU-IC81
	// R-EF0V-TP9R
	root := t.TempDir()
	writeManifest(t, root, "crm")
	writeManifest(t, root, "gmail")
	writeManifest(t, root, "prompts")

	peers := Discover(context.Background(), root, "owner-1", "owner@example.com", "prompt-1", "")
	if len(peers) != 2 {
		t.Fatalf("Discover returned %d peers, want 2: %#v", len(peers), peers)
	}
	byName := make(map[string]Peer, len(peers))
	for _, peer := range peers {
		byName[peer.Name] = peer
		wantHeaders := map[string]string{
			"X-Owner-Id":    "owner-1",
			"X-Owner-Email": "owner@example.com",
			"X-Client-Id":   "prompts:prompt-1",
		}
		if !maps.Equal(peer.Headers, wantHeaders) {
			t.Fatalf("headers for %q = %#v, want %#v", peer.Name, peer.Headers, wantHeaders)
		}
	}
	for _, service := range []string{"crm", "gmail"} {
		peer, ok := byName[service]
		if !ok {
			t.Fatalf("missing bare peer for %q: %#v", service, peers)
		}
		want := registry.BaseURL(service) + "/mcp"
		if peer.BaseURL != want {
			t.Fatalf("%s BaseURL = %q, want %q", service, peer.BaseURL, want)
		}
	}
	if _, ok := byName["prompts"]; ok {
		t.Fatalf("Discover included prompts self entry: %#v", peers)
	}
}

func TestDiscoverCorrelationHeaderPresentExactlyOrAbsent(t *testing.T) {
	// R-P5DX-Y02K
	// R-HPLU-D8WR
	root := t.TempDir()
	writeManifest(t, root, "crm")
	writeManifest(t, root, "gmail")

	const correlationID = "chain-X"
	withChain := Discover(context.Background(), root, "owner-1", "owner@example.com", "prompt-1", correlationID)
	if len(withChain) != 2 {
		t.Fatalf("Discover with chain returned %d peers, want 2", len(withChain))
	}
	for _, peer := range withChain {
		want := map[string]string{
			"X-Owner-Id":       "owner-1",
			"X-Owner-Email":    "owner@example.com",
			"X-Client-Id":      "prompts:prompt-1",
			correlation.Header: correlationID,
		}
		if !maps.Equal(peer.Headers, want) {
			t.Fatalf("headers for %q = %#v, want %#v", peer.Name, peer.Headers, want)
		}
	}

	withoutChain := Discover(context.Background(), root, "owner-1", "owner@example.com", "prompt-1", "")
	for _, peer := range withoutChain {
		if value, ok := peer.Headers[correlation.Header]; ok {
			t.Fatalf("headers for %q contain %s=%q, want key absent", peer.Name, correlation.Header, value)
		}
	}
}

func TestPeerHeadersAreInjectedByRealClient(t *testing.T) {
	// R-ZF7Q-W3YQ
	// R-HS1N-4SE5
	assertPeerHeadersInjected(t)
}

func TestCatalogUsesFirstSentenceAndPreservesPeerOrder(t *testing.T) {
	// R-OT6Y-4ANM
	peers := []Peer{{Name: "gmail"}, {Name: "crm"}}
	instructions := map[string]string{
		"gmail": "Send and search email. Use exact addresses when possible.",
		"crm":   "Manage customer records! Changes are audited.",
	}

	got := Catalog(context.Background(), peers, func(peer Peer) InstructionLister {
		return instructionStub{instructions: instructions[peer.Name]}
	})
	want := []CatalogEntry{
		{Name: "gmail", Summary: "Send and search email."},
		{Name: "crm", Summary: "Manage customer records!"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Catalog = %#v, want %#v", got, want)
	}
}

func TestCatalogKeepsRowWhenPeerRefusesConnection(t *testing.T) {
	// R-OUEU-I2EB
	healthy := newInstructionServer(t, "Healthy peer answers. More detail follows.", nil)
	defer healthy.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadURL := "http://" + listener.Addr().String() + "/mcp"
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	peers := []Peer{
		{Name: "dead", BaseURL: deadURL},
		{Name: "healthy", BaseURL: healthy.URL + "/mcp"},
	}
	got := Catalog(context.Background(), peers, func(peer Peer) InstructionLister {
		return mcpclient.New(healthy.Client(), peer.BaseURL, peer.Headers)
	})
	want := []CatalogEntry{
		{Name: "dead", Summary: ""},
		{Name: "healthy", Summary: "Healthy peer answers."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Catalog = %#v, want both rows %#v", got, want)
	}
}

func TestCatalogFetchesPeersConcurrently(t *testing.T) {
	peers := []Peer{{Name: "crm"}, {Name: "gmail"}}
	started := make(chan string, len(peers))
	release := make(chan struct{})
	done := make(chan []CatalogEntry, 1)
	go func() {
		done <- Catalog(context.Background(), peers, func(peer Peer) InstructionLister {
			return instructionStub{instructions: peer.Name + " works.", started: started, release: release, name: peer.Name}
		})
	}()

	seen := make(map[string]bool, len(peers))
	for range peers {
		seen[<-started] = true
	}
	close(release)
	got := <-done
	if !seen["crm"] || !seen["gmail"] || len(got) != 2 {
		t.Fatalf("concurrent starts = %#v, entries = %#v", seen, got)
	}
}

func assertPeerHeadersInjected(t *testing.T) {
	t.Helper()
	wantHeaders := map[string]string{
		"X-Owner-Id":       "owner-1",
		"X-Owner-Email":    "owner@example.com",
		"X-Client-Id":      "prompts:prompt-1",
		correlation.Header: "chain-X",
	}
	var mu sync.Mutex
	seen := make([]http.Header, 0, 2)
	peerServer := newInstructionServer(t, "Peer instructions.", func(header http.Header) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, header)
	})
	defer peerServer.Close()

	peer := Peer{Name: "crm", BaseURL: peerServer.URL + "/mcp", Headers: wantHeaders}
	instructions, err := mcpclient.New(peerServer.Client(), peer.BaseURL, peer.Headers).Instructions(context.Background())
	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}
	if instructions != "Peer instructions." {
		t.Fatalf("instructions = %q", instructions)
	}
	for i, header := range seen {
		for name, value := range wantHeaders {
			if got := header.Get(name); got != value {
				t.Fatalf("request %d header %s = %q, want %q", i, name, got, value)
			}
		}
	}
}

type instructionStub struct {
	instructions string
	err          error
	started      chan<- string
	release      <-chan struct{}
	name         string
}

func (s instructionStub) Instructions(context.Context) (string, error) {
	if s.started != nil {
		s.started <- s.name
		<-s.release
	}
	return s.instructions, s.err
}

func newInstructionServer(t *testing.T, instructions string, observe func(http.Header)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if observe != nil {
			observe(r.Header.Clone())
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if request.Method != "initialize" {
			t.Errorf("method = %q, want initialize", request.Method)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{
				"protocolVersion": "2025-11-25",
				"instructions":    instructions,
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

func writeManifest(t *testing.T, root, service string) {
	t.Helper()
	dir := filepath.Join(root, service, "etc", "current")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	port, _ := registry.Port(service)
	data := []byte("APP=" + service + "\nMOUNT=/" + service + "/\nPORT=" + fmt.Sprint(port) + "\nMCP=true\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.env"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

var _ InstructionLister = instructionStub{err: errors.New("unavailable")}
