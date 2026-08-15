// Package suite maps the box inventory to reachable peers and describes them.
package suite

import (
	"appkit/inventory"
	"context"
	"eventplane/correlation"
	"fmt"
	"registry"
	"strings"
	"sync"
)

const (
	selfName       = "prompts"
	clientIDPrefix = "prompts:"
)

// Peer is one reachable suite service, addressed and identified for the run.
type Peer struct {
	Name    string
	BaseURL string
	Headers map[string]string
}

// Discover returns the non-prompts services in the inventory as reachable
// peers. Inventory and registry failures are handled by omitting the affected
// service, so the returned slice is always non-nil.
func Discover(_ context.Context, manifestRoot, ownerID, ownerEmail, promptID, correlationID string) []Peer {
	services, err := inventory.Read(manifestRoot)
	if err != nil {
		return []Peer{}
	}

	peers := make([]Peer, 0, len(services))
	for _, svc := range services {
		if svc.Name == selfName {
			continue
		}
		port, ok := registry.Port(svc.Name)
		if !ok {
			continue
		}
		headers := map[string]string{
			"X-Owner-Id":    ownerID,
			"X-Owner-Email": ownerEmail,
			"X-Client-Id":   clientIDPrefix + promptID,
		}
		if correlationID != "" {
			headers[correlation.Header] = correlationID
		}
		peers = append(peers, Peer{
			Name:    svc.Name,
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
			Headers: headers,
		})
	}
	return peers
}

// CatalogEntry is one catalog row. Summary is the first sentence of the
// peer's MCP server instructions; it is empty when the peer did not answer.
type CatalogEntry struct {
	Name    string
	Summary string
}

// InstructionLister obtains a peer's live server instructions.
type InstructionLister interface {
	Instructions(context.Context) (string, error)
}

// Catalog fetches every peer's instructions concurrently while preserving
// peer order. A peer failure leaves that peer's summary empty.
func Catalog(ctx context.Context, peers []Peer, clients func(Peer) InstructionLister) []CatalogEntry {
	entries := make([]CatalogEntry, len(peers))
	var wg sync.WaitGroup
	for i, peer := range peers {
		entries[i].Name = peer.Name
		wg.Add(1)
		go func() {
			defer wg.Done()
			instructions, err := clients(peer).Instructions(ctx)
			if err == nil {
				entries[i].Summary = firstSentence(instructions)
			}
		}()
	}
	wg.Wait()
	return entries
}

func firstSentence(instructions string) string {
	instructions = strings.TrimSpace(instructions)
	if end := strings.IndexAny(instructions, ".!?"); end >= 0 {
		return strings.TrimSpace(instructions[:end+1])
	}
	return instructions
}
