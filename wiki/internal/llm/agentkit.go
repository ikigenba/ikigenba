package llm

import (
	"io"

	agentkit "github.com/ikigenba/agentkit"
)

// AgentKitProvider is the production provider boundary supplied at composition.
type AgentKitProvider = agentkit.Provider

// Client is the production LLM client shell shared by wiki services.
type Client struct {
	prov  AgentKitProvider
	log   io.Writer
	model string
}

// NewClient records the provider and model selected at the composition root.
func NewClient(provider AgentKitProvider, model string) *Client {
	return &Client{prov: provider, model: model}
}

// Model reports the configured model id.
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// Provider reports the configured AgentKit provider.
func (c *Client) Provider() AgentKitProvider {
	if c == nil {
		return nil
	}
	return c.prov
}

// ToAgentKit converts the narrow wiki prompt shape to AgentKit messages.
func ToAgentKit(messages []Message) []agentkit.Message {
	out := make([]agentkit.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, agentkit.Message{
			Role:   agentkit.Role(msg.Role),
			Blocks: []agentkit.Block{agentkit.TextBlock{Text: msg.Content}},
		})
	}
	return out
}
